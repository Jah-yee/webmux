package session

import (
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"
)

func TestStatusBoundaries(t *testing.T) {
	now := time.Now()
	patterns := []*regexp.Regexp{regexp.MustCompile(`[$?]$`)}
	for _, row := range []struct {
		age             time.Duration
		line            string
		exited, blocked bool
		want            Status
	}{{time.Second, "", false, false, StatusRunning}, {2 * time.Second, "", false, false, StatusRunning}, {3500 * time.Millisecond, "background timer", false, false, StatusRunning}, {31 * time.Second, "done", false, false, StatusIdle}, {31 * time.Second, "Continue?", false, false, StatusWaiting}, {31 * time.Second, "", false, true, StatusWaiting}, {time.Second, "$", true, true, StatusExited}} {
		got := classify(statusInput{now: now, last: now.Add(-row.age), line: row.line, exited: row.exited, blocked: row.blocked, idleAfter: 30 * time.Second, patterns: patterns})
		if got != row.want {
			t.Fatalf("%+v got %s", row, got)
		}
	}
}
func TestPreviewSplitUnicodeAndANSI(t *testing.T) {
	var p Preview
	raw := []byte("\x1b[32m中文预览\x1b[0m\r\x1b]2;hidden title\aContinue? ")
	for _, b := range raw {
		p.Feed([]byte{b})
	}
	if got := p.last; got != "Continue?" {
		t.Fatal(got)
	}
}

func TestStatusUsesLatestPreviewLine(t *testing.T) {
	now := time.Now()
	var snapshot processSnapshot
	for _, row := range []struct {
		name, output, pattern string
		want                  Status
	}{
		{"earlier prompt", "Continue?\r\nfinished", `Continue\?`, StatusIdle},
		{"current prompt", "working\r\nContinue?", `^Continue\?$`, StatusWaiting},
		{"trailing blanks", "working\r\nContinue?\r\n\r\n", `^Continue\?$`, StatusWaiting},
		{"rewritten prompt", "working\r\nContinue?\r\x1b[Kdone", `Continue\?`, StatusIdle},
		{"erased prompt", "Continue?\r\x1b[2K", `Continue\?`, StatusIdle},
	} {
		t.Run(row.name, func(t *testing.T) {
			s := Session{
				info: Info{LastActiveAt: now.Add(-31 * time.Second)},
				cmd:  &exec.Cmd{Process: &os.Process{Pid: -1}},
			}
			s.info.Preview = s.preview.Feed([]byte(row.output))
			s.evaluate(now, snapshot, 30*time.Second, []*regexp.Regexp{regexp.MustCompile(row.pattern)})
			if s.info.Status != row.want {
				t.Fatalf("preview %q, latest %q: status = %s, want %s", s.info.Preview, s.preview.last, s.info.Status, row.want)
			}
		})
	}
}

func TestWorkspaceMetadataParticipatesInDisplayComparison(t *testing.T) {
	a := Info{}
	b := Info{Workspace: &Workspace{RepositoryID: "repository", WorktreeID: "tree", WorktreePath: "/tree"}}
	if sameDisplay(a, b) {
		t.Fatal("association change was invisible")
	}
	a.Workspace = &Workspace{RepositoryID: "repository", WorktreeID: "tree", WorktreePath: "/tree"}
	if !sameDisplay(a, b) {
		t.Fatal("equal workspace values were compared by pointer")
	}
	b.Workspace.WorktreePath = "/moved"
	if sameDisplay(a, b) {
		t.Fatal("workspace path change was invisible")
	}
}

func TestReportedCwdParticipatesInDisplayComparison(t *testing.T) {
	a := Info{}
	since := time.Now()
	b := Info{ReportedCwd: &ReportedCwd{Path: "/work/sub", Host: "", Since: since}}
	if sameDisplay(a, b) {
		t.Fatal("reported directory change was invisible")
	}
	a.ReportedCwd = &ReportedCwd{Path: "/work/sub", Host: "", Since: since}
	if !sameDisplay(a, b) {
		t.Fatal("equal reported directories were compared by pointer")
	}
	b.ReportedCwd.Remote = true
	if sameDisplay(a, b) {
		t.Fatal("remote classification change was invisible")
	}
}

func TestStatusComposition(t *testing.T) {
	now := time.Now()
	patterns := []*regexp.Regexp{regexp.MustCompile(`[$?]$`)}
	record := func(state string) *programRecord { return &programRecord{state: state} }
	for _, row := range []struct {
		name string
		age  time.Duration
		in   statusInput
		want Status
	}{
		{"exit beats record", 0, statusInput{exited: true, program: record("working")}, StatusExited},
		{"working ignores silence", 10 * time.Second, statusInput{program: record("working")}, StatusRunning},
		{"working stale", 31 * time.Second, statusInput{program: record("working")}, StatusIdle},
		{"idle despite output", 0, statusInput{program: record("idle"), line: "Continue?"}, StatusIdle},
		{"blocked", 31 * time.Second, statusInput{program: record("blocked")}, StatusWaiting},
		{"unseen done", 0, statusInput{program: record("done"), attention: true}, StatusWaiting},
		{"seen done", 0, statusInput{program: record("done")}, StatusIdle},
		{"seen error", 0, statusInput{program: record("error"), blocked: true}, StatusIdle},
		{"attention", 0, statusInput{attention: true}, StatusWaiting},
		{"structured skips prompt pattern", 3 * time.Second, statusInput{line: "Continue?", structured: true}, StatusRunning},
		{"structured skips terminal read", 3 * time.Second, statusInput{blocked: true, structured: true}, StatusRunning},
		{"structured goes idle", 31 * time.Second, statusInput{blocked: true, structured: true}, StatusIdle},
		{"structured recent output", 0, statusInput{structured: true}, StatusRunning},
		{"heuristic prompt", 3 * time.Second, statusInput{line: "Continue?"}, StatusWaiting},
		{"heuristic terminal read", 3 * time.Second, statusInput{blocked: true}, StatusWaiting},
	} {
		in := row.in
		in.now, in.last = now, now.Add(-row.age)
		in.idleAfter, in.patterns = 30*time.Second, patterns
		if got := classify(in); got != row.want {
			t.Errorf("%s: got %s, want %s", row.name, got, row.want)
		}
	}
}
