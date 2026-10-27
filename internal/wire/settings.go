package wire

import (
	"encoding/json"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/jsonutil"
	"github.com/rainchan/webmux/internal/settings"
)

func SettingsValues(v settings.Values) contract.SettingsValues {
	out := contract.SettingsValues{Theme: v.Theme, FontSize: v.FontSize, HomeWorktrees: v.HomeWorktrees, ForceSoftKeys: v.ForceSoftKeys}
	if v.Preview != nil {
		out.Preview = &contract.SettingsPreview{Lines: v.Preview.Lines, Skip: v.Preview.Skip}
	}
	if v.CodeReader != nil {
		out.CodeReader = &contract.SettingsCodeReader{LineNumbers: v.CodeReader.LineNumbers, DiffOverflow: v.CodeReader.DiffOverflow}
	}
	if v.QuickCommands != nil {
		list := make([]contract.SettingsCommand, len(*v.QuickCommands))
		for i, c := range *v.QuickCommands {
			list[i] = contract.SettingsCommand{ID: c.ID, Name: c.Name, Command: c.Command, Enter: c.Enter}
		}
		out.QuickCommands = &list
	}
	return out
}
func SettingsSnapshot(s settings.Snapshot) contract.SettingsSnapshot {
	return contract.SettingsSnapshot{Revision: s.Revision, UpdatedAt: s.UpdatedAt, Values: SettingsValues(s.Values)}
}
func SettingsChanges(p contract.SettingsPatch) (settings.Changes, error) {
	if p.BaseRevision == nil || *p.BaseRevision < 0 || len(p.Changes) == 0 {
		return settings.Changes{}, settings.ErrInvalid
	}
	data, err := json.Marshal(p.Changes)
	if err != nil {
		return settings.Changes{}, settings.ErrInvalid
	}
	var v contract.SettingsValues
	if err = jsonutil.Decode(data, &v); err != nil {
		return settings.Changes{}, settings.ErrInvalid
	}
	// A changed compound value is complete; omitted nested fields must not
	// silently become Go zero values (notably command.enter and preview.skip).
	shapes := map[string]any{"theme": v.Theme, "fontSize": v.FontSize, "preview": v.Preview, "codeReader": v.CodeReader, "quickCommands": v.QuickCommands, "homeWorktrees": v.HomeWorktrees, "forceSoftKeys": v.ForceSoftKeys}
	for key, raw := range p.Changes {
		if err := jsonutil.Required(raw, shapes[key]); err != nil {
			return settings.Changes{}, settings.ErrInvalid
		}
	}
	c := settings.Changes{Fields: map[string]bool{}, Values: settings.Values{Theme: v.Theme, FontSize: v.FontSize, ForceSoftKeys: v.ForceSoftKeys, HomeWorktrees: v.HomeWorktrees}}
	for k := range p.Changes {
		c.Fields[k] = true
	}
	if v.Preview != nil {
		c.Values.Preview = &settings.Preview{Lines: v.Preview.Lines, Skip: v.Preview.Skip}
	}
	if v.CodeReader != nil {
		c.Values.CodeReader = &settings.CodeReader{LineNumbers: v.CodeReader.LineNumbers, DiffOverflow: v.CodeReader.DiffOverflow}
	}
	if v.QuickCommands != nil {
		list := make([]settings.Command, len(*v.QuickCommands))
		for i, x := range *v.QuickCommands {
			list[i] = settings.Command{ID: x.ID, Name: x.Name, Command: x.Command, Enter: x.Enter}
		}
		c.Values.QuickCommands = &list
	}
	return c, nil
}
func SettingsEvent(e settings.Event) contract.SettingsEvent {
	if e.Snapshot != nil {
		s := SettingsSnapshot(*e.Snapshot)
		return contract.SettingsEvent{Type: "settings", Settings: &s}
	}
	v := SettingsValues(e.Changes.Values)
	all := map[string]any{"theme": v.Theme, "fontSize": v.FontSize, "preview": v.Preview, "codeReader": v.CodeReader, "quickCommands": v.QuickCommands, "homeWorktrees": v.HomeWorktrees, "forceSoftKeys": v.ForceSoftKeys}
	changes := map[string]any{}
	for k := range e.Changes.Fields {
		changes[k] = all[k]
	}
	return contract.SettingsEvent{Type: "settings.update", Revision: e.Revision, UpdatedAt: e.UpdatedAt, Changes: changes}
}
