package push

import (
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"github.com/rainchan/webmux/internal/attentionkind"
	"github.com/rainchan/webmux/internal/session"
)

// Alert is one attention signal worth a notification. It carries no cwd,
// command, preview or terminal text.
type Alert struct {
	SessionID string
	Kind      attentionkind.Kind
	Source    session.AttentionSource
	Since     time.Time
	Title     string
}

// Notification text shared with the service worker fallback in
// web/public/sw.js. TestServiceWorkerFallback keeps both copies in sync.
const (
	fallbackTitle = "WebMux"
	fallbackBody  = "有会话需要关注"
	testTitle     = "WebMux 测试通知"
	testBody      = "若看到此消息，说明推送已生效"
)

// kindBodies is the second line of a notification: the short label the session
// list also shows, then what the user can do about it.
var kindBodies = map[attentionkind.Kind]string{
	attentionkind.Question:   "需要回答 · Agent 正在等待你的输入",
	attentionkind.Permission: "等待授权 · Agent 请求执行工具操作",
	attentionkind.Error:      "出错停止 · 点击查看错误原因",
	attentionkind.Done:       "本轮完成 · 等待你的下一步",
	attentionkind.Notify:     "有通知 · 程序发来了提醒",
	attentionkind.Bell:       "响铃提醒 · 点击查看终端",
}

// urgency asks push services to wake a sleeping device only for signals that
// block the agent on the user.
func urgency(kind attentionkind.Kind) string {
	switch kind {
	case attentionkind.Question, attentionkind.Permission, attentionkind.Error:
		return "high"
	}
	return "normal"
}

const maxTitle = 80

// cleanTitle drops control characters and limits the title to maxTitle runes.
func cleanTitle(title string) string {
	title = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, title))
	if runes := []rune(title); len(runes) > maxTitle {
		title = string(runes[:maxTitle])
	}
	return title
}

type notificationPayload struct {
	WebPush      int          `json:"web_push"`
	Notification notification `json:"notification"`
	WebMux       any          `json:"webmux"`
}

type notification struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Navigate string `json:"navigate"`
	Tag      string `json:"tag"`
	Silent   bool   `json:"silent"`
}

type alertMeta struct {
	V         int                     `json:"v"`
	SessionID string                  `json:"sessionId"`
	Kind      attentionkind.Kind      `json:"kind"`
	Source    session.AttentionSource `json:"source"`
	Since     time.Time               `json:"since"`
}

// payload is the JSON push message: Declarative Web Push fields (RFC 8030
// extension, iOS 18.4+) that sw.js also reads, plus WebMux metadata.
func payload(a Alert, origin string, includeTitle bool) ([]byte, error) {
	title := ""
	if includeTitle {
		title = cleanTitle(a.Title)
	}
	if title == "" {
		title = fallbackTitle
	}
	body, ok := kindBodies[a.Kind]
	if !ok {
		body = fallbackBody
	}
	return json.Marshal(notificationPayload{
		WebPush: 8030,
		Notification: notification{
			Title:    title,
			Body:     body,
			Navigate: origin + "/s/" + a.SessionID,
			Tag:      "webmux-" + a.SessionID,
		},
		WebMux: alertMeta{V: 1, SessionID: a.SessionID, Kind: a.Kind, Source: a.Source, Since: a.Since},
	})
}

func testPayload(origin string) ([]byte, error) {
	return json.Marshal(notificationPayload{
		WebPush:      8030,
		Notification: notification{Title: testTitle, Body: testBody, Navigate: origin + "/", Tag: "webmux-test"},
		WebMux:       map[string]any{"v": 1, "test": true},
	})
}

type attentionKey struct {
	kind  attentionkind.Kind
	since int64 // UnixNano; == on time.Time also compares monotonic readings
}

// Tracker turns the session list stream into alerts. The key (kind, since)
// changes only when a signal is newly raised or upgraded, so repeated reports,
// spinner output and reconnect snapshots never alert twice.
type Tracker struct {
	kinds    map[attentionkind.Kind]bool
	seen     map[string]attentionKey
	baseline bool
}

func NewTracker(kinds []string) *Tracker {
	t := &Tracker{kinds: map[attentionkind.Kind]bool{}, seen: map[string]attentionKey{}}
	for _, kind := range kinds {
		t.kinds[attentionkind.Kind(kind)] = true
	}
	return t
}

// Observe applies one list event. The first snapshot only records a baseline;
// later snapshots, delivered after a resubscribe, are compared like updates so
// signals raised while disconnected alert once.
func (t *Tracker) Observe(e session.Event) []Alert {
	switch e.Type {
	case "snapshot":
		if !t.baseline {
			t.baseline = true
			for _, info := range e.Sessions {
				t.seen[info.ID] = keyOf(info)
			}
			return nil
		}
		present := make(map[string]bool, len(e.Sessions))
		var alerts []Alert
		for i := range e.Sessions {
			present[e.Sessions[i].ID] = true
			if a, ok := t.update(&e.Sessions[i]); ok {
				alerts = append(alerts, a)
			}
		}
		for id := range t.seen {
			if !present[id] {
				delete(t.seen, id)
			}
		}
		return alerts
	case "created", "updated":
		if e.Session == nil {
			return nil
		}
		if a, ok := t.update(e.Session); ok {
			return []Alert{a}
		}
	case "removed":
		delete(t.seen, e.ID)
	}
	return nil
}

func (t *Tracker) update(info *session.Info) (Alert, bool) {
	key := keyOf(*info)
	previous, known := t.seen[info.ID]
	t.seen[info.ID] = key
	if info.Attention == nil || known && previous == key || !t.kinds[key.kind] {
		return Alert{}, false
	}
	return Alert{SessionID: info.ID, Kind: info.Attention.Kind, Source: info.Attention.Source, Since: info.Attention.Since, Title: info.Title}, true
}

func keyOf(info session.Info) attentionKey {
	if info.Attention == nil {
		return attentionKey{}
	}
	return attentionKey{kind: info.Attention.Kind, since: info.Attention.Since.UnixNano()}
}
