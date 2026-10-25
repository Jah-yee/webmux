// Package runtime owns the server's runtime directory: the versioned record
// that lets clients find and identify a running instance, the single-instance
// lock, process identity and Unix-socket peer credentials.
package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/statefile"
)

// RecordVersion is the only record format this build reads and writes.
const RecordVersion = 1

const (
	recordName = "server.json"
	lockName   = "server.lock"
	socketName = "webmux.sock"
)

var (
	ErrNoRecord           = errors.New("no runtime record")
	ErrUnsupportedVersion = errors.New("unsupported runtime record version")
	ErrCorrupt            = errors.New("corrupt runtime record")
)

// Identity tells a process apart from a later one that reuses its PID.
type Identity struct {
	Start string `json:"start"`
	// Exe is the executable path, compared on Linux only.
	Exe string `json:"exe,omitempty"`
}

// Record never contains the token itself.
type Record struct {
	V        int      `json:"v"`
	PID      int      `json:"pid"`
	Identity Identity `json:"identity"`
	Version  string   `json:"version"`
	// Listen is the bound address; port 0 is resolved to the real port.
	Listen string `json:"listen"`
	// ConfiguredListen is the requested address, compared with the config
	// file to detect drift.
	ConfiguredListen string `json:"configuredListen"`
	ConfigPath       string `json:"configPath"`
	TokenConfigured  bool   `json:"tokenConfigured"`
	TokenFingerprint string `json:"tokenFingerprint,omitempty"`
	// TokenGenerated marks a per-process token absent from the config.
	TokenGenerated bool      `json:"tokenGenerated,omitempty"`
	Launcher       string    `json:"launcher"`
	LogFile        string    `json:"logFile"`
	Socket         string    `json:"socket"`
	StartedAt      time.Time `json:"startedAt"`
}

// Dir is the runtime subdirectory of a state directory.
func Dir(stateDir string) string { return filepath.Join(stateDir, "run") }

func RecordPath(dir string) string { return filepath.Join(dir, recordName) }

// EnsureDir creates dir as 0700 and tightens an existing one.
func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.Chmod(dir, 0700)
}

// Write atomically replaces the record with a 0600 file.
func Write(dir string, r Record) error {
	r.V = RecordVersion
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return statefile.Write(RecordPath(dir), append(data, '\n'), statefile.Options{TempPattern: ".server-*.json"})
}

// Read returns ErrNoRecord, ErrUnsupportedVersion or ErrCorrupt for records
// it cannot use.
func Read(dir string) (Record, error) {
	data, err := os.ReadFile(RecordPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, ErrNoRecord
	}
	if err != nil {
		return Record{}, err
	}
	var version struct {
		V int `json:"v"`
	}
	if err := json.Unmarshal(data, &version); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if version.V != RecordVersion {
		return Record{}, fmt.Errorf("%w %d", ErrUnsupportedVersion, version.V)
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if r.PID <= 0 || r.Identity.Start == "" || r.Listen == "" {
		return Record{}, fmt.Errorf("%w: missing pid, identity or listen address", ErrCorrupt)
	}
	return r, nil
}

// Remove deletes the record only when it still describes pid, so a stopping
// instance never deletes a successor's record.
func Remove(dir string, pid int) {
	if r, err := Read(dir); err == nil && r.PID != pid {
		return
	}
	_ = os.Remove(RecordPath(dir))
}

// TokenFingerprint is a truncated one-way digest used only for drift checks.
func TokenFingerprint(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("webmux-token-fingerprint\x00" + token))
	return hex.EncodeToString(sum[:])[:16]
}

// Launcher reports who started the server, in precedence order.
func Launcher(getenv func(string) string) string {
	if getenv("WEBMUX_LAUNCHER") == contract.LauncherWindowsTray {
		return contract.LauncherWindowsTray
	}
	if getenv("INVOCATION_ID") != "" {
		return contract.LauncherSystemd
	}
	if name := getenv("XPC_SERVICE_NAME"); name != "" && name != "0" {
		return contract.LauncherLaunchd
	}
	return contract.LauncherManual
}

// ManagerHint is the stop command of a service manager, or "".
func ManagerHint(launcher string) string {
	switch launcher {
	case contract.LauncherSystemd:
		return "systemctl --user stop webmux"
	case contract.LauncherLaunchd:
		return fmt.Sprintf("launchctl bootout gui/%d/com.webmux", os.Getuid())
	}
	return ""
}

// ErrNoProcess means the PID is gone or belongs to a different process.
var ErrNoProcess = errors.New("process not running")

// Alive verifies that the recorded process still runs.
func (r Record) Alive() error {
	id, err := ProcessIdentity(r.PID)
	if err != nil {
		return err
	}
	if id.Start != r.Identity.Start || id.Exe != r.Identity.Exe {
		return fmt.Errorf("%w: PID %d was reused", ErrNoProcess, r.PID)
	}
	return nil
}
