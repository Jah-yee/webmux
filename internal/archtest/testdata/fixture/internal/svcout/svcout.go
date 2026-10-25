// Package svcout is a registered outbound service: client symbols only.
package svcout

import (
	"context"
	"net/http"
)

func Send(ctx context.Context, c *http.Client) (bool, error) {
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://push.example", http.NoBody)
	if err != nil {
		return false, err
	}
	res, err := c.Do(req)
	if err != nil {
		return false, err
	}
	return res.StatusCode == http.StatusGone, res.Body.Close()
}

var _ http.Handler = nil // want R6

func Cookie() *http.Cookie { return nil } // want R6

func Inbound(r *http.Request) {} // want R6
