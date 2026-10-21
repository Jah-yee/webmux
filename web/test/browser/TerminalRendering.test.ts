import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { commands, page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import Harness from "./TerminalHarness.svelte";
import { Terminal } from "@xterm/xterm";

declare module "vitest/internal/browser" {
  interface BrowserCommands {
    /** Real finger, iframe-local CSS points; release or cancel after the last. */
    nativeTouchGesture(
      points: { x: number; y: number }[],
      finish?: "end" | "cancel",
      holdMilliseconds?: number,
    ): Promise<void>;
  }
}
// Exercise the real transport framing, hook, xterm buffer and both renderers.
// This socket never opens a network connection or supplies a synthetic echo.
class FixtureSocket extends EventTarget {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  readonly url: string;
  binaryType = "blob";
  readyState = FixtureSocket.CONNECTING;
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  bufferedAmount = 0;
  sent: Uint8Array[] = [];
  closeCount = 0;

  constructor(url: string | URL) {
    super();
    this.url = String(url);
    sockets.push(this);
  }

  open() {
    this.readyState = FixtureSocket.OPEN;
    this.onopen?.(new Event("open"));
  }

  receive(kind: number, text: string) {
    const payload = new TextEncoder().encode(text);
    const frame = new Uint8Array(payload.length + 1);
    frame[0] = kind;
    frame.set(payload, 1);
    this.onmessage?.(new MessageEvent("message", { data: frame.buffer }));
  }

  send(data: Uint8Array) {
    if (this.readyState !== FixtureSocket.OPEN) {
      throw new Error("Fixture socket is not open");
    }
    this.sent.push(new Uint8Array(data));
  }

  // The real server gives control to the first client of a session.
  grantControl(cols = 80, rows = 24) {
    const clientId = new URL(this.url).searchParams.get("client");
    this.receive(
      6,
      JSON.stringify({
        controller: {
          clientId,
          label: "fixture",
          since: new Date().toISOString(),
        },
        cols,
        rows,
      }),
    );
  }

  close() {
    this.closeCount++;
    this.readyState = FixtureSocket.CLOSED;
    this.onclose?.(new CloseEvent("close", { code: 1000 }));
  }
}

let sockets: FixtureSocket[];
let observations: ControlledObserver[];
let resolutionQueries: ResolutionQuery[];
let dpr: number;
let measurement: "consistent" | "css-pixels";
let glyphPixels: WeakMap<HTMLCanvasElement, number>;
let webglCanvases: WeakSet<HTMLCanvasElement>;
let resizeListeners: Set<EventListenerOrEventListenerObject>;
const nativeResizeObserver = window.ResizeObserver;
const nativeMatchMedia = window.matchMedia.bind(window);
let previousFont: string | null;
let touchTeardowns: (() => void)[];
let previousDark: boolean;

// Chromium's device emulator can change DPR without changing the actual
// physical-pixel observations. Only that browser API boundary is controlled;
// native ResizeObserver still determines when real layout changes are delivered.
function measuredEntry(
  target: Element,
  contentRect: DOMRectReadOnly,
  pixels?: ResizeObserverSize,
): ResizeObserverEntry {
  const size = pixels ?? {
    inlineSize: Math.round(
      contentRect.width * (measurement === "consistent" ? dpr : 1),
    ),
    blockSize: Math.round(
      contentRect.height * (measurement === "consistent" ? dpr : 1),
    ),
  };
  return {
    target,
    contentRect,
    contentBoxSize: [
      { inlineSize: contentRect.width, blockSize: contentRect.height },
    ],
    borderBoxSize: [
      { inlineSize: contentRect.width, blockSize: contentRect.height },
    ],
    devicePixelContentBoxSize: [size],
  };
}

class ControlledObserver implements ResizeObserver {
  readonly targets = new Set<Element>();
  readonly native: ResizeObserver;
  readonly callback: ResizeObserverCallback;
  disconnected = false;
  observeCount = 0;
  deliveryCount = 0;

  constructor(callback: ResizeObserverCallback) {
    this.callback = callback;
    this.native = new nativeResizeObserver((entries) => {
      this.deliveryCount++;
      callback(
        entries.map((entry) => measuredEntry(entry.target, entry.contentRect)),
        this,
      );
    });
    observations.push(this);
  }

  observe(target: Element, options?: ResizeObserverOptions) {
    this.disconnected = false;
    this.observeCount++;
    this.targets.add(target);
    this.native.observe(target, options);
  }

  unobserve(target: Element) {
    this.targets.delete(target);
    this.native.unobserve(target);
  }

  disconnect() {
    this.disconnected = true;
    this.targets.clear();
    this.native.disconnect();
  }

  deliver(entry: ResizeObserverEntry) {
    // Deliberately usable after disconnect: a queued browser callback must be safe.
    this.callback([entry], this);
  }
}

class ResolutionQuery extends EventTarget {
  readonly listeners = new Set<EventListenerOrEventListenerObject>();
  readonly legacyListeners = new Set<EventListener>();
  readonly media: string;
  readonly ratio: number;
  onchange: ((event: MediaQueryListEvent) => void) | null = null;

  constructor(media: string, ratio: number) {
    super();
    this.media = media;
    this.ratio = ratio;
  }

  get matches() {
    return this.ratio === dpr;
  }

  override addEventListener(
    type: string,
    callback: EventListenerOrEventListenerObject | null,
    options?: AddEventListenerOptions | boolean,
  ) {
    if (type === "change" && callback) this.listeners.add(callback);
    super.addEventListener(type, callback, options);
  }

  override removeEventListener(
    type: string,
    callback: EventListenerOrEventListenerObject | null,
    options?: EventListenerOptions | boolean,
  ) {
    if (type === "change" && callback) this.listeners.delete(callback);
    super.removeEventListener(type, callback, options);
  }

  addListener(callback: ((event: MediaQueryListEvent) => void) | null) {
    if (!callback) return;
    this.legacyListeners.add(callback as EventListener);
    super.addEventListener("change", callback as EventListener);
  }

  removeListener(callback: ((event: MediaQueryListEvent) => void) | null) {
    if (!callback) return;
    this.legacyListeners.delete(callback as EventListener);
    super.removeEventListener("change", callback as EventListener);
  }
}

function setDpr(value: number) {
  const changed = resolutionQueries.filter(
    (query) => query.matches !== (query.ratio === value),
  );
  dpr = value;
  vi.stubGlobal("devicePixelRatio", value);
  for (const query of changed) {
    const event = new MediaQueryListEvent("change", {
      media: query.media,
      matches: query.matches,
    });
    query.dispatchEvent(event);
    query.onchange?.(event);
  }
}

// Read actual glyph pixels immediately after the real GL draw, before Chromium
// discards a non-preserved drawing buffer. Rendering/context creation are not mocked.
function observeGlyphPixels(unavailable = false) {
  const getContext = HTMLCanvasElement.prototype.getContext;
  const seen = new WeakSet<WebGL2RenderingContext>();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation(
    function (this: HTMLCanvasElement, contextId: string, options?: unknown) {
      if (unavailable && contextId === "webgl2") return null;
      const context = getContext.call(this, contextId, options);
      if (contextId === "webgl2" && context) {
        const gl = context as WebGL2RenderingContext;
        webglCanvases.add(this);
        if (!seen.has(gl)) {
          seen.add(gl);
          // oxlint-disable-next-line typescript/no-this-alias -- Retain the canvas inside the draw callback.
          const canvas = this;
          const draw = gl.drawElementsInstanced.bind(gl);
          vi.spyOn(gl, "drawElementsInstanced").mockImplementation(
            (...args) => {
              draw(...args);
              if (!canvas.isConnected || gl.isContextLost()) return;
              const width = gl.drawingBufferWidth;
              const height = Math.min(
                gl.drawingBufferHeight,
                Math.ceil(100 * dpr),
              );
              if (!width || !height) return;
              const pixels = new Uint8Array(width * height * 4);
              gl.readPixels(
                0,
                gl.drawingBufferHeight - height,
                width,
                height,
                gl.RGBA,
                gl.UNSIGNED_BYTE,
                pixels,
              );
              let red = 0;
              for (let index = 0; index < pixels.length; index += 4) {
                if (
                  pixels[index] > 180 &&
                  pixels[index + 1] < 80 &&
                  pixels[index + 2] < 80
                )
                  red++;
              }
              glyphPixels.set(canvas, red);
            },
          );
        }
      }
      return context;
    } as typeof HTMLCanvasElement.prototype.getContext,
  );
}

function host() {
  const element = document.querySelector<HTMLDivElement>(
    '[data-testid="terminal-host"]',
  );
  if (!element) throw new Error("Missing terminal host");
  return element;
}

function webglCanvas() {
  // Link overlays also use canvases; identify the one with a real GL context.
  return (
    Array.from(
      host().querySelectorAll<HTMLCanvasElement>(".xterm-screen > canvas"),
    ).find((canvas) => webglCanvases.has(canvas)) ?? null
  );
}

function hostObserver() {
  const observer = observations.find((observer) =>
    observer.targets.has(host()),
  );
  if (!observer) throw new Error("Missing terminal host observer");
  return observer;
}

function domText() {
  return host().querySelector(".xterm-rows")?.textContent ?? "";
}

async function expectDomText(text: string) {
  await expect.poll(domText).toContain(text);
  const rows = host().querySelector<HTMLElement>(".xterm-rows")!;
  await expect.element(page.elementLocator(rows)).toBeVisible();
  expect(rows.getBoundingClientRect().width).toBeGreaterThan(0);
  expect(rows.getBoundingClientRect().height).toBeGreaterThan(0);
  expect(webglCanvas()).toBeNull();
}

async function expectGlyphs() {
  await expect
    .poll(() => {
      const canvas = webglCanvas();
      return canvas ? (glyphPixels.get(canvas) ?? 0) : 0;
    })
    .toBeGreaterThan(100);
  const canvas = webglCanvas()!;
  await expect.element(page.elementLocator(canvas)).toBeVisible();
  const bounds = canvas.getBoundingClientRect();
  // xterm rounds the CSS size after converting the backing pixels by DPR.
  expect(bounds.width).toBe(Math.round(canvas.width / dpr));
  expect(bounds.height).toBe(Math.round(canvas.height / dpr));
  return canvas;
}

const prompt = "fixture@mobile:~/project$ omp";
const snapshot = `\x1b[38;2;255;0;0m${prompt}\x1b[0m\r\n`;

async function openTerminal() {
  const rendered = await render(Harness);
  // The terminal waits for the bundled monospace face before opening a socket.
  await expect.poll(() => sockets.length, { timeout: 5000 }).toBe(1);
  const socket = sockets[0];
  socket.open();
  socket.receive(1, snapshot);
  socket.grantControl();
  await expect
    .element(page.getByRole("status", { name: "Terminal connection" }))
    .toHaveTextContent("connected");
  await expect
    .element(page.getByRole("status", { name: "Terminal restoration" }))
    .toHaveTextContent("ready");
  return { socket, rendered };
}

function expectNoSessionMutation(socket: FixtureSocket) {
  expect(sockets).toEqual([socket]);
  expect(socket.closeCount).toBe(0);
  // Resize frames are legitimate; input and resync frames are not.
  expect(socket.sent.length).toBeGreaterThan(0);
  expect(socket.sent.every((frame) => frame[0] === 1)).toBe(true);
}

function inputText(socket: FixtureSocket) {
  return socket.sent
    .filter((frame) => frame[0] === 0)
    .map((frame) => new TextDecoder().decode(frame.subarray(1)))
    .join("");
}

function geometry(socket: FixtureSocket) {
  const resize = socket.sent.filter((frame) => frame[0] === 1).at(-1)!;
  const { cols, rows } = JSON.parse(
    new TextDecoder().decode(resize.subarray(1)),
  ) as { cols: number; rows: number };
  const bounds = host().querySelector(".xterm-screen")!.getBoundingClientRect();
  return {
    row: bounds.height / rows,
    cell: bounds.width / cols,
    bounds,
  };
}

function gestureStart(socket: FixtureSocket, column = 8, row = 12) {
  const { bounds, cell, row: height } = geometry(socket);
  return {
    x: bounds.left + (column - 0.5) * cell,
    y: bounds.top + (row - 0.5) * height,
  };
}

async function swipe(
  socket: FixtureSocket,
  lines: number,
  start = gestureStart(socket),
  horizontal = 0,
) {
  const height = geometry(socket).row;
  // Every step must clear the 8 px long-press jitter tolerance. Smaller steps
  // let a slow runner reach the 500 ms long press mid-swipe and select instead.
  const travel = Math.hypot(horizontal, lines * height);
  const steps = Math.max(1, Math.min(8, Math.floor(travel / 8)));
  await commands.nativeTouchGesture(
    Array.from({ length: steps + 1 }, (_, index) => ({
      x: start.x + (horizontal * index) / steps,
      y: start.y + (lines * height * index) / steps,
    })),
  );
}

function historyLine(line: number) {
  return `history-${String(line).padStart(4, "0")}`;
}

function firstHistoryLine() {
  const text = host().querySelector(".xterm-rows > div")?.textContent ?? "";
  return Number(/history-(\d+)/.exec(text)?.[1] ?? NaN);
}

async function populateHistory(socket: FixtureSocket) {
  // More than three viewports: assertions read the actual visible DOM renderer.
  socket.receive(
    1,
    Array.from({ length: 160 }, (_, index) => historyLine(index + 1)).join(
      "\r\n",
    ),
  );
  await expectDomText(historyLine(160));
  expect(firstHistoryLine()).toBeGreaterThan(1);
  return firstHistoryLine();
}

function blurAndTrackFocus() {
  const textarea = host().querySelector<HTMLTextAreaElement>(
    ".xterm-helper-textarea",
  )!;
  textarea.blur();
  const focused = vi.fn();
  textarea.addEventListener("focus", focused);
  touchTeardowns.push(() => textarea.removeEventListener("focus", focused));
  return { textarea, focused };
}

function pointer(
  target: HTMLElement,
  type: string,
  id: number,
  point: { x: number; y: number },
) {
  target.dispatchEvent(
    new PointerEvent(type, {
      bubbles: true,
      cancelable: true,
      pointerType: "touch",
      pointerId: id,
      clientX: point.x,
      clientY: point.y,
      buttons: type === "pointerup" ? 0 : 1,
    }),
  );
}

function syntheticPointerCapture(target: HTMLElement) {
  // Synthetic pointer ids cannot be captured by Chromium. Only that browser
  // boundary is replaced; the real hook, fit, rendering and xterm protocol run.
  return {
    set: vi.spyOn(target, "setPointerCapture").mockImplementation(() => {}),
    release: vi
      .spyOn(target, "releasePointerCapture")
      .mockImplementation(() => {}),
  };
}

beforeEach(async () => {
  sockets = [];
  previousDark = document.documentElement.classList.contains("dark");
  previousFont = localStorage.getItem("webmux.fontSize");
  localStorage.setItem("webmux.fontSize", "14");
  touchTeardowns = [];
  observations = [];
  resolutionQueries = [];
  resizeListeners = new Set();
  glyphPixels = new WeakMap();
  webglCanvases = new WeakSet();
  dpr = 1;
  measurement = "consistent";
  await page.viewport(412, 924);
  const addWindowListener = window.addEventListener.bind(window);
  vi.spyOn(window, "addEventListener").mockImplementation(
    (type, listener, options) => {
      if (type === "resize" && listener) resizeListeners.add(listener);
      addWindowListener(type, listener, options);
    },
  );
  vi.stubGlobal("WebSocket", FixtureSocket);
  vi.stubGlobal("ResizeObserver", ControlledObserver);
  vi.stubGlobal("devicePixelRatio", dpr);
  vi.stubGlobal("matchMedia", (query: string) => {
    const resolution = /\(resolution:\s*([\d.]+)dppx\)/.exec(query);
    if (!resolution) return nativeMatchMedia(query);
    const result = new ResolutionQuery(query, Number(resolution[1]));
    resolutionQueries.push(result);
    return result as unknown as MediaQueryList;
  });
});

afterEach(async () => {
  try {
    await cleanup();
    // Terminal disposal waits until xterm's pending viewport frame has read
    // renderer dimensions. Observer assertions run after that frame.
    await new Promise<void>((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
    });
    await userEvent.cleanup();
    expect(
      observations
        .filter((observer) => observer.targets.size > 0)
        .map((observer) =>
          [...observer.targets].map((target) => target.className),
        ),
    ).toEqual([]);
    // Assert the hook's modern listeners. xterm 5.5's legacy ScreenDprMonitor
    // is not registered for disposal; isolate that dependency in teardown.
    expect(
      resolutionQueries
        .filter((query) => query.listeners.size > 0)
        .map((query) => ({
          media: query.media,
          listeners: query.listeners.size,
        })),
    ).toEqual([]);
    expect(sockets.map((socket) => socket.closeCount)).toEqual(
      sockets.map(() => 1),
    );
  } finally {
    for (const teardown of touchTeardowns) teardown();
    document.documentElement.classList.toggle("dark", previousDark);
    if (previousFont === null) localStorage.removeItem("webmux.fontSize");
    else localStorage.setItem("webmux.fontSize", previousFont);
    for (const observer of observations) observer.native.disconnect();
    for (const query of resolutionQueries) {
      // oxlint-disable-next-line unicorn/no-useless-spread -- Removing a listener mutates the set being traversed.
      for (const listener of [...query.listeners])
        query.removeEventListener("change", listener);
      // oxlint-disable-next-line unicorn/no-useless-spread -- Removing a listener mutates the set being traversed.
      for (const listener of [...query.legacyListeners])
        query.removeListener(listener);
    }
    for (const listener of resizeListeners)
      window.removeEventListener("resize", listener);
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  }
});

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

test("mobile high-DPR CSS-pixel observations render the restored prompt and retain working input", async () => {
  setDpr(2.625);
  measurement = "css-pixels";
  observeGlyphPixels();
  const { socket } = await openTerminal();
  await expectDomText(prompt);
  expectNoSessionMutation(socket);

  await page.getByRole("button", { name: "Focus terminal input" }).click();
  const textarea = host().querySelector<HTMLTextAreaElement>(
    ".xterm-helper-textarea",
  )!;
  expect(document.activeElement).toBe(textarea);
  expect(textarea.disabled).toBe(false);
  await userEvent.keyboard("x");
  await expect
    .poll(() => socket.sent.filter((frame) => frame[0] === 0).length)
    .toBe(1);
  const input = socket.sent.find((frame) => frame[0] === 0)!;
  expect(new TextDecoder().decode(input.subarray(1))).toBe("x");
  expect(sockets).toEqual([socket]);
  expect(socket.closeCount).toBe(0);
});

test.each([
  { viewport: 1280, width: 960, height: 640, ratio: 1 },
  { viewport: 412, width: 400, height: 750, ratio: 2.625 },
  { viewport: 360, width: 350, height: 700, ratio: 3 },
])(
  "fit fills the available grid at $width CSS pixels and DPR $ratio",
  async ({ viewport, width, height, ratio }) => {
    setDpr(ratio);
    observeGlyphPixels();
    await page.viewport(viewport, 924);
    const { socket } = await openTerminal();
    host().style.width = `${width}px`;
    host().style.height = `${height}px`;
    await expectGlyphs();
    await expect
      .poll(() => {
        const terminal = host().querySelector<HTMLElement>(".xterm")!;
        const screen = terminal.querySelector<HTMLElement>(".xterm-screen")!;
        const scrollbar = terminal.querySelector<HTMLElement>(
          ".xterm-scrollable-element > .scrollbar.vertical",
        )!;
        const style = getComputedStyle(terminal);
        const availableWidth =
          host().clientWidth -
          parseFloat(style.paddingLeft) -
          parseFloat(style.paddingRight) -
          scrollbar.getBoundingClientRect().width;
        const availableHeight =
          host().clientHeight -
          parseFloat(style.paddingTop) -
          parseFloat(style.paddingBottom);
        const grid = geometry(socket);
        const unusedWidth =
          availableWidth - screen.getBoundingClientRect().width;
        const unusedHeight =
          availableHeight - screen.getBoundingClientRect().height;
        // CSS canvas dimensions are rounded once; unused space must be under one cell.
        return (
          unusedWidth >= -0.5 &&
          unusedWidth < grid.cell + 0.5 &&
          unusedHeight >= -0.5 &&
          unusedHeight < grid.row + 0.5
        );
      })
      .toBe(true);
    expectNoSessionMutation(socket);
  },
);

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

test("desktop to incompatible high-DPR fallback preserves rendered history and the same socket", async () => {
  observeGlyphPixels();
  const { socket, rendered } = await openTerminal();
  const canvas = await expectGlyphs();
  const snapshotPixels = glyphPixels.get(canvas)!;
  socket.receive(0, "output before DPR transition\r\n");
  socket.receive(0, "\x1b[38;2;255;0;0mvisible history\x1b[0m\r\n");
  // Additional red glyph pixels prove new output was actually drawn, not just queued.
  await expect
    .poll(() => glyphPixels.get(canvas) ?? 0)
    .toBeGreaterThan(snapshotPixels + 100);

  const observer = hostObserver();
  const observeCount = observer.observeCount;
  measurement = "css-pixels";
  setDpr(2.625);
  expect(observer.observeCount).toBeGreaterThan(observeCount);
  await expectDomText(prompt);
  await expectDomText("output before DPR transition");
  await expectDomText("visible history");
  socket.receive(0, "output after DPR transition\r\n");
  await expectDomText("output after DPR transition");
  expectNoSessionMutation(socket);

  const oldHost = host();
  const lateEntry = measuredEntry(oldHost, oldHost.getBoundingClientRect());
  const oldQueries = [...resolutionQueries];
  await rendered.unmount();
  expect(observer.disconnected).toBe(true);
  expect(socket.closeCount).toBe(1);
  const sentCount = socket.sent.length;
  const unmountedObserveCount = observer.observeCount;
  expect(() => observer.deliver(lateEntry)).not.toThrow();
  for (const query of oldQueries) query.dispatchEvent(new Event("change"));
  expect(socket.sent).toHaveLength(sentCount);
  expect(sockets).toEqual([socket]);
  expect(observer.observeCount).toBe(unmountedObserveCount);
  expect(oldQueries.every((query) => query.listeners.size === 0)).toBe(true);
});

test("fractional DPR, one-pixel rounding, hidden boxes and absent physical measurements keep real WebGL glyphs", async () => {
  setDpr(1.25);
  observeGlyphPixels();
  const { socket } = await openTerminal();
  const canvas = await expectGlyphs();
  const observer = hostObserver();
  const bounds = host().getBoundingClientRect();
  observer.deliver(
    measuredEntry(host(), bounds, {
      inlineSize: bounds.width * dpr + 1,
      blockSize: Math.round(bounds.height * dpr),
    }),
  );
  observer.deliver(
    measuredEntry(host(), new DOMRect(0, 0, 0, 0), {
      inlineSize: 0,
      blockSize: 0,
    }),
  );
  const { devicePixelContentBoxSize: _physical, ...unsupported } =
    measuredEntry(host(), bounds);
  observer.deliver(unsupported as ResizeObserverEntry);
  glyphPixels.delete(canvas);
  socket.receive(
    0,
    "\x1b[38;2;255;0;0mstill rendered at fractional DPR\x1b[0m\r\n",
  );
  expect(await expectGlyphs()).toBe(canvas);

  const deliveries = observer.deliveryCount;
  setDpr(2.625);
  await expect.poll(() => observer.deliveryCount).toBeGreaterThan(deliveries);
  glyphPixels.delete(canvas);
  socket.receive(
    0,
    "\x1b[38;2;255;0;0mconsistent fresh physical pixels\x1b[0m\r\n",
  );
  expect(await expectGlyphs()).toBe(canvas);
  expectNoSessionMutation(socket);
});

test("unavailable WebGL renders snapshot and later output visibly with the DOM renderer", async () => {
  setDpr(2.625);
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  await expectDomText(prompt);
  socket.receive(0, "output without WebGL\r\n");
  await expectDomText("output without WebGL");
  expectNoSessionMutation(socket);
});

test("real WebGL context loss falls back visibly without discarding existing history", async () => {
  observeGlyphPixels();
  const { socket } = await openTerminal();
  const canvas = await expectGlyphs();
  const gl = canvas.getContext("webgl2")!;
  const extension = gl.getExtension("WEBGL_lose_context");
  expect(extension).not.toBeNull();
  extension!.loseContext();
  // addon-webgl allows three seconds for context restoration before fallback.
  await expect.poll(domText, { timeout: 10000 }).toContain(prompt);
  await expectDomText(prompt);
  socket.receive(0, "output after real context loss\r\n");
  await expectDomText("output after real context loss");
  expectNoSessionMutation(socket);
}, 15000);

test("native Chrome swipes keep scrolling visibly through row redraws without keyboard, duplicate scroll or outer-page pan", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  const bottom = await populateHistory(socket);
  const { textarea, focused } = blurAndTrackFocus();
  const spacer = document.createElement("div");
  spacer.style.height = "2000px";
  document.body.append(spacer);
  touchTeardowns.push(() => {
    spacer.remove();
    window.scrollTo(0, 0);
  });
  expect(document.documentElement.scrollHeight).toBeGreaterThan(innerHeight);
  const outerY = scrollY;
  const events: string[] = [];
  const record = (event: Event) => {
    if (event.isTrusted) events.push(event.type);
  };
  const types = ["pointermove", "pointerup", "pointercancel", "touchcancel"];
  for (const type of types) document.addEventListener(type, record, true);
  touchTeardowns.push(() => {
    for (const type of types) document.removeEventListener(type, record, true);
  });

  const originalRow = host().querySelector(".xterm-rows > div > span")!;
  await swipe(socket, 4.25);
  await expect.poll(firstHistoryLine).toBe(bottom - 4);
  await expectDomText(historyLine(bottom - 4));
  // xterm replaces the hit-tested span during scroll. Pointer capture must keep
  // receiving the rest of the real finger, including its final release.
  expect(originalRow.isConnected).toBe(false);
  expect(
    events.filter((type) => type === "pointermove").length,
  ).toBeGreaterThanOrEqual(7);
  expect(events.filter((type) => type === "pointerup")).toHaveLength(1);
  expect(events.filter((type) => type.endsWith("cancel"))).toEqual([]);

  await swipe(socket, -4.25, gestureStart(socket, 8, 30));
  await expect.poll(firstHistoryLine).toBe(bottom);
  // A fresh gesture must discard the previous quarter-row remainder.
  await swipe(socket, 0.8);
  expect(firstHistoryLine()).toBe(bottom);
  // Exact per-gesture distances are asserted above. These loops only need to
  // reach the end, where scrolling clamps; a slow runner can lose a few rows
  // of one gesture, so one spare swipe keeps the margin from being ~2 rows.
  for (let index = 0; index <= Math.ceil(bottom / 20); index++) {
    await swipe(socket, 20.25, gestureStart(socket, 8, 6));
  }
  await expect.poll(firstHistoryLine).toBe(1);
  await expectDomText(historyLine(1));
  await swipe(socket, 8.25);
  expect(firstHistoryLine()).toBe(1);
  for (let index = 0; index <= Math.ceil(bottom / 20); index++) {
    await swipe(socket, -20.25, gestureStart(socket, 8, 30));
  }
  await expect.poll(firstHistoryLine).toBe(bottom);
  await expectDomText(historyLine(160));
  await swipe(socket, -8.25, gestureStart(socket, 8, 30));
  expect(firstHistoryLine()).toBe(bottom);
  expect(scrollY).toBe(outerY);
  expect(events.filter((type) => type.endsWith("cancel"))).toEqual([]);
  expect(document.activeElement).not.toBe(textarea);
  expect(focused).not.toHaveBeenCalled();
  expectNoSessionMutation(socket);
  // About 20 real swipes, each waiting two animation frames per touch step:
  // ~10 s on Linux CI, over 20 s on hosted macOS runners.
}, 60000);

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

