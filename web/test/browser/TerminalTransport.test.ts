// Send-queue backpressure, PTY geometry, control, device replies, session
// frames, submissions and invalid server frames.
import { expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte/pure";
import type { SubmissionProgress } from "../../src/schemas";
import Harness from "./TerminalHarness.svelte";
import {
  expectDomText,
  FixtureSocket,
  geometry,
  host,
  inputText,
  installTerminalFixture,
  observeGlyphPixels,
  openTerminal,
  openTerminalApi,
  reconnected,
  snapshot,
  sockets,
} from "./terminal-fixture";

installTerminalFixture();

test("saturated input reports rejection and clears it after a successful send", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  socket.bufferedAmount = 1024 * 1024;
  await page.getByRole("button", { name: "Focus terminal input" }).click();
  await userEvent.keyboard("x");
  await expect
    .element(page.getByRole("status", { name: "Terminal error" }))
    .toHaveTextContent("终端输入未发送：连接不可用或发送队列已满，请重试。");
  expect(inputText(socket)).toBe("");
  socket.bufferedAmount = 0;
  await userEvent.keyboard("y");
  await expect.poll(() => inputText(socket)).toBe("y");
  await expect
    .element(page.getByRole("status", { name: "Terminal error" }))
    .toHaveTextContent("");
});

test("initial terminal geometry is delivered after the send queue drains", async () => {
  observeGlyphPixels(true);
  await render(Harness);
  await expect.poll(() => sockets.length).toBe(1);
  const socket = sockets[0];
  socket.bufferedAmount = 1024 * 1024;
  socket.open();
  socket.receive(1, snapshot);
  // Only the controlling client resizes the PTY.
  socket.grantControl();
  await expect
    .element(page.getByRole("status", { name: "Terminal restoration" }))
    .toHaveTextContent("ready");
  expect(socket.sent).toEqual([]);

  socket.bufferedAmount = 0;
  await expect.poll(() => socket.sent.length).toBe(1);
  expect(socket.sent[0][0]).toBe(1);
  const size = JSON.parse(new TextDecoder().decode(socket.sent[0].subarray(1)));
  expect(size.cols).toBeGreaterThan(0);
  expect(size.rows).toBeGreaterThan(0);
  expect(sockets).toEqual([socket]);
  expect(socket.closeCount).toBe(0);
});

