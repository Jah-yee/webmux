import { onMount } from "svelte";
import type { Terminal } from "@xterm/xterm";
import { SearchAddon } from "@xterm/addon-search";
import {
  terminalFindError,
  terminalFindLimit,
  type TerminalFindOptions,
  type TerminalFindResults,
} from "../terminal-search";
import { stored, store } from "../api";
import {
  FontSizeSchema,
  type ControlState,
  type SessionInfo,
  type SubmissionProgress,
} from "../schemas";
import type { Connection } from "../ws";
import { modifiedInput, type Modifiers } from "../input";
import { loadTerminalFont } from "../fonts";
import { hasTouchInput } from "../touchInput";
import { attachTouchGestures } from "./touch";
import {
  createTerminalRenderer,
  type TerminalRenderer,
} from "./terminal-renderer";
import {
  attachTerminalInput,
  type TerminalClipboard,
  type TerminalUserInput,
} from "./terminal-input";
import { createTerminalSocket } from "./terminal-socket.svelte";

export interface TerminalController {
  readonly ready: boolean;
  readonly info: SessionInfo | undefined;
  readonly connection: Connection;
  readonly restoring: boolean;
  readonly exitCode: number | null;
  readonly fontSize: number;
  readonly modifiers: Modifiers;
  readonly error: string;
  readonly selection: string;
  // Whether the newest output is in view (see `updateBottom`).
  readonly atBottom: boolean;
  readonly historyTruncated: boolean;
  readonly control: ControlState | undefined;
  readonly controlling: boolean;
  readonly clientId: string;
  readonly canFind: boolean;
  readonly findResults: TerminalFindResults | null;
  readonly findReplayed: boolean;
  readonly bufferType: "normal" | "alternate";
  readonly scrollback: number;
  findNext(
    query: string,
    options: TerminalFindOptions,
    incremental?: boolean,
  ): void;
  findPrevious(query: string, options: TerminalFindOptions): void;
  clearFind(): void;
  claim(): void;
  submit(
    submissionId: string,
    text: string,
    enter: boolean,
    report: (state: SubmissionProgress) => void,
  ): boolean;
  track(
    submissionId: string,
    report: (state: SubmissionProgress) => void,
  ): void;
  untrack(submissionId: string): void;
  copySelection(): void;
  clearSelection(): void;
  send(input: string): boolean;
  setFontSize(value: number): void;
  resetModifiers(): void;
  toggleModifier(key: keyof Modifiers): void;
  paste(text: string): boolean;
  keyboard(): void;
  clear(): void;
  resync(): void;
  fit(): void;
  scrollToBottom(): void;
  focus(): void;
}

