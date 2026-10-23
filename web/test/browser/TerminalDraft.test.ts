import { expect, test } from "vitest";
import { createTerminalDrafts } from "../../src/terminal-draft.svelte";
import {
  createDraftStorage,
  scratchEntriesLimit,
  scratchEntryLimit,
  scratchStorageLimit,
} from "../../src/draftStorage";

test("draft edits preserve Unicode, selections and session isolation across mode switches", () => {
  const drafts = createTerminalDrafts();
  const a = drafts.get("session-a");
  const b = drafts.get("session-b");
  drafts.setEnabled(a, true);
  drafts.edit(a, "中文🙂 replace 尾");
  drafts.select(a, 5, 12);
  expect(drafts.insert(a, "引用")).toBe("draft");
  expect(a.text).toBe("中文🙂 引用 尾");
  expect(a.selectionStart).toBe(7);
  expect(a.selectionEnd).toBe(7);
  drafts.setEnabled(a, false);
  drafts.setEnabled(b, true);
  drafts.edit(b, "another session");
  drafts.setEnabled(a, true);
  expect(drafts.get("session-a").text).toBe("中文🙂 引用 尾");
  expect(b.text).toBe("another session");
  drafts.clear(a);
  expect(a.text).toBe("");
  expect(a.enabled).toBe(true);
  expect(b.text).toBe("another session");
});

test("async input cannot downgrade after draft mode was enabled and closed", () => {
  const drafts = createTerminalDrafts();
  const entry = drafts.get("session-a");
  const directOperation = drafts.begin(entry);
  expect(drafts.destination(entry, directOperation)).toBe("terminal");
  drafts.setEnabled(entry, true);
  const draftOperation = drafts.begin(entry);
  drafts.setEnabled(entry, false);
  expect(drafts.destination(entry, directOperation)).toBe("draft");
  expect(drafts.destination(entry, draftOperation)).toBe("draft");
  drafts.insert(entry, "uploaded-path ", directOperation);
  drafts.insert(entry, "clipboard", draftOperation);
  expect(entry.text).toBe("uploaded-path clipboard");
  expect(entry.enabled).toBe(false);
  expect(entry.updatedWhileHidden).toBe(true);
  // A new action after mode is closed is direct; the prior operations stay pinned.
  expect(drafts.destination(entry, drafts.begin(entry))).toBe("terminal");
  drafts.setEnabled(entry, true);
  expect(entry.updatedWhileHidden).toBe(false);
  expect(entry.text).toBe("uploaded-path clipboard");
});

test("late operations cannot resurrect removed drafts or target another session", () => {
  const drafts = createTerminalDrafts();
  const old = drafts.get("session-a");
  drafts.setEnabled(old, true);
  const pending = drafts.begin(old);
  drafts.remove("session-a");
  const fresh = drafts.get("session-a");
  const other = drafts.get("session-b");
  drafts.setEnabled(fresh, true);
  drafts.setEnabled(other, true);
  expect(drafts.insert(old, "late", pending)).toBe("failed");
  expect(drafts.insert(fresh, "late", pending)).toBe("failed");
  expect(drafts.insert(other, "wrong-session", pending)).toBe("failed");
  expect(old.text).toBe("");
  expect(fresh.text).toBe("");
  expect(other.text).toBe("");
});

test("authoritative reconciliation removes only absent sessions and authentication reset invalidates pending input", () => {
  const drafts = createTerminalDrafts();
  const retained = drafts.get("retained");
  const removed = drafts.get("removed");
  drafts.setEnabled(retained, true);
  drafts.edit(retained, "still useful after exit");
  const pending = drafts.begin(retained);
  drafts.reconcile(["retained"]);
  expect(drafts.valid(retained)).toBe(true);
  expect(drafts.valid(removed)).toBe(false);
  expect(retained.text).toBe("still useful after exit");
  drafts.clearAll();
  expect(drafts.insert(retained, "late", pending)).toBe("failed");
  const newAuthentication = drafts.get("retained");
  expect(newAuthentication.text).toBe("");
  expect(drafts.destination(newAuthentication, pending)).toBe("failed");
});

