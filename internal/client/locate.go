// Package client implements the Unix CLI side of server control: locating a
// running instance through the configuration, runtime record and local
// socket, and the status/stop fallbacks when the local API is unavailable.
package client

import (
	"errors"
	"os"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/pathutil"
	"github.com/rainchan/webmux/internal/runtime"
)

// Target is everything a client command knows before talking to a server.
type Target struct {
	ConfigPath     string
	ConfigExplicit bool
	ConfigExists   bool
	// ConfigErr is a load or validation failure; Config is unusable then.
	ConfigErr error
	Config    config.Config
	RunDir    string
	// Record is set only for a record whose process is still alive.
	Record *runtime.Record
	// RecordErr explains a missing or stale record.
	RecordErr error
	// Socket is the record's socket, or the derived path without a record.
	Socket string
}

// LoadConfig applies the server's rules: a default path may be missing, an
// explicit one must exist, and the result is validated.
func LoadConfig(path string, explicit bool) (config.Config, error) {
	c, err := config.Load(path, explicit)
	if err != nil {
		return c, err
	}
	err = c.Validate()
	return c, err
}

// Locate never modifies files, including a stale record.
func Locate(path string, explicit bool) Target {
	t := Target{ConfigPath: path, ConfigExplicit: explicit}
	if expanded, err := pathutil.Expand(path); err == nil {
		t.ConfigPath = expanded
	}
	_, statErr := os.Stat(t.ConfigPath)
	t.ConfigExists = statErr == nil
	t.Config, t.ConfigErr = LoadConfig(t.ConfigPath, explicit)
	if t.ConfigErr != nil {
		return t
	}
	t.RunDir = runtime.Dir(t.Config.StateDir)
	record, err := runtime.Read(t.RunDir)
	if err == nil {
		err = record.Alive()
	}
	if err != nil {
		t.RecordErr = err
		t.Socket, _ = runtime.SocketPath(t.Config.StateDir, os.Getenv)
		return t
	}
	t.Record = &record
	t.Socket = record.Socket
	return t
}

// Stale reports whether a record exists but no longer describes a process.
func (t Target) Stale() bool {
	return t.RecordErr != nil && !errors.Is(t.RecordErr, runtime.ErrNoRecord)
}
