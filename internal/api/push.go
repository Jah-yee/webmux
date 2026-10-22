package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/rainchan/webmux/internal/push"
)

// Push availability reasons shared with web/src/schemas.ts.
const (
	pushDisabled          = "disabled"
	pushStorage           = "storage"
	pushQuickTunnel       = "quick_tunnel"
	pushUnsupportedOrigin = "unsupported_origin"
)

var pushOriginErrors = map[string]error{
	pushQuickTunnel:       errors.New("推送需要 Cloudflare Named Tunnel：Quick Tunnel 地址每次启动都会变化，旧订阅点开即失效"),
	pushUnsupportedOrigin: errors.New("推送只支持 Cloudflare Named Tunnel 公网地址与本机回环地址"),
}

// pushOrigin decides whether the page that sent r may subscribe. It returns
// the canonical origin to anchor notification links, or a reason. Only two
// origins are stable secure contexts this server can vouch for: the
// configured Named Tunnel hostname (whether or not the tunnel runs now) and
// loopback.
func (s *Server) pushOrigin(r *http.Request) (string, string) {
	if s.local {
		return "", pushUnsupportedOrigin
	}
	named := s.Config.Tunnel.NamedHostname()
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Same-origin GETs omit Origin. Tunnel traffic reaches this server
		// over plain HTTP, but the browser saw https.
		host := authorityHostname(r.Host)
		scheme := "http"
		if r.TLS != nil || host != "" && (host == named || s.Public.hostAllowed(host)) {
			scheme = "https"
		}
		origin = scheme + "://" + r.Host
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || strings.ContainsAny(origin, "?#") {
		return "", pushUnsupportedOrigin
	}
	host := authorityHostname(u.Host)
	switch {
	case named != "" && u.Scheme == "https" && u.Host == named:
		return "https://" + named, ""
	case (u.Scheme == "http" || u.Scheme == "https") && (host == "127.0.0.1" || host == "::1" || host == "localhost"):
		return u.Scheme + "://" + u.Host, ""
	case named == "" && s.Public.load() != nil && origin == s.Public.load().origin:
		return "", pushQuickTunnel
	}
	return "", pushUnsupportedOrigin
}

func pushUnavailableReason(err error) string {
	if errors.Is(err, push.ErrStorage) {
		return pushStorage
	}
	return pushDisabled
}

func pushConflict(w http.ResponseWriter, reason string, err error) {
	reply(w, http.StatusConflict, map[string]string{"error": err.Error(), "reason": reason})
}

func (s *Server) pushRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/push", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]any{"enabled": s.Push != nil, "available": false, "subscriptions": s.Push.Subscriptions()}
		if err := s.Push.Unavailable(); err != nil {
			status["reason"] = pushUnavailableReason(err)
		} else if _, reason := s.pushOrigin(r); reason != "" {
			status["reason"] = reason
		} else {
			status["available"] = true
			status["publicKey"] = s.Push.PublicKey()
		}
		reply(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /api/push/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		// The body is PushSubscription.toJSON(). Browsers always include
		// expirationTime (usually null); it is accepted but not stored, because
		// a stale subscription is pruned when the push service answers 404/410.
		var req struct {
			Endpoint       string   `json:"endpoint"`
			ExpirationTime *float64 `json:"expirationTime"`
			Keys           struct {
				P256dh string `json:"p256dh"`
				Auth   string `json:"auth"`
			} `json:"keys"`
		}
		if !decode(w, r, &req) {
			return
		}
		if err := s.Push.Unavailable(); err != nil {
			pushConflict(w, pushUnavailableReason(err), err)
			return
		}
		if err := push.Validate(req.Endpoint, req.Keys.P256dh, req.Keys.Auth); err != nil {
			failure(w, http.StatusBadRequest, err)
			return
		}
		origin, reason := s.pushOrigin(r)
		if reason != "" {
			pushConflict(w, reason, pushOriginErrors[reason])
			return
		}
		id, err := s.Push.Register(req.Endpoint, req.Keys.P256dh, req.Keys.Auth, origin)
		switch {
		case errors.Is(err, push.ErrFull):
			failure(w, http.StatusConflict, err)
		case err != nil:
			failure(w, http.StatusInternalServerError, errors.New("推送订阅保存失败，请查看服务日志"))
		default:
			reply(w, http.StatusOK, map[string]string{"id": id})
		}
	})
	mux.HandleFunc("DELETE /api/push/subscriptions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Push.Unregister(r.PathValue("id")); err != nil {
			if unavailable := s.Push.Unavailable(); unavailable != nil {
				pushConflict(w, pushUnavailableReason(unavailable), unavailable)
				return
			}
			failure(w, http.StatusInternalServerError, errors.New("推送订阅删除失败，请查看服务日志"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/push/test", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &req) {
			return
		}
		err := s.Push.SendTest(r.Context(), req.ID)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, push.ErrDisabled) || errors.Is(err, push.ErrStorage):
			pushConflict(w, pushUnavailableReason(err), err)
		case errors.Is(err, push.ErrNoSuchDevice):
			failure(w, http.StatusNotFound, err)
		case errors.Is(err, push.ErrGone):
			failure(w, http.StatusGone, err)
		default:
			// Delivery errors name only the push service host.
			failure(w, http.StatusBadGateway, err)
		}
	})
}
