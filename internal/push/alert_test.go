package push

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/attentionkind"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/session"
)

func info(id string, kind attentionkind.Kind, source session.AttentionSource, since time.Time) session.Info {
	i := session.Info{ID: id, Title: "t-" + id, Status: session.StatusRunning}
	if kind != "" {
		i.Status = session.StatusWaiting
		i.Attention = &session.Attention{Kind: kind, Source: source, Since: since}
	}
	return i
}

func updated(i session.Info) session.Event     { return session.Event{Type: "updated", Session: &i} }
func snapshot(i ...session.Info) session.Event { return session.Event{Type: "snapshot", Sessions: i} }

func TestTracker(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1, t2 := t0.Add(time.Second), t0.Add(2*time.Second)
	const agent, terminal = session.SourceAgent, session.SourceTerminal
	type step struct {
		event session.Event
		want  []string // "<session>:<kind>"
	}
	for _, row := range []struct {
		name  string
		kinds []string
		steps []step
	}{
		{name: "initial snapshot is only a baseline", steps: []step{
			{snapshot(info("aaaaaaaa", attentionkind.Question, agent, t0)), nil},
			{updated(info("aaaaaaaa", attentionkind.Question, agent, t0)), nil},
		}},
		{name: "new question alerts once", steps: []step{
			{snapshot(), nil},
			{session.Event{Type: "created", Session: new(info("aaaaaaaa", "", "", time.Time{}))}, nil},
			{updated(info("aaaaaaaa", attentionkind.Question, agent, t0)), []string{"aaaaaaaa:question"}},
			{updated(info("aaaaaaaa", attentionkind.Question, agent, t0)), nil},
			{updated(info("aaaaaaaa", attentionkind.Question, terminal, t0)), nil},
		}},
		{name: "cleared then raised again", steps: []step{
			{snapshot(info("aaaaaaaa", attentionkind.Question, agent, t0)), nil},
			{updated(info("aaaaaaaa", "", "", time.Time{})), nil},
			{updated(info("aaaaaaaa", attentionkind.Question, agent, t1)), []string{"aaaaaaaa:question"}},
		}},
		{name: "done upgrades to permission", steps: []step{
			{snapshot(), nil},
			{updated(info("aaaaaaaa", attentionkind.Done, agent, t0)), []string{"aaaaaaaa:done"}},
			{updated(info("aaaaaaaa", attentionkind.Permission, agent, t1)), []string{"aaaaaaaa:permission"}},
		}},
		{name: "default kinds skip bell", kinds: config.DefaultPushKinds(), steps: []step{
			{snapshot(), nil},
			{updated(info("aaaaaaaa", attentionkind.Bell, terminal, t0)), nil},
			{updated(info("aaaaaaaa", attentionkind.Notify, terminal, t1)), []string{"aaaaaaaa:notify"}},
		}},
		{name: "unlisted kind then listed upgrade", kinds: []string{"question", "permission"}, steps: []step{
			{snapshot(), nil},
			{updated(info("aaaaaaaa", attentionkind.Done, agent, t0)), nil},
			{updated(info("aaaaaaaa", attentionkind.Permission, agent, t1)), []string{"aaaaaaaa:permission"}},
		}},
		{name: "heuristic waiting and exit never alert", steps: []step{
			{snapshot(), nil},
			{updated(session.Info{ID: "aaaaaaaa", Status: session.StatusWaiting}), nil},
			{updated(session.Info{ID: "aaaaaaaa", Status: session.StatusExited, ExitCode: new(1)}), nil},
		}},
		{name: "resubscribe snapshot catches up once", steps: []step{
			{snapshot(info("aaaaaaaa", attentionkind.Question, agent, t0), info("bbbbbbbb", "", "", time.Time{}), info("cccccccc", attentionkind.Done, agent, t0)), nil},
			{snapshot(info("aaaaaaaa", attentionkind.Question, agent, t0), info("bbbbbbbb", attentionkind.Error, agent, t2)), []string{"bbbbbbbb:error"}},
			{snapshot(info("aaaaaaaa", attentionkind.Question, agent, t0), info("bbbbbbbb", attentionkind.Error, agent, t2)), nil},
			// cccccccc vanished from the snapshot, so a reused ID starts fresh.
			{updated(info("cccccccc", attentionkind.Done, agent, t0)), []string{"cccccccc:done"}},
		}},
		{name: "removed session forgets its key", steps: []step{
			{snapshot(), nil},
			{updated(info("aaaaaaaa", attentionkind.Error, agent, t0)), []string{"aaaaaaaa:error"}},
			{session.Event{Type: "removed", ID: "aaaaaaaa"}, nil},
			{updated(info("aaaaaaaa", attentionkind.Error, agent, t0)), []string{"aaaaaaaa:error"}},
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			kinds := row.kinds
			if kinds == nil {
				kinds = []string{"question", "permission", "error", "done", "notify", "bell"}
			}
			tracker := NewTracker(kinds)
			for i, s := range row.steps {
				var got []string
				for _, a := range tracker.Observe(s.event) {
					got = append(got, a.SessionID+":"+string(a.Kind))
				}
				if strings.Join(got, ",") != strings.Join(s.want, ",") {
					t.Fatalf("step %d (%s): alerts %v, want %v", i, s.event.Type, got, s.want)
				}
			}
		})
	}
}

