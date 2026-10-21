// Package forward is the tray's HTTP-aware relay for WSL NAT networking: it
// listens on one Windows host address and forwards same-origin requests to
// WebMux's TCP listener, never to the local socket. Authentication stays
// entirely with WebMux.
package forward

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Defaults from windows-tray-forwarding.
const (
	DefaultMaxConns    = 64
	DefaultIdleTimeout = 10 * time.Minute
)

// Handler checks Host and Origin against the listen address and relays to
// target (the WebMux local link host:port). listen must be an IP literal
// with a port, so the Host check cannot be bypassed by DNS rebinding.
func Handler(listen, target string, logf func(string, ...any)) http.Handler {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	self := "http://" + listen
	upstream := &url.URL{Scheme: "http", Host: target}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			pr.Out.Host = target
			if pr.In.Header.Get("Origin") != "" {
				pr.Out.Header.Set("Origin", "http://"+target)
			}
			for name := range pr.Out.Header {
				if strings.HasPrefix(name, "X-Forwarded-") {
					pr.Out.Header.Del(name)
				}
			}
		},
		// Errors are logged without the URL, whose query may carry a token.
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if !errors.Is(err, context.Canceled) {
				logf("forward %s: %v", listen, err)
			}
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != listen {
			http.Error(w, "host not allowed", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != self {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// Options configure one forwarding listener.
type Options struct {
	// Listen is "<windows-ip>:<port>"; never a wildcard or loopback address.
	Listen string
	// Target is the WebMux local link authority, "127.0.0.1:<port>".
	Target      string
	MaxConns    int
	IdleTimeout time.Duration
	Logf        func(string, ...any)
}

// Forwarder serves one address until Close.
type Forwarder struct {
	server   *http.Server
	listener *limitListener
	done     chan struct{}
}

// ErrAddress rejects wildcard, loopback and non-literal listen addresses.
var ErrAddress = errors.New("forwarding needs a specific non-loopback IP address")

// Start listens immediately so a busy port is reported to the caller.
func Start(o Options) (*Forwarder, error) {
	host, _, err := net.SplitHostPort(o.Listen)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return nil, ErrAddress
	}
	l, err := net.Listen("tcp", o.Listen)
	if err != nil {
		return nil, err
	}
	return serve(l, o), nil
}

// serve relays on an already bound listener.
func serve(l net.Listener, o Options) *Forwarder {
	if o.MaxConns == 0 {
		o.MaxConns = DefaultMaxConns
	}
	if o.IdleTimeout == 0 {
		o.IdleTimeout = DefaultIdleTimeout
	}
	limited := &limitListener{Listener: l, max: o.MaxConns, conns: map[net.Conn]struct{}{}}
	f := &Forwarder{
		listener: limited,
		done:     make(chan struct{}),
		server: &http.Server{
			Handler:           Handler(o.Listen, o.Target, o.Logf),
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       o.IdleTimeout,
		},
	}
	go func() {
		_ = f.server.Serve(limited)
		close(f.done)
	}()
	return f
}

// Addr is the bound address.
func (f *Forwarder) Addr() net.Addr { return f.listener.Addr() }

// Close stops listening and closes every connection, including upgraded
// WebSocket connections the HTTP server no longer tracks.
func (f *Forwarder) Close() error {
	err := f.server.Close()
	f.listener.closeAll()
	<-f.done
	return err
}

// limitListener closes connections beyond max immediately and remembers
// open ones so Close can end hijacked connections too.
type limitListener struct {
	net.Listener
	max   int
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		l.mu.Lock()
		if len(l.conns) >= l.max {
			l.mu.Unlock()
			_ = c.Close()
			continue
		}
		tracked := &trackedConn{Conn: c, owner: l}
		l.conns[tracked] = struct{}{}
		l.mu.Unlock()
		return tracked, nil
	}
}

func (l *limitListener) closeAll() {
	l.mu.Lock()
	conns := make([]net.Conn, 0, len(l.conns))
	for c := range l.conns {
		conns = append(conns, c)
	}
	l.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

type trackedConn struct {
	net.Conn
	owner *limitListener
	once  sync.Once
}

func (c *trackedConn) Close() error {
	c.once.Do(func() {
		c.owner.mu.Lock()
		delete(c.owner.conns, c)
		c.owner.mu.Unlock()
	})
	return c.Conn.Close()
}
