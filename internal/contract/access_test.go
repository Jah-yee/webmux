package contract

import (
	"net/url"
	"strings"
	"testing"
)

func TestAccessURL(t *testing.T) {
	for _, row := range []struct{ base, token, want string }{
		{"http://127.0.0.1:7681", "s3cret", "http://127.0.0.1:7681/#token=s3cret"},
		{"http://127.0.0.1:7681/", "s3cret", "http://127.0.0.1:7681/#token=s3cret"},
		{"http://127.0.0.1:7681", "", "http://127.0.0.1:7681/"},
		{"https://abc.trycloudflare.com", "a b&c#d", "https://abc.trycloudflare.com/#token=a%20b%26c%23d"},
	} {
		got := AccessURL(row.base, row.token)
		if got != row.want {
			t.Errorf("AccessURL(%q, %q) = %q, want %q", row.base, row.token, got, row.want)
		}
		// The frontend decodes with decodeURIComponent; PathUnescape matches it.
		if _, fragment, ok := strings.Cut(got, "#token="); ok {
			if decoded, err := url.PathUnescape(fragment); err != nil || decoded != row.token {
				t.Errorf("fragment %q decodes to %q %v", fragment, decoded, err)
			}
		}
	}
}
