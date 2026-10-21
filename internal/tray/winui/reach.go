//go:build windows

package winui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rainchan/webmux/internal/tray"
)

// NewReacher checks the local link from Windows: GET /api/health without a
// token and without any proxy; 200 or 401 means WebMux answers.
func NewReacher() tray.Reacher {
	client := &http.Client{
		Transport: &http.Transport{Proxy: nil, MaxIdleConns: 1, IdleConnTimeout: 30 * time.Second},
		Timeout:   3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return func(ctx context.Context, url string) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/api/health", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		switch resp.StatusCode {
		case http.StatusOK, http.StatusUnauthorized:
			return nil
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
}
