package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/client"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/launch"
	"github.com/rainchan/webmux/internal/push"
	"github.com/rainchan/webmux/internal/runtime"
	"github.com/rainchan/webmux/internal/settings"
	"github.com/rainchan/webmux/internal/statefile"
	"github.com/rainchan/webmux/internal/tailscalesettings"
	"github.com/rainchan/webmux/internal/tunnelsettings"
	"github.com/rainchan/webmux/internal/workspace"
)

func configuration(path string, c config.Config, view string, exists bool) contract.Configuration {
	fields := map[string]contract.ConfigField{}
	for k, v := range config.Fields(c) {
		fields[k] = contract.ConfigField{Value: v.Value, Source: v.Source}
	}
	return contract.Configuration{ConfigV1: contract.ConfigV1{V: contract.V, Path: path, Exists: exists, Listen: c.Listen, TokenConfigured: c.Token != "", DefaultCwd: c.DefaultCwd, StateDir: c.StateDir, AllowedOrigins: append([]string{}, c.AllowedOrigins...)}, View: view, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Fields: fields, Stores: []contract.StoreHealth{}}
}
func storeHealth(dir string, id statefile.ID, err error, live bool) contract.StoreHealth {
	d := statefile.Describe(id)
	h := contract.StoreHealth{ID: string(id), Path: filepath.Join(dir, d.Path), Status: "available", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Lock: "not_checked"}
	if live {
		h.Lock = "held"
	}
	if err != nil {
		h.Status = "unavailable"
		var e *statefile.Error
		if errors.As(err, &e) {
			h.Status = string(e.Kind)
			h.Phase = string(e.Phase)
			if e.Kind == statefile.Locked {
				h.Lock = "locked"
			}
		}
		if live && h.Status != "commit_uncertain" {
			h.Lock = "unavailable"
		}
	}
	return h
}
func webSources(v *contract.Configuration, tunnel, tailscale bool) {
	for _, group := range []struct {
		web  bool
		keys []string
	}{{tunnel, []string{"tunnel.enabled", "tunnel.token", "tunnel.hostname"}}, {tailscale, []string{"tailscale.mode", "tailscale.port", "tailscale.provider"}}} {
		if group.web {
			for _, key := range group.keys {
				f := v.Fields[key]
				f.Source = "web"
				v.Fields[key] = f
			}
		}
	}
}
func configuredView(path string, c config.Config) contract.Configuration {
	tunnel, tw, te := tunnelsettings.Inspect(c.StateDir, c.Tunnel)
	ts, tsw, tse := tailscalesettings.Inspect(c.StateDir, c.Tailscale)
	if te != nil && !statefile.IsKind(te, statefile.Missing) {
		tunnel.Enabled = false
	}
	if tse != nil && !statefile.IsKind(tse, statefile.Missing) {
		ts.Mode = config.TailscaleOff
	}
	c.Tunnel, c.Tailscale = tunnel, ts
	v := configuration(path, c, "configured", exists(path))
	webSources(&v, tw, tsw)
	for _, d := range statefile.Catalog() {
		var err error
		switch d.ID {
		case statefile.Launch:
			err = launch.Inspect(c.StateDir)
		case statefile.Repositories:
			err = workspace.Inspect(c.StateDir)
		case statefile.Push:
			err = push.Inspect(c.StateDir)
		case statefile.Tunnel:
			err = te
		case statefile.TailscaleSettings:
			err = tse
		case statefile.TailscaleOwnership:
			_, err = statefile.ReadSnapshot[tailscaleState](c.StateDir, d.ID, validateOwnership)
		case statefile.Settings:
			_, err = settings.Inspect(c.StateDir)
		case statefile.Runtime:
			_, err = statefile.ReadRuntime[runtime.Record](runtime.Dir(c.StateDir), func(r runtime.Record) error {
				if r.PID <= 0 || r.Identity.Start == "" || r.Listen == "" {
					return errors.New("invalid runtime record")
				}
				return nil
			})
		}
		v.Stores = append(v.Stores, storeHealth(c.StateDir, d.ID, err, false))
	}
	return v
}
func liveConfiguration(path string, explicit bool) (contract.Configuration, error) {
	t := client.Locate(path, explicit)
	if t.ConfigErr != nil {
		return contract.Configuration{}, fmt.Errorf("configuration unavailable: %s", path)
	}
	if t.Record == nil {
		return contract.Configuration{}, errors.New("live configuration unavailable: no verified running instance")
	}
	c := t.LocalClient()
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), client.RequestTimeout)
	defer cancel()
	var view contract.Configuration
	if err := c.Do(ctx, http.MethodGet, contract.PathConfiguration, nil, &view); err != nil {
		return view, err
	}
	if view.View != "effective" || view.Instance == nil || view.Instance.PID != t.Record.PID || view.Instance.Identity != t.Record.Identity.Start {
		return contract.Configuration{}, errors.New("live configuration instance changed; retry")
	}
	return view, nil
}
func effectiveView(c serverRun, record runtime.Record, deps api.Deps) contract.Configuration {
	// Only adopted in-memory state: never reopen stores or re-read TOML here.
	tc, tw := c.TunnelSettings.Effective()
	ts, tsw := c.TailscaleSettings.Effective()
	tunnelEnabledSource := ""
	if controller, ok := deps.TunnelSettings.(*tunnelController); ok {
		controller.mu.Lock()
		tc, tw = controller.cfg, controller.web
		tunnelEnabledSource = controller.enabledSource
		controller.mu.Unlock()
	}
	c.Tunnel, c.Tailscale = tc, ts
	if controller, ok := deps.Tailscale.(*tailscaleController); ok {
		controller.mu.Lock()
		c.Tailscale = controller.cfg
		tsw = controller.web
		controller.mu.Unlock()
	}
	v := configuration(c.ConfigPath, c.Config, "effective", c.ConfigFound)
	webSources(&v, tw, tsw)
	if tunnelEnabledSource != "" {
		f := v.Fields["tunnel.enabled"]
		f.Source = tunnelEnabledSource
		v.Fields["tunnel.enabled"] = f
	}
	v.Listen = record.Listen
	f := v.Fields["listen"]
	f.Value = record.Listen
	v.Fields["listen"] = f
	v.Instance = &contract.ConfigInstance{PID: record.PID, Identity: record.Identity.Start, StartedAt: record.StartedAt.UTC().Format(time.RFC3339Nano)}
	for _, d := range statefile.Catalog() {
		var err error
		switch d.ID {
		case statefile.Launch:
			err = deps.Launch.Unavailable()
		case statefile.Repositories:
			err = deps.Workspace.StorageError()
		case statefile.Push:
			err = deps.Push.Unavailable()
		case statefile.Tunnel:
			err = c.TunnelSettings.Unavailable()
		case statefile.TailscaleSettings:
			err = c.TailscaleSettings.Unavailable()
		case statefile.Settings:
			_, err = deps.Settings.Snapshot()
		case statefile.TailscaleOwnership:
			if tc, ok := deps.Tailscale.(*tailscaleController); ok {
				tc.mu.Lock()
				err = tc.storageErr
				if err == nil && tc.ownership != nil {
					err = tc.ownership.Err()
				}
				tc.mu.Unlock()
			}
		}
		h := storeHealth(c.StateDir, d.ID, err, true)
		if d.ID == statefile.Push && deps.Push == nil {
			h.Status = "disabled"
			h.Lock = "not_opened"
		}
		if d.ID == statefile.TailscaleOwnership {
			if tc, ok := deps.Tailscale.(*tailscaleController); ok {
				tc.mu.Lock()
				if tc.ownership == nil {
					if err == nil {
						h.Status = "not_opened"
					}
					h.Lock = "not_opened"
				}
				tc.mu.Unlock()
			}
		}
		v.Stores = append(v.Stores, h)
	}
	return v
}
