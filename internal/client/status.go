package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
	"github.com/rainchan/webmux/internal/runtime"
)

// RequestTimeout bounds every local API request and TCP health check.
const RequestTimeout = 3 * time.Second

// FromRecord fills the instance fields of a status from a runtime record.
func FromRecord(r runtime.Record, interfaces func() ([]Interface, error)) contract.StatusV1 {
	pid := r.PID
	s := contract.StatusV1{
		V:               contract.V,
		Record:          true,
		PID:             &pid,
		Version:         r.Version,
		Launcher:        r.Launcher,
		Listen:          r.Listen,
		TokenConfigured: r.TokenConfigured,
		Drift:           []string{},
		Warnings:        []string{},
		LogFile:         r.LogFile,
		Socket:          r.Socket,
	}
	if !r.StartedAt.IsZero() {
		s.StartedAt = r.StartedAt.UTC().Format(time.RFC3339)
	}
	var warnings []string
	s.URLs, warnings = Links(r.Listen, r.TokenConfigured, interfaces)
	s.Warnings = append(s.Warnings, warnings...)
	return s
}

// Drift names the settings whose configured values differ from the running
// instance. A per-process tunnel token is not a drift.
func Drift(r runtime.Record, c config.Config) []string {
	drift := []string{}
	if c.Listen != r.ConfiguredListen {
		drift = append(drift, "listen")
	}
	if !r.TokenGenerated && runtime.TokenFingerprint(c.Token) != r.TokenFingerprint {
		drift = append(drift, "token")
	}
	return drift
}

// ApplyConfig re-reads the configuration file into the config and drift
// fields; the server calls it for every /local/v1/status.
func ApplyConfig(s *contract.StatusV1, r runtime.Record, path string, explicit bool) {
	_, err := os.Stat(path)
	s.Config = contract.ConfigRef{Path: path, Exists: err == nil}
	c, err := LoadConfig(path, explicit)
	if err != nil {
		s.Config.Error = err.Error()
		return
	}
	s.Drift = Drift(r, c)
}

