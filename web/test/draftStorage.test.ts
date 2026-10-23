import { describe, expect, test } from "vitest";
import {
  createDraftStorage,
  draftRetention,
  draftStorageLimit,
  scratchEntriesLimit,
  scratchEntryLimit,
  scratchStorageLimit,
} from "../src/draftStorage";
import type { StoredDraft, StoredScratch } from "../src/schemas";

class MemoryStorage implements Storage {
  data = new Map<string, string>();
  get length() {
    return this.data.size;
  }
  clear() {
    this.data.clear();
  }
  getItem(key: string) {
    return this.data.get(key) ?? null;
  }
  key(index: number) {
    return [...this.data.keys()][index] ?? null;
  }
  removeItem(key: string) {
    this.data.delete(key);
  }
  setItem(key: string, value: string) {
    this.data.set(key, value);
  }
}

const draft = (sessionId: string, savedAt: number, text = "draft") =>
  ({
    v: 1,
    sessionId,
    generation: "0123456789abcdef",
    text,
    savedAt,
    submission: null,
  }) satisfies StoredDraft;

const scratch = (
  sessionId: string,
  savedAt: number,
  entries: { id: string; text: string }[],
) =>
  ({
    v: 1,
    sessionId,
    generation: "0123456789abcdef",
    entries: entries.map((entry) => ({
      ...entry,
      createdAt: savedAt,
      updatedAt: savedAt,
    })),
    savedAt,
  }) satisfies StoredScratch;

