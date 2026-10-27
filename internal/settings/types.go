// Package settings owns server preferences and ordered change subscriptions.
package settings

import (
	"errors"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

var (
	ErrUnavailable = errors.New("settings unavailable")
	ErrInvalid     = errors.New("invalid settings")
	ErrConflict    = errors.New("settings changed; read the current revision and retry")
)

type Preview struct {
	Lines int `json:"lines"`
	Skip  int `json:"skip"`
}
type CodeReader struct {
	LineNumbers  string `json:"lineNumbers"`
	DiffOverflow string `json:"diffOverflow"`
}
type Command struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Enter   bool   `json:"enter"`
}
type Values struct {
	Theme         *string             `json:"theme"`
	FontSize      *int                `json:"fontSize"`
	Preview       *Preview            `json:"preview"`
	CodeReader    *CodeReader         `json:"codeReader"`
	QuickCommands *[]Command          `json:"quickCommands"`
	HomeWorktrees map[string][]string `json:"homeWorktrees"`
	ForceSoftKeys *bool               `json:"forceSoftKeys"`
}
type Snapshot struct {
	Revision  int64  `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
	Values    Values `json:"values"`
}
type Changes struct {
	Fields map[string]bool
	Values Values
}
type Event struct {
	Snapshot  *Snapshot
	Revision  int64
	UpdatedAt string
	Changes   Changes
}
type storedFile struct {
	V int `json:"v"`
	Snapshot
}

// Zod's string limits count UTF-16 code units. Keep accepted API values
// readable by every browser, including non-BMP characters.
func validText(s string, max int) bool {
	if !utf8.ValidString(s) {
		return false
	}
	size := 0
	for _, r := range s {
		size += utf16.RuneLen(r)
	}
	return size <= max
}
func validate(f storedFile) error {
	if f.V != 1 || f.Revision < 0 || f.Revision > 9007199254740991 {
		return ErrInvalid
	}
	if f.Revision > 0 {
		if _, err := time.Parse(time.RFC3339Nano, f.UpdatedAt); err != nil {
			return ErrInvalid
		}
	} else if f.UpdatedAt != "" {
		return ErrInvalid
	}
	return validateValues(f.Values)
}
func validateValues(v Values) error {
	if v.Theme != nil && *v.Theme != "light" && *v.Theme != "dark" {
		return ErrInvalid
	}
	if v.FontSize != nil && (*v.FontSize < 8 || *v.FontSize > 20) {
		return ErrInvalid
	}
	if v.Preview != nil && (v.Preview.Lines < 1 || v.Preview.Lines > 50 || v.Preview.Skip < 0 || v.Preview.Skip > 50) {
		return ErrInvalid
	}
	if v.CodeReader != nil && ((v.CodeReader.LineNumbers != "show" && v.CodeReader.LineNumbers != "hide") || (v.CodeReader.DiffOverflow != "scroll" && v.CodeReader.DiffOverflow != "wrap")) {
		return ErrInvalid
	}
	if v.QuickCommands != nil {
		if len(*v.QuickCommands) > 50 {
			return ErrInvalid
		}
		ids := map[string]bool{}
		for _, c := range *v.QuickCommands {
			if c.ID == "" || !validText(c.ID, 64) || ids[c.ID] || !validText(c.Name, 40) || strings.TrimSpace(c.Command) == "" || !validText(c.Command, 4096) {
				return ErrInvalid
			}
			ids[c.ID] = true
		}
	}
	if len(v.HomeWorktrees) > 200 {
		return ErrInvalid
	}
	total := 0
	for id, list := range v.HomeWorktrees {
		if id == "" || !validText(id, 128) || list == nil || len(list) > 1000 {
			return ErrInvalid
		}
		total += len(list)
		seen := map[string]bool{}
		for _, wid := range list {
			if wid == "" || !validText(wid, 128) || seen[wid] {
				return ErrInvalid
			}
			seen[wid] = true
		}
	}
	if total > 2000 {
		return ErrInvalid
	}
	return nil
}
func normalize(v Values) Values {
	v = cloneValues(v)
	if v.QuickCommands != nil {
		for i := range *v.QuickCommands {
			c := &(*v.QuickCommands)[i]
			c.Name = strings.TrimSpace(c.Name)
			c.Command = strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(c.Command, "\r\n", "\n"), "\r", "\n"), "\n")
		}
	}
	return v
}
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func cloneValues(v Values) Values {
	v.Theme = clonePtr(v.Theme)
	v.FontSize = clonePtr(v.FontSize)
	v.Preview = clonePtr(v.Preview)
	v.CodeReader = clonePtr(v.CodeReader)
	v.ForceSoftKeys = clonePtr(v.ForceSoftKeys)
	if v.QuickCommands != nil {
		list := append([]Command{}, (*v.QuickCommands)...)
		v.QuickCommands = &list
	}
	if v.HomeWorktrees != nil {
		m := map[string][]string{}
		for k, list := range v.HomeWorktrees {
			m[k] = append([]string{}, list...)
		}
		v.HomeWorktrees = m
	}
	return v
}
func clone(s Snapshot) Snapshot { s.Values = cloneValues(s.Values); return s }
func apply(v Values, c Changes) (Values, error) {
	if len(c.Fields) == 0 {
		return v, ErrInvalid
	}
	for key := range c.Fields {
		switch key {
		case "theme":
			v.Theme = c.Values.Theme
		case "fontSize":
			v.FontSize = c.Values.FontSize
		case "preview":
			v.Preview = c.Values.Preview
		case "codeReader":
			v.CodeReader = c.Values.CodeReader
		case "quickCommands":
			v.QuickCommands = c.Values.QuickCommands
		case "homeWorktrees":
			v.HomeWorktrees = c.Values.HomeWorktrees
		case "forceSoftKeys":
			v.ForceSoftKeys = c.Values.ForceSoftKeys
		default:
			return v, ErrInvalid
		}
	}
	v = normalize(v)
	return v, validateValues(v)
}
