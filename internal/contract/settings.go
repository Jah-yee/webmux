package contract

import "encoding/json"

type SettingsPreview struct {
	Lines int `json:"lines"`
	Skip  int `json:"skip"`
}
type SettingsCodeReader struct {
	LineNumbers  string `json:"lineNumbers"`
	DiffOverflow string `json:"diffOverflow"`
}
type SettingsCommand struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Enter   bool   `json:"enter"`
}
type SettingsValues struct {
	Theme         *string             `json:"theme"`
	FontSize      *int                `json:"fontSize"`
	Preview       *SettingsPreview    `json:"preview"`
	CodeReader    *SettingsCodeReader `json:"codeReader"`
	QuickCommands *[]SettingsCommand  `json:"quickCommands"`
	HomeWorktrees map[string][]string `json:"homeWorktrees"`
	ForceSoftKeys *bool               `json:"forceSoftKeys"`
}
type SettingsSnapshot struct {
	Revision  int64          `json:"revision"`
	UpdatedAt string         `json:"updatedAt"`
	Values    SettingsValues `json:"values"`
}
type SettingsChanges map[string]json.RawMessage

type SettingsPatch struct {
	BaseRevision *int64          `json:"baseRevision"`
	Changes      SettingsChanges `json:"changes"`
}
type SettingsEvent struct {
	Type      string            `json:"type"`
	Settings  *SettingsSnapshot `json:"settings,omitempty"`
	Revision  int64             `json:"revision,omitempty"`
	UpdatedAt string            `json:"updatedAt,omitempty"`
	Changes   map[string]any    `json:"changes,omitempty"`
}
