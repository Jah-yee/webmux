package wire

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/attentionkind"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/launch"
	"github.com/rainchan/webmux/internal/push"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

func encode(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Conversions keep nil as null and empty as []/{}: the browser schema
// distinguishes them.
func TestNilAndEmptyCollectionsKeepTheirShape(t *testing.T) {
	for _, row := range []struct {
		name string
		got  any
		want []string
	}{
		{"nil sessions", Sessions(nil), []string{"null"}},
		{"empty sessions", Sessions([]session.Info{}), []string{"[]"}},
		{"nil cmd", Session(session.Info{}), []string{`"cmd":null`, `"workspace":null`, `"attention":null`, `"reportedCwd":null`, `"exitCode":null`}},
		{"empty cmd", Session(session.Info{Cmd: []string{}}), []string{`"cmd":[]`}},
		{"empty env", LaunchConfig(launch.Config{Env: map[string]string{}}), []string{`"env":{}`}},
		{"nil env", LaunchConfig(launch.Config{}), []string{`"env":null`}},
		{"empty env names", LaunchSummaries([]launch.Summary{{EnvNames: []string{}}}), []string{`"envNames":[]`}},
		{"repository without worktrees", Repository(workspace.Repository{}), []string{`"worktrees":null`}},
		{"repository with no worktrees", Repository(workspace.Repository{Worktrees: []workspace.Worktree{}}), []string{`"worktrees":[]`}},
		{"preview", Preview(workspace.Preview{Sessions: []session.Info{}, Blockers: []string{}}), []string{`"sessions":[]`, `"blockers":[]`}},
		{"test report", PushTestReport(push.TestReport{Removed: []string{}, Errors: []string{}}), []string{`"removed":[]`, `"errors":[]`}},
		{"listing", CodeListing(codeview.Listing{Children: []codeview.Node{}}), []string{`"children":[]`}},
		{"node link omitted", CodeListing(codeview.Listing{Children: []codeview.Node{{}}}), []string{`"mediaType":""`}},
		{"check maps", CodeCheck(codeview.Check{Snapshots: map[string]string{}}), []string{`"snapshots":{}`, `"directories":null`}},
	} {
		got := encode(t, row.got)
		for _, want := range row.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: %s lacks %s", row.name, got, want)
			}
		}
	}
	if got := encode(t, CodeListing(codeview.Listing{Children: []codeview.Node{{}}})); strings.Contains(got, "linkTarget") {
		t.Errorf("empty link not omitted: %s", got)
	}
}

func TestSessionCopiesNestedValues(t *testing.T) {
	code := 3
	since := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	in := session.Info{
		ID: "abcdefgh", Status: session.StatusExited, ExitCode: &code,
		Workspace:   &session.Workspace{RepositoryID: "r", WorktreeID: "w", WorktreePath: "/p"},
		Attention:   &session.Attention{Kind: attentionkind.Question, Source: session.SourceAgent, Since: since},
		ReportedCwd: &session.ReportedCwd{Path: "/x", Host: "h", Remote: true, Since: since},
	}
	out := Session(in)
	if out.Status != "exited" || *out.ExitCode != 3 || out.Workspace.WorktreePath != "/p" || out.Attention.Kind != "question" || out.Attention.Source != "agent" || !out.ReportedCwd.Remote {
		t.Fatalf("converted %+v", out)
	}
	in.Workspace.WorktreeID = "changed"
	if out.Workspace.WorktreeID != "w" {
		t.Fatal("workspace aliases the service value")
	}
}

func TestRequestsConvertToServiceInputs(t *testing.T) {
	id := "abcdefgh"
	in := SessionInput(contractRequest(id))
	if in.Cwd != "/w" || in.Workspace == nil || in.Workspace.WorktreeID != "t" || *in.ConfigID != id {
		t.Fatalf("session input %+v", in)
	}
	if SessionInput(contractRequest("")).Workspace == nil {
		t.Fatal("reference dropped")
	}
}

func contractRequest(id string) contract.CreateSessionRequest {
	return contract.CreateSessionRequest{Cwd: "/w", Workspace: &contract.WorkspaceReference{RepositoryID: "r", WorktreeID: "t"}, ConfigID: &id}
}