func TestPayload(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	alert := Alert{SessionID: "abcdefgh", Kind: attentionkind.Permission, Source: session.SourceAgent, Since: since, Title: "deploy"}
	var got struct {
		WebPush      int `json:"web_push"`
		Notification struct {
			Title, Body, Navigate, Tag string
		} `json:"notification"`
		WebMux struct {
			V         int    `json:"v"`
			SessionID string `json:"sessionId"`
			Kind      string `json:"kind"`
			Source    string `json:"source"`
			Since     time.Time
		} `json:"webmux"`
	}
	raw, err := payload(alert, "https://term.example.com", true)
	if err != nil || json.Unmarshal(raw, &got) != nil {
		t.Fatalf("payload %s: %v", raw, err)
	}
	n := got.Notification
	if got.WebPush != 8030 || n.Title != "deploy" || n.Body != "等待授权" || n.Navigate != "https://term.example.com/s/abcdefgh" || n.Tag != "webmux-abcdefgh" {
		t.Fatalf("notification %+v", got)
	}
	if got.WebMux.V != 1 || got.WebMux.SessionID != "abcdefgh" || got.WebMux.Kind != "permission" || got.WebMux.Source != "agent" || !got.WebMux.Since.Equal(since) {
		t.Fatalf("webmux %+v", got.WebMux)
	}

	for _, row := range []struct {
		name, title string
		include     bool
		want        string
	}{
		{"hidden", "deploy", false, "WebMux"},
		{"empty", "  ", true, "WebMux"},
		{"control characters", "a\x1b[31mb\x07\nc", true, "a[31mbc"},
		{"truncated", strings.Repeat("长", 100), true, strings.Repeat("长", 80)},
	} {
		raw, err := payload(Alert{SessionID: "abcdefgh", Kind: attentionkind.Done, Title: row.title}, "http://127.0.0.1:7681", row.include)
		if err != nil || json.Unmarshal(raw, &got) != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		if got.Notification.Title != row.want {
			t.Errorf("%s: title %q, want %q", row.name, got.Notification.Title, row.want)
		}
		if row.name == "hidden" && strings.Contains(string(raw), "deploy") {
			t.Errorf("hidden title leaked: %s", raw)
		}
	}
}

// An alert built from a notify signal carries none of the OSC body, cwd or
// command: those never enter Alert.
func TestPayloadOmitsTerminalContent(t *testing.T) {
	i := info("abcdefgh", attentionkind.Notify, session.SourceTerminal, time.Now())
	i.Cwd, i.Cmd, i.Preview = "/home/u/secret", []string{"make deploy"}, "构建失败: /home/u/secret"
	tracker := NewTracker(config.DefaultPushKinds())
	tracker.Observe(snapshot())
	alerts := tracker.Observe(updated(i))
	if len(alerts) != 1 {
		t.Fatalf("alerts %v", alerts)
	}
	raw, err := payload(alerts[0], "https://term.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret", "make deploy", "构建失败"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("payload leaks %q: %s", secret, raw)
		}
	}
}
