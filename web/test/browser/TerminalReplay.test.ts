// Output budget, snapshot replay and stream continuity, exit restoration,
// hidden pages and OSC 52 copies.
import { expect, test, vi } from "vitest";
import { page } from "vitest/browser";
import { render } from "vitest-browser-svelte/pure";
import { Terminal } from "@xterm/xterm";
import type { TerminalController } from "../../src/hooks/terminal.svelte";
import Harness from "./TerminalHarness.svelte";
import {
  domText,
  expectDomText,
  FixtureSocket,
  inputText,
  installTerminalFixture,
  observeGlyphPixels,
  openTerminal,
  prompt,
  reconnected,
  snapshot,
  sockets,
} from "./terminal-fixture";

installTerminalFixture();

test("a burst of live output drains before one replay and resumes rendering", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  const chunk = "x".repeat(32768);
  for (let i = 0; i < 160; i++) socket.receive(0, chunk);
  await expect
    .poll(() => socket.sent.filter((frame) => frame[0] === 2).length, {
      timeout: 15000,
    })
    .toBe(1);
  socket.receive(1, "recovered snapshot\r\n");
  await expectDomText("recovered snapshot");
  socket.receive(0, "live output resumes\r\n");
  await expectDomText("live output resumes");
  expect(inputText(socket)).toBe("");
}, 20000);

test("drained output waits for an already scheduled reconnect", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  const schedule = window.setTimeout.bind(window);
  vi.spyOn(window, "setTimeout").mockImplementation(
    (handler, timeout, ...args) =>
      schedule(
        handler,
        timeout && timeout >= 250 && timeout <= 500 ? 60000 : timeout,
        ...args,
      ),
  );
  const chunk = "x".repeat(32768);
  const marker = "\r\nqueued output parsed\r\n";
  for (let i = 0; i < 128; i++)
    socket.receive(
      0,
      i === 127 ? chunk.slice(0, -marker.length) + marker : chunk,
    );
  socket.receive(0, chunk);
  socket.readyState = FixtureSocket.CLOSED;
  socket.onclose?.(new CloseEvent("close", { code: 1006 }));
  await expectDomText("queued output parsed");
  expect(sockets).toHaveLength(1);
  expect(socket.sent.filter((frame) => frame[0] === 2)).toHaveLength(0);
}, 20000);

test("exit during output recovery restores the final retained buffer", async () => {
  observeGlyphPixels(true);
  const resets = vi.spyOn(Terminal.prototype, "reset");
  const { socket } = await openTerminal();
  const fetched = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValue(
      new Response(new TextEncoder().encode("final retained output\r\n")),
    );
  const chunk = "x".repeat(32768);
  for (let i = 0; i < 160; i++) socket.receive(0, chunk);
  socket.receive(3, JSON.stringify({ exitCode: 0 }));
  await expectDomText("final retained output");
  const term = resets.mock.contexts.at(-1)!;
  const buffer = term.buffer.active;
  expect(buffer.length).toBe(term.rows);
  expect(buffer.baseY).toBe(0);
  expect(buffer.getLine(0)?.translateToString(true)).toBe(
    "final retained output",
  );
  expect(buffer.cursorX).toBe(0);
  expect(buffer.cursorY).toBe(1);
  expect(fetched).toHaveBeenCalledTimes(1);
  expect(inputText(socket)).toBe("");
}, 20000);

function osc52(
  selection: string,
  text: string,
  terminator: "bel" | "st" = "bel",
) {
  const bytes = new TextEncoder().encode(text);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  const encoded = btoa(binary).replace(/=+$/, "");
  const wrapped = encoded.replace(/(.{4})/g, "$1\n");
  const end = terminator === "bel" ? "\x07" : "\x1b\\";
  return `\x1b]52;${selection};${wrapped}${end}`;
}

async function expectCopied(text: string) {
  await expect
    .poll(
      () =>
        document.querySelector('[aria-label="Copied terminal text"]')
          ?.textContent,
    )
    .toBe(text);
}

test("application OSC 52 copy is reported without terminal input or disturbing wide text", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  socket.receive(0, `prefix 中文 tail\r\n${osc52("c", "中文 tail\nsecond")}`);
  await expectDomText("prefix 中文 tail");
  await expectCopied("中文 tail\nsecond");
  const copied = inputText(socket);
  socket.receive(0, "\x1b]52;c;?\x07");
  socket.receive(0, "\x1b]52;0;YQ==\x07");
  await expectCopied("中文 tail\nsecond");
  socket.receive(0, osc52("p", "primary-中文", "st"));
  await expectCopied("primary-中文");
  expect(inputText(socket)).toBe(copied);
  expect(domText()).toContain("prefix 中文 tail");
  expect(domText()).not.toContain("primary-中文");
});

test("replayed OSC 52 copies are not repeated while live copies still are", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  const copies = () =>
    document.querySelector('[aria-label="Copy requests"]')?.textContent;
  const replayed = async (text: string) => {
    socket.receive(1, `${snapshot}${osc52("c", "replayed-old")}${text}\r\n`);
    await expectDomText(text);
    await expect
      .element(page.getByRole("status", { name: "Terminal restoration" }))
      .toHaveTextContent("ready");
  };
  await replayed("first replay");
  expect(copies()).toBe("0");
  socket.receive(0, osc52("c", "live-new"));
  await expectCopied("live-new");
  expect(copies()).toBe("1");
  await replayed("second replay");
  socket.receive(0, "after replay\r\n");
  await expectDomText("after replay");
  expect(copies()).toBe("1");
  expect(inputText(socket)).toBe("");
});

