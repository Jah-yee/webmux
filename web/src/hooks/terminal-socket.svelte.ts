import type { Terminal } from "@xterm/xterm";
import { apiBuffer } from "../api";
import {
  ControlStateSchema,
  ExitSchema,
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
import { osc52CopyText } from "../terminalClipboard";
import { createOutputBudget } from "../terminal-output";

const inputSendError = "终端输入未发送：连接不可用或发送队列已满，请重试。";

// What the session socket needs from the mounted terminal view.
export interface TerminalSocketView {
  // Fits the grid to the host, or to the controller's grid while observing.
  fit(): void;
  // Fits, then shows the newest output.
  scrollToBottom(): void;
  isVisible(): boolean;
  // An application copy request (OSC 52) made by live output.
  copy(text: string): void;
}

export interface TerminalSocketAttachment {
  // Re-announces the colour scheme after a page theme change.
  themeChanged(): void;
  dispose(): void;
}

// The terminal stream of one session (`/ws/s/{id}`, frames in
// docs/protocol.md, mirrored by internal/hub): output continuity and replay,
// session info, exit, control, submissions and replay-aware parser handlers.
// State outlives the mounted terminal; `attach` binds one xterm instance.
export function createTerminalSocket(id: string) {
  let info = $state<SessionInfo>();
  let connection = $state<Connection>("connecting");
  let restoring = $state(true);
  let exitCode = $state<number | null>(null);
  let error = $state("");
  let historyTruncated = $state(false);
  let control = $state<ControlState>();
  let terminal: Terminal | undefined;
  let socket: SocketConnection | undefined;
  let restoreBuffer: ((payload: Uint8Array) => void) | undefined;
  const encoder = new TextEncoder();
  const decoder = new TextDecoder();
  const identity = clientIdentity();
  // Output continuity: `offset` counts the stream bytes handed to xterm since
  // the snapshot of run `generation`. While `synced`, a reconnect asks the
  // server for only the bytes after it.
  let generation: string | undefined;
  let offset = 0;
  let synced = false;
  const controlling = () =>
    !!control && control.controller?.clientId === identity.id;
  // Unconfirmed submissions, re-queried after every reconnect.
  const submissions = new Map<string, (state: SubmissionProgress) => void>();
  const accepting = () =>
    connection === "connected" && !restoring && exitCode === null;
  const submission = () => {
    const run = generation ?? info?.generation;
    if (!terminal || !socket || !run || !accepting()) return undefined;
    return { term: terminal, ws: socket, run };
  };

  function attach(
    term: Terminal,
    view: TerminalSocketView,
  ): TerminalSocketAttachment {
    let disposed = false;
    let stale = false;
    let exited = false;
    let restoreFrame = 0;
    let restoreVersion = 0;
    terminal = term;
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
          view.scrollToBottom();
          restoring = output.recovering;
        });
      });
    };
    restoreBuffer = restore;
    const sendSize = () => {
      if (controlling() && view.isVisible() && term.cols > 0 && term.rows > 0)
        ws.send(
          1,
          encoder.encode(JSON.stringify({ cols: term.cols, rows: term.rows })),
        );
    };
    // DEC mode 2031 reports: CSI ? 997 ; 1 n is dark, 2 is light. Programs
    // answer them by re-querying colors (OSC 10/11), which xterm answers
    // from the theme. Sent as device replies, so only the responsible
    // connection's report reaches the PTY.
    const root = document.documentElement;
    // Read the class directly: the renderer's theme observer runs later, and
    // a control frame may arrive in between.
    let reportedDark: boolean | undefined;
    const reportScheme = () => {
      reportedDark = root.classList.contains("dark");
      term.input(`\x1b[?997;${reportedDark ? 1 : 2}n`, false);
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
                if (!disposed) view.scrollToBottom();
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
            if (!appending) view.scrollToBottom();
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
          view.fit();
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
          view.fit();
          // The PTY grid follows only after the server confirms control.
          if (submissions.size)
            ws.send(
              4,
              encoder.encode(JSON.stringify({ ids: [...submissions.keys()] })),
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
    const resize = term.onResize(sendSize);
    // OSC 52 is the application copy request handled by xterm's clipboard
    // addon. Selection copy (Ctrl+Shift+C and the selection toolbar) already
    // uses this same callback, which writes the browser clipboard.
    const oscClipboard = term.parser.registerOscHandler(52, (data) => {
      const text = osc52CopyText(data);
      if (text === undefined) return false;
      if (!replaying) view.copy(text);
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

    return {
      themeChanged() {
        if (disposed) return;
        if (
          control?.colorSchemeReports &&
          reportedDark !== root.classList.contains("dark")
        )
          reportScheme();
      },
      dispose() {
        disposed = true;
        cancelAnimationFrame(restoreFrame);
        document.removeEventListener("visibilitychange", visible);
        resize.dispose();
        oscClipboard.dispose();
        schemeQuery.dispose();
        output.dispose();
        ws.close();
        terminal = undefined;
        socket = undefined;
        synced = false;
        restoreBuffer = undefined;
      },
    };
  }

  return {
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
    get error() {
      return error;
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
    // Until the server reports control, fit locally without resizing the PTY.
    get observing() {
      return !!control && !controlling();
    },
    // Connected, restored and still running: input can reach the program.
    get accepting() {
      return accepting();
    },
    get submittable() {
      return !!submission();
    },
    get clientId() {
      return identity.id;
    },
    attach,
    // Sends user input. A rejected send is reported, never queued.
    sendInput(input: string) {
      const sent = socket?.send(0, encoder.encode(input)) ?? false;
      if (!sent) error = inputSendError;
      else if (error === inputSendError) error = "";
      return sent;
    },
    // Sends one of xterm's automatic replies to a device query.
    sendReply(reply: string) {
      socket?.send(6, encoder.encode(reply));
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
      const target = submission();
      if (!target) return false;
      const body = preparePaste(text, target.term.modes.bracketedPasteMode);
      submissions.set(submissionId, report);
      if (
        !target.ws.send(
          3,
          encodeSubmission(submissionId, target.run, body, enter ? "\r" : ""),
        )
      ) {
        submissions.delete(submissionId);
        return false;
      }
      report("sending");
      target.term.scrollToBottom();
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
    // Replaces the terminal contents with the server's current replay; an
    // exited session's socket is closed, so its buffer is fetched instead.
    resync() {
      if (exitCode !== null) {
        void apiBuffer(id)
          .then((payload) => restoreBuffer?.(payload))
          .catch((err) => {
            error = (err as Error).message;
          });
      } else socket?.send(2, new Uint8Array());
    },
  };
}
