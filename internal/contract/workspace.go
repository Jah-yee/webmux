package contract

import "time"

// WorkspaceCapability reports whether Git and the repository registry work.
type WorkspaceCapability struct {
	GitAvailable      bool   `json:"gitAvailable"`
	RegistryAvailable bool   `json:"registryAvailable"`
	GitError          string `json:"gitError"`
	RegistryError     string `json:"registryError"`
}

// Repository is a registered Git repository and its worktrees.
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

type BranchBase struct {
	Name string `json:"name"`
	Head string `json:"head"`
}

// Branches is the body of GET /api/repositories/{id}/branches.
type Branches struct {
	Branches      []Branch     `json:"branches"`
	Bases         []BranchBase `json:"bases"`
	DefaultBase   *string      `json:"defaultBase"`
	DefaultParent string       `json:"defaultParent"`
}

// RegisterRepositoryRequest is the body of POST /api/repositories.
type RegisterRepositoryRequest struct {
	Path string `json:"path"`
}

// CreateWorktreeRequest is the body of POST /api/repositories/{id}/worktrees.
type CreateWorktreeRequest struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Branch string `json:"branch"`
	Base   string `json:"base"`
}

// RemovalPreviewRequest is the empty body of a removal preview.
type RemovalPreviewRequest struct{}

// RemovalPreview describes what removing a worktree would affect.
type RemovalPreview struct {
	ConfirmationID string        `json:"confirmationId"`
	ExpiresAt      time.Time     `json:"expiresAt"`
	Worktree       Worktree      `json:"worktree"`
	Sessions       []SessionInfo `json:"sessions"`
	Blockers       []string      `json:"blockers"`
	ForceAllowed   bool          `json:"forceAllowed"`
}

// RemoveWorktreeRequest confirms a previewed removal.
type RemoveWorktreeRequest struct {
	ConfirmationID string `json:"confirmationId"`
	Mode           string `json:"mode"`
	ConfirmPath    string `json:"confirmPath"`
	CloseSessions  bool   `json:"closeSessions"`
}
