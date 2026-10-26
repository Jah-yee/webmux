package contract

import "time"

// The REST and WebSocket shapes below mirror web/src/schemas.ts. Field
// names, omitempty and nil-versus-empty behavior are part of the wire
// contract and are pinned by internal/api/testdata/wire.

// SessionStatus is the display state of a session.
type SessionStatus string

const (
	SessionStatusRunning SessionStatus = "running"
	SessionStatusWaiting SessionStatus = "waiting"
	SessionStatusIdle    SessionStatus = "idle"
	SessionStatusExited  SessionStatus = "exited"
)

// AttentionKind names the signal that a session needs the user.
type AttentionKind string

const (
	AttentionKindQuestion   AttentionKind = "question"
	AttentionKindPermission AttentionKind = "permission"
	AttentionKindDone       AttentionKind = "done"
	AttentionKindError      AttentionKind = "error"
	AttentionKindNotify     AttentionKind = "notify"
	AttentionKindBell       AttentionKind = "bell"
)

// Valid reports whether k is one of the named kinds.
func (k AttentionKind) Valid() bool {
	switch k {
	case AttentionKindQuestion, AttentionKindPermission, AttentionKindError, AttentionKindDone, AttentionKindNotify, AttentionKindBell:
		return true
	}
	return false
}

// AttentionSource identifies the component that raised an attention signal.
type AttentionSource string

const (
	AttentionSourceAgent    AttentionSource = "agent"
	AttentionSourceTerminal AttentionSource = "terminal"
)

// SessionInfo is one session in GET /api/sessions, POST /api/sessions,
// /ws/sessions events and the 0x02 terminal frame.
type SessionInfo struct {
	ID           string              `json:"id"`
	Generation   string              `json:"generation"`
	Title        string              `json:"title"`
	Cwd          string              `json:"cwd"`
	Cmd          []string            `json:"cmd"`
	Status       SessionStatus       `json:"status"`
	ExitCode     *int                `json:"exitCode"`
	Preview      string              `json:"preview"`
	CreatedAt    time.Time           `json:"createdAt"`
	LastActiveAt time.Time           `json:"lastActiveAt"`
	Cols         uint16              `json:"cols"`
	Rows         uint16              `json:"rows"`
	Clients      int                 `json:"clients"`
	Workspace    *SessionWorkspace   `json:"workspace"`
	Attention    *SessionAttention   `json:"attention"`
	ReportedCwd  *SessionReportedCwd `json:"reportedCwd"`
	// ConfigID is the saved launch config used at creation, if any. It is
	// omitted for sessions started from an explicit command.
	ConfigID string `json:"configId,omitempty"`
}

// SessionWorkspace is the registered worktree a session was started in.
type SessionWorkspace struct {
	RepositoryID string `json:"repositoryId"`
	WorktreeID   string `json:"worktreeId"`
	WorktreePath string `json:"worktreePath"`
}

// SessionAttention is a latched signal that the session needs the user.
type SessionAttention struct {
	Kind   AttentionKind   `json:"kind"`
	Source AttentionSource `json:"source"`
	Since  time.Time       `json:"since"`
}

// SessionReportedCwd is the directory a program reported through OSC 7.
type SessionReportedCwd struct {
	Path   string    `json:"path"`
	Host   string    `json:"host"`
	Remote bool      `json:"remote"`
	Since  time.Time `json:"since"`
}

// WorkspaceReference selects a registered worktree.
type WorkspaceReference struct {
	RepositoryID string `json:"repositoryId"`
	WorktreeID   string `json:"worktreeId"`
}

// CreateSessionRequest is the body of POST /api/sessions. ConfigID excludes
// Cmd and Env. omitempty only shapes what Go clients send;
// the server treats an absent field like its zero value.
type CreateSessionRequest struct {
	Cwd       string              `json:"cwd"`
	Cmd       []string            `json:"cmd"`
	Name      string              `json:"name,omitempty"`
	Workspace *WorkspaceReference `json:"workspace,omitempty"`
	Env       map[string]string   `json:"env,omitempty"`
	ConfigID  *string             `json:"configId,omitempty"`
}
