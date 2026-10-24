import { onMount } from "svelte";
import { Terminal, type IDisposable } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { apiBuffer, stored, store } from "../api";
import {
  ControlStateSchema,
  ExitSchema,
  FontSizeSchema,
  SessionInfoSchema,
  StreamPositionSchema,
  SubmissionStatusSchema,
  type ControlState,
  type SessionInfo,
  type StreamPosition,
  type SubmissionProgress,
} from "../schemas";
import {
  clientIdentity,
  encodeSubmission,
  preparePaste,
} from "../terminalProtocol";
import { connect, type Connection, type SocketConnection } from "../ws";
import { modifiedInput, type Modifiers } from "../input";
import {
  loadTerminalFont,
  terminalFontFamilyAlt,
  terminalFontFamilyCss,
  terminalLineHeight,
} from "../fonts";
import { osc52CopyText } from "../terminalClipboard";
import { createOutputBudget } from "../terminal-output";
import { attachTouchGestures } from "./touch";
import { hasTouchInput, isIOS } from "../touchInput";
import "@xterm/xterm/css/xterm.css";
import "../fonts-terminal.css";

// Read the same theme tokens used by the shadcn surfaces, including live theme changes.
function terminalTheme() {
  const css = getComputedStyle(document.documentElement);
  return {
    background: css.getPropertyValue("--terminal-background").trim(),
    foreground: css.getPropertyValue("--terminal-foreground").trim(),
    cursor: css.getPropertyValue("--terminal-blue").trim(),
    selectionBackground: css.getPropertyValue("--terminal-selection").trim(),
    black: css.getPropertyValue("--terminal-black").trim(),
    red: css.getPropertyValue("--terminal-red").trim(),
    green: css.getPropertyValue("--terminal-green").trim(),
    yellow: css.getPropertyValue("--terminal-yellow").trim(),
    blue: css.getPropertyValue("--terminal-blue").trim(),
    magenta: css.getPropertyValue("--terminal-magenta").trim(),
    cyan: css.getPropertyValue("--terminal-cyan").trim(),
    white: css.getPropertyValue("--terminal-white").trim(),
  };
}

