// Package wire converts between service types and the wire types of
// internal/contract. It belongs to the controller layer: services never see
// the wire shapes, and the controller and hub never encode service types.
//
// Conversions keep nil slices and maps nil and empty ones empty, because
// null and []/{} differ on the wire.
package wire

import (
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/launch"
	"github.com/rainchan/webmux/internal/push"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

// each maps a slice, keeping nil nil.
func each[S, D any](in []S, f func(S) D) []D {
	if in == nil {
		return nil
	}
	out := make([]D, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

// Session converts one session snapshot.
func Session(i session.Info) contract.SessionInfo {
	out := contract.SessionInfo{
		ID: i.ID, Generation: i.Generation, Title: i.Title, Cwd: i.Cwd, Preset: i.Preset,
		Cmd: i.Cmd, Status: string(i.Status), ExitCode: i.ExitCode, Preview: i.Preview,
		CreatedAt: i.CreatedAt, LastActiveAt: i.LastActiveAt, Cols: i.Cols, Rows: i.Rows, Clients: i.Clients,
	}
	if i.Workspace != nil {
		w := contract.SessionWorkspace(*i.Workspace)
		out.Workspace = &w
	}
	if i.Attention != nil {
		out.Attention = &contract.SessionAttention{Kind: string(i.Attention.Kind), Source: string(i.Attention.Source), Since: i.Attention.Since}
	}
	if i.ReportedCwd != nil {
		r := contract.SessionReportedCwd(*i.ReportedCwd)
		out.ReportedCwd = &r
	}
	return out
}

func Sessions(in []session.Info) []contract.SessionInfo { return each(in, Session) }

func Presets(in []config.Preset) []contract.Preset {
	return each(in, func(p config.Preset) contract.Preset { return contract.Preset(p) })
}

// SessionInput converts the body of POST /api/sessions.
func SessionInput(r contract.CreateSessionRequest) workspace.SessionInput {
	in := workspace.SessionInput{Cwd: r.Cwd, Preset: r.Preset, Cmd: r.Cmd, Args: r.Args, Name: r.Name, Env: r.Env, ConfigID: r.ConfigID}
	if r.Workspace != nil {
		ref := workspace.Reference(*r.Workspace)
		in.Workspace = &ref
	}
	return in
}

func Capability(c workspace.Capability) contract.WorkspaceCapability {
	return contract.WorkspaceCapability(c)
}

func Worktree(w workspace.Worktree) contract.Worktree { return contract.Worktree(w) }

func Repository(r workspace.Repository) contract.Repository {
	return contract.Repository{
		ID: r.ID, Name: r.Name, CommonDir: r.CommonDir, AnchorPath: r.AnchorPath,
		Availability: r.Availability, Error: r.Error, CheckedAt: r.CheckedAt,
		Worktrees: each(r.Worktrees, Worktree),
	}
}

func Repositories(in []workspace.Repository) []contract.Repository { return each(in, Repository) }

func Branches(b workspace.Branches) contract.Branches {
	return contract.Branches{
		Branches:      each(b.Branches, func(v workspace.Branch) contract.Branch { return contract.Branch(v) }),
		Bases:         each(b.Bases, func(v workspace.Base) contract.BranchBase { return contract.BranchBase(v) }),
		DefaultBase:   b.DefaultBase,
		DefaultParent: b.DefaultParent,
	}
}

func Preview(p workspace.Preview) contract.RemovalPreview {
	return contract.RemovalPreview{
		ConfirmationID: p.ConfirmationID, ExpiresAt: p.ExpiresAt, Worktree: Worktree(p.Worktree),
		Sessions: Sessions(p.Sessions), Blockers: p.Blockers, ForceAllowed: p.ForceAllowed,
	}
}

func CreateWorktree(r contract.CreateWorktreeRequest) workspace.CreateRequest {
	return workspace.CreateRequest(r)
}

func RemoveWorktree(r contract.RemoveWorktreeRequest) workspace.RemoveRequest {
	return workspace.RemoveRequest(r)
}

func LaunchConfig(c launch.Config) contract.LaunchConfig { return contract.LaunchConfig(c) }

func LaunchSummaries(in []launch.Summary) []contract.LaunchConfigSummary {
	return each(in, func(s launch.Summary) contract.LaunchConfigSummary { return contract.LaunchConfigSummary(s) })
}

func LaunchInput(r contract.LaunchConfigInput) launch.Input { return launch.Input(r) }

func PushTestReport(r push.TestReport) contract.PushTestReport { return contract.PushTestReport(r) }

func CodeContext(c codeview.Context) contract.CodeContext { return contract.CodeContext(c) }

func codeNode(n codeview.Node) contract.CodeNode { return contract.CodeNode(n) }

func CodeListing(l codeview.Listing) contract.CodeListing {
	return contract.CodeListing{Children: each(l.Children, codeNode), Next: l.Next, Limited: l.Limited, Complete: l.Complete, Version: l.Version, Checked: l.Checked}
}

func CodeLocation(l codeview.Location) contract.CodeLocation { return contract.CodeLocation(l) }

func codeDocument(d codeview.Document) contract.CodeDocument {
	return contract.CodeDocument{
		NodeID: d.NodeID, Path: d.Path, Absolute: d.Absolute, Source: d.Source, OID: d.OID,
		Version: d.Version, Kind: d.Kind, Text: d.Text, Lines: d.Lines, Referenceable: d.Referenceable,
		Reason: d.Reason, MediaType: d.MediaType, Size: d.Size, Downloadable: d.Downloadable,
	}
}

func CodeFile(f codeview.File) contract.CodeFile {
	return contract.CodeFile{Snapshot: f.Snapshot, Document: codeDocument(f.Document), Checked: f.Checked}
}

func codeChange(c codeview.Change) contract.CodeChange { return contract.CodeChange(c) }

func CodeChanges(c codeview.Changes) contract.CodeChanges {
	return contract.CodeChanges{Next: c.Next, Total: c.Total, Entries: each(c.Entries, codeChange), Version: c.Version, Checked: c.Checked}
}

func CodeDiff(d codeview.Diff) contract.CodeDiff {
	return contract.CodeDiff{
		Snapshot: d.Snapshot, Old: codeDocument(d.Old), New: codeDocument(d.New),
		Kind: d.Kind, Reason: d.Reason, Checked: d.Checked, Change: codeChange(d.Change),
	}
}

func CodeCheck(c codeview.Check) contract.CodeCheck { return contract.CodeCheck(c) }

func CodeCheckRequest(r contract.CodeCheckRequest) codeview.CheckRequest {
	return codeview.CheckRequest(r)
}

// SessionEvent converts one session list event.
func SessionEvent(e session.Event) contract.SessionEvent {
	out := contract.SessionEvent{Type: e.Type, Sessions: Sessions(e.Sessions), ID: e.ID}
	if e.Session != nil {
		info := Session(*e.Session)
		out.Session = &info
	}
	return out
}

func StreamPosition(p session.Position) contract.StreamPosition {
	return contract.StreamPosition(p)
}

func SubmissionStatus(s session.SubmissionStatus) contract.SubmissionStatus {
	return contract.SubmissionStatus{ID: s.ID, State: string(s.State), Error: s.Error}
}

func ControlState(c session.ControlState) contract.ControlState {
	out := contract.ControlState{Cols: c.Cols, Rows: c.Rows, ColorSchemeReports: c.ColorSchemeReports}
	if c.Controller != nil {
		controller := contract.TerminalController(*c.Controller)
		out.Controller = &controller
	}
	return out
}
