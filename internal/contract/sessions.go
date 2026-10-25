package contract

import "time"

// The REST and WebSocket shapes below mirror web/src/schemas.ts. Field
// names, omitempty and nil-versus-empty behavior are part of the wire
// contract and are pinned by internal/api/testdata/wire.

// SessionInfo is one session in GET /api/sessions, POST /api/sessions,
// /ws/sessions events and the 0x02 terminal frame.
type SessionInfo struct {
	ID           string              `json:"id"`
	Generation   string              `json:"generation"`
	Title        string              `json:"title"`
	Cwd          string              `json:"cwd"`
	Preset       string              `json:"preset"`
	Cmd          []string            `json:"cmd"`
	Status       string              `json:"status"`
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
}

// SessionWorkspace is the registered worktree a session was started in.
type SessionWorkspace struct {
	RepositoryID string `json:"repositoryId"`
	WorktreeID   string `json:"worktreeId"`
	WorktreePath string `json:"worktreePath"`
}

// SessionAttention is a latched signal that the session needs the user.
type SessionAttention struct {
	Kind   string    `json:"kind"`
	Source string    `json:"source"`
	Since  time.Time `json:"since"`
}

// SessionReportedCwd is the directory a program reported through OSC 7.
type SessionReportedCwd struct {
	Path   string    `json:"path"`
	Host   string    `json:"host"`
	Remote bool      `json:"remote"`
	Since  time.Time `json:"since"`
}

// Preset is one configured launch command in GET /api/presets.
type Preset struct {
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Cmd       []string `json:"cmd"`
	AllowArgs bool     `json:"allowArgs"`
}

// WorkspaceReference selects a registered worktree.
type WorkspaceReference struct {
	RepositoryID string `json:"repositoryId"`
	WorktreeID   string `json:"worktreeId"`
}

// CreateSessionRequest is the body of POST /api/sessions. ConfigID excludes
// Preset, Cmd, Args and Env. omitempty only shapes what Go clients send;
// the server treats an absent field like its zero value.
type CreateSessionRequest struct {
	Cwd       string              `json:"cwd"`
	Preset    string              `json:"preset"`
	Cmd       []string            `json:"cmd"`
	Args      []string            `json:"args,omitempty"`
	Name      string              `json:"name,omitempty"`
	Workspace *WorkspaceReference `json:"workspace,omitempty"`
	Env       map[string]string   `json:"env,omitempty"`
	ConfigID  *string             `json:"configId,omitempty"`
}
