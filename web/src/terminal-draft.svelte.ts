import {
  scratchEntriesLimit,
  scratchEntryLimit,
  scratchStorageLimit,
  type DraftStorage,
} from "./draftStorage";
import type { ScratchEntry, SubmissionProgress } from "./schemas";

interface DraftSubmission {
  readonly id: string;
  state: SubmissionProgress;
  readonly at: number;
}

export type ScratchAddResult =
  | "added"
  | "empty"
  | "full"
  | "too-long"
  | "over-total"
  | "locked"
  | "invalid";
export type ScratchTakeMode = "append" | "replace";
export type ScratchTakeResult =
  | "taken"
  | "empty"
  | "locked"
  | "invalid"
  | "full"
  | "too-long"
  | "over-total";

export interface TerminalDraft {
  readonly sessionId: string;
  readonly identity: symbol;
  enabled: boolean;
  text: string;
  selectionStart: number | null;
  selectionEnd: number | null;
  draftEpoch: number;
  updatedWhileHidden: boolean;
  // The session run this text was written for, once known.
  generation: string | null;
  // An unconfirmed submission of `text`. While present the text is locked:
  // it is exactly what may already have reached the terminal.
  submission: DraftSubmission | null;
  // When this draft was restored from browser storage, its saved time.
  restoredAt: number | null;
  // False when the text could not be kept on this device.
  persisted: boolean;
  // Separate notes the user keeps for this session. Taking one moves it into
  // `text`; nothing here is ever sent without the explicit send action.
  scratch: ScratchEntry[];
  // False when the scratch list could not be kept on this device.
  scratchPersisted: boolean;
  // The run generation the saved scratch list was written for.
  scratchGeneration: string | null;
  scratchUpdatedWhileHidden: boolean;
}

export interface DraftOperation {
  readonly sessionId: string;
  readonly entryIdentity: symbol;
  readonly draftEpoch: number;
  readonly startedInDraft: boolean;
}

export type InputDestination = "draft" | "terminal" | "failed";
export type TextInsertion = InputDestination;

// States after which the server will not change a submission any more.
const settled = new Set<SubmissionProgress>([
  "written",
  "partial",
  "failed",
  "missing",
]);

