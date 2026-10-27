import { describe, expect, test } from "vitest";
import { emptySettings, settingsIntent } from "../src/settings-intent";
import { SettingsSnapshotSchema } from "../src/schemas";

describe("preference conflict replay", () => {
  test("replays only changed compound fields, including explicit resets", () => {
    const before = { ...emptySettings, preview: { lines: 10, skip: 1 } };
    const latest = { ...before, preview: { lines: 30, skip: 4 } };
    expect(
      settingsIntent(before, { preview: { lines: 20, skip: 1 } })(latest),
    ).toEqual({ preview: { lines: 20, skip: 4 } });
    expect(
      settingsIntent(before, { preview: { lines: 20, skip: 3 } })(latest),
    ).toEqual({ preview: { lines: 20, skip: 3 } });
    expect(settingsIntent(before, { preview: null })(latest)).toEqual({
      preview: null,
    });
    expect(
      settingsIntent(before, { preview: { lines: 20, skip: 1 } })(
        emptySettings,
      ),
    ).toEqual({ preview: { lines: 20, skip: 1 } });
  });
  test("preserves remote commands while applying local edits and deletes by ID", () => {
    const a = { id: "a", name: "", command: "echo a", enter: false };
    const b = { ...a, id: "b" };
    const c = { ...a, id: "c" };
    const before = { ...emptySettings, quickCommands: [a, b] };
    const latest = { ...before, quickCommands: [a, b, c] };
    expect(
      settingsIntent(before, { quickCommands: [{ ...a, name: "edited" }] })(
        latest,
      ).quickCommands,
    ).toEqual([{ ...a, name: "edited" }, c]);
    expect(
      settingsIntent(before, { quickCommands: [] })(latest).quickCommands,
    ).toEqual([]);
    expect(
      settingsIntent(before, { quickCommands: null })(latest).quickCommands,
    ).toBeNull();
  });
  test("preserves concurrently pinned worktrees", () => {
    const before = { ...emptySettings, homeWorktrees: { repo: ["a", "b"] } };
    const latest = {
      ...before,
      homeWorktrees: { repo: ["a", "b", "remote"], other: ["x"] },
    };
    expect(
      settingsIntent(before, { homeWorktrees: { repo: ["b", "new"] } })(latest)
        .homeWorktrees,
    ).toEqual({ repo: ["b", "new", "remote"], other: ["x"] });
    expect(
      settingsIntent(before, { homeWorktrees: {} })(latest).homeWorktrees,
    ).toEqual({ repo: ["remote"], other: ["x"] });
  });
  test("validates null, empty collections and bounded revisions", () => {
    const snapshot = { revision: 0, updatedAt: "", values: emptySettings };
    expect(SettingsSnapshotSchema.safeParse(snapshot).success).toBe(true);
    expect(
      SettingsSnapshotSchema.safeParse({
        ...snapshot,
        revision: Number.MAX_SAFE_INTEGER + 1,
      }).success,
    ).toBe(false);
    expect(
      SettingsSnapshotSchema.safeParse({
        ...snapshot,
        values: { ...emptySettings, quickCommands: [] },
      }).success,
    ).toBe(true);
    expect(
      SettingsSnapshotSchema.safeParse({
        ...snapshot,
        values: { ...emptySettings, fontSize: "14" },
      }).success,
    ).toBe(false);
  });
});