// Probe performs one TCP health check against base (an http origin).
func Probe(ctx context.Context, base, token string) (contract.Health, int) {
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/health", nil)
	if err != nil {
		return contract.Health{Status: contract.HealthError, Message: err.Error()}, 0
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	resp, err := client.Do(req)
	if err != nil {
		var netErr net.Error
		switch {
		case errors.Is(err, syscall.ECONNREFUSED):
			return contract.Health{Status: contract.HealthRefused, Message: "connection refused"}, 0
		case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout():
			return contract.Health{Status: contract.HealthTimeout, Message: "no response within " + RequestTimeout.String()}, 0
		}
		return contract.Health{Status: contract.HealthError, Message: err.Error()}, 0
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized {
		return contract.Health{Status: contract.HealthUnauthorized, Message: "the token was rejected"}, 0
	}
	var body struct {
		OK       *bool `json:"ok"`
		Sessions *int  `json:"sessions"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&body) != nil || body.OK == nil || !*body.OK || body.Sessions == nil {
		return contract.Health{Status: contract.HealthNotWebMux, Message: fmt.Sprintf("HTTP %d is not a WebMux health response", resp.StatusCode)}, 0
	}
	return contract.Health{Status: contract.HealthOK}, *body.Sessions
}

// LocalClient talks to the target's socket.
func (t Target) LocalClient() *localclient.Client {
	return localclient.New(localclient.UnixDialer{Path: t.Socket})
}

func configRef(t Target) contract.ConfigRef {
	ref := contract.ConfigRef{Path: t.ConfigPath, Exists: t.ConfigExists}
	if t.ConfigErr != nil {
		ref.Error = t.ConfigErr.Error()
	}
	return ref
}

// Status implements `webmux status`: local API first, then the runtime
// record with a TCP health check, then a health check of the configured
// address. It returns the exit code (0 healthy, 3 not running, 1 otherwise).
func Status(ctx context.Context, t Target, reveal bool, interfaces func() ([]Interface, error)) (contract.StatusV1, int) {
	if t.ConfigErr != nil {
		s := emptyStatus(contract.StateError)
		s.Config = configRef(t)
		return s, contract.ExitError
	}
	local := t.LocalClient()
	defer local.Close()
	requestCtx, cancel := context.WithTimeout(ctx, RequestTimeout)
	s, err := local.Status(requestCtx)
	cancel()
	if err == nil {
		s.LocalAPI = true
		if reveal && s.TokenConfigured {
			tokenCtx, cancel := context.WithTimeout(ctx, RequestTimeout)
			token, tokenErr := local.Token(tokenCtx)
			cancel()
			if tokenErr != nil {
				s.Warnings = append(s.Warnings, "cannot reveal the token: "+tokenErr.Error())
			}
			s.Token = token
		}
		return s, exitFor(s)
	}
	localErr := err
	if t.Record != nil {
		s = FromRecord(*t.Record, interfaces)
		s.Config = configRef(t)
		s.Drift = Drift(*t.Record, t.Config)
		s.Warnings = append(s.Warnings, "local API unavailable: "+localErr.Error())
		health, sessions := Probe(ctx, s.URLs.Local, t.Config.Token)
		s.Health = health
		switch health.Status {
		case contract.HealthOK:
			s.State = contract.StateRunning
			s.Sessions = &sessions
		case contract.HealthUnauthorized:
			s.State = contract.StateRunning
		default:
			s.State = contract.StateUnresponsive
			s.Health.Message = fmt.Sprintf("process %d exists but does not respond: %s", t.Record.PID, health.Message)
		}
		if reveal && t.Config.Token != "" {
			s.Token = t.Config.Token
		}
		return s, exitFor(s)
	}
	s = emptyStatus(contract.StateStopped)
	s.Config = configRef(t)
	s.Socket = t.Socket
	s.Listen = t.Config.Listen
	s.TokenConfigured = t.Config.Token != ""
	var warnings []string
	s.URLs, warnings = Links(t.Config.Listen, s.TokenConfigured, interfaces)
	health, sessions := ProbeConfigured(ctx, t)
	s.Health = health
	switch health.Status {
	case contract.HealthOK, contract.HealthUnauthorized:
		s.State = contract.StateRunning
		if health.Status == contract.HealthOK {
			s.Sessions = &sessions
		}
		s.Warnings = append(s.Warnings, warnings...)
		s.Warnings = append(s.Warnings, "running without a runtime record (an older WebMux or another state directory); it cannot be stopped with `webmux stop`")
		if reveal && t.Config.Token != "" {
			s.Token = t.Config.Token
		}
	case contract.HealthRefused:
	case contract.HealthNotWebMux:
		s.Warnings = append(s.Warnings, "another program answers on "+t.Config.Listen)
	default:
		s.State = contract.StateError
	}
	return s, exitFor(s)
}

// ProbeConfigured checks the configured address of an instance without a
// record. A dynamic port (":0") cannot be probed and counts as refused.
func ProbeConfigured(ctx context.Context, t Target) (contract.Health, int) {
	if _, port, err := net.SplitHostPort(t.Config.Listen); err != nil || port == "0" {
		return contract.Health{Status: contract.HealthRefused, Message: "dynamic port; nothing to probe without a runtime record"}, 0
	}
	return Probe(ctx, LocalURL(t.Config.Listen), t.Config.Token)
}

func emptyStatus(state contract.State) contract.StatusV1 {
	return contract.StatusV1{
		V:        contract.V,
		State:    state,
		Health:   contract.Health{Status: contract.HealthError},
		URLs:     contract.URLs{Remote: []contract.RemoteURL{}},
		Drift:    []string{},
		Warnings: []string{},
	}
}

func exitFor(s contract.StatusV1) int {
	switch {
	case s.State == contract.StateStopped:
		return contract.ExitNotRunning
	case s.State == contract.StateRunning && s.Health.Status == contract.HealthOK:
		return contract.ExitOK
	}
	return contract.ExitError
}