// Owned by App. With storage, drafts and unconfirmed submissions survive
// reloads in this browser. An operation can only use its original entry.
export function createTerminalDrafts(
  options: { storage?: DraftStorage | null; now?: () => number } = {},
): TerminalDrafts {
  const storage = options.storage ?? null;
  const now = options.now ?? Date.now;
  const entries = new Map<string, TerminalDraft>();
  function persist(entry: TerminalDraft) {
    if (!storage || !valid(entry)) return;
    entry.persisted = storage.save({
      v: 1,
      sessionId: entry.sessionId,
      generation: entry.generation,
      text: entry.text,
      savedAt: now(),
      submission: entry.submission && {
        id: entry.submission.id,
        state: entry.submission.state,
        at: entry.submission.at,
      },
    });
  }
  function persistScratch(entry: TerminalDraft) {
    if (!storage || !valid(entry)) return;
    entry.scratchPersisted = storage.saveScratch({
      v: 1,
      sessionId: entry.sessionId,
      generation: entry.generation,
      entries: entry.scratch.map((item) => ({ ...item })),
      savedAt: now(),
    });
    if (entry.scratchPersisted) entry.scratchGeneration = entry.generation;
  }
  function scratchTotal(entry: TerminalDraft) {
    return entry.scratch.reduce((total, item) => total + item.text.length, 0);
  }
  // IDs avoid randomUUID, which browsers withhold outside a secure context.
  function scratchId() {
    return Array.from(crypto.getRandomValues(new Uint8Array(12)), (byte) =>
      byte.toString(16).padStart(2, "0"),
    ).join("");
  }
  function insertScratch(
    entry: TerminalDraft,
    text: string,
    index = entry.scratch.length,
  ): ScratchAddResult {
    if (!text) return "empty";
    if (entry.scratch.length >= scratchEntriesLimit) return "full";
    if (text.length > scratchEntryLimit) return "too-long";
    if (scratchTotal(entry) + text.length > scratchStorageLimit)
      return "over-total";
    const at = now();
    entry.scratch.splice(index, 0, {
      id: scratchId(),
      text,
      createdAt: at,
      updatedAt: at,
    });
    return "added";
  }
  function get(sessionId: string): TerminalDraft {
    let entry = entries.get(sessionId);
    if (!entry) {
      const saved = storage?.load(sessionId) ?? null;
      const savedScratch = storage?.loadScratch(sessionId) ?? null;
      const created = $state<TerminalDraft>({
        sessionId,
        identity: Symbol(sessionId),
        // Restored text is shown at once; it is still sent only on request.
        enabled: !!saved?.text,
        text: saved?.text ?? "",
        selectionStart: null,
        selectionEnd: null,
        draftEpoch: saved?.text ? 1 : 0,
        updatedWhileHidden: false,
        generation: saved?.generation ?? null,
        // Whatever happened after the save is unknown until the server answers.
        submission:
          saved?.submission && !settled.has(saved.submission.state)
            ? { ...saved.submission, state: "unknown" }
            : null,
        restoredAt: saved?.text ? saved.savedAt : null,
        persisted: true,
        scratch: savedScratch?.entries ?? [],
        scratchPersisted: true,
        scratchGeneration: savedScratch?.generation ?? null,
        scratchUpdatedWhileHidden: false,
      });
      entry = created;
      entries.set(sessionId, entry);
    }
    return entry;
  }
  function valid(entry: TerminalDraft) {
    return entries.get(entry.sessionId) === entry;
  }
  function begin(entry: TerminalDraft): DraftOperation {
    return {
      sessionId: entry.sessionId,
      entryIdentity: entry.identity,
      draftEpoch: entry.draftEpoch,
      startedInDraft: entry.enabled,
    };
  }
  function destination(
    entry: TerminalDraft,
    operation = begin(entry),
  ): InputDestination {
    if (
      !valid(entry) ||
      operation.sessionId !== entry.sessionId ||
      operation.entryIdentity !== entry.identity
    )
      return "failed";
    return entry.enabled ||
      operation.startedInDraft ||
      entry.draftEpoch > operation.draftEpoch
      ? "draft"
      : "terminal";
  }
  function select(
    entry: TerminalDraft,
    start: number | null,
    end: number | null,
  ) {
    if (!valid(entry)) return;
    entry.selectionStart =
      start === null ? null : Math.max(0, Math.min(entry.text.length, start));
    entry.selectionEnd =
      end === null
        ? null
        : Math.max(entry.selectionStart ?? 0, Math.min(entry.text.length, end));
  }
  function edit(
    entry: TerminalDraft,
    text: string,
    start: number | null = null,
    end: number | null = start,
  ) {
    if (!valid(entry) || entry.submission) return;
    entry.text = text;
    select(entry, start, end);
    persist(entry);
  }
  function insert(
    entry: TerminalDraft,
    text: string,
    operation = begin(entry),
    submit?: (text: string) => boolean,
  ): TextInsertion {
    const target = destination(entry, operation);
    if (target === "terminal") return submit?.(text) ? "terminal" : "failed";
    if (target === "failed" || entry.submission) return "failed";
    const start = entry.selectionStart ?? entry.text.length;
    const end = entry.selectionEnd ?? start;
    edit(
      entry,
      entry.text.slice(0, start) + text + entry.text.slice(end),
      start + text.length,
    );
    if (!entry.enabled) entry.updatedWhileHidden = true;
    return "draft";
  }
  return {
    get,
    valid,
    begin,
    destination,
    select,
    edit,
    insert,
    addScratch(entry: TerminalDraft, text: string): ScratchAddResult {
      if (!valid(entry)) return "invalid";
      const added = insertScratch(entry, text);
      if (added !== "added") return added;
      if (!entry.enabled) entry.scratchUpdatedWhileHidden = true;
      persistScratch(entry);
      return "added";
    },
    editScratch(entry: TerminalDraft, id: string, text: string): boolean {
      if (!valid(entry)) return false;
      const item = entry.scratch.find((candidate) => candidate.id === id);
      if (!item) return false;
      if (text.length > scratchEntryLimit) return false;
      if (
        scratchTotal(entry) - item.text.length + text.length >
        scratchStorageLimit
      )
        return false;
      item.text = text;
      item.updatedAt = now();
      if (!entry.enabled) entry.scratchUpdatedWhileHidden = true;
      persistScratch(entry);
      return true;
    },
    removeScratch(entry: TerminalDraft, id: string) {
      if (!valid(entry)) return;
      const index = entry.scratch.findIndex((item) => item.id === id);
      if (index < 0) return;
      entry.scratch.splice(index, 1);
      persistScratch(entry);
    },
    moveScratch(entry: TerminalDraft, id: string, delta: -1 | 1): boolean {
      if (!valid(entry)) return false;
      const index = entry.scratch.findIndex((item) => item.id === id);
      const target = index + delta;
      if (index < 0 || target < 0 || target >= entry.scratch.length)
        return false;
      const [item] = entry.scratch.splice(index, 1);
      entry.scratch.splice(target, 0, item);
      persistScratch(entry);
      return true;
    },
    stash(entry: TerminalDraft): ScratchAddResult {
      if (!valid(entry)) return "invalid";
      if (entry.submission) return "locked";
      if (!entry.text) return "empty";
      const added = insertScratch(entry, entry.text);
      if (added !== "added") return added;
      entry.text = "";
      select(entry, 0, 0);
      entry.restoredAt = null;
      if (!entry.enabled) entry.scratchUpdatedWhileHidden = true;
      persistScratch(entry);
      persist(entry);
      return "added";
    },
    takeScratch(
      entry: TerminalDraft,
      id: string,
      mode: ScratchTakeMode,
    ): ScratchTakeResult {
      if (!valid(entry)) return "invalid";
      if (entry.submission) return "locked";
      const index = entry.scratch.findIndex((item) => item.id === id);
      if (index < 0) return "invalid";
      const target = entry.scratch[index];
      if (!target.text) return "empty";
      if (mode === "replace" && entry.text) {
        // Keep the displaced body as a note before replacing it.
        entry.scratch.splice(index, 1);
        const stashed = insertScratch(entry, entry.text, index);
        if (stashed !== "added") {
          entry.scratch.splice(index, 0, target);
          return stashed;
        }
      } else entry.scratch.splice(index, 1);
      if (mode === "append" && entry.text) {
        entry.text += `${entry.text.endsWith("\n") ? "" : "\n"}${target.text}`;
      } else entry.text = target.text;
      if (!entry.enabled) {
        entry.enabled = true;
        entry.draftEpoch++;
        entry.updatedWhileHidden = false;
      }
      entry.scratchUpdatedWhileHidden = false;
      entry.restoredAt = null;
      select(entry, entry.text.length, entry.text.length);
      persistScratch(entry);
      persist(entry);
      return "taken";
    },
    clearScratch(entry: TerminalDraft) {
      if (!valid(entry) || !entry.scratch.length) return;
      entry.scratch = [];
      persistScratch(entry);
    },
    setEnabled(entry: TerminalDraft, enabled: boolean) {
      if (!valid(entry)) return;
      if (enabled && !entry.enabled) entry.draftEpoch++;
      entry.enabled = enabled;
      if (enabled) {
        entry.updatedWhileHidden = false;
        entry.scratchUpdatedWhileHidden = false;
      }
    },
    clear(entry: TerminalDraft) {
      if (!valid(entry)) return;
      entry.submission = null;
      entry.restoredAt = null;
      edit(entry, "", 0);
      entry.updatedWhileHidden = false;
    },
    remove(sessionId: string) {
      storage?.remove(sessionId);
      forget(sessionId);
    },
    reconcile(sessionIds: Iterable<string>) {
      const present = new Set(sessionIds);
      storage?.keep(present);
      for (const id of entries.keys()) if (!present.has(id)) this.remove(id);
    },
    // Memory only: saved drafts outlive authentication changes and page exit.
    clearAll() {
      for (const id of entries.keys()) forget(id);
    },
    bindGeneration(entry: TerminalDraft, generation: string) {
      if (!valid(entry) || entry.generation === generation) return;
      // Text written for another run of a same-named session is kept, but
      // nothing about its submission can be learned from this run.
      if (entry.generation !== null) entry.submission = null;
      entry.generation = generation;
      persist(entry);
    },
    beginSubmission(entry: TerminalDraft, id: string) {
      if (!valid(entry) || entry.submission || !entry.text) return false;
      entry.submission = { id, state: "saved", at: now() };
      persist(entry);
      return true;
    },
    updateSubmission(
      entry: TerminalDraft,
      id: string,
      state: SubmissionProgress,
    ) {
      if (!valid(entry) || entry.submission?.id !== id) return;
      if (state === "written") {
        entry.submission = null;
        entry.restoredAt = null;
        edit(entry, "", 0);
        entry.enabled = false;
        entry.updatedWhileHidden = false;
        return;
      }
      // Nothing reached the terminal: the text becomes an ordinary draft,
      // and sending it again is a new submission.
      if (state === "failed" || state === "missing") entry.submission = null;
      else entry.submission.state = state;
      persist(entry);
    },
    releaseSubmission(entry: TerminalDraft) {
      if (!valid(entry) || !entry.submission) return;
      entry.submission = null;
      persist(entry);
    },
  };
  function forget(sessionId: string) {
    const entry = entries.get(sessionId);
    if (entry) {
      entry.text = "";
      entry.submission = null;
      entry.selectionStart = entry.selectionEnd = null;
      entry.enabled = false;
      entry.updatedWhileHidden = false;
      entry.scratch = [];
      entry.scratchPersisted = true;
      entry.scratchGeneration = null;
      entry.scratchUpdatedWhileHidden = false;
    }
    entries.delete(sessionId);
  }
}

