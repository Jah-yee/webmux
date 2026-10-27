package wire

import (
	"reflect"
	"testing"

	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/push"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

// These pairs are converted with a direct type conversion, which is only safe
// while the exported fields match in name and order. Field renames must update
// the converter at the same time.
func TestDirectConversionsKeepFieldOrder(t *testing.T) {
	pairs := []struct {
		name     string
		service  any
		contract any
	}{
		{"Capability", workspace.Capability{}, contract.WorkspaceCapability{}},
		{"Worktree", workspace.Worktree{}, contract.Worktree{}},
		{"Base", workspace.Base{}, contract.BranchBase{}},
		{"Reference", workspace.Reference{}, contract.WorkspaceReference{}},
		{"CreateRequest", workspace.CreateRequest{}, contract.CreateWorktreeRequest{}},
		{"RemoveRequest", workspace.RemoveRequest{}, contract.RemoveWorktreeRequest{}},
		{"CodeContext", codeview.Context{}, contract.CodeContext{}},
		{"CodeNode", codeview.Node{}, contract.CodeNode{}},
		{"CodeLocation", codeview.Location{}, contract.CodeLocation{}},
		{"CodeChange", codeview.Change{}, contract.CodeChange{}},
		{"CodeCheck", codeview.Check{}, contract.CodeCheck{}},
		{"CodeCheckRequest", codeview.CheckRequest{}, contract.CodeCheckRequest{}},
		{"Position", session.Position{}, contract.StreamPosition{}},
		{"Workspace", session.Workspace{}, contract.SessionWorkspace{}},
		{"ReportedCwd", session.ReportedCwd{}, contract.SessionReportedCwd{}},
		{"Controller", session.Controller{}, contract.TerminalController{}},
		{"PushTestReport", push.TestReport{}, contract.PushTestReport{}},
	}
	for _, pair := range pairs {
		t.Run(pair.name, func(t *testing.T) {
			service := exportedFields(reflect.TypeOf(pair.service))
			wire := exportedFields(reflect.TypeOf(pair.contract))
			if !reflect.DeepEqual(service, wire) {
				t.Fatalf("fields = %v, contract = %v", service, wire)
			}
		})
	}
}

func exportedFields(t reflect.Type) []string {
	fields := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		if field := t.Field(i); field.IsExported() {
			fields = append(fields, field.Name)
		}
	}
	return fields
}
