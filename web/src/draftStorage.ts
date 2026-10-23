import {
  StoredDraftSchema,
  StoredScratchSchema,
  type StoredDraft,
  type StoredScratch,
} from "./schemas";

const draftPrefix = "webmux.draft.";
const scratchPrefix = "webmux.scratch.";
const settingKey = "webmux.draftPersistence";
export const draftRetention = 7 * 24 * 60 * 60 * 1000;
// Large drafts stay in memory only rather than exhausting the origin quota.
export const draftStorageLimit = 200_000;
export const scratchEntriesLimit = 20;
export const scratchEntryLimit = 20_000;
export const scratchStorageLimit = 100_000;

export interface DraftStorage {
  readonly enabled: boolean;
  setEnabled(enabled: boolean): void;
  load(sessionId: string): StoredDraft | null;
  // False when the draft could not be kept on this device.
  save(draft: StoredDraft): boolean;
  loadScratch(sessionId: string): StoredScratch | null;
  // False when the scratch list could not be kept on this device.
  saveScratch(scratch: StoredScratch): boolean;
  remove(sessionId: string): void;
  // Drops drafts of sessions the server no longer has.
  keep(sessionIds: Iterable<string>): void;
  clear(): void;
  count(): number;
  scratchCount(): number;
}

// Drafts live in this browser only, keyed by session and bound to the run
// (generation) they were written for. Every access tolerates unavailable or
// corrupt storage: persistence is a convenience, never a requirement.
export function createDraftStorage(
  storage: Storage | null,
  now: () => number = Date.now,
): DraftStorage {
  const attempt = <T>(fallback: T, action: (storage: Storage) => T): T => {
    if (!storage) return fallback;
    try {
      return action(storage);
    } catch {
      return fallback;
    }
  };
  const keys = (prefix: string) =>
    attempt<string[]>([], (storage) =>
      Array.from({ length: storage.length }, (_, index) =>
        storage.key(index),
      ).filter((key): key is string => !!key?.startsWith(prefix)),
    );
  const read = (key: string) =>
    attempt<StoredDraft | null>(null, (storage) => {
      const parsed = StoredDraftSchema.safeParse(
        JSON.parse(storage.getItem(key) ?? "null"),
      );
      return parsed.success &&
        draftPrefix + parsed.data.sessionId === key &&
        now() - parsed.data.savedAt < draftRetention
        ? parsed.data
        : null;
    });
  const readScratch = (key: string) =>
    attempt<StoredScratch | null>(null, (storage) => {
      const parsed = StoredScratchSchema.safeParse(
        JSON.parse(storage.getItem(key) ?? "null"),
      );
      if (!parsed.success || scratchPrefix + parsed.data.sessionId !== key)
        return null;
      if (now() - parsed.data.savedAt >= draftRetention) return null;
      const total = parsed.data.entries.reduce(
        (sum, entry) => sum + entry.text.length,
        0,
      );
      return parsed.data.entries.length <= scratchEntriesLimit &&
        total <= scratchStorageLimit &&
        parsed.data.entries.every(
          (entry) => entry.text.length <= scratchEntryLimit,
        )
        ? parsed.data
        : null;
    });
  const drop = (key: string) =>
    attempt(undefined, (storage) => storage.removeItem(key));
  const result: DraftStorage = {
    get enabled() {
      return attempt(false, (storage) => storage.getItem(settingKey) !== "off");
    },
    setEnabled(enabled) {
      attempt(undefined, (storage) => {
        if (enabled) storage.removeItem(settingKey);
        else storage.setItem(settingKey, "off");
      });
      if (!enabled) result.clear();
    },
    load(sessionId) {
      return result.enabled ? read(draftPrefix + sessionId) : null;
    },
    save(draft) {
      const key = draftPrefix + draft.sessionId;
      if (!result.enabled) return false;
      if (!draft.text && !draft.submission) {
        drop(key);
        return true;
      }
      if (draft.text.length > draftStorageLimit) {
        drop(key);
        return false;
      }
      return attempt(false, (storage) => {
        storage.setItem(key, JSON.stringify(draft));
        return true;
      });
    },
    loadScratch(sessionId) {
      return result.enabled ? readScratch(scratchPrefix + sessionId) : null;
    },
    saveScratch(scratch) {
      const key = scratchPrefix + scratch.sessionId;
      if (!result.enabled) return false;
      if (!scratch.entries.length) {
        drop(key);
        return true;
      }
      const total = scratch.entries.reduce(
        (sum, entry) => sum + entry.text.length,
        0,
      );
      // A rejected write keeps the previous record so one oversized note does
      // not erase notes that were already saved.
      if (
        scratch.entries.length > scratchEntriesLimit ||
        total > scratchStorageLimit ||
        scratch.entries.some((entry) => entry.text.length > scratchEntryLimit)
      )
        return false;
      return attempt(false, (storage) => {
        storage.setItem(key, JSON.stringify(scratch));
        return true;
      });
    },
    remove(sessionId) {
      drop(draftPrefix + sessionId);
      drop(scratchPrefix + sessionId);
    },
    keep(sessionIds) {
      const present = new Set(sessionIds);
      for (const key of [...keys(draftPrefix), ...keys(scratchPrefix)]) {
        const id = key.startsWith(draftPrefix)
          ? key.slice(draftPrefix.length)
          : key.slice(scratchPrefix.length);
        if (!present.has(id)) drop(key);
      }
    },
    clear() {
      for (const key of [...keys(draftPrefix), ...keys(scratchPrefix)])
        drop(key);
    },
    count() {
      return keys(draftPrefix).filter((key) => read(key)).length;
    },
    scratchCount() {
      return keys(scratchPrefix).reduce(
        (total, key) => total + (readScratch(key)?.entries.length ?? 0),
        0,
      );
    },
  };
  // Expired or corrupt entries are pruned whenever storage is opened.
  for (const key of keys(draftPrefix)) if (!read(key)) drop(key);
  for (const key of keys(scratchPrefix)) if (!readScratch(key)) drop(key);
  return result;
}

let shared: DraftStorage | undefined;
export function browserDraftStorage() {
  return (shared ??= createDraftStorage(
    (() => {
      try {
        return localStorage;
      } catch {
        return null;
      }
    })(),
  ));
}