// One session's terminal: composes the xterm renderer, the session socket,
// keyboard/IME/draft input and touch gestures on the host from
// `getContainer`, and releases all of them when the component unmounts.
export function useTerminal(
  getContainer: () => HTMLDivElement,
  id: string,
  clipboard: TerminalClipboard,
  isVisible: () => boolean = () => true,
  userInput?: TerminalUserInput,
  isKeyboardDismissed: () => boolean = () => false,
): TerminalController {
  let terminal: Terminal | undefined;
  let renderer: TerminalRenderer | undefined;
  let scrollOnFit = false;
  const stream = createTerminalSocket(id);
  const defaultFont = matchMedia("(pointer: coarse)").matches ? 13 : 14;
  let fontSize = $state(
    FontSizeSchema.catch(defaultFont).parse(
      stored("fontSize", String(defaultFont)),
    ),
  );
  let modifiers = $state<Modifiers>({ ctrl: false, alt: false });
  let selection = $state("");
  let search: SearchAddon | undefined;
  let canFind = $state(false);
  let findResults = $state<TerminalFindResults | null>(null);
  let findReplayed = $state(false);
  let bufferType = $state<"normal" | "alternate">("normal");
  let scrollback = $state(10000);
  let findActive = false;
  function clearFind() {
    findActive = false;
    search?.clearDecorations();
    terminal?.clearSelection();
    // clearDecorations does not emit a result-reset event.
    findResults = null;
    findReplayed = false;
  }
  function find(
    query: string,
    options: TerminalFindOptions,
    previous: boolean,
    incremental = false,
  ) {
    if (!query) {
      clearFind();
      return;
    }
    if (
      !terminal ||
      !search ||
      stream.restoring ||
      terminalFindError(query, options.regex)
    )
      return;
    if (incremental || !findActive) {
      search.clearDecorations();
      // The addon starts at row zero without a selection. Seed a one-cell
      // anchor at the viewport, synchronously replaced by the actual match
      // (or cleared on no match). Never seed before validating the query.
      terminal.select(0, terminal.buffer.active.viewportY, 1);
    }
    findActive = true;
    findReplayed = false;
    const theme = terminal.options.theme;
    const searchOptions = {
      ...options,
      incremental,
      decorations: {
        matchBackground: theme?.yellow,
        matchBorder: theme?.yellow,
        matchOverviewRuler: theme?.yellow ?? "#a16207",
        activeMatchBackground: theme?.blue,
        activeMatchBorder: theme?.blue,
        activeMatchColorOverviewRuler: theme?.blue ?? "#2563eb",
      },
    };
    if (previous) search.findPrevious(query, searchOptions);
    else search.findNext(query, searchOptions);
  }
  // Whether the newest output is in view: the xterm viewport sits at the
  // buffer end and, for an observer panning a larger grid, the host does too.
  let atBottom = $state(true);
  let pasteResult = false;
  let keyboardReopenAttempted = false;
  function safeFit() {
    const parent = getContainer();
    if (isVisible() && parent?.clientWidth > 0 && parent?.clientHeight > 0) {
      // Observers render the controller's grid and scroll it, never fit.
      const control = stream.control;
      parent.classList.toggle("observing", stream.observing);
      if (control && stream.observing) {
        if (
          terminal?.cols !== control.cols ||
          terminal?.rows !== control.rows
        ) {
          terminal?.resize(control.cols, control.rows);
          // Keep the newest rows, where prompts appear, in view.
          parent.scrollTop = parent.scrollHeight;
        }
      } else renderer?.fit();
      if (scrollOnFit && terminal) {
        terminal.scrollToBottom();
        if (stream.observing) parent.scrollTop = parent.scrollHeight;
        scrollOnFit = false;
      }
      updateBottom();
    }
  }
  function updateBottom() {
    const parent = getContainer();
    if (!terminal || !parent) return;
    const buffer = terminal.buffer.active;
    atBottom =
      buffer.viewportY >= buffer.baseY &&
      (!parent.classList.contains("observing") ||
        parent.scrollTop + parent.clientHeight >= parent.scrollHeight - 1);
  }
  function scrollToBottom() {
    scrollOnFit = true;
    safeFit();
  }
  function send(input: string) {
    const value = modifiedInput(input, modifiers);
    modifiers = { ctrl: false, alt: false };
    pasteResult = stream.sendInput(value);
    return pasteResult;
  }

  function setFontSize(value: number) {
    fontSize = Math.max(12, Math.min(20, Math.round(value)));
    if (terminal) {
      terminal.options.fontSize = fontSize;
      safeFit();
    }
    store("fontSize", String(fontSize));
  }

  onMount(() => {
    const parent = getContainer();
    let disposed = false;
    let release = () => {};
    void loadTerminalFont(fontSize).then(() => {
      if (!disposed) release = mountTerminal();
    });

    function mountTerminal() {
      const view = createTerminalRenderer(parent, fontSize, {
        layout: safeFit,
        // Called only by the theme observer, once `attachment` exists.
        themeChanged: () => attachment.themeChanged(),
      });
      const term = view.term;
      renderer = view;
      terminal = term;
      search = new SearchAddon({ highlightLimit: terminalFindLimit });
      term.loadAddon(search);
      canFind = true;
      scrollback = term.options.scrollback ?? 10000;
      bufferType = term.buffer.active.type;
      const resultsChanged = search.onDidChangeResults((results) => {
        if (findActive) {
          // The addon clears then reselects on its delayed output refresh.
          // xterm omits the second selection event for unchanged coordinates,
          // so read the completed search's selection rather than its clear.
          selection = term.getSelection();
          findResults = results;
        }
      });
      const bufferChanged = term.buffer.onBufferChange((buffer) => {
        clearFind();
        bufferType = buffer.type;
        // Replayed TUI entry/exit must not erase the full-replay notice.
        findReplayed = stream.restoring;
      });
      const attachment = stream.attach(term, {
        fit: safeFit,
        scrollToBottom,
        isVisible,
        copy: (text) => clipboard.copySelection(text),
        resetFind: () => {
          clearFind();
          findReplayed = true;
        },
        preserveViewport: () => findActive,
      });
      const detachInput = attachTerminalInput(term, parent, {
        userInput,
        clipboard,
        onData: (data, user) => {
          if (user) send(data);
          else stream.sendReply(data);
        },
      });
      const selected = term.onSelectionChange(() => {
        selection = term.getSelection();
      });
      const scrolled = term.onScroll(updateBottom);
      const parsed = term.onWriteParsed(updateBottom);
      parent.addEventListener("scroll", updateBottom, { passive: true });
      const detachTouch = attachTouchGestures(term, parent, {
        fontSize: () => fontSize,
        setFontSize,
        hasSelection: () => Boolean(selection),
        keyboardDismissed: isKeyboardDismissed,
      });
      safeFit();
      if (!hasTouchInput()) term.focus();
      return () => {
        disposed = true;
        // Observers go first; the terminal itself is disposed two frames
        // later, after the socket below has closed.
        view.dispose();
        detachTouch();
        detachInput();
        scrolled.dispose();
        parsed.dispose();
        parent.removeEventListener("scroll", updateBottom);
        selected.dispose();
        resultsChanged.dispose();
        bufferChanged.dispose();
        clearFind();
        search?.dispose();
        search = undefined;
        canFind = false;
        attachment.dispose();
        terminal = undefined;
        renderer = undefined;
        parent.classList.remove("observing");
        atBottom = true;
        scrollOnFit = false;
      };
    }

    return () => {
      disposed = true;
      release();
    };
  });

  return {
    get ready() {
      return !!terminal && stream.accepting;
    },
    get info() {
      return stream.info;
    },
    get connection() {
      return stream.connection;
    },
    get restoring() {
      return stream.restoring;
    },
    get exitCode() {
      return stream.exitCode;
    },
    get fontSize() {
      return fontSize;
    },
    get modifiers() {
      return modifiers;
    },
    get error() {
      return stream.error;
    },
    get selection() {
      return selection;
    },
    get atBottom() {
      return atBottom;
    },
    get historyTruncated() {
      return stream.historyTruncated;
    },
    get control() {
      return stream.control;
    },
    get controlling() {
      return stream.controlling;
    },
    get clientId() {
      return stream.clientId;
    },
    get canFind() {
      return canFind;
    },
    get findResults() {
      return findResults;
    },
    get findReplayed() {
      return findReplayed;
    },
    get bufferType() {
      return bufferType;
    },
    get scrollback() {
      return scrollback;
    },
    findNext(query, options, incremental = false) {
      find(query, options, false, incremental);
    },
    findPrevious(query, options) {
      find(query, options, true);
    },
    clearFind,
    claim: stream.claim,
    submit(submissionId, text, enter, report) {
      if (!stream.submittable) return false;
      modifiers = { ctrl: false, alt: false };
      return stream.submit(submissionId, text, enter, report);
    },
    track: stream.track,
    untrack: stream.untrack,
    copySelection() {
      if (selection) clipboard.copySelection(selection);
    },
    clearSelection() {
      terminal?.clearSelection();
    },
    send,
    setFontSize,
    resetModifiers() {
      modifiers = { ctrl: false, alt: false };
    },
    toggleModifier(key: keyof Modifiers) {
      modifiers = { ...modifiers, [key]: !modifiers[key] };
    },
    paste(text: string) {
      if (!terminal || !stream.accepting) return false;
      pasteResult = false;
      modifiers = { ctrl: false, alt: false };
      terminal.paste(text);
      return pasteResult;
    },
    keyboard() {
      const textarea = terminal?.textarea;
      if (!terminal || !textarea) return;
      const focused = document.activeElement === textarea;
      if (focused && isKeyboardDismissed() && !keyboardReopenAttempted) {
        // Android can hide the IME with Back while the helper keeps focus;
        // focus() alone is then a no-op, so restart focus once to reopen it.
        // If the browser still declines, the next press toggles normally.
        keyboardReopenAttempted = true;
        stream.claim();
        textarea.blur();
        terminal.focus();
        return;
      }
      keyboardReopenAttempted = false;
      if (focused) {
        terminal.blur();
        return;
      }
      stream.claim();
      terminal.focus();
    },
    clear() {
      clearFind();
      terminal?.clear();
    },
    resync: stream.resync,
    fit: safeFit,
    scrollToBottom,
    focus() {
      terminal?.focus();
    },
  };
}
