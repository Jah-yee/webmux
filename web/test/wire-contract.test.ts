import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { z } from "zod";
import * as S from "../src/schemas";

const directory = fileURLToPath(
  new URL("../../internal/api/testdata/wire/", import.meta.url),
);
const groups: Array<[z.ZodType, string[]]> = [
  [S.CodeChangesSchema, ["code-changes"]],
  [S.CodeCheckSchema, ["code-check"]],
  [S.CodeContextSchema, ["code-context"]],
  [S.CodeDiffSchema, ["code-diff"]],
  [S.CodeFileSchema, ["code-file", "code-snapshot-file"]],
  [S.CodeListingSchema, ["code-files", "code-tree"]],
  [S.CodeLocationSchema, ["code-locate"]],
  [S.ConfigSchema, ["config"]],
  [S.DirectoriesSchema, ["fs-dirs-default", "fs-dirs-empty", "fs-dirs-prefix"]],
  [S.ErrorSchema, ["settings-conflict"]],
  [S.NoContentSchema, ["fs-mkdir", "launch-delete", "push-unsubscribe"]],
  [S.HealthSchema, ["health"]],
  [
    S.LaunchConfigSchema,
    ["launch-create-no-env", "launch-create", "launch-get", "launch-update"],
  ],
  [S.LaunchConfigsSchema, ["launch-list-empty", "launch-list"]],
  [
    S.PushStatusSchema,
    ["push-status-disabled", "push-status-quick-tunnel", "push-status"],
  ],
  [S.PushSubscriptionResultSchema, ["push-subscribe-gone", "push-subscribe"]],
  [S.PushTestResultSchema, ["push-test"]],
  [S.RepositoriesSchema, ["repositories-empty", "repositories"]],
  [S.BranchesSchema, ["repository-branches"]],
  [
    S.RepositorySchema,
    [
      "repository-register-again",
      "repository-register",
      "repository-worktrees",
    ],
  ],
  [
    S.NoContentSchema,
    ["repository-unregister", "session-delete-close", "session-delete-remove"],
  ],
  [
    S.SessionInfoSchema,
    [
      "session-create-exiting",
      "session-create-from-config",
      "session-create-workspace",
      "session-create",
      "session-get",
      "ws-terminal-state-exited",
      "ws-terminal-state",
    ],
  ],
  [z.array(S.SessionInfoSchema), ["sessions-list-empty", "sessions-list"]],
  [
    S.SettingsSnapshotSchema,
    ["settings-get", "settings-patch", "settings-reset"],
  ],
  [S.SettingsEventSchema, ["ws-settings-snapshot", "ws-settings-update"]],
  [
    S.TunnelSettingsSchema,
    ["tunnel-enabled", "tunnel-get", "tunnel-reset", "tunnel-settings"],
  ],
  [S.UploadSchema, ["upload"]],
  [S.WorktreeSchema, ["worktree-create"]],
  [S.RemovalPreviewSchema, ["worktree-removal-preview"]],
  [
    S.SessionEventSchema,
    [
      "ws-sessions-created",
      "ws-sessions-removed",
      "ws-sessions-snapshot-empty",
      "ws-sessions-snapshot",
      "ws-sessions-updated",
    ],
  ],
  [
    S.ControlStateSchema,
    [
      "ws-terminal-control-claimed",
      "ws-terminal-control-initial",
      "ws-terminal-control-observer",
      "ws-terminal-control-uncontrolled",
    ],
  ],
  [S.ExitSchema, ["ws-terminal-exit"]],
  [
    S.StreamPositionSchema,
    ["ws-terminal-position-live", "ws-terminal-position-reset"],
  ],
  [
    S.SubmissionStatusSchema,
    [
      "ws-terminal-submission-failed",
      "ws-terminal-submission-missing",
      "ws-terminal-submission-received",
      "ws-terminal-submission-written",
    ],
  ],
];
const snapshotSchemas = new Map<string, z.ZodType>();
for (const [schema, names] of groups)
  for (const name of names) snapshotSchemas.set(`${name}.json`, schema);

// This endpoint belongs to the native local client, not the browser contract.
const nonWebSnapshots = new Set(["local-status-tunnel-settings.json"]);

function normalize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(normalize);
  if (value && typeof value === "object")
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, normalize(item)]),
    );
  if (typeof value !== "string") return value;
  const id = /^<id#(\d+)>$/.exec(value);
  if (id?.[1]) {
    const suffix = id[1].replace(/\d/g, (digit) =>
      "abcdefghij".charAt(Number(digit)),
    );
    return suffix.padStart(8, "a");
  }
  if (/^<generation#\d+>$/.test(value)) return "0123456789abcdef";
  if (value === "<time>") return "2026-01-02T03:04:05Z";
  return value.replaceAll("<tmp>", "/tmp");
}

const files = readdirSync(directory).filter((name) => name.endsWith(".json"));
describe("Go wire snapshots", () => {
  it("has an explicit schema decision for every snapshot", () => {
    for (const file of files)
      expect(
        snapshotSchemas.has(file) ||
          file.startsWith("error-") ||
          file.startsWith("tunnel-error-") ||
          nonWebSnapshots.has(file),
        file,
      ).toBe(true);
  });

  for (const file of files) {
    if (nonWebSnapshots.has(file)) continue;
    it(`${file} matches the frontend schema`, () => {
      const snapshot = JSON.parse(readFileSync(directory + file, "utf8")) as {
        status?: number;
        body: unknown;
      };
      const schema =
        file.startsWith("error-") || file.startsWith("tunnel-error-")
          ? S.ErrorSchema
          : snapshotSchemas.get(file);
      expect(schema, file).toBeDefined();
      const body =
        snapshot.status === 204 && snapshot.body === null
          ? undefined
          : normalize(snapshot.body);
      expect(schema!.parse(body), file).toEqual(body);
    });
  }

  it("keeps document and change node IDs required, not merely accepted", () => {
    for (const schema of [S.CodeDocumentSchema, S.CodeChangeSchema])
      expect(schema.shape.nodeId.safeParse(undefined).success).toBe(false);
    for (const file of ["code-file.json", "code-changes.json"]) {
      const snapshot = JSON.parse(readFileSync(directory + file, "utf8")) as {
        body: { document?: object; entries?: object[] };
      };
      for (const value of snapshot.body.entries ?? [snapshot.body.document])
        expect(value).toHaveProperty("nodeId");
    }
  });
});