test("external insert appends without an editing position and clamps stale positions", () => {
  const drafts = createTerminalDrafts();
  const entry = drafts.get("a");
  drafts.setEnabled(entry, true);
  drafts.edit(entry, "prefix ");
  drafts.insert(entry, "file:12 ");
  expect(entry.text).toBe("prefix file:12 ");
  drafts.edit(entry, "x", 100, 200);
  drafts.insert(entry, "🙂");
  expect(entry.text).toBe("x🙂");
  expect(entry.selectionStart).toBe(3);
});

function memoryDrafts() {
  const data = new Map<string, string>();
  const memory = {
    get length() {
      return data.size;
    },
    clear: () => data.clear(),
    getItem: (key: string) => data.get(key) ?? null,
    key: (index: number) => [...data.keys()][index] ?? null,
    removeItem: (key: string) => void data.delete(key),
    setItem: (key: string, value: string) => void data.set(key, value),
  } satisfies Storage;
  return { data, storage: () => createDraftStorage(memory, () => 5000) };
}

test("saved drafts are restored visibly, bound to their run, and kept across auth loss", () => {
  const saved = memoryDrafts();
  const drafts = createTerminalDrafts({
    storage: saved.storage(),
    now: () => 5000,
  });
  const entry = drafts.get("aaaaaaaa");
  drafts.setEnabled(entry, true);
  drafts.edit(entry, "引用 /tmp/upload.png\nplease review");
  drafts.bindGeneration(entry, "0123456789abcdef");
  drafts.clearAll();

  const reloaded = createTerminalDrafts({ storage: saved.storage() });
  const restored = reloaded.get("aaaaaaaa");
  expect(restored.text).toBe("引用 /tmp/upload.png\nplease review");
  expect(restored.enabled).toBe(true);
  expect(restored.restoredAt).toBe(5000);
  expect(restored.generation).toBe("0123456789abcdef");
  expect(reloaded.destination(restored)).toBe("draft");

  reloaded.reconcile(["bbbbbbbb"]);
  expect(saved.data.size).toBe(0);
});

test("an unconfirmed submission locks its text until the server settles it", () => {
  const saved = memoryDrafts();
  const drafts = createTerminalDrafts({ storage: saved.storage() });
  const entry = drafts.get("aaaaaaaa");
  drafts.setEnabled(entry, true);
  drafts.edit(entry, "run tests");
  expect(drafts.beginSubmission(entry, "s-00000001")).toBe(true);
  expect(drafts.beginSubmission(entry, "s-00000002")).toBe(false);
  drafts.edit(entry, "changed");
  expect(drafts.insert(entry, "more")).toBe("failed");
  expect(entry.text).toBe("run tests");
  drafts.updateSubmission(entry, "s-00000001", "received");

  // After a reload nothing after the save is known: the record is "unknown".
  const reloaded = createTerminalDrafts({ storage: saved.storage() });
  const restored = reloaded.get("aaaaaaaa");
  expect(restored.submission).toMatchObject({
    id: "s-00000001",
    state: "unknown",
  });
  reloaded.updateSubmission(restored, "s-other-0001", "written");
  expect(restored.text).toBe("run tests");
  reloaded.updateSubmission(restored, "s-00000001", "missing");
  expect(restored.submission).toBeNull();
  expect(restored.text).toBe("run tests");

  reloaded.beginSubmission(restored, "s-00000003");
  reloaded.updateSubmission(restored, "s-00000003", "partial");
  expect(restored.submission?.state).toBe("partial");
  reloaded.releaseSubmission(restored);
  reloaded.edit(restored, "run tests again");
  reloaded.beginSubmission(restored, "s-00000004");
  reloaded.updateSubmission(restored, "s-00000004", "written");
  expect(restored.text).toBe("");
  expect(restored.enabled).toBe(false);
  expect(restored.submission).toBeNull();
  expect(saved.data.size).toBe(0);
});

