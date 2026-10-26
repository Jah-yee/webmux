package api

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
)

// PublicOrigins authorizes HTTPS origins per entry-point source. Tailscale
// Serve/Funnel and Cloudflare Tunnel can run together: revoking one source
// never removes another. Entries are immutable snapshots published atomically.
type PublicOrigins struct {
	current atomic.Pointer[originSet]
}

type originSet map[string]publicOrigin

type publicOrigin struct {
	origin    string // https://<authority>, exactly as browsers send it
	authority string // <host> or <host>:<port>, port omitted for 443
}

// Set authorizes origin for source. origin must be an https URL without path,
// query, fragment or credentials; a non-443 port must be included.
func (p *PublicOrigins) Set(source, origin string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Path != "" || strings.ContainsAny(origin, "?#") {
		return fmt.Errorf("public origin %q must be https://<hostname>", origin)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("public origin %q has an invalid hostname", origin)
	}
	port := u.Port()
	if port == "443" {
		port = ""
	}
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return fmt.Errorf("public origin %q has an invalid port", origin)
		}
	}
	authority := host
	if port != "" {
		authority = net.JoinHostPort(host, port)
	}
	canonical := "https://" + authority
	for {
		old := p.current.Load()
		next := make(originSet, len(deref(old))+1)
		if old != nil {
			for k, v := range *old {
				next[k] = v
			}
		}
		next[source] = publicOrigin{origin: canonical, authority: authority}
		if p.current.CompareAndSwap(old, &next) {
			return nil
		}
	}
}

// Clear revokes one source without touching any other.
func (p *PublicOrigins) Clear(source string) {
	if p == nil {
		return
	}
	for {
		old := p.current.Load()
		if old == nil {
			return
		}
		next := make(originSet, len(*old))
		for k, v := range *old {
			if k != source {
				next[k] = v
			}
		}
		if p.current.CompareAndSwap(old, &next) {
			return
		}
	}
}

func (p *PublicOrigins) load(source string) *publicOrigin {
	if p == nil {
		return nil
	}
	set := p.current.Load()
	if set == nil {
		return nil
	}
	entry, ok := (*set)[source]
	if !ok {
		return nil
	}
	return &entry
}

// hostAllowed reports whether the full authority belongs to any public source.
func (p *PublicOrigins) hostAllowed(authority string) bool {
	if p == nil {
		return false
	}
	set := p.current.Load()
	if set == nil {
		return false
	}
	canonical, ok := normalizeAuthority(authority)
	if !ok {
		return false
	}
	for _, entry := range *set {
		if entry.authority == canonical {
			return true
		}
	}
	return false
}

// originAllowed accepts only an https origin exactly matching the authority of
// the same public source.
func (p *PublicOrigins) originAllowed(origin, authority string) bool {
	if p == nil {
		return false
	}
	set := p.current.Load()
	if set == nil {
		return false
	}
	canonicalAuthority, ok := normalizeAuthority(authority)
	if !ok {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Path != "" || strings.ContainsAny(origin, "?#") {
		return false
	}
	originAuthority, ok := normalizeAuthority(u.Host)
	if !ok || originAuthority != canonicalAuthority {
		return false
	}
	canonicalOrigin := "https://" + originAuthority
	for _, entry := range *set {
		if entry.authority == canonicalAuthority && entry.origin == canonicalOrigin {
			return true
		}
	}
	return false
}

func deref(set *originSet) originSet {
	if set == nil {
		return nil
	}
	return *set
}

// normalizeAuthority canonicalizes a host or host:port, dropping :443.
func normalizeAuthority(authority string) (string, bool) {
	u, err := url.Parse("http://" + authority)
	if err != nil || u.User != nil || u.Host != authority || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	port := u.Port()
	if port == "443" {
		port = ""
	}
	if port == "" {
		return host, true
	}
	return net.JoinHostPort(host, port), true
}