const generation = "0123456789abcdef";
const byteLength = (text: string) => new TextEncoder().encode(text).length;

function position(
  mode: "reset" | "append",
  start: number,
  end: number,
  run = generation,
  truncated = false,
) {
  return JSON.stringify({ generation: run, mode, start, end, truncated });
}

function restoration() {
  return page.getByRole("status", { name: "Terminal restoration" });
}

// Opens like the real server: a reset position, then its snapshot, so the
// terminal knows the stream offset it can resume from.
async function openSyncedTerminal(truncated = false) {
  let api: TerminalController | undefined;
  await render(Harness, { expose: (terminal) => (api = terminal) });
  await expect.poll(() => sockets.length, { timeout: 5000 }).toBe(1);
  const socket = sockets[0];
  expect(new URL(socket.url).searchParams.has("generation")).toBe(false);
  socket.open();
  socket.receive(
    4,
    position("reset", 0, byteLength(snapshot), generation, truncated),
  );
  socket.receive(1, snapshot);
  socket.grantControl();
  await expect.element(restoration()).toHaveTextContent("ready");
  return { socket, api: api! };
}

test("a synced terminal resumes from its stream offset and appends only the missing tail", async () => {
  observeGlyphPixels(true);
  const { socket: first, api } = await openSyncedTerminal();
  expect(api.historyTruncated).toBe(false);
  const live = "live before drop\r\n";
  first.receive(0, live);
  await expectDomText("live before drop");
  const offset = byteLength(snapshot) + byteLength(live);

  first.drop();
  await expect
    .element(page.getByRole("status", { name: "Terminal connection" }))
    .toHaveTextContent("reconnecting");
  // A synced terminal stays visible while it reattaches.
  await expect.element(restoration()).toHaveTextContent("ready");
  const second = await reconnected(2);
  const query = new URL(second.url).searchParams;
  expect(query.get("generation")).toBe(generation);
  expect(query.get("offset")).toBe(String(offset));
  second.open();
  const tail = "missing tail\r\n";
  second.receive(4, position("append", offset, offset + byteLength(tail)));
  second.receive(0, tail);
  second.grantControl();
  await expectDomText("missing tail");
  expect(domText()).toContain("live before drop");
  expect(domText()).toContain(prompt);
  await expect.element(restoration()).toHaveTextContent("ready");

  // The appended tail advances the resume point too.
  second.drop();
  const third = await reconnected(3);
  expect(new URL(third.url).searchParams.get("offset")).toBe(
    String(offset + byteLength(tail)),
  );
  for (const socket of [first, second, third])
    expect(socket.sent.filter((frame) => frame[0] === 2)).toEqual([]);
  expect(inputText(second)).toBe("");
});

test("a resume point from another run requests a fresh snapshot and forgets the old offset", async () => {
  observeGlyphPixels(true);
  const { socket: first, api } = await openSyncedTerminal(true);
  expect(api.historyTruncated).toBe(true);
  first.receive(0, "old run output\r\n");
  await expectDomText("old run output");
  first.drop();
  const second = await reconnected(2);
  second.open();
  const offset = Number(new URL(second.url).searchParams.get("offset"));
  second.receive(4, position("append", offset, offset + 4, "fedcba9876543210"));
  // A mismatched position is not trusted: ask for a snapshot instead.
  await expect
    .poll(() => second.sent.filter((frame) => frame[0] === 2))
    .toEqual([new Uint8Array([2])]);
  second.receive(4, position("reset", 0, 11, "fedcba9876543210"));
  second.receive(1, "fresh run\r\n");
  await expectDomText("fresh run");
  expect(domText()).not.toContain("old run output");
  expect(api.historyTruncated).toBe(false);

  // A snapshot without a position leaves nothing to resume from.
  second.receive(1, "unpositioned\r\n");
  await expectDomText("unpositioned");
  second.drop();
  const third = await reconnected(3);
  expect(new URL(third.url).searchParams.has("generation")).toBe(false);
  expect(new URL(third.url).searchParams.has("offset")).toBe(false);
});

test("output and snapshots arriving while the page is hidden wait for the foreground replay", async () => {
  observeGlyphPixels(true);
  const { socket: first } = await openTerminal();
  const writes = vi.spyOn(Terminal.prototype, "write");
  const hidden = vi.spyOn(Document.prototype, "hidden", "get");
  hidden.mockReturnValue(true);
  first.receive(0, "missed while hidden\r\n");
  first.receive(1, "hidden snapshot\r\n");
  // Neither frame reaches xterm; the visible terminal keeps its contents.
  expect(writes).not.toHaveBeenCalled();
  expect(domText()).toContain(prompt);

  hidden.mockReturnValue(false);
  document.dispatchEvent(new Event("visibilitychange"));
  const second = await reconnected(2);
  expect(first.closeCount).toBe(1);
  await expect.element(restoration()).toHaveTextContent("restoring");
  second.open();
  second.receive(1, "foreground snapshot\r\n");
  await expectDomText("foreground snapshot");
  await expect.element(restoration()).toHaveTextContent("ready");
  second.receive(0, "live again\r\n");
  await expectDomText("live again");
  expect(domText()).not.toContain("missed while hidden");
});
