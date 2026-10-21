package api

import (
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
)

// PublicOrigin is the HTTPS origin of a running tunnel. The tunnel terminates
// TLS at the edge, so requests reach this server over plain HTTP while the
// browser reports an https Origin for the public hostname.
type PublicOrigin struct {
	current atomic.Pointer[publicOrigin]
}

type publicOrigin struct {
	origin string // https://host, exactly as browsers send it
	host   string // normalized hostname
}

// Set authorizes origin, which must be an https URL without path, query,
// fragment, credentials, or port.
func (p *PublicOrigin) Set(origin string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Port() != "" || u.Path != "" || strings.ContainsAny(origin, "?#") {
		return fmt.Errorf("public origin %q must be https://<hostname>", origin)
	}
	host := authorityHostname(u.Host)
	if host == "" {
		return fmt.Errorf("public origin %q has an invalid hostname", origin)
	}
	p.current.Store(&publicOrigin{origin: "https://" + host, host: host})
	return nil
}

// Clear revokes the public origin, for example after the tunnel exits.
func (p *PublicOrigin) Clear() {
	if p != nil {
		p.current.Store(nil)
	}
}

func (p *PublicOrigin) load() *publicOrigin {
	if p == nil {
		return nil
	}
	return p.current.Load()
}

// hostAllowed reports whether the normalized hostname belongs to the tunnel.
func (p *PublicOrigin) hostAllowed(host string) bool {
	current := p.load()
	return current != nil && host == current.host
}

// originAllowed accepts only the https public origin on requests addressed to
// the public hostname; direct connections keep the scheme-matching rule.
func (p *PublicOrigin) originAllowed(origin, authority string) bool {
	current := p.load()
	return current != nil && origin == current.origin && authorityHostname(authority) == current.host
}
