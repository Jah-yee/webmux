// Package codeview owns bounded, read-only session code contexts and immutable snapshots.
package codeview

import (
	"context"
	"errors"
	"fmt"
	"github.com/rainchan/webmux/internal/workspace"
	"os"
	"time"
)

const MaxBytes = 2 * 1024 * 1024
const MaxLines = 20000
const CacheBytes = 64 * 1024 * 1024
const Lifetime = 10 * time.Minute

// Error kinds. Each wraps a transport-neutral category; the controller maps
// kinds to HTTP statuses.
var (
	ErrInvalid     = errors.New("invalid code view request")
	ErrForbidden   = errors.New("code view access denied")
	ErrNotFound    = errors.New("code view item not found")
	ErrConflict    = errors.New("code view state conflict")
	ErrGone        = errors.New("code view snapshot expired")
	ErrTooLarge    = errors.New("code view content too large")
	ErrBusy        = errors.New("code view busy")
	ErrInternal    = errors.New("code view internal error")
	ErrUnavailable = errors.New("code view unavailable")
	ErrTimeout     = errors.New("code view operation timed out")
)

// Error carries the user-facing message; its kind is reachable through
// errors.Is.
type Error struct {
	kind    error
	message string
}

func (e *Error) Error() string { return e.message }
func (e *Error) Unwrap() error { return e.kind }
func problem(kind error, format string, args ...any) error {
	return &Error{kind, fmt.Sprintf(format, args...)}
}

// errorKind classifies err like the controller does. Workspace size and timeout
// failures keep their meaning; other workspace errors are internal.
func errorKind(err error) error {
	var e *Error
	switch {
	case errors.As(err, &e):
		return e.kind
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return ErrTimeout
	case errors.Is(err, os.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, workspace.ErrTooLarge):
		return ErrTooLarge
	case errors.Is(err, workspace.ErrTimeout):
		return ErrTimeout
	}
	return ErrInternal
}

type Context struct {
	ID     string `json:"contextId"`
	Root   string `json:"rootPath"`
	Kind   string `json:"rootKind"`
	Browse bool   `json:"browseAvailable"`
	Diff   bool   `json:"diffAvailable"`
	Ignore bool   `json:"ignoreAvailable"`
	Reason string `json:"reason"`
}
type Node struct {
	ID            string `json:"nodeId"`
	Path          string `json:"path"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Status        string `json:"gitStatus"`
	Ignored       bool   `json:"ignored"`
	Referenceable bool   `json:"referenceable"`
	Link          string `json:"linkTarget,omitempty"`
	MediaType     string `json:"mediaType"`
}
type Listing struct {
	Children []Node    `json:"children"`
	Next     string    `json:"nextCursor"`
	Limited  bool      `json:"limited"`
	Complete bool      `json:"complete"`
	Version  string    `json:"contentVersion"`
	Checked  time.Time `json:"checkedAt"`
}

// Location is a root-relative node found from a path the user typed.
type Location struct {
	NodeID string `json:"nodeId"`
	Path   string `json:"path"`
	Kind   string `json:"kind"`
}
type Document struct {
	NodeID        string `json:"nodeId"`
	rawPath       string
	Path          string `json:"path"`
	Absolute      string `json:"absolutePath"`
	Source        string `json:"source"`
	OID           string `json:"oid"`
	Version       string `json:"contentVersion"`
	Kind          string `json:"kind"`
	Text          string `json:"text"`
	Lines         int    `json:"lineCount"`
	Referenceable bool   `json:"referenceable"`
	Reason        string `json:"reason"`
	MediaType     string `json:"mediaType"`
	Size          int64  `json:"size"`
	Downloadable  bool   `json:"downloadable"`
	// identity pins the worktree file a snapshot was read from (raw bytes).
	identity *identity
}

// identity is the stat tuple a worktree raw transfer must still match.
type identity struct {
	dev, ino uint64
	size     int64
	mtime    int64
}
type File struct {
	Snapshot string    `json:"snapshotId"`
	Document Document  `json:"document"`
	Checked  time.Time `json:"checkedAt"`
}
type Change struct {
	NodeID  string `json:"nodeId"`
	ID      string `json:"changeId"`
	Group   string `json:"group"`
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
	Status  string `json:"status"`
	OldOID  string `json:"oldOid"`
	NewOID  string `json:"newOid"`
	OldMode string `json:"oldMode"`
	NewMode string `json:"newMode"`
}
type ChangeQuery struct {
	Cursor string
	Q      string
	NodeID string
	Group  string
}
type Changes struct {
	Next    string    `json:"nextCursor"`
	Total   int       `json:"total"`
	Entries []Change  `json:"entries"`
	Version string    `json:"contentVersion"`
	Checked time.Time `json:"checkedAt"`
}
type Diff struct {
	Snapshot string    `json:"snapshotId"`
	Old      Document  `json:"old"`
	New      Document  `json:"new"`
	Kind     string    `json:"kind"`
	Reason   string    `json:"reason"`
	Checked  time.Time `json:"checkedAt"`
	Change   Change    `json:"change"`
}
type CheckRequest struct {
	Context        string
	Snapshots      []string
	Directories    map[string]string
	Changes        string
	IncludeIgnored bool
}
type Check struct {
	Snapshots   map[string]string `json:"snapshots"`
	Directories map[string]string `json:"directories"`
	Changes     string            `json:"changes"`
	Checked     time.Time         `json:"checkedAt"`
}