export function useTerminal(
  getContainer: () => HTMLDivElement,
  id: string,
  clipboard: { paste: () => void; copySelection: (text: string) => void },
  isVisible: () => boolean = () => true,
  userInput?: {
    draftEnabled: () => boolean;
    focusDraft: () => void;
    key: (event: KeyboardEvent) => void;
    insertText: (text: string) => void;
  },
) {
  let terminal: Terminal | undefined;
  let socket: SocketConnection | undefined;
  let fit: FitAddon | undefined;
  let restoreBuffer: ((payload: Uint8Array) => void) | undefined;
  let scrollOnFit = false;
  let info = $state<SessionInfo>();
  let connection = $state<Connection>("connecting");
  let restoring = $state(true);
  let exitCode = $state<number | null>(null);
  const defaultFont = matchMedia("(pointer: coarse)").matches ? 13 : 14;
  let fontSize = $state(
    FontSizeSchema.catch(defaultFont).parse(
      stored("fontSize", String(defaultFont)),
    ),
  );
  let modifiers = $state<Modifiers>({ ctrl: false, alt: false });
  let error = $state("");
  let selection = $state("");
  // Whether the newest output is in view: the xterm viewport sits at the
  // buffer end and, for an observer panning a larger grid, the host does too.
  let atBottom = $state(true);
  const encoder = new TextEncoder();
  const decoder = new TextDecoder();
  const identity = clientIdentity();
  // Output continuity: `offset` counts the stream bytes handed to xterm since
  // the snapshot of run `generation`. While `synced`, a reconnect asks the
  // server for only the bytes after it.
  let generation: string | undefined;
  let offset = 0;
  let synced = false;
  let historyTruncated = $state(false);
  let control = $state<ControlState>();
  const controlling = () =>
    !!control && control.controller?.clientId === identity.id;
  // Until the server reports control, fit locally without resizing the PTY.
  const observing = () => !!control && !controlling();
  // Unconfirmed submissions, re-queried after every reconnect.
  const submissions = new Map<string, (state: SubmissionProgress) => void>();

  const inputSendError = "终端输入未发送：连接不可用或发送队列已满，请重试。";
  let pasteResult = false;
  function safeFit() {
    const parent = getContainer();
    if (isVisible() && parent?.clientWidth > 0 && parent?.clientHeight > 0) {
      // Observers render the controller's grid and scroll it, never fit.
      parent.classList.toggle("observing", observing());
      if (control && observing()) {
        if (
          terminal?.cols !== control.cols ||
          terminal?.rows !== control.rows
        ) {
          terminal?.resize(control.cols, control.rows);
          // Keep the newest rows, where prompts appear, in view.
          parent.scrollTop = parent.scrollHeight;
        }
      } else fit?.fit();
      if (scrollOnFit && terminal) {
        terminal.scrollToBottom();
        if (observing()) parent.scrollTop = parent.scrollHeight;
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
    pasteResult = socket?.send(0, encoder.encode(value)) ?? false;
    if (!pasteResult) error = inputSendError;
    else if (error === inputSendError) error = "";
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
      const term = new Terminal({
        fontSize,
        fontFamily: terminalFontFamilyCss,
        fontWeight: 400,
        fontWeightBold: 700,
        lineHeight: terminalLineHeight,
        scrollback: 10000,
        cursorBlink: true,
        minimumContrastRatio: 4.5,
        allowProposedApi: true,
        macOptionIsMeta: true,
        theme: terminalTheme(),
      });
      terminal = term;
      const fitAddon = new FitAddon();
      fit = fitAddon;
      term.loadAddon(fitAddon);
      term.open(parent);
      let webgl: WebglAddon | undefined;
      const useDomRenderer = () => {
        if (!webgl) return;
        const addon = webgl;
        webgl = undefined;
        addon.dispose();
        term.refresh(0, term.rows - 1);
      };
      try {
        webgl = new WebglAddon();
        term.loadAddon(webgl);
        webgl.onContextLoss(useDomRenderer);
      } catch {
        useDomRenderer();
        // xterm's built-in DOM renderer remains available without WebGL.
      }
      // DEC mode 2031 reports: CSI ? 997 ; 1 n is dark, 2 is light. Programs
      // answer them by re-querying colors (OSC 10/11), which xterm answers
      // from the theme. Sent as device replies, so only the responsible
      // connection's report reaches the PTY.
      const root = document.documentElement;
      // Read the class directly: the theme observer below runs later, and a
      // control frame may arrive in between.
      let reportedDark: boolean | undefined;
      const reportScheme = () => {
        reportedDark = root.classList.contains("dark");
        term.input(`\x1b[?997;${reportedDark ? 1 : 2}n`, false);
      };
      const theme = () => {
        if (disposed) return;
        term.options.theme = terminalTheme();
        if (
          control?.colorSchemeReports &&
          reportedDark !== root.classList.contains("dark")
        )
          reportScheme();
      };
      const colors = new MutationObserver(theme);
      colors.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ["class"],
      });
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
        if (!userInput?.draftEnabled() || event.clipboardData?.files.length)
          return;
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
      parent.addEventListener(
        "beforeinput",
        directInput as EventListener,
        true,
      );
      parent.addEventListener("compositionstart", trackComposition, true);
      parent.addEventListener("compositionend", trackComposition, true);
      let stale = false;
      let exited = false;
      let restoreFrame = 0;
      let restoreVersion = 0;
      // Snapshots replay output whose OSC 52 copies already ran when it was
      // live. xterm parses writes and runs their callbacks in order, so an
      // empty write's callback opens the replay window exactly where the
      // snapshot starts and the snapshot's callback closes it.
      let replaying = false;
      const output = createOutputBudget(
        (data, parsed) => term.write(data, parsed),
        () => {
          // A scheduled reconnect already brings a fresh snapshot.
          if (connection !== "connected") return;
          if (!ws.send(2, new Uint8Array())) ws.reconnect();
        },
      );
      const restore = (payload: Uint8Array) => {
        output.replay();
        const version = ++restoreVersion;
        cancelAnimationFrame(restoreFrame);
        restoring = true;
        term.write("", () => {
          if (disposed) return;
          term.reset();
          replaying = true;
        });
        term.write(payload, () => {
          replaying = false;
          if (disposed || version !== restoreVersion) return;
          restoreFrame = requestAnimationFrame(() => {
            if (disposed || version !== restoreVersion) return;
            scrollToBottom();
            restoring = output.recovering;
          });
        });
      };
      restoreBuffer = restore;
      const sendSize = () => {
        if (controlling() && isVisible() && term.cols > 0 && term.rows > 0)
          ws.send(
            1,
            encoder.encode(
              JSON.stringify({ cols: term.cols, rows: term.rows }),
            ),
          );
      };
      let resetPosition: StreamPosition | undefined;
      let appending = false;
      // Whether this connection has seen the program enable scheme reports.
      let schemeReports = false;
      const ws = connect(
        () => {
          const query = new URLSearchParams({
            client: identity.id,
            label: identity.label,
          });
          if (synced && generation) {
            query.set("generation", generation);
            query.set("offset", String(offset));
          }
          return `/ws/s/${id}?${query}`;
        },
        (data) => {
          if (typeof data === "string") return;
          const frame = new Uint8Array(data);
          const payload = frame.subarray(1);
          if (frame[0] === 0) {
            // Output missed while hidden leaves a gap: stop writing, keep
            // the offset, and let the foreground reconnect fill it in.
            // Bytes xterm cannot keep up with are dropped the same way; the
            // offset counts only bytes handed to xterm, so either resumes.
            if (document.hidden || stale) stale = true;
            else if (output.write(payload)) {
              offset += payload.length;
              if (appending) {
                // Like a full replay, a resumed terminal lands on its newest
                // output once the missing tail has been parsed.
                appending = false;
                term.write("", () => {
                  if (!disposed) scrollToBottom();
                });
              }
            } else {
              stale = true;
              restoring = true;
            }
          } else if (frame[0] === 4) {
            const position = StreamPositionSchema.parse(
              JSON.parse(decoder.decode(payload)),
            );
            historyTruncated = position.truncated;
            resetPosition = undefined;
            appending = false;
            if (position.mode === "reset") resetPosition = position;
            else if (
              synced &&
              position.generation === generation &&
              position.start === offset
            ) {
              // The bytes that follow are exactly the missing tail.
              output.replay();
              stale = false;
              restoring = false;
              appending = position.end > position.start;
              if (!appending) scrollToBottom();
            } else {
              synced = false;
              ws.send(2, new Uint8Array());
            }
          } else if (frame[0] === 1) {
            const position = resetPosition;
            resetPosition = undefined;
            // A hidden page keeps its terminal and resume point unchanged.
            if (document.hidden) stale = true;
            else {
              stale = false;
              restore(payload);
              synced = !!position;
              generation = position?.generation;
              offset = position?.end ?? 0;
            }
          } else if (frame[0] === 5) {
            const status = SubmissionStatusSchema.parse(
              JSON.parse(decoder.decode(payload)),
            );
            submissions.get(status.id)?.(status.state);
            if (status.state !== "received") submissions.delete(status.id);
          } else if (frame[0] === 6) {
            const wasControlling = controlling();
            control = ControlStateSchema.parse(
              JSON.parse(decoder.decode(payload)),
            );
            // Announce the scheme whenever this terminal starts answering
            // for the program: it may have asked before any browser was
            // attached, or been answered with another device's theme.
            if (
              control.colorSchemeReports &&
              (!schemeReports || (controlling() && !wasControlling))
            )
              reportScheme();
            schemeReports = control.colorSchemeReports;
            safeFit();
            sendSize();
          } else if (frame[0] === 2) {
            info = SessionInfoSchema.parse(JSON.parse(decoder.decode(payload)));
            document.title = info.title + " · WebMux";
          } else if (frame[0] === 3) {
            exited = true;
            exitCode = ExitSchema.parse(
              JSON.parse(decoder.decode(payload)),
            ).exitCode;
            if (stale && !document.hidden) restoreExited();
          }
        },
        (state) => {
          connection = state;
          if (state === "connected") {
            safeFit();
            // The PTY grid follows only after the server confirms control.
            if (submissions.size)
              ws.send(
                4,
                encoder.encode(
                  JSON.stringify({ ids: [...submissions.keys()] }),
                ),
              );
          }
          if (state === "connecting" || state === "reconnecting") {
            // A synced terminal stays visible and is extended in place.
            if (!synced) restoring = true;
            schemeReports = false;
            for (const report of submissions.values()) report("unknown");
          }
          if (state === "missing") restoring = false;
        },
        (message) => {
          error = message;
        },
      );
      socket = ws;
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
        if (user) send(data);
        else ws.send(6, encoder.encode(data));
      });
      const selected = term.onSelectionChange(() => {
        selection = term.getSelection();
      });
      // OSC 52 is the application copy request handled by xterm's clipboard
      // addon. Selection copy (Ctrl+Shift+C and the selection toolbar) already
      // uses this same callback, which writes the browser clipboard.
      const oscClipboard = term.parser.registerOscHandler(52, (data) => {
        const text = osc52CopyText(data);
        if (text === undefined) return false;
        if (!replaying) clipboard.copySelection(text);
        return true;
      });
      // CSI ? 996 n asks for the current scheme. Replayed history must not
      // answer a query the program made long ago.
      const schemeQuery = term.parser.registerCsiHandler(
        { prefix: "?", final: "n" },
        (params) => {
          if (params.length !== 1 || params[0] !== 996) return false;
          if (!replaying) reportScheme();
          return true;
        },
      );
      const resize = term.onResize(sendSize);
      const scrolled = term.onScroll(updateBottom);
      const parsed = term.onWriteParsed(updateBottom);
      parent.addEventListener("scroll", updateBottom, { passive: true });
      let timer: number | undefined;
      const observer = new ResizeObserver(([entry]) => {
        if (disposed) return;
        if (webgl) {
          const pixels = entry?.devicePixelContentBoxSize?.[0];
          const css = entry?.contentRect;
          const dpr = window.devicePixelRatio || 1;
          // Chromium emulation can report physical pixels at the host DPR while
          // xterm renders at the emulated DPR. The WebGL addon then shrinks its
          // backing canvas without updating its render dimensions. DOM rendering
          // avoids that incompatible measurement; allow normal pixel rounding.
          if (
            pixels &&
            css.width > 0 &&
            css.height > 0 &&
            pixels.inlineSize > 0 &&
            pixels.blockSize > 0 &&
            (Math.abs(pixels.inlineSize - css.width * dpr) > 1 ||
              Math.abs(pixels.blockSize - css.height * dpr) > 1)
          ) {
            useDomRenderer();
          }
        }
        clearTimeout(timer);
        timer = window.setTimeout(() => safeFit(), 100);
      });
      const observeSize = () => {
        try {
          observer.observe(parent, { box: "device-pixel-content-box" });
        } catch {
          observer.observe(parent);
        }
      };
      let resolution = matchMedia(
        `(resolution: ${window.devicePixelRatio || 1}dppx)`,
      );
      const resolutionChanged = () => {
        if (disposed) return;
        resolution.removeEventListener("change", resolutionChanged);
        resolution = matchMedia(
          `(resolution: ${window.devicePixelRatio || 1}dppx)`,
        );
        resolution.addEventListener("change", resolutionChanged);
        // A DPR change need not change the CSS box. Request a fresh observation
        // rather than comparing the new DPR against stale physical dimensions.
        observer.unobserve(parent);
        observeSize();
      };
      resolution.addEventListener("change", resolutionChanged);
      observeSize();
      function restoreExited() {
        void apiBuffer(id)
          .then((payload) => {
            if (!disposed) {
              stale = false;
              synced = false;
              restore(payload);
            }
          })
          .catch((err) => {
            if (!disposed) error = (err as Error).message;
          });
      }
      const visible = () => {
        // Live sessions reattach in connect(); exited sessions cannot reopen that socket.
        if (!document.hidden && stale && exited) {
          restoreExited();
        }
      };
      document.addEventListener("visibilitychange", visible);

      const detachTouch = attachTouchGestures(term, parent, {
        fontSize: () => fontSize,
        setFontSize,
        hasSelection: () => Boolean(selection),
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
          const sequences: Record<string, string> = /Mac/.test(
            navigator.platform,
          )
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
      let remeasureFrame = 0;
      const remeasure = () => {
        cancelAnimationFrame(remeasureFrame);
        remeasureFrame = requestAnimationFrame(() => {
          if (disposed) return;
          // Later unicode-range slices must be rasterized into the cell grid
          // that was measured from the Latin face.
          webgl?.clearTextureAtlas();
          const current = term.options.fontFamily;
          term.options.fontFamily =
            current === terminalFontFamilyCss
              ? terminalFontFamilyAlt
              : terminalFontFamilyCss;
          term.options.fontFamily = current ?? terminalFontFamilyCss;
          safeFit();
        });
      };
      document.fonts.addEventListener("loadingdone", remeasure);
      safeFit();
      if (!hasTouchInput()) term.focus();
      return () => {
        disposed = true;
        cancelAnimationFrame(remeasureFrame);
        cancelAnimationFrame(restoreFrame);
        document.fonts.removeEventListener("loadingdone", remeasure);
        clearTimeout(timer);
        observer.disconnect();
        resolution.removeEventListener("change", resolutionChanged);
        document.removeEventListener("visibilitychange", visible);
        detachTouch();
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
        resize.dispose();
        scrolled.dispose();
        parsed.dispose();
        parent.removeEventListener("scroll", updateBottom);
        selected.dispose();
        oscClipboard.dispose();
        schemeQuery.dispose();
        colors.disconnect();
        output.dispose();
        ws.close();
        const dying = term;
        // Viewport.reset() queues syncScrollArea for the next frame, and that
        // frame reads RenderService.dimensions. Disposing in this turn removes
        // the renderer first, so the callback throws after a restore or theme
        // change. Let that frame, and the scroll refresh it may schedule, run
        // before the terminal is disposed.
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            // Older WebGL addons recreate the DOM renderer on dispose, so
            // release the addon while the terminal's services still exist.
            webgl?.dispose();
            webgl = undefined;
            dying.dispose();
          });
        });
        terminal = undefined;
        socket = undefined;
        synced = false;
        parent.classList.remove("observing");
        atBottom = true;
        fit = undefined;
        restoreBuffer = undefined;
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
      return (
        !!terminal &&
        connection === "connected" &&
        !restoring &&
        exitCode === null
      );
    },
    get info() {
      return info;
    },
    get connection() {
      return connection;
    },
    get restoring() {
      return restoring;
    },
    get exitCode() {
      return exitCode;
    },
    get fontSize() {
      return fontSize;
    },
    get modifiers() {
      return modifiers;
    },
    get error() {
      return error;
    },
    get selection() {
      return selection;
    },
    get atBottom() {
      return atBottom;
    },
    get historyTruncated() {
      return historyTruncated;
    },
    get control() {
      return control;
    },
    get controlling() {
      return controlling();
    },
    get clientId() {
      return identity.id;
    },
    // Takes input and grid control for this browser tab.
    claim() {
      if (control && !controlling()) socket?.send(5, new Uint8Array());
    },
    // Sends text and an optional Enter as one acknowledged submission. The
    // body is prepared like xterm's paste; `report` receives each state.
    submit(
      submissionId: string,
      text: string,
      enter: boolean,
      report: (state: SubmissionProgress) => void,
    ) {
      const run = generation ?? info?.generation;
      if (
        !terminal ||
        !socket ||
        !run ||
        connection !== "connected" ||
        restoring ||
        exitCode !== null
      )
        return false;
      modifiers = { ctrl: false, alt: false };
      const body = preparePaste(text, terminal.modes.bracketedPasteMode);
      submissions.set(submissionId, report);
      if (
        !socket.send(
          3,
          encodeSubmission(submissionId, run, body, enter ? "\r" : ""),
        )
      ) {
        submissions.delete(submissionId);
        return false;
      }
      report("sending");
      terminal.scrollToBottom();
      return true;
    },
    // Follows a submission whose acknowledgement may have been lost.
    track(submissionId: string, report: (state: SubmissionProgress) => void) {
      submissions.set(submissionId, report);
      if (connection === "connected")
        socket?.send(
          4,
          encoder.encode(JSON.stringify({ ids: [submissionId] })),
        );
    },
    untrack(submissionId: string) {
      submissions.delete(submissionId);
    },
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
      if (
        !terminal ||
        connection !== "connected" ||
        restoring ||
        exitCode !== null
      )
        return false;
      pasteResult = false;
      modifiers = { ctrl: false, alt: false };
      terminal.paste(text);
      return pasteResult;
    },
    keyboard() {
      if (document.activeElement === terminal?.textarea) terminal?.blur();
      else {
        if (control && !controlling()) socket?.send(5, new Uint8Array());
        terminal?.focus();
      }
    },
    clear() {
      terminal?.clear();
    },
    resync() {
      if (exitCode !== null) {
        void apiBuffer(id)
          .then((payload) => restoreBuffer?.(payload))
          .catch((err) => {
            error = (err as Error).message;
          });
      } else socket?.send(2, new Uint8Array());
    },
    fit: safeFit,
    scrollToBottom,
    focus() {
      terminal?.focus();
    },
  };
}