test("scratch stays separate from the draft and survives editing, reordering and removal", () => {
  const saved = memoryDrafts();
  const drafts = createTerminalDrafts({
    storage: saved.storage(),
    now: () => 5000,
  });
  const entry = drafts.get("aaaaaaaa");
  drafts.setEnabled(entry, true);
  drafts.edit(entry, "current");
  expect(drafts.addScratch(entry, "first")).toBe("added");
  expect(drafts.addScratch(entry, "second")).toBe("added");
  expect(drafts.addScratch(entry, "")).toBe("empty");
  expect(entry.text).toBe("current");
  expect(entry.scratch.map((item) => item.text)).toEqual(["first", "second"]);
  const [first, second] = entry.scratch;
  expect(drafts.editScratch(entry, second.id, "second edited")).toBe(true);
  expect(drafts.editScratch(entry, "missing-id", "text")).toBe(false);
  expect(drafts.moveScratch(entry, second.id, -1)).toBe(true);
  expect(drafts.moveScratch(entry, second.id, -1)).toBe(false);
  expect(entry.scratch.map((item) => item.text)).toEqual([
    "second edited",
    "first",
  ]);
  expect(entry.scratch[0].id).toBe(second.id);
  drafts.removeScratch(entry, first.id);
  expect(entry.scratch.map((item) => item.text)).toEqual(["second edited"]);
  expect(entry.text).toBe("current");

  const reloaded = createTerminalDrafts({ storage: saved.storage() });
  const restored = reloaded.get("aaaaaaaa");
  expect(restored.scratch.map((item) => item.text)).toEqual(["second edited"]);
  expect(restored.text).toBe("current");
});

test("taking scratch appends or replaces while preserving the displaced draft", () => {
  const drafts = createTerminalDrafts();
  const entry = drafts.get("a");
  drafts.setEnabled(entry, true);
  drafts.edit(entry, "current");
  drafts.addScratch(entry, "next");
  const appended = entry.scratch[0];
  expect(drafts.takeScratch(entry, appended.id, "append")).toBe("taken");
  expect(entry.text).toBe("current\nnext");
  expect(entry.scratch).toHaveLength(0);

  drafts.edit(entry, "current2");
  drafts.addScratch(entry, "next2");
  const replaced = entry.scratch[0];
  expect(drafts.takeScratch(entry, replaced.id, "replace")).toBe("taken");
  expect(entry.text).toBe("next2");
  expect(entry.scratch.map((item) => item.text)).toEqual(["current2"]);
  expect(entry.enabled).toBe(true);
  expect(entry.selectionStart).toBe("next2".length);
});

test("appending scratch does not add a second newline", () => {
  const drafts = createTerminalDrafts();
  const entry = drafts.get("a");
  drafts.setEnabled(entry, true);
  drafts.edit(entry, "current\n");
  drafts.addScratch(entry, "next");
  const target = entry.scratch[0];
  expect(drafts.takeScratch(entry, target.id, "append")).toBe("taken");
  expect(entry.text).toBe("current\nnext");
});

test("a failed replace keeps the target note and the current draft", () => {
  const drafts = createTerminalDrafts();
  const entry = drafts.get("a");
  drafts.setEnabled(entry, true);
  const large = "x".repeat(scratchEntryLimit + 1);
  drafts.edit(entry, large);
  drafts.addScratch(entry, "next");
  const target = entry.scratch[0];
  expect(drafts.takeScratch(entry, target.id, "replace")).toBe("too-long");
  expect(entry.text).toBe(large);
  expect(entry.scratch.map((item) => item.text)).toEqual(["next"]);
  expect(drafts.takeScratch(entry, target.id, "append")).toBe("taken");
  expect(entry.text).toBe(`${large}\nnext`);
});

test("stashing moves the draft into scratch and clears only the draft", () => {
  const drafts = createTerminalDrafts();
  const entry = drafts.get("a");
  drafts.setEnabled(entry, true);
  drafts.edit(entry, "current");
  expect(drafts.stash(entry)).toBe("added");
  expect(entry.text).toBe("");
  expect(entry.scratch.map((item) => item.text)).toEqual(["current"]);
  expect(drafts.stash(entry)).toBe("empty");
});

test("an unconfirmed submission blocks stashing and taking scratch", () => {
  const drafts = createTerminalDrafts();
  const entry = drafts.get("a");
  drafts.setEnabled(entry, true);
  drafts.edit(entry, "pending");
  drafts.addScratch(entry, "later");
  const later = entry.scratch[0];
  expect(drafts.beginSubmission(entry, "s-00000001")).toBe(true);
  expect(drafts.stash(entry)).toBe("locked");
  expect(drafts.takeScratch(entry, later.id, "replace")).toBe("locked");
  expect(entry.text).toBe("pending");
  expect(entry.scratch).toHaveLength(1);
  drafts.releaseSubmission(entry);
  expect(drafts.takeScratch(entry, later.id, "replace")).toBe("taken");
  expect(entry.text).toBe("later");
});

