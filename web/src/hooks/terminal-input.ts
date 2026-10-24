import type { IDisposable, Terminal } from "@xterm/xterm";
import { isIOS } from "../touchInput";

// Draft-mode routing supplied by the page: while `draftEnabled()` holds, user
// keys, text and IME commits go to the draft instead of the PTY.
export interface TerminalUserInput {
  draftEnabled: () => boolean;
  focusDraft: () => void;
  key: (event: KeyboardEvent) => void;
  insertText: (text: string) => void;
}

export interface TerminalClipboard {
  paste: () => void;
  copySelection: (text: string) => void;
}

export interface TerminalInputOptions {
  userInput?: TerminalUserInput;
  clipboard: TerminalClipboard;
  // Bytes xterm emits; `user` is false for its automatic device replies.
  onData: (data: string, user: boolean) => void;
}

// Keyboard, IME, clipboard-shortcut and draft routing for xterm in `parent`.
// Returns the cleanup that removes every listener it added.
export function attachTerminalInput(
  term: Terminal,
  parent: HTMLElement,
  { userInput, clipboard, onData }: TerminalInputOptions,
) {
  term.textarea?.setAttribute("autocapitalize", "off");
  term.textarea?.setAttribute("autocorrect", "off");
  term.textarea?.setAttribute("spellcheck", "false");
  if (isIOS()) parent.classList.add("ios-ime");
  // Divert user events, never onData: device replies and explicit paste
  // must retain their normal PTY path while a draft is being edited.
  const focusDraft = () => {
    if (userInput?.draftEnabled()) userInput.focusDraft();
  };
  const draftPaste = (event: ClipboardEvent) => {
    if (!userInput?.draftEnabled() || event.clipboardData?.files.length) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    userInput.insertText(event.clipboardData?.getData("text/plain") ?? "");
    userInput.focusDraft();
  };
  const draftInput = (event: InputEvent) => {
    if (!userInput?.draftEnabled()) return;
    // If beforeinput cannot be cancelled, consume its subsequent input
    // instead. Composition is committed only at compositionend.
    if (event.type === "beforeinput" && !event.cancelable) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    userInput.focusDraft();
    if (
      event.data &&
      !event.isComposing &&
      event.inputType !== "insertCompositionText" &&
      event.inputType !== "insertFromComposition"
    )
      userInput.insertText(event.data);
  };
  const draftComposition = (event: CompositionEvent) => {
    if (!userInput?.draftEnabled()) return;
    event.stopImmediatePropagation();
    userInput.focusDraft();
    if (event.type === "compositionend" && event.data)
      userInput.insertText(event.data);
  };
  parent.addEventListener("focusin", focusDraft, true);
  parent.addEventListener("paste", draftPaste, true);
  // beforeinput covers browsers dispatching insertion to the previously
  // focused xterm textarea after focus has already moved.
  parent.addEventListener("beforeinput", draftInput as EventListener, true);
  parent.addEventListener("input", draftInput as EventListener, true);
  parent.addEventListener("compositionstart", draftComposition, true);
  parent.addEventListener("compositionupdate", draftComposition, true);
  parent.addEventListener("compositionend", draftComposition, true);
  // xterm accepts a composed insertText only while no keydown is
  // pending, otherwise relying on a textarea diff scheduled by a keyCode
  // 229 keydown. iOS CJK keyboards commit space, digits and punctuation as
  // insertText outside that window (keyups of IME-handled keys go missing),
  // so xterm drops them. Cancel the edit so that diff sees no change and
  // forward the text once. xterm already sent keys it cancelled at keydown
  // and printable keys it handled at keypress, which it leaves uncancelled;
  // compositions remain xterm's, including Safari's trailing insertText.
  let keypressed = false;
  let composing = false;
  let compositionTimer: number | undefined;
  const trackKey = (event: KeyboardEvent) => {
    keypressed = event.type === "keypress";
  };
  const trackComposition = (event: CompositionEvent) => {
    clearTimeout(compositionTimer);
    composing = true;
    if (event.type === "compositionend")
      compositionTimer = window.setTimeout(() => (composing = false));
  };
  const directInput = (event: InputEvent) => {
    if (
      userInput?.draftEnabled() ||
      event.target !== term.textarea ||
      event.inputType !== "insertText" ||
      !event.data ||
      event.isComposing ||
      composing ||
      keypressed ||
      !event.cancelable
    )
      return;
    event.preventDefault();
    event.stopImmediatePropagation();
    term.input(event.data, true);
  };
  for (const type of ["keydown", "keypress", "keyup"] as const)
    parent.addEventListener(type, trackKey, true);
  parent.addEventListener("beforeinput", directInput as EventListener, true);
  parent.addEventListener("compositionstart", trackComposition, true);
  parent.addEventListener("compositionend", trackComposition, true);

  // xterm emits keypresses and its own answers to device queries through
  // the same event, sometimes as identical bytes (Shift+F3 and a cursor
  // report are both "ESC [ 1 ; 2 R"). Its core fires onUserInput right
  // before onData only for the former; the server needs that provenance
  // to write one answer per query and to let only real input take
  // control. Without the hook, everything is sent as user input.
  let typed = false;
  const core = (
    term as unknown as {
      _core?: {
        coreService?: {
          onUserInput?: (listener: () => void) => IDisposable;
        };
      };
    }
  )._core;
  const provenance = core?.coreService?.onUserInput?.(() => {
    typed = true;
  });
  const input = term.onData((data) => {
    const user = typed || !provenance;
    typed = false;
    onData(data, user);
  });

  term.attachCustomKeyEventHandler((event) => {
    if (userInput?.draftEnabled()) {
      if (
        event.type === "keydown" &&
        (event.ctrlKey || event.metaKey) &&
        event.key.toLowerCase() === "v"
      ) {
        if (event.shiftKey) {
          event.preventDefault();
          clipboard.paste();
        } else userInput.focusDraft();
      } else {
        event.preventDefault();
        userInput.key(event);
      }
      return false;
    }
    if (
      event.type === "keydown" &&
      event.altKey &&
      !event.ctrlKey &&
      !event.metaKey &&
      !event.shiftKey &&
      !event.isComposing &&
      event.keyCode !== 229
    ) {
      // xterm 6 changed Alt-arrow sequences. Keep shell word navigation
      // and classify these bytes as user input, just like native keys.
      const sequences: Record<string, string> = /Mac/.test(navigator.platform)
        ? { ArrowLeft: "\x1bb", ArrowRight: "\x1bf" }
        : {
            ArrowLeft: "\x1b[1;5D",
            ArrowRight: "\x1b[1;5C",
            ArrowUp: "\x1b[1;5A",
            ArrowDown: "\x1b[1;5B",
          };
      const sequence = sequences[event.key];
      if (sequence) {
        event.preventDefault();
        term.input(sequence, true);
        return false;
      }
    }
    if (
      event.type === "keydown" &&
      !event.altKey &&
      (event.ctrlKey || event.metaKey) &&
      event.key.toLowerCase() === "v"
    ) {
      // Native Ctrl/Cmd+V works without Clipboard API permissions, including
      // HTTP deployments. Shift+V retains the explicit clipboard action.
      if (event.shiftKey) {
        event.preventDefault();
        clipboard.paste();
      }
      return false;
    }
    if (event.type === "keydown" && event.ctrlKey && event.shiftKey) {
      if (event.key.toLowerCase() === "c") {
        clipboard.copySelection(term.getSelection());
        return false;
      }
    }
    return true;
  });

  return () => {
    parent.removeEventListener("focusin", focusDraft, true);
    parent.removeEventListener("paste", draftPaste, true);
    parent.removeEventListener(
      "beforeinput",
      draftInput as EventListener,
      true,
    );
    parent.removeEventListener("input", draftInput as EventListener, true);
    parent.removeEventListener("compositionstart", draftComposition, true);
    parent.removeEventListener("compositionupdate", draftComposition, true);
    parent.removeEventListener("compositionend", draftComposition, true);
    parent.removeEventListener(
      "beforeinput",
      directInput as EventListener,
      true,
    );
    for (const type of ["keydown", "keypress", "keyup"] as const)
      parent.removeEventListener(type, trackKey, true);
    parent.removeEventListener("compositionstart", trackComposition, true);
    parent.removeEventListener("compositionend", trackComposition, true);
    clearTimeout(compositionTimer);
    parent.classList.remove("ios-ime");
    input.dispose();
    provenance?.dispose();
  };
}
