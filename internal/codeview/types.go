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

type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string { return e.Message }
func problem(code int, format string, args ...any) error {
	return &Error{code, fmt.Sprintf(format, args...)}
}
func Status(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return 504
	}
	if errors.Is(err, os.ErrNotExist) {
		return 404
	}
	if workspace.Status(err) == 413 || workspace.Status(err) == 504 {
		return workspace.Status(err)
	}
	return 500
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
	Context        string            `json:"contextId"`
	Snapshots      []string          `json:"snapshotIds"`
	Directories    map[string]string `json:"directories"`
	Changes        string            `json:"changesVersion"`
	IncludeIgnored bool              `json:"includeIgnored"`
}
type Check struct {
	Snapshots   map[string]string `json:"snapshots"`
	Directories map[string]string `json:"directories"`
	Changes     string            `json:"changes"`
	Checked     time.Time         `json:"checkedAt"`
}