test("native fullscreen gestures report row-scaled SGR wheel input at stable starting cells", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  socket.receive(0, "\x1b[?1049h\x1b[?1000h\x1b[?1006hmouse-ready");
  await expectDomText("mouse-ready");
  const { textarea, focused } = blurAndTrackFocus();
  const start = gestureStart(socket, 8, 12);

  // Horizontal finger drift and vertical row changes must not move the
  // application's wheel anchor. Exact protocol equality excludes clicks,
  // cursor arrows, malformed reports and duplicate xterm touch scrolling.
  await swipe(socket, 3.25, start, geometry(socket).cell * 4);
  expect(inputText(socket)).toBe("\x1b[<64;8;12M".repeat(3));
  await swipe(socket, -6.25, start, -geometry(socket).cell * 4);
  expect(inputText(socket)).toBe(
    "\x1b[<64;8;12M".repeat(3) + "\x1b[<65;8;12M".repeat(6),
  );
  expect(document.activeElement).not.toBe(textarea);
  expect(focused).not.toHaveBeenCalled();
});

test("native alternate-buffer wheel fallback honors cursor-key mode and normal-buffer restoration", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  const bottom = await populateHistory(socket);
  const { focused } = blurAndTrackFocus();
  socket.receive(0, "\x1b[?1049h\x1b[?1lalt-normal");
  await expectDomText("alt-normal");
  await swipe(socket, 2.25);
  await swipe(socket, -3.25);
  expect(inputText(socket)).toBe("\x1b[A".repeat(2) + "\x1b[B".repeat(3));

  socket.receive(0, "\x1b[?1h\r\nalt-application");
  await expectDomText("alt-application");
  await swipe(socket, 4.25);
  await swipe(socket, -2.25);
  expect(inputText(socket)).toBe(
    "\x1b[A".repeat(2) +
      "\x1b[B".repeat(3) +
      "\x1bOA".repeat(4) +
      "\x1bOB".repeat(2),
  );
  const beforeRestore = inputText(socket);
  socket.receive(0, "\x1b[?1049l");
  await expectDomText(historyLine(160));
  await swipe(socket, 5.25);
  await expect.poll(firstHistoryLine).toBe(bottom - 5);
  await expectDomText(historyLine(bottom - 5));
  await swipe(socket, -5.25);
  await expect.poll(firstHistoryLine).toBe(bottom);
  expect(inputText(socket)).toBe(beforeRestore);
  expect(focused).not.toHaveBeenCalled();
});