test("scratch updates while hidden are signalled and survive authentication reset", () => {
  const saved = memoryDrafts();
  const drafts = createTerminalDrafts({
    storage: saved.storage(),
    now: () => 5000,
  });
  const entry = drafts.get("aaaaaaaa");
  expect(drafts.addScratch(entry, "hidden")).toBe("added");
  expect(entry.scratchUpdatedWhileHidden).toBe(true);
  drafts.setEnabled(entry, true);
  expect(entry.scratchUpdatedWhileHidden).toBe(false);
  drafts.setEnabled(entry, false);
  expect(drafts.editScratch(entry, entry.scratch[0].id, "hidden edited")).toBe(
    true,
  );
  expect(entry.scratchUpdatedWhileHidden).toBe(true);
  drafts.clearAll();
  const restored = drafts.get("aaaaaaaa");
  expect(restored.scratch.map((item) => item.text)).toEqual(["hidden edited"]);
  expect(restored.scratchUpdatedWhileHidden).toBe(false);
  expect(restored.text).toBe("");
});

test("scratch enforces entry, size and total limits in the model", () => {
  const drafts = createTerminalDrafts();
  const many = drafts.get("many");
  for (let index = 0; index < scratchEntriesLimit; index++)
    expect(drafts.addScratch(many, String(index))).toBe("added");
  expect(drafts.addScratch(many, "overflow")).toBe("full");

  const long = drafts.get("long");
  expect(drafts.addScratch(long, "x".repeat(scratchEntryLimit + 1))).toBe(
    "too-long",
  );

  const total = drafts.get("total");
  const chunk = "x".repeat(scratchEntryLimit);
  for (let index = 0; index < scratchStorageLimit / scratchEntryLimit; index++)
    expect(drafts.addScratch(total, chunk)).toBe("added");
  expect(drafts.addScratch(total, "x")).toBe("over-total");
  expect(drafts.editScratch(total, total.scratch[0].id, chunk + "x")).toBe(
    false,
  );
});

test("taking scratch never writes another session or the terminal", () => {
  const drafts = createTerminalDrafts();
  const a = drafts.get("a");
  const b = drafts.get("b");
  drafts.setEnabled(a, true);
  drafts.addScratch(a, "a note");
  drafts.insert(a, "uploaded");
  expect(a.text).toBe("uploaded");
  expect(a.scratch.map((item) => item.text)).toEqual(["a note"]);
  expect(b.scratch).toHaveLength(0);
  expect(b.text).toBe("");

  drafts.remove("a");
  expect(drafts.get("a").scratch).toHaveLength(0);
  expect(b.scratch).toHaveLength(0);
});

test("scratch saved for an earlier run is kept and marked by generation", () => {
  const saved = memoryDrafts();
  const drafts = createTerminalDrafts({
    storage: saved.storage(),
    now: () => 5000,
  });
  const entry = drafts.get("aaaaaaaa");
  drafts.bindGeneration(entry, "0123456789abcdef");
  drafts.addScratch(entry, "old run");
  expect(entry.scratchGeneration).toBe("0123456789abcdef");
  drafts.clearAll();

  const reloaded = createTerminalDrafts({ storage: saved.storage() });
  const restored = reloaded.get("aaaaaaaa");
  expect(restored.scratchGeneration).toBe("0123456789abcdef");
  reloaded.bindGeneration(restored, "fedcba9876543210");
  expect(restored.scratchGeneration).toBe("0123456789abcdef");
  expect(restored.scratch.map((item) => item.text)).toEqual(["old run"]);
  expect(restored.submission).toBeNull();
});

test("a draft written for another run keeps its text but forgets its submission", () => {
  const drafts = createTerminalDrafts({ storage: memoryDrafts().storage() });
  const entry = drafts.get("aaaaaaaa");
  drafts.bindGeneration(entry, "0123456789abcdef");
  drafts.edit(entry, "text");
  drafts.beginSubmission(entry, "s-00000001");
  drafts.bindGeneration(entry, "fedcba9876543210");
  expect(entry.submission).toBeNull();
  expect(entry.text).toBe("text");
  expect(entry.generation).toBe("fedcba9876543210");
});
