package hub

import (
	"context"
	"encoding/json"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/settings"
	"github.com/rainchan/webmux/internal/wire"
)

// Settings has one data writer; CloseRead consumes ping/close control frames.
func Settings(ctx context.Context, sub *settings.Subscription, c *websocket.Conn) {
	defer func() { _ = c.CloseNow() }()
	ctx = c.CloseRead(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Events:
			if !ok {
				_ = c.Close(websocket.StatusTryAgainLater, "settings changed; reconnect")
				return
			}
			data, err := json.Marshal(wire.SettingsEvent(event))
			if err != nil {
				return
			}
			if write(ctx, c, websocket.MessageText, data) != nil {
				return
			}
		}
	}
}
