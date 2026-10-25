package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/rainchan/webmux/internal/session"
)

const readTimeout = 10 * time.Second
const writeTimeout = 30 * time.Second
const confirmationLifetime = 5 * time.Minute

// Error kinds. Each wraps a transport-neutral category; the controller maps
// kinds to HTTP statuses.
var (
	ErrInvalid     = errors.New("invalid workspace request")
	ErrNotFound    = errors.New("workspace item not found")
	ErrConflict    = errors.New("workspace state conflict")
	ErrTooLarge    = errors.New("workspace output too large")
	ErrInternal    = errors.New("workspace internal error")
	ErrUnavailable = errors.New("workspace unavailable")
	ErrTimeout     = errors.New("workspace operation timed out")
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

// errorKind classifies err like the controller does: its own kind, a timeout for
// cancellation, otherwise an internal error.
func errorKind(err error) error {
	var e *Error
	if errors.As(err, &e) {
		return e.kind
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrTimeout
	}
	return ErrInternal
}

type Capability struct {
	GitAvailable      bool   `json:"gitAvailable"`
	RegistryAvailable bool   `json:"registryAvailable"`
	GitError          string `json:"gitError"`
	RegistryError     string `json:"registryError"`
}
type Repository struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	CommonDir    string     `json:"commonDir"`
	AnchorPath   string     `json:"anchorPath"`
	Availability string     `json:"availability"`
	Error        string     `json:"error"`
	CheckedAt    time.Time  `json:"checkedAt"`
	Worktrees    []Worktree `json:"worktrees"`
}
type Worktree struct {
	ID           string  `json:"id"`
	RepositoryID string  `json:"repositoryId"`
	Path         string  `json:"path"`
	Kind         string  `json:"kind"`
	Branch       *string `json:"branch"`
	Head         *string `json:"head"`
	Detached     bool    `json:"detached"`
	Locked       bool    `json:"locked"`
	LockReason   string  `json:"lockReason"`
	Prunable     bool    `json:"prunable"`
	Available    bool    `json:"available"`
	Dirty        *bool   `json:"dirty"`
	HasUntracked bool    `json:"hasUntracked"`
	HasIgnored   bool    `json:"hasIgnored"`
	Error        string  `json:"error"`
}
type Branch struct {
	Name          string   `json:"name"`
	Head          string   `json:"head"`
	WorktreePaths []string `json:"worktreePaths"`
}
type Base struct {
	Name string `json:"name"`
	Head string `json:"head"`
}
type Branches struct {
	Branches      []Branch `json:"branches"`
	Bases         []Base   `json:"bases"`
	DefaultBase   *string  `json:"defaultBase"`
	DefaultParent string   `json:"defaultParent"`
}
type Reference struct {
	RepositoryID string
	WorktreeID   string
}
type CreateRequest struct {
	Path   string
	Mode   string
	Branch string
	Base   string
}
type RemoveRequest struct {
	ConfirmationID string
	Mode           string
	ConfirmPath    string
	CloseSessions  bool
}
type Preview struct {
	ConfirmationID string         `json:"confirmationId"`
	ExpiresAt      time.Time      `json:"expiresAt"`
	Worktree       Worktree       `json:"worktree"`
	Sessions       []session.Info `json:"sessions"`
	Blockers       []string       `json:"blockers"`
	ForceAllowed   bool           `json:"forceAllowed"`
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
func worktreeID(repository, path string) string {
	sum := sha256.Sum256([]byte(repository + "\x00" + path))
	return hex.EncodeToString(sum[:16])
}
func strptr(s string) *string { return &s }
