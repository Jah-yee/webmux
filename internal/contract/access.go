package contract

import (
	"net/url"
	"strings"
)

// AccessURL appends the token as a URL fragment, which browsers never send
// to the server, so it stays out of request and edge logs. Every reserved
// character is escaped (space as %20) so decodeURIComponent restores it.
func AccessURL(base, token string) string {
	base = strings.TrimSuffix(base, "/") + "/"
	if token == "" {
		return base
	}
	return base + "#token=" + strings.ReplaceAll(url.QueryEscape(token), "+", "%20")
}
