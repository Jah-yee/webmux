package api

import (
	"reflect"
	"testing"
)

func TestWireNormalizerSchemaPaths(t *testing.T) {
	const stamp = "2026-01-02T03:04:05Z"
	const id = "random-session-id"
	for _, row := range []struct {
		name, snapshot string
		input, want    any
	}{
		{
			name: "session metadata only", snapshot: "session-get",
			input: map[string]any{
				"id": id, "generation": "random-generation", "createdAt": stamp,
				"lastActiveAt": "0001-01-01T00:00:00Z", "cwd": "/fixture/repo/sub",
				"preview": id + " " + stamp, "title": stamp,
				"env": map[string]any{"id": id, "createdAt": stamp},
			},
			want: map[string]any{
				"id": "<id#1>", "generation": "<generation#1>", "createdAt": "<time>",
				"lastActiveAt": "<zero-time>", "cwd": "<repo>/sub",
				"preview": id + " " + stamp, "title": stamp,
				"env": map[string]any{"id": id, "createdAt": stamp},
			},
		},
		{
			name: "code content is literal", snapshot: "code-file",
			input: map[string]any{
				"checkedAt": stamp, "snapshotId": "random-snapshot",
				"document": map[string]any{
					"absolutePath": "/fixture/repo/file", "contentVersion": "fixed-content-digest",
					"text": stamp + " " + id + " /fixture/repo/webmux-upload-123",
					"oid":  "fixed-git-object", "reason": id,
				},
			},
			want: map[string]any{
				"checkedAt": "<time>", "snapshotId": "<snapshotId#1>",
				"document": map[string]any{
					"absolutePath": "<repo>/file", "contentVersion": "fixed-content-digest",
					"text": stamp + " " + id + " /fixture/repo/webmux-upload-123",
					"oid":  "fixed-git-object", "reason": id,
				},
			},
		},
		{
			name: "filesystem error path only", snapshot: "error-400-fs-dirs",
			input: map[string]any{"error": "open /fixture/repo/missing: " + id + " " + stamp},
			want:  map[string]any{"error": "open <repo>/missing: " + id + " " + stamp},
		},
		{
			name: "path prefixes require boundaries", snapshot: "error-400-fs-dirs",
			input: map[string]any{"error": "open /fixture/repo-other and x/fixture/repo: " + id},
			want:  map[string]any{"error": "open /fixture/repo-other and x/fixture/repo: " + id},
		},
		{
			name: "unrelated errors remain literal", snapshot: "error-400-code-path",
			input: map[string]any{"error": stamp + " " + id + " /fixture/repo"},
			want:  map[string]any{"error": stamp + " " + id + " /fixture/repo"},
		},
		{
			name: "unknown schemas remain literal", snapshot: "unrecognized-endpoint",
			input: map[string]any{"id": id, "createdAt": stamp, "path": "/fixture/repo", "body": stamp},
			want:  map[string]any{"id": id, "createdAt": stamp, "path": "/fixture/repo", "body": stamp},
		},
		{
			name: "upload generated directory not filename", snapshot: "upload",
			input: map[string]any{"path": "/fixture/tmp/webmux-upload-123/webmux-upload-456"},
			want:  map[string]any{"path": "<tmp>/webmux-upload-<n>/webmux-upload-456"},
		},
		{
			name: "business branch values stay literal", snapshot: "repository-branches",
			input: map[string]any{
				"defaultBase": "HEAD", "defaultParent": "/fixture/repo.worktrees",
				"branches": []any{map[string]any{"head": "fixed-git-object", "name": stamp, "worktreePaths": []any{"/fixture/repo"}}},
			},
			want: map[string]any{
				"defaultBase": "HEAD", "defaultParent": "<repo>.worktrees",
				"branches": []any{map[string]any{"head": "fixed-git-object", "name": stamp, "worktreePaths": []any{"<repo>"}}},
			},
		},
		{
			name: "check states are not identifiers", snapshot: "code-check",
			input: map[string]any{
				"changes": "unchanged", "checkedAt": stamp,
				"snapshots":   map[string]any{"random-snapshot": "changed"},
				"directories": map[string]any{"node": "unavailable"},
			},
			want: map[string]any{
				"changes": "unchanged", "checkedAt": "<time>",
				"snapshots":   map[string]any{"<snapshotId#1>": "changed"},
				"directories": map[string]any{"node": "unavailable"},
			},
		},
		{
			name: "change hashes remain deterministic", snapshot: "code-changes",
			input: map[string]any{
				"checkedAt": stamp, "contentVersion": "fixed-changes-digest",
				"entries": []any{map[string]any{"changeId": "fixed-change-digest", "oldOid": "fixed-git-object", "newOid": "0000000000000000000000000000000000000000"}},
			},
			want: map[string]any{
				"checkedAt": "<time>", "contentVersion": "fixed-changes-digest",
				"entries": []any{map[string]any{"changeId": "fixed-change-digest", "oldOid": "fixed-git-object", "newOid": "0000000000000000000000000000000000000000"}},
			},
		},
		{
			name: "hostname only at config root", snapshot: "config",
			input: map[string]any{"hostname": "fixture-host", "tunnel": map[string]any{"hostname": "fixture-host"}},
			want:  map[string]any{"hostname": "<hostname>", "tunnel": map[string]any{"hostname": "fixture-host"}},
		},
		{
			name: "caller chosen submission ID", snapshot: "ws-terminal-submission-failed",
			input: map[string]any{"id": id, "error": "generation", "state": "failed"},
			want:  map[string]any{"id": id, "error": "generation", "state": "failed"},
		},
		{
			name: "caller chosen controller ID", snapshot: "ws-terminal-control-initial",
			input: map[string]any{"controller": map[string]any{"clientId": id, "label": stamp, "since": stamp}},
			want:  map[string]any{"controller": map[string]any{"clientId": id, "label": stamp, "since": "<time>"}},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			n := newWireNormalizer()
			n.path("/fixture/repo", "<repo>")
			n.path("/fixture/repo.worktrees", "<repo>.worktrees")
			n.path("/fixture/tmp", "<tmp>")
			n.hostname = "fixture-host"
			// IDs already observed on other responses must not rewrite content.
			n.id("id", id)
			if got := n.normalize(row.snapshot, row.input); !reflect.DeepEqual(got, row.want) {
				t.Fatalf("normalized payload:\n got %#v\nwant %#v", got, row.want)
			}
		})
	}
}

func TestWireNormalizerDistinguishesContent(t *testing.T) {
	for _, field := range []string{"body", "preview", "error", "text"} {
		t.Run(field, func(t *testing.T) {
			n := newWireNormalizer()
			first := map[string]any{
				"id": "first-id", "createdAt": "2026-01-02T03:04:05Z",
				field: "first-id 2026-01-02T03:04:05Z",
			}
			second := map[string]any{
				"id": "second-id", "createdAt": "2027-02-03T04:05:06Z",
				field: "second-id 2027-02-03T04:05:06Z",
			}
			a := n.normalize("session-get", first).(map[string]any)
			b := n.normalize("session-get", second).(map[string]any)
			if a[field] != first[field] || b[field] != second[field] || a[field] == b[field] {
				t.Fatalf("content lost its distinction: %#v %#v", a, b)
			}
			if a["createdAt"] != "<time>" || b["createdAt"] != "<time>" || a["id"] == first["id"] || b["id"] == second["id"] {
				t.Fatalf("dynamic metadata was not normalized: %#v %#v", a, b)
			}
		})
	}
}
