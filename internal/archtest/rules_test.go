package archtest

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// fixture classifies testdata/fixture. internal/unlisted is deliberately
// missing and internal/ghost deliberately stale.
var fixture = manifest{
	module: "example.com/fixture",
	layers: map[string]layer{
		"internal/api":       controller,
		"internal/ctrl":      controller,
		"internal/hubx":      controller,
		"internal/svc":       service,
		"internal/svchttp":   service,
		"internal/svcout":    service,
		"internal/shared":    shared,
		"internal/statefile": shared,
		"internal/client":    client,
		"internal/ghost":     shared,
		"internal/contract":  shared,
		"internal/wirectl":   controller,
	},
	outbound: map[string]string{"internal/svcout": "fixture outbound client"},
	pure: map[string]string{
		"internal/svc.Pure":           "fixture stateless string validation",
		"internal/svc.NoReason":       " \t",
		"internal/svc.removedPure":    "stale missing function",
		"internal/svc.UnusedPure":     "stale unused function",
		"internal/svc.NotAFunction":   "invalid variable registration",
		"internal/svc.(*Service).Do":  "invalid method registration",
		"internal/shared.notAService": "registration outside a service",
	},
	contract:  "internal/contract",
	wireScope: []string{"internal/wirectl"},
	sinks: map[string]sink{
		"internal/wirectl.reply": {arg: 0, typeArg: -1},
		"internal/wirectl.serve": {typeArg: 0},
	},
	encoders: map[string]string{
		"internal/svc.digest":            "fixture digest",
		"internal/svc.registeredAlias":   "fixture direct call with rejected alias",
		"internal/svc.(*Service).Encode": "fixture method",
		"internal/svc.removed":           "stale registration",
		"internal/shared.notAService":    "registration outside a service",
	},
}

var wantComment = regexp.MustCompile(`\bwant((?: R\d+)+)\s*$`)

// TestRulesOnFixture proves every rule: each "// want Rn" line of the
// fixture is reported with exactly those rules, and nothing else is.
func TestRulesOnFixture(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	p, err := load(filepath.Join("testdata", "fixture"), fixture)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"internal/unlisted:0 R0": true,
		"internal/ghost:0 R0":    true,
		"internal/svc:0 R0":      true,
		"internal/shared:0 R0":   true,
	}
	for _, pkg := range p.pkgs {
		for _, file := range pkg.Syntax {
			for _, group := range file.Comments {
				for _, c := range group.List {
					m := wantComment.FindStringSubmatch(c.Text)
					if m == nil {
						continue
					}
					pos := p.fset.Position(c.Pos())
					rel, _ := filepath.Rel(p.dir, pos.Filename)
					for _, rule := range strings.Fields(m[1]) {
						want[fmt.Sprintf("%s:%d %s", filepath.ToSlash(rel), pos.Line, rule)] = true
					}
				}
			}
		}
	}
	got := map[string]string{}
	messages := map[string]bool{}
	for _, v := range p.check() {
		got[fmt.Sprintf("%s:%d %s", v.file, v.line, v.rule)] = v.String()
		messages[v.msg] = true
	}
	// Package-level R0 reports share a position; check each registration
	// diagnostic so one invalid entry cannot mask another.
	for _, msg := range []string{
		"pure registration internal/svc.NoReason has no reason",
		"registered pure function internal/svc.removedPure is not a package function",
		"registered pure function internal/svc.UnusedPure is not used by a controller",
		"registered pure function internal/svc.NotAFunction is not a package function",
		"registered pure function internal/svc.(*Service).Do is not a package function",
		"pure registration internal/shared.notAService is not in a service package",
	} {
		if !messages[msg] {
			t.Errorf("registration violation not detected: %s", msg)
		}
	}
	var missing, unexpected []string
	for key := range want {
		if _, ok := got[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key, text := range got {
		if !want[key] {
			unexpected = append(unexpected, text)
		}
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	if len(missing) > 0 {
		t.Errorf("violations not detected:\n%s", strings.Join(missing, "\n"))
	}
	if len(unexpected) > 0 {
		t.Errorf("compliant code reported:\n%s", strings.Join(unexpected, "\n"))
	}
	if len(want) < 30 {
		t.Fatalf("fixture lost its annotations: %d", len(want))
	}
}

// TestViolationsReportedTogether checks one run lists violations of several
// rules, sorted by file and line, each with a position, rule and hint.
func TestViolationsReportedTogether(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	p, err := load(filepath.Join("testdata", "fixture"), fixture)
	if err != nil {
		t.Fatal(err)
	}
	violations := p.check()
	rules := map[string]bool{}
	for i, v := range violations {
		rules[v.rule] = true
		if v.hint == "" || v.msg == "" || !strings.Contains(v.String(), "["+v.rule+"]") {
			t.Errorf("incomplete report %q", v.String())
		}
		if i > 0 && (violations[i-1].file > v.file || violations[i-1].file == v.file && violations[i-1].line > v.line) {
			t.Errorf("unsorted: %q before %q", violations[i-1], v)
		}
	}
	for _, rule := range []string{"R0", "R1", "R2", "R3", "R4", "R5", "R6", "R7", "R8", "R9", "R10"} {
		if !rules[rule] {
			t.Errorf("no %s violation in one run", rule)
		}
	}
}
