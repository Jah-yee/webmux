import { onMount } from "svelte";
import type { Terminal } from "@xterm/xterm";
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
  // Whether the newest output is in view: the xterm viewport sits at the
  // buffer end and, for an observer panning a larger grid, the host does too.
  let atBottom = $state(true);
  let pasteResult = false;
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
      const attachment = stream.attach(term, {
        fit: safeFit,
        scrollToBottom,
        isVisible,
        copy: (text) => clipboard.copySelection(text),
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
      if (document.activeElement === terminal?.textarea) terminal?.blur();
      else {
        stream.claim();
        terminal?.focus();
      }
    },
    clear() {
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
