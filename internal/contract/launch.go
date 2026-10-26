package contract

import "time"

// PreviewSettings selects the session output preview rows. A null config
// preview inherits the browser's global preview setting.
type PreviewSettings struct {
	Lines int `json:"lines"`
	Skip  int `json:"skip"`
}

// LaunchConfig is one saved launch config with its environment values.
type LaunchConfig struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Icon      string            `json:"icon,omitempty"`
	Cmd       []string          `json:"cmd"`
	Env       map[string]string `json:"env"`
	Preview   *PreviewSettings  `json:"preview"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

// LaunchConfigSummary is a list entry; it names environment variables only.
type LaunchConfigSummary struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Icon      string           `json:"icon,omitempty"`
	Cmd       []string         `json:"cmd"`
	EnvNames  []string         `json:"envNames"`
	Preview   *PreviewSettings `json:"preview"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

// LaunchConfigInput is the body of a launch config create or update.
type LaunchConfigInput struct {
	Name    string            `json:"name"`
	Icon    string            `json:"icon,omitempty"`
	Cmd     []string          `json:"cmd"`
	Env     map[string]string `json:"env"`
	Preview *PreviewSettings  `json:"preview"`
}