test("terminal resize and resync recover on the same socket after saturation", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  const original = geometry(socket);
  socket.sent = [];
  socket.bufferedAmount = 1024 * 1024;
  const bounds = () =>
    host().querySelector(".xterm-screen")!.getBoundingClientRect();
  host().style.width = "600px";
  await expect
    .poll(() => bounds().width)
    .toBeGreaterThan(original.bounds.width);
  const intermediateWidth = bounds().width;
  host().style.width = "800px";
  host().style.height = "650px";
  await expect.poll(() => bounds().width).toBeGreaterThan(intermediateWidth);
  await expect.poll(() => bounds().height).toBeLessThan(original.bounds.height);
  const expected = {
    cols: Math.round(bounds().width / original.cell),
    rows: Math.round(bounds().height / original.row),
  };
  await page
    .getByRole("button", { name: "Resync terminal", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Resync terminal", exact: true })
    .click();
  expect(socket.sent).toEqual([]);

  socket.bufferedAmount = 0;
  await expect.poll(() => socket.sent.length).toBe(2);
  const resize = socket.sent.find((frame) => frame[0] === 1)!;
  expect(JSON.parse(new TextDecoder().decode(resize.subarray(1)))).toEqual(
    expected,
  );
  expect(socket.sent.filter((frame) => frame[0] === 2)).toEqual([
    new Uint8Array([2]),
  ]);
  expect(sockets).toEqual([socket]);
  expect(socket.closeCount).toBe(0);
});

test("an observer sizes to the controller's grid before parsing cursor-addressed history", async () => {
  observeGlyphPixels(true);
  await render(Harness);
  await expect.poll(() => sockets.length, { timeout: 5000 }).toBe(1);
  const socket = sockets[0];
  socket.open();
  // The server sends control before the replay: another client controls a
  // 120-column grid, far wider than this 400 px host fits.
  socket.receive(
    6,
    JSON.stringify({
      controller: {
        clientId: "another-client",
        label: "desk",
        since: new Date().toISOString(),
      },
      cols: 120,
      rows: 30,
      colorSchemeReports: false,
    }),
  );
  socket.receive(1, "\x1b[1;100Hfar-right");
  await expect
    .poll(() =>
      (host().querySelector(".xterm-rows > div")?.textContent ?? "")
        .replaceAll("\u00a0", " ")
        .trimEnd(),
    )
    .toMatch(/^ {99}far-right$/);
  expect(host().classList.contains("observing")).toBe(true);
  // Observers never resize the PTY.
  expect(socket.sent.filter((frame) => frame[0] === 1)).toEqual([]);
});

test("an observer panning above the controller's newest rows is viewing history", async () => {
  observeGlyphPixels(true);
  await render(Harness);
  await expect.poll(() => sockets.length, { timeout: 5000 }).toBe(1);
  const socket = sockets[0];
  socket.open();
  // Sixty rows are taller than the 750 px host, so the observer pans it.
  socket.receive(
    6,
    JSON.stringify({
      controller: {
        clientId: "another-client",
        label: "desk",
        since: new Date().toISOString(),
      },
      cols: 40,
      rows: 60,
      colorSchemeReports: false,
    }),
  );
  socket.receive(1, "\x1b[60;1Hbottom-row");
  await expect.poll(() => host().classList.contains("observing")).toBe(true);
  await expect
    .poll(() => host().scrollHeight)
    .toBeGreaterThan(host().clientHeight);
  // Restoration ends by scrolling to the newest rows; pan only after it.
  await expect
    .element(page.getByRole("status", { name: "Terminal restoration" }))
    .toHaveTextContent("ready");
  const position = page.getByRole("status", { name: "Terminal position" });
  await expect.element(position).toHaveTextContent("latest");
  host().scrollTop = 0;
  await expect.element(position).toHaveTextContent("history");
  await page.getByRole("button", { name: "Scroll to latest" }).click();
  await expect.element(position).toHaveTextContent("latest");
  expect(host().scrollTop + host().clientHeight).toBeGreaterThanOrEqual(
    host().scrollHeight - 1,
  );
});

test("automatic replies to device queries are sent apart from user input, even when the bytes match a key", async () => {
  const { socket } = await openTerminal();
  const frames = (kind: number) =>
    socket.sent
      .filter((frame) => frame[0] === kind)
      .map((frame) => new TextDecoder().decode(frame.subarray(1)));
  // Primary device attributes and a cursor position report at row 1.
  socket.receive(0, "\x1b[H\x1b[c\x1b[6n");
  // oxlint-disable-next-line no-control-regex -- Match the terminal's device-attributes escape sequence.
  await expect.poll(() => frames(6).join("")).toMatch(/\x1b\[\?[\d;]+c/);
  await expect.poll(() => frames(6).join("")).toContain("\x1b[1;1R");
  expect(frames(0)).toEqual([]);
  // Shift+F3 produces the same shape as a cursor report, but it is a key.
  const textarea = host().querySelector("textarea")!;
  textarea.focus();
  textarea.dispatchEvent(
    new KeyboardEvent("keydown", {
      key: "F3",
      code: "F3",
      keyCode: 114,
      shiftKey: true,
      bubbles: true,
      cancelable: true,
    }),
  );
  await expect.poll(() => frames(0)).toEqual(["\x1b[1;2R"]);
});

test("color scheme reports follow the page theme once the program enables them", async () => {
  const root = document.documentElement;
  root.classList.remove("dark");
  const { socket } = await openTerminal();
  const reports = () =>
    socket.sent
      .filter((frame) => frame[0] === 6)
      .map((frame) => new TextDecoder().decode(frame.subarray(1)))
      .filter((reply) => reply.startsWith("\x1b[?997;"));
  // Without DEC mode 2031 a shell would read the report as typed text.
  root.classList.add("dark");
  // Enabling it announces the current scheme, so a program that queried
  // before this browser attached learns it now.
  socket.grantControl(80, 24, true);
  await expect.poll(reports).toEqual(["\x1b[?997;1n"]);
  root.classList.remove("dark");
  await expect.poll(reports).toEqual(["\x1b[?997;1n", "\x1b[?997;2n"]);
  // A query in replayed history was answered when it was live.
  socket.receive(1, "\x1b[?996n");
  socket.receive(0, "\x1b[?996n");
  await expect
    .poll(reports)
    .toEqual(["\x1b[?997;1n", "\x1b[?997;2n", "\x1b[?997;2n"]);
  expect(socket.sent.filter((frame) => frame[0] === 0)).toEqual([]);
});

const generation = "0123456789abcdef";

function sessionInfo(title: string) {
  const now = new Date().toISOString();
  return JSON.stringify({
    id: "abcdefgh",
    generation,
    title,
    cwd: "/tmp",
    preset: "shell",
    cmd: ["bash"],
    status: "running",
    exitCode: null,
    preview: "",
    createdAt: now,
    lastActiveAt: now,
    cols: 80,
    rows: 24,
    clients: 1,
    workspace: null,
    attention: null,
    reportedCwd: null,
  });
}

function framesOf(socket: FixtureSocket, kind: number) {
  return socket.sent
    .filter((frame) => frame[0] === kind)
    .map((frame) => new TextDecoder().decode(frame.subarray(1)));
}

test("submissions carry the run generation and follow acknowledgements across reconnects until settled", async () => {
  observeGlyphPixels(true);
  const title = document.title;
  const { socket: first, api } = await openTerminalApi();
  const reports: SubmissionProgress[] = [];
  const report = (state: SubmissionProgress) => reports.push(state);
  // Without a known run the server could not reject a stale submission.
  expect(api.submit("submission-1", "echo 1", true, report)).toBe(false);
  first.receive(2, sessionInfo("fixture title"));
  expect(api.info?.generation).toBe(generation);
  expect(document.title).toBe("fixture title · WebMux");

  api.toggleModifier("ctrl");
  expect(api.submit("submission-1", "echo 1\nok", true, report)).toBe(true);
  expect(api.modifiers).toEqual({ ctrl: false, alt: false });
  expect(reports).toEqual(["sending"]);
  // One JSON header line, then the paste-prepared body and the Enter key.
  const header = JSON.stringify({
    id: "submission-1",
    generation,
    bodyBytes: 9,
  });
  expect(framesOf(first, 3)).toEqual([`${header}\necho 1\rok\r`]);
  first.receive(5, JSON.stringify({ id: "submission-1", state: "received" }));
  expect(reports).toEqual(["sending", "received"]);

  first.drop();
  expect(reports).toEqual(["sending", "received", "unknown"]);
  // Every connection attempt reports it again until the server answers.
  const second = await reconnected(2);
  expect(reports).toEqual(["sending", "received", "unknown", "unknown"]);
  second.open();
  const pending = JSON.stringify({ ids: ["submission-1"] });
  expect(framesOf(second, 4)).toEqual([pending]);
  second.receive(5, JSON.stringify({ id: "submission-1", state: "written" }));
  expect(reports.at(-1)).toBe("written");
  const settled = reports.length;

  const followed: SubmissionProgress[] = [];
  api.track("submission-2", (state) => followed.push(state));
  expect(framesOf(second, 4)).toEqual([
    pending,
    JSON.stringify({ ids: ["submission-2"] }),
  ]);
  api.untrack("submission-2");
  second.drop();
  const third = await reconnected(3);
  third.open();
  expect(framesOf(third, 4)).toEqual([]);
  expect(followed).toEqual([]);
  expect(reports).toHaveLength(settled);
  expect(inputText(first) + inputText(second)).toBe("");
  document.title = title;
});

test("an exited session refuses input, stays closed and resyncs from the retained buffer", async () => {
  observeGlyphPixels(true);
  const title = document.title;
  const { socket, api } = await openTerminalApi();
  socket.receive(2, sessionInfo("exit fixture"));
  expect(api.ready).toBe(true);
  socket.receive(3, JSON.stringify({ exitCode: 3 }));
  expect(api.exitCode).toBe(3);
  expect(api.ready).toBe(false);
  expect(api.submit("submission-3", "late", true, () => {})).toBe(false);
  expect(api.paste("late")).toBe(false);
  socket.drop(4410);
  await expect
    .element(page.getByRole("status", { name: "Terminal connection" }))
    .toHaveTextContent("exited");

  const fetched = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValue(
      new Response(new TextEncoder().encode("retained after exit\r\n")),
    );
  await page
    .getByRole("button", { name: "Resync terminal", exact: true })
    .click();
  await expectDomText("retained after exit");
  expect(fetched).toHaveBeenCalledTimes(1);
  expect(String(fetched.mock.calls[0][0])).toContain(
    "/sessions/terminal-render-fixture/buffer",
  );
  expect(sockets).toEqual([socket]);
  expect(framesOf(socket, 0)).toEqual([]);
  expect(framesOf(socket, 2)).toEqual([]);
  expect(framesOf(socket, 3)).toEqual([]);
  document.title = title;
});

test("claim and keyboard take control only from another controller", async () => {
  observeGlyphPixels(true);
  const { socket, api } = await openTerminalApi();
  const textarea = host().querySelector<HTMLTextAreaElement>(
    ".xterm-helper-textarea",
  )!;
  expect(api.clientId).toBe(new URL(socket.url).searchParams.get("client"));
  expect(api.controlling).toBe(true);
  api.claim();
  expect(framesOf(socket, 5)).toEqual([]);

  socket.receive(
    6,
    JSON.stringify({
      controller: {
        clientId: "another-client",
        label: "desk",
        since: new Date().toISOString(),
      },
      cols: 80,
      rows: 24,
      colorSchemeReports: false,
    }),
  );
  expect(api.controlling).toBe(false);
  expect(api.control?.controller?.label).toBe("desk");
  api.claim();
  expect(framesOf(socket, 5)).toEqual([""]);
  textarea.blur();
  api.keyboard();
  expect(document.activeElement).toBe(textarea);
  expect(framesOf(socket, 5)).toEqual(["", ""]);
  // A second press hides the keyboard without claiming again.
  api.keyboard();
  expect(document.activeElement).not.toBe(textarea);
  expect(framesOf(socket, 5)).toEqual(["", ""]);
  expect(inputText(socket)).toBe("");
});

test("an invalid server frame is reported and the terminal reattaches on a fresh socket", async () => {
  observeGlyphPixels(true);
  const { socket: first } = await openTerminal();
  first.receive(
    6,
    JSON.stringify({
      controller: null,
      cols: 0,
      rows: 24,
      colorSchemeReports: false,
    }),
  );
  await expect
    .poll(
      () =>
        document.querySelector('[aria-label="Terminal error"]')?.textContent,
    )
    .not.toBe("");
  expect(first.closeCount).toBe(1);
  const second = await reconnected(2);
  second.open();
  second.receive(1, "after invalid frame\r\n");
  second.grantControl();
  await expectDomText("after invalid frame");
  expect(inputText(first) + inputText(second)).toBe("");
  // ws.ts closes the rejected socket, then again when it reopens; a real
  // WebSocket ignores the second call.
  expect(first.closeCount).toBe(2);
  first.closeCount = 1;
});