test("pinch resizes real cells and rebases one-finger scrolling, while cancel and capture loss discard ghost gestures", async () => {
  observeGlyphPixels(true);
  const { socket, rendered } = await openTerminal();
  socket.receive(0, "\x1b[?1049h\x1b[?1000h\x1b[?1006hedge-ready");
  await expectDomText("edge-ready");
  const target = host();
  const { textarea, focused } = blurAndTrackFocus();
  const start = gestureStart(socket);
  const originalHeight = geometry(socket).row;
  await commands.nativeTouchGesture(
    [start, { x: start.x, y: start.y + originalHeight * 0.8 }],
    "cancel",
  );
  expect(inputText(socket)).toBe("");
  expect(focused).not.toHaveBeenCalled();
  const capture = syntheticPointerCapture(target);
  pointer(target, "pointerdown", 1, start);
  const first = { x: start.x, y: start.y + originalHeight * 0.8 };
  pointer(target, "pointermove", 1, first);
  pointer(target, "pointerdown", 2, { x: first.x + 100, y: first.y });
  pointer(target, "pointermove", 2, { x: first.x + 130, y: first.y });
  await expect.poll(() => geometry(socket).row).toBeGreaterThan(originalHeight);
  await expectDomText("edge-ready");
  expect(Number(localStorage.getItem("webmux.fontSize"))).toBeGreaterThan(14);
  pointer(target, "pointerup", 2, { x: first.x + 130, y: first.y });
  expect(inputText(socket)).toBe("");
  const height = geometry(socket).row;
  pointer(target, "pointermove", 1, { x: first.x, y: first.y + height * 0.6 });
  expect(inputText(socket)).toBe("");
  pointer(target, "pointermove", 1, { x: first.x, y: first.y + height * 1.2 });
  // oxlint-disable-next-line no-control-regex -- Match a terminal mouse-report escape sequence.
  expect(inputText(socket)).toMatch(/^\x1b\[<64;\d+;\d+M$/);
  pointer(target, "pointerup", 1, { x: first.x, y: first.y + height * 1.2 });
  expect(focused).not.toHaveBeenCalled();

  for (const cancellation of ["pointercancel", "lostpointercapture"]) {
    const before = inputText(socket);
    const fresh = gestureStart(socket);
    pointer(target, "pointerdown", 1, fresh);
    pointer(target, "pointermove", 1, {
      x: fresh.x,
      y: fresh.y + height * 0.8,
    });
    pointer(target, cancellation, 1, fresh);
    pointer(target, "pointermove", 1, { x: fresh.x, y: fresh.y + height * 12 });
    pointer(target, "pointerup", 1, fresh);
    expect(inputText(socket)).toBe(before);
    expect(focused).not.toHaveBeenCalled();

    pointer(target, "pointerdown", 1, fresh);
    pointer(target, "pointermove", 1, {
      x: fresh.x,
      y: fresh.y + height * 0.4,
    });
    expect(inputText(socket)).toBe(before);
    pointer(target, "pointermove", 1, {
      x: fresh.x,
      y: fresh.y + height * 1.2,
    });
    pointer(target, "pointerup", 1, fresh);
    expect(inputText(socket).slice(before.length)).toMatch(
      // oxlint-disable-next-line no-control-regex -- Match a terminal mouse-report escape sequence.
      /^\x1b\[<64;\d+;\d+M$/,
    );
  }

  expect(focused).not.toHaveBeenCalled();
  expect(document.activeElement).not.toBe(textarea);
  capture.set.mockRestore();
  capture.release.mockRestore();
  socket.receive(0, "\x1b[?1000l\x1b[?1006l\x1b[?1049l");
  await expectDomText(prompt);
  const beforeTap = inputText(socket);
  await commands.nativeTouchGesture([gestureStart(socket)]);
  expect(document.activeElement).not.toBe(textarea);
  expect(focused).not.toHaveBeenCalled();
  expect(inputText(socket)).toBe(beforeTap);
  syntheticPointerCapture(target);

  // A live gesture disposed before its release must not keep any socket or
  // focus effects attached to the old host, even with queued late events.
  pointer(target, "pointerdown", 1, start);
  await rendered.unmount();
  const sent = socket.sent.length;
  for (const type of [
    "pointermove",
    "pointercancel",
    "pointerup",
    "lostpointercapture",
  ]) {
    pointer(target, type, 1, { x: start.x, y: start.y + height * 12 });
  }
  pointer(target, "pointerdown", 2, start);
  pointer(target, "pointerup", 2, start);
  expect(socket.sent).toHaveLength(sent);
  expect(socket.closeCount).toBe(1);
  expect(focused).not.toHaveBeenCalled();
});

test("terminal measures the bundled monospace face before painting", async () => {
  observeGlyphPixels(true);
  await openTerminal();
  const rows = host().querySelector<HTMLElement>(".xterm-rows");
  expect(rows).toBeTruthy();
  expect(getComputedStyle(rows!).fontFamily).toContain("Maple Mono NF CN");
  expect(document.fonts.check('14px "Maple Mono NF CN"')).toBe(true);
  const context = document.createElement("canvas").getContext("2d")!;
  for (const weight of [400, 700]) {
    // Loading the shared icon face must not shadow the Latin unicode slice.
    await document.fonts.load(`${weight} 14px "Maple Mono NF CN"`, "\uf07b");
    for (const size of [14, 16]) {
      context.font = `${weight} ${size}px "Maple Mono NF CN", sans-serif`;
      expect(
        context.measureText("WWWW").width,
        `weight ${weight}: Latin glyphs must use the bundled monospace face`,
      ).toBeCloseTo(context.measureText("iiii").width, 2);
      // Maple's advance is 0.6em. Linux font hinting can round each glyph
      // to a whole pixel; macOS retains the fractional advance.
      const advance = context.measureText("WWWW").width / 4;
      expect(advance).toBeGreaterThanOrEqual(Math.floor(size * 0.6));
      expect(advance).toBeLessThanOrEqual(Math.ceil(size * 0.6));
    }
  }
});

test("explicit black ANSI input remains readable in dark theme and updates on theme changes", async () => {
  observeGlyphPixels(true);
  document.documentElement.classList.add("dark");
  const { socket } = await openTerminal();
  socket.receive(1, "\x1b[38;2;0;0;0m中文 input\x1b[0m");
  await expectDomText("中文 input");
  const ratio = () => {
    const row = [...host().querySelectorAll(".xterm-rows > div")].find(
      (element) =>
        element.textContent?.replaceAll("\u00a0", " ").trimEnd() ===
        "中文 input",
    );
    const spans = [
      ...(row?.querySelectorAll<HTMLElement>("span") ?? []),
    ].filter((element) => element.textContent?.trim());
    expect(spans.length).toBeGreaterThan(0);
    const luminance = (color: string) => {
      const rgb = color
        .match(/\d+/g)!
        .slice(0, 3)
        .map((value) => {
          const channel = Number(value) / 255;
          return channel <= 0.04045
            ? channel / 12.92
            : ((channel + 0.055) / 1.055) ** 2.4;
        });
      return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722;
    };
    const background = luminance(
      getComputedStyle(host().querySelector(".xterm")!).backgroundColor,
    );
    return Math.min(
      ...spans.map((span) => {
        const foreground = luminance(getComputedStyle(span).color);
        return (
          (Math.max(foreground, background) + 0.05) /
          (Math.min(foreground, background) + 0.05)
        );
      }),
    );
  };
  await expect.poll(ratio).toBeGreaterThanOrEqual(4.49);
  document.documentElement.classList.remove("dark");
  await expect.poll(ratio).toBeGreaterThanOrEqual(4.49);
  document.documentElement.classList.add("dark");
  await expect.poll(ratio).toBeGreaterThanOrEqual(4.49);
});

test("pending viewport reset after a theme change does not read a disposed renderer", async () => {
  observeGlyphPixels(true);
  document.documentElement.classList.add("dark");
  const { socket, rendered } = await openTerminal();
  await expectDomText(prompt);
  const queued: FrameRequestCallback[] = [];
  const realFrame = window.requestAnimationFrame.bind(window);
  const realCancel = window.cancelAnimationFrame.bind(window);
  window.requestAnimationFrame = (callback) => {
    queued.push(callback);
    return queued.length;
  };
  window.cancelAnimationFrame = () => {};
  const errors: unknown[] = [];
  const onError = (event: ErrorEvent) => {
    errors.push(event.error ?? event.message);
  };
  window.addEventListener("error", onError);
  try {
    // Queue real rendering work before unmounting, without relying on a
    // particular xterm viewport implementation or callback name.
    socket.receive(1, "\x1b[38;2;0;0;0m中文 input\x1b[0m");
    await expect.poll(() => queued.length).toBeGreaterThan(0);
    document.documentElement.classList.remove("dark");
    document.documentElement.classList.add("dark");
    await rendered.unmount();
    window.requestAnimationFrame = realFrame;
    window.cancelAnimationFrame = realCancel;
    for (const callback of queued) {
      try {
        callback(0);
      } catch (error) {
        errors.push(error);
      }
    }
    await new Promise<void>((resolve) => {
      realFrame(() => realFrame(() => resolve()));
    });
  } finally {
    window.requestAnimationFrame = realFrame;
    window.cancelAnimationFrame = realCancel;
    window.removeEventListener("error", onError);
  }
  expect(errors).toEqual([]);
});

test("native long press selects wide text and dragging copies multiple lines without terminal input or keyboard", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  socket.receive(1, "prefix 中文 tail\r\nsecond line");
  await expectDomText("second line");
  const { textarea, focused } = blurAndTrackFocus();
  // Column 9 is the trailing half of 中, not a separate blank cell.
  const start = gestureStart(socket, 9, 1);
  await commands.nativeTouchGesture([start], "end", 650);
  await expect
    .element(page.getByRole("status", { name: "Terminal selection" }))
    .toHaveTextContent("中文");
  await page.getByRole("button", { name: "Copy selection" }).click();
  await expect
    .element(page.getByRole("status", { name: "Copied terminal text" }))
    .toHaveTextContent("中文");
  await commands.nativeTouchGesture(
    [start, gestureStart(socket, 6, 2)],
    "end",
    650,
  );
  await expect
    .poll(
      () =>
        document.querySelector('[aria-label="Terminal selection"]')
          ?.textContent,
    )
    .toBe("中文 tail\nsecond");
  await page.getByRole("button", { name: "Copy selection" }).click();
  await expect
    .poll(
      () =>
        document.querySelector('[aria-label="Copied terminal text"]')
          ?.textContent,
    )
    .toBe("中文 tail\nsecond");
  expect(inputText(socket)).toBe("");
  expect(document.activeElement).not.toBe(textarea);
  expect(focused).not.toHaveBeenCalled();
  await page.getByRole("button", { name: "Clear selection" }).click();
  await expect
    .element(page.getByRole("status", { name: "Terminal selection" }))
    .toHaveTextContent("");
});

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

// Replays soft-keyboard IME event orders observed on iOS Chinese/Japanese
// keyboards: directly committed text (space, digits, punctuation) arrives as
// insertText while keyCode is 229, sometimes before its keydown or while an
// earlier keydown is still unreleased. The browser's default action is
// modelled: an uncancelled beforeinput edits the textarea, then input fires.
function imeKey(textarea: HTMLTextAreaElement, type: "keydown" | "keyup") {
  const event = new KeyboardEvent(type, {
    key: "Unidentified",
    bubbles: true,
    cancelable: true,
    composed: true,
  });
  Object.defineProperty(event, "keyCode", { value: 229 });
  textarea.dispatchEvent(event);
}

function imeInsert(
  textarea: HTMLTextAreaElement,
  data: string,
  inputType = "insertText",
  isComposing = false,
) {
  const init = { inputType, data, isComposing, bubbles: true, composed: true };
  const before = new InputEvent("beforeinput", { ...init, cancelable: true });
  if (!textarea.dispatchEvent(before)) return;
  textarea.value += data;
  textarea.dispatchEvent(new InputEvent("input", init));
}

async function settle() {
  await new Promise((resolve) => setTimeout(resolve, 20));
}

test("IME-committed spaces, digits and punctuation reach the terminal exactly once", async () => {
  const { socket } = await openTerminal();
  await page.getByRole("button", { name: "Focus terminal input" }).click();
  const textarea = host().querySelector<HTMLTextAreaElement>(
    ".xterm-helper-textarea",
  )!;
  expect(document.activeElement).toBe(textarea);

  // keydown(229) before the insertion.
  imeKey(textarea, "keydown");
  imeInsert(textarea, " ");
  imeKey(textarea, "keyup");
  await settle();
  expect(inputText(socket)).toBe(" ");

  // Insertion before its keydown while a previous key is still held.
  imeKey(textarea, "keydown");
  await settle();
  imeInsert(textarea, "1");
  imeKey(textarea, "keydown");
  imeKey(textarea, "keyup");
  await settle();
  expect(inputText(socket)).toBe(" 1");

  // Insertion without any keydown while a previous key never released, as
  // when the keyboard swallows keyups of keys its IME handled.
  imeKey(textarea, "keydown");
  await settle();
  imeInsert(textarea, "，");
  await settle();
  imeKey(textarea, "keyup");
  imeInsert(textarea, " ");
  await settle();
  expect(inputText(socket)).toBe(" 1， ");

  // Composition still commits once through xterm's composition handling.
  imeKey(textarea, "keydown");
  textarea.dispatchEvent(
    new CompositionEvent("compositionstart", { data: "" }),
  );
  imeInsert(textarea, "ni", "insertCompositionText", true);
  textarea.value = textarea.value.slice(0, -2);
  imeInsert(textarea, "你", "insertCompositionText", true);
  textarea.dispatchEvent(
    new CompositionEvent("compositionend", { data: "你" }),
  );
  imeKey(textarea, "keyup");
  await settle();
  expect(inputText(socket)).toBe(" 1， 你");

  // Safari may commit with insertText just after compositionend.
  imeKey(textarea, "keydown");
  textarea.dispatchEvent(
    new CompositionEvent("compositionstart", { data: "" }),
  );
  imeInsert(textarea, "hao", "insertCompositionText", true);
  textarea.value = textarea.value.slice(0, -3);
  textarea.dispatchEvent(
    new CompositionEvent("compositionend", { data: "好" }),
  );
  imeInsert(textarea, "好");
  imeKey(textarea, "keyup");
  await settle();
  expect(inputText(socket)).toBe(" 1， 你好");

  // Hardware and English soft keyboards still use xterm's keydown and
  // keypress paths; keypress leaves the insertion uncancelled.
  await userEvent.keyboard(" 2");
  await settle();
  expect(inputText(socket)).toBe(" 1， 你好 2");
});

test("iOS IME composition keeps the helper caret fixed without dropping commits", async () => {
  const descriptor = Object.getOwnPropertyDescriptor(
    Navigator.prototype,
    "userAgent",
  );
  Object.defineProperty(Navigator.prototype, "userAgent", {
    configurable: true,
    get: () => "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
  });
  try {
    const { socket } = await openTerminal();
    await page.getByRole("button", { name: "Focus terminal input" }).click();
    const textarea = host().querySelector<HTMLTextAreaElement>(
      ".xterm-helper-textarea",
    )!;
    expect(host().classList.contains("ios-ime")).toBe(true);
    expect(document.activeElement).toBe(textarea);

    socket.receive(0, "\r\n\r\n");
    await settle();
    textarea.dispatchEvent(
      new CompositionEvent("compositionstart", { data: "" }),
    );
    textarea.dispatchEvent(
      new CompositionEvent("compositionupdate", { data: "nihao" }),
    );
    // xterm writes the cursor position inline; the pin must win so WebKit
    // has no moving caret to pan the visual viewport toward. This first
    // assertion pins that xterm internal: if an upgrade stops writing inline
    // positions, revisit the stylesheet override rather than the pin checks.
    expect(textarea.style.top).not.toBe("0px");
    const pinned = getComputedStyle(textarea);
    expect(pinned.left).toBe("0px");
    expect(pinned.top).toBe("0px");
    expect(pinned.width).toBe("16px");
    expect(pinned.height).toBe("16px");
    expect(pinned.fontSize).toBe("16px");
    expect({ x: window.scrollX, y: window.scrollY }).toEqual({ x: 0, y: 0 });

    textarea.dispatchEvent(
      new CompositionEvent("compositionend", { data: "你" }),
    );
    imeInsert(textarea, "你");
    await settle();
    expect(inputText(socket)).toContain("你");
    expect(inputText(socket)).not.toContain("nihao");
  } finally {
    if (descriptor)
      Object.defineProperty(Navigator.prototype, "userAgent", descriptor);
  }
});

test("Alt arrows retain shell navigation and are classified as user input", async () => {
  const { socket } = await openTerminal();
  const textarea = host().querySelector("textarea")!;
  textarea.focus();
  const mac = /Mac/.test(navigator.platform);
  const arrows: [string, number, string][] = [
    ["ArrowLeft", 37, mac ? "\x1bb" : "\x1b[1;5D"],
    ["ArrowRight", 39, mac ? "\x1bf" : "\x1b[1;5C"],
    ...(!mac
      ? ([
          ["ArrowUp", 38, "\x1b[1;5A"],
          ["ArrowDown", 40, "\x1b[1;5B"],
        ] as [string, number, string][])
      : []),
  ];
  for (const [key, keyCode] of arrows) {
    const event = new KeyboardEvent("keydown", {
      key,
      code: key,
      keyCode,
      altKey: true,
      bubbles: true,
      cancelable: true,
    });
    textarea.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);
  }
  await expect
    .poll(() => inputText(socket))
    .toBe(arrows.map(([, , text]) => text).join(""));
  expect(socket.sent.filter((frame) => frame[0] === 6)).toEqual([]);
});
