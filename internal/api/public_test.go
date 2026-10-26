package api

import "testing"

func TestPublicOriginsPortsAndSources(t *testing.T) {
	public := &PublicOrigins{}
	if err := public.Set("cloudflare", "https://term.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := public.Set("tailscale", "https://node.tailnet.ts.net:8443"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, host, origin string
		want               bool
	}{
		{"443 host", "term.example.com", "https://term.example.com", true},
		{"443 with explicit port", "term.example.com:443", "https://term.example.com:443", true},
		{"8443 host and origin", "node.tailnet.ts.net:8443", "https://node.tailnet.ts.net:8443", true},
		{"8443 host without port", "node.tailnet.ts.net", "https://node.tailnet.ts.net:8443", false},
		{"8443 origin without port", "node.tailnet.ts.net:8443", "https://node.tailnet.ts.net", false},
		{"wrong port", "node.tailnet.ts.net:10000", "https://node.tailnet.ts.net:10000", false},
		{"http origin", "term.example.com", "http://term.example.com", false},
		{"unknown host", "evil.example.com", "https://evil.example.com", false},
	}
	for _, row := range cases {
		if got := public.hostAllowed(row.host) && public.originAllowed(row.origin, row.host); got != row.want {
			t.Errorf("%s: allowed=%v, want %v", row.name, got, row.want)
		}
	}
	public.Clear("tailscale")
	if public.hostAllowed("node.tailnet.ts.net:8443") {
		t.Fatal("tailscale source survived Clear")
	}
	if !public.hostAllowed("term.example.com") || !public.originAllowed("https://term.example.com", "term.example.com") {
		t.Fatal("cloudflare source was cleared with tailscale")
	}
	for _, invalid := range []string{"http://a.example", "https://a.example/path", "https://a.example:0", "https://a.example:70000", "https://u@a.example", "https://a.example?x"} {
		if public.Set("test", invalid) == nil {
			t.Errorf("accepted %q", invalid)
		}
	}
}