export interface TerminalDrafts {
  get(sessionId: string): TerminalDraft;
  valid(entry: TerminalDraft): boolean;
  begin(entry: TerminalDraft): DraftOperation;
  destination(
    entry: TerminalDraft,
    operation?: DraftOperation,
  ): InputDestination;
  select(entry: TerminalDraft, start: number | null, end: number | null): void;
  edit(
    entry: TerminalDraft,
    text: string,
    start?: number | null,
    end?: number | null,
  ): void;
  insert(
    entry: TerminalDraft,
    text: string,
    operation?: DraftOperation,
    submit?: (text: string) => boolean,
  ): TextInsertion;
  addScratch(entry: TerminalDraft, text: string): ScratchAddResult;
  editScratch(entry: TerminalDraft, id: string, text: string): boolean;
  removeScratch(entry: TerminalDraft, id: string): void;
  moveScratch(entry: TerminalDraft, id: string, delta: -1 | 1): boolean;
  stash(entry: TerminalDraft): ScratchAddResult;
  takeScratch(
    entry: TerminalDraft,
    id: string,
    mode: ScratchTakeMode,
  ): ScratchTakeResult;
  clearScratch(entry: TerminalDraft): void;
  setEnabled(entry: TerminalDraft, enabled: boolean): void;
  clear(entry: TerminalDraft): void;
  remove(sessionId: string): void;
  reconcile(sessionIds: Iterable<string>): void;
  clearAll(): void;
  bindGeneration(entry: TerminalDraft, generation: string): void;
  beginSubmission(entry: TerminalDraft, id: string): boolean;
  updateSubmission(
    entry: TerminalDraft,
    id: string,
    state: SubmissionProgress,
  ): void;
  releaseSubmission(entry: TerminalDraft): void;
}
