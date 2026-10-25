package archtest

import (
	"strings"
	"testing"
)

// webmux is the layer manifest. Every package under internal/ appears here;
// see docs/architecture.md for what each layer may depend on. Exceptions
// are listed with their reason and are visible in review.
var webmux = manifest{
	module: "github.com/rainchan/webmux",
	layers: map[string]layer{
		// HTTP and WebSocket transport.
		"internal/api":  controller,
		"internal/hub":  controller,
		"internal/wire": controller,

		// Business rules, state, filesystem and processes.
		"internal/session":   service,
		"internal/workspace": service,
		"internal/launch":    service,
		"internal/push":      service,
		"internal/codeview":  service,
		"internal/files":     service,

		// Wire contracts and infrastructure every layer may use.
		"internal/contract":      shared,
		"internal/config":        shared,
		"internal/runtime":       shared,
		"internal/statefile":     shared,
		"internal/fetch":         shared,
		"internal/pathutil":      shared,
		"internal/attentionkind": shared,

		// Programs that run outside the server and talk to it over the wire.
		"internal/client":       client,
		"internal/localclient":  client,
		"internal/attach":       client,
		"internal/sessioncmd":   client,
		"internal/tray":         client,
		"internal/tray/forward": client,
		"internal/tray/winui":   client,
		"internal/wslclient":    client,
		"internal/tunnel":       client,
		"internal/service":      client,

		"internal/archtest": testOnly,
	},
	outbound: map[string]string{
		"internal/push": "delivers Web Push messages to browser push services",
	},
	pure: map[string]string{
		"internal/push.Validate": "validates subscription strings without service state or I/O, before the controller checks Origin",
	},
	contract:  "internal/contract",
	wireScope: []string{"internal/api", "internal/wire", "internal/hub"},
	// The controller's JSON boundary: their values are checked where they
	// are called.
	sinks: map[string]sink{
		"internal/api.reply":          {arg: 2, typeArg: -1},
		"internal/api.decode":         {arg: 2, typeArg: -1},
		"internal/api.decodeOptional": {arg: 2, typeArg: -1},
		"internal/api.serveCode":      {typeArg: 0},
		"internal/hub.jsonFrame":      {arg: 1, typeArg: -1},
	},
	encoders: map[string]string{
		"internal/push.payload":     "Web Push message body, read by the service worker",
		"internal/push.testPayload": "Web Push test message body",
		"internal/push.vapidHeader": "VAPID JWT header and claims sent to push services",
		"internal/codeview.version": "content digest of a snapshot, never sent as JSON",
	},
}

func TestArchitecture(t *testing.T) {
	p, err := load("../..", webmux)
	if err != nil {
		t.Fatal(err)
	}
	if violations := p.check(); len(violations) > 0 {
		lines := make([]string, len(violations))
		for i, v := range violations {
			lines[i] = v.String()
		}
		t.Errorf("%d architecture violations (docs/architecture.md):\n%s", len(violations), strings.Join(lines, "\n"))
	}
}