describe("browser draft storage", () => {
  test("round-trips drafts and unconfirmed submissions per session", () => {
    const memory = new MemoryStorage();
    const storage = createDraftStorage(memory, () => 1000);
    const pending = {
      ...draft("aaaaaaaa", 1000, "中文🙂\nsecond"),
      submission: { id: "s-00000001", state: "received" as const, at: 900 },
    };
    expect(storage.save(pending)).toBe(true);
    expect(storage.save(draft("bbbbbbbb", 1000))).toBe(true);
    expect(createDraftStorage(memory, () => 2000).load("aaaaaaaa")).toEqual(
      pending,
    );
    expect(storage.count()).toBe(2);
    // An empty draft without a submission leaves nothing behind.
    storage.save(draft("bbbbbbbb", 1000, ""));
    expect(storage.load("bbbbbbbb")).toBeNull();
    expect(memory.getItem("webmux.draft.bbbbbbbb")).toBeNull();
  });

  test("expired, corrupt and misfiled entries are pruned on open", () => {
    const memory = new MemoryStorage();
    memory.setItem(
      "webmux.draft.aaaaaaaa",
      JSON.stringify(draft("aaaaaaaa", 0)),
    );
    memory.setItem("webmux.draft.bbbbbbbb", "{not json");
    memory.setItem(
      "webmux.draft.cccccccc",
      JSON.stringify(draft("dddddddd", draftRetention)),
    );
    memory.setItem(
      "webmux.draft.eeeeeeee",
      JSON.stringify(draft("eeeeeeee", draftRetention)),
    );
    memory.setItem("webmux.token", "secret");
    const storage = createDraftStorage(memory, () => draftRetention + 1);
    expect([...memory.data.keys()].sort()).toEqual([
      "webmux.draft.eeeeeeee",
      "webmux.token",
    ]);
    expect(storage.load("eeeeeeee")?.text).toBe("draft");
  });

  test("keep() drops drafts of sessions the server no longer has", () => {
    const storage = createDraftStorage(new MemoryStorage(), () => 1);
    storage.save(draft("aaaaaaaa", 1));
    storage.save(draft("bbbbbbbb", 1));
    storage.keep(["bbbbbbbb", "cccccccc"]);
    expect(storage.load("aaaaaaaa")).toBeNull();
    expect(storage.load("bbbbbbbb")).not.toBeNull();
  });

  test("turning persistence off deletes saved drafts and stops saving", () => {
    const memory = new MemoryStorage();
    const storage = createDraftStorage(memory, () => 1);
    storage.save(draft("aaaaaaaa", 1));
    storage.setEnabled(false);
    expect(storage.enabled).toBe(false);
    expect(storage.count()).toBe(0);
    expect(storage.save(draft("aaaaaaaa", 1))).toBe(false);
    expect(createDraftStorage(memory).enabled).toBe(false);
    storage.setEnabled(true);
    expect(storage.save(draft("aaaaaaaa", 1))).toBe(true);
  });

  test("round-trips scratch entries and counts entries separately from drafts", () => {
    const memory = new MemoryStorage();
    const storage = createDraftStorage(memory, () => 1000);
    expect(
      storage.saveScratch(
        scratch("aaaaaaaa", 1000, [
          { id: "s-000001", text: "first" },
          { id: "s-000002", text: "中文🙂" },
        ]),
      ),
    ).toBe(true);
    expect(
      storage.saveScratch(
        scratch("bbbbbbbb", 1000, [{ id: "s-000003", text: "other" }]),
      ),
    ).toBe(true);
    expect(
      createDraftStorage(memory, () => 2000).loadScratch("aaaaaaaa")?.entries,
    ).toEqual([
      {
        id: "s-000001",
        text: "first",
        createdAt: 1000,
        updatedAt: 1000,
      },
      {
        id: "s-000002",
        text: "中文🙂",
        createdAt: 1000,
        updatedAt: 1000,
      },
    ]);
    expect(storage.count()).toBe(0);
    expect(storage.scratchCount()).toBe(3);
    // Empty scratch lists leave nothing behind.
    storage.saveScratch(scratch("bbbbbbbb", 1000, []));
    expect(storage.loadScratch("bbbbbbbb")).toBeNull();
    expect(memory.getItem("webmux.scratch.bbbbbbbb")).toBeNull();
  });

  test("expired, corrupt, duplicate and oversized scratch records are pruned on open", () => {
    const memory = new MemoryStorage();
    memory.setItem(
      "webmux.scratch.aaaaaaaa",
      JSON.stringify(scratch("aaaaaaaa", 0, [{ id: "s-000001", text: "old" }])),
    );
    memory.setItem("webmux.scratch.bbbbbbbb", "{not json");
    memory.setItem(
      "webmux.scratch.cccccccc",
      JSON.stringify(
        scratch("cccccccc", draftRetention, [
          { id: "s-000002", text: "duplicate" },
          { id: "s-000002", text: "duplicate" },
        ]),
      ),
    );
    memory.setItem(
      "webmux.scratch.dddddddd",
      JSON.stringify(
        scratch("dddddddd", draftRetention, [
          { id: "s-000003", text: "x".repeat(scratchEntryLimit + 1) },
        ]),
      ),
    );
    memory.setItem(
      "webmux.scratch.eeeeeeee",
      JSON.stringify(
        scratch("eeeeeeee", draftRetention, [{ id: "s-000004", text: "keep" }]),
      ),
    );
    const storage = createDraftStorage(memory, () => draftRetention + 1);
    expect([...memory.data.keys()].sort()).toEqual(["webmux.scratch.eeeeeeee"]);
    expect(storage.loadScratch("eeeeeeee")?.entries[0].text).toBe("keep");
  });

  test("keep, remove and clear cover scratch and current drafts", () => {
    const memory = new MemoryStorage();
    const storage = createDraftStorage(memory, () => 1);
    storage.save(draft("aaaaaaaa", 1));
    storage.saveScratch(
      scratch("aaaaaaaa", 1, [{ id: "s-000001", text: "a" }]),
    );
    storage.saveScratch(
      scratch("bbbbbbbb", 1, [{ id: "s-000002", text: "b" }]),
    );
    storage.keep(["bbbbbbbb"]);
    expect(storage.load("aaaaaaaa")).toBeNull();
    expect(storage.loadScratch("aaaaaaaa")).toBeNull();
    expect(storage.loadScratch("bbbbbbbb")).not.toBeNull();
    storage.remove("bbbbbbbb");
    expect(memory.data.size).toBe(0);
    storage.save(draft("cccccccc", 1));
    storage.saveScratch(
      scratch("cccccccc", 1, [{ id: "s-000003", text: "c" }]),
    );
    storage.clear();
    expect(memory.data.size).toBe(0);
  });

  test("scratch limits and unavailable storage reject only the scratch list", () => {
    const memory = new MemoryStorage();
    const storage = createDraftStorage(memory, () => 1);
    storage.save(draft("aaaaaaaa", 1));
    storage.saveScratch(
      scratch("aaaaaaaa", 1, [{ id: "s-000000", text: "saved" }]),
    );
    const tooLong = scratch("aaaaaaaa", 1, [
      { id: "s-000001", text: "x".repeat(scratchEntryLimit + 1) },
    ]);
    expect(storage.saveScratch(tooLong)).toBe(false);
    // A rejected write keeps the last successfully saved scratch record.
    expect(storage.loadScratch("aaaaaaaa")?.entries[0].text).toBe("saved");
    expect(storage.load("aaaaaaaa")).not.toBeNull();
    const tooMany = scratch(
      "aaaaaaaa",
      1,
      Array.from({ length: scratchEntriesLimit + 1 }, (_, index) => ({
        id: `s-${String(index).padStart(6, "0")}`,
        text: "x",
      })),
    );
    expect(storage.saveScratch(tooMany)).toBe(false);
    expect(storage.loadScratch("aaaaaaaa")?.entries[0].text).toBe("saved");
    const tooMuch = scratch(
      "aaaaaaaa",
      1,
      Array.from({ length: 6 }, (_, index) => ({
        id: `s-${String(index).padStart(6, "0")}`,
        text: "x".repeat(scratchEntryLimit),
      })),
    );
    expect(scratchStorageLimit).toBeLessThan(6 * scratchEntryLimit);
    expect(storage.saveScratch(tooMuch)).toBe(false);
    expect(storage.loadScratch("aaaaaaaa")?.entries[0].text).toBe("saved");
    const full = new MemoryStorage();
    full.setItem = () => {
      throw new DOMException("quota", "QuotaExceededError");
    };
    expect(
      createDraftStorage(full, () => 1).saveScratch(
        scratch("aaaaaaaa", 1, [{ id: "s-000001", text: "x" }]),
      ),
    ).toBe(false);
    const missing = createDraftStorage(null);
    expect(
      missing.saveScratch(
        scratch("aaaaaaaa", 1, [{ id: "s-000001", text: "x" }]),
      ),
    ).toBe(false);
  });

  test("oversized drafts and unavailable storage degrade to memory only", () => {
    const storage = createDraftStorage(new MemoryStorage(), () => 1);
    storage.save(draft("aaaaaaaa", 1));
    expect(
      storage.save(draft("aaaaaaaa", 1, "x".repeat(draftStorageLimit + 1))),
    ).toBe(false);
    expect(storage.load("aaaaaaaa")).toBeNull();
    const full = new MemoryStorage();
    full.setItem = () => {
      throw new DOMException("quota", "QuotaExceededError");
    };
    expect(createDraftStorage(full).save(draft("aaaaaaaa", 1))).toBe(false);
    const missing = createDraftStorage(null);
    expect(missing.enabled).toBe(false);
    expect(missing.save(draft("aaaaaaaa", 1))).toBe(false);
    expect(missing.load("aaaaaaaa")).toBeNull();
  });
});
