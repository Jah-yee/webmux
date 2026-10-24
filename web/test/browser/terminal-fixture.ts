// Shared harness for the Terminal*.test.ts browser suites.
import type { ComponentProps } from "svelte";
import { afterEach, beforeEach, expect, vi } from "vitest";
import { commands, page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import type { TerminalController } from "../../src/hooks/terminal.svelte";
import Harness from "./TerminalHarness.svelte";

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
export class FixtureSocket extends EventTarget {
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
  grantControl(cols = 80, rows = 24, colorSchemeReports = false) {
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
        colorSchemeReports,
      }),
    );
  }

  close() {
    this.closeCount++;
    this.readyState = FixtureSocket.CLOSED;
    this.onclose?.(new CloseEvent("close", { code: 1000 }));
  }

  // An abnormal close initiated by the network, not by the hook.
  drop(code = 1006) {
    this.readyState = FixtureSocket.CLOSED;
    this.onclose?.(new CloseEvent("close", { code }));
  }
}

export let sockets: FixtureSocket[];
let observations: ControlledObserver[];
export let resolutionQueries: ResolutionQuery[];
export let dpr: number;
let measurement: "consistent" | "css-pixels";
export let glyphPixels: WeakMap<HTMLCanvasElement, number>;
let webglCanvases: WeakSet<HTMLCanvasElement>;
let resizeListeners: Set<EventListenerOrEventListenerObject>;
const nativeResizeObserver = window.ResizeObserver;
const nativeMatchMedia = window.matchMedia.bind(window);
let previousFont: string | null;
export let touchTeardowns: (() => void)[];
let previousDark: boolean;

// Physical-pixel observations either follow the emulated DPR or, like
// Chromium's device emulator, stay in CSS pixels.
export function setMeasurement(value: "consistent" | "css-pixels") {
  measurement = value;
}

// Chromium's device emulator can change DPR without changing the actual
// physical-pixel observations. Only that browser API boundary is controlled;
// native ResizeObserver still determines when real layout changes are delivered.
export function measuredEntry(
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

export function setDpr(value: number) {
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
export function observeGlyphPixels(unavailable = false) {
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

export function host() {
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

export function hostObserver() {
  const observer = observations.find((observer) =>
    observer.targets.has(host()),
  );
  if (!observer) throw new Error("Missing terminal host observer");
  return observer;
}

export function domText() {
  return host().querySelector(".xterm-rows")?.textContent ?? "";
}

export async function expectDomText(text: string) {
  await expect.poll(domText).toContain(text);
  const rows = host().querySelector<HTMLElement>(".xterm-rows")!;
  await expect.element(page.elementLocator(rows)).toBeVisible();
  expect(rows.getBoundingClientRect().width).toBeGreaterThan(0);
  expect(rows.getBoundingClientRect().height).toBeGreaterThan(0);
  expect(webglCanvas()).toBeNull();
}

export async function expectGlyphs() {
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

export const prompt = "fixture@mobile:~/project$ omp";
export const snapshot = `\x1b[38;2;255;0;0m${prompt}\x1b[0m\r\n`;

export async function openTerminal(props: ComponentProps<typeof Harness> = {}) {
  const rendered = await render(Harness, props);
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

// openTerminal, plus the hook's public API as Terminal.svelte receives it.
export async function openTerminalApi(
  props: Omit<ComponentProps<typeof Harness>, "expose"> = {},
) {
  let api: TerminalController | undefined;
  const opened = await openTerminal({
    ...props,
    expose: (terminal) => (api = terminal),
  });
  return { ...opened, api: api! };
}

// Waits for the hook's next connection attempt (ws.ts backs off at most one
// second for the first two consecutive drops) and returns that socket.
export async function reconnected(count: number) {
  await expect.poll(() => sockets.length, { timeout: 3000 }).toBe(count);
  return sockets[count - 1];
}

export function expectNoSessionMutation(socket: FixtureSocket) {
  expect(sockets).toEqual([socket]);
  expect(socket.closeCount).toBe(0);
  // Resize frames are legitimate; input and resync frames are not.
  expect(socket.sent.length).toBeGreaterThan(0);
  expect(socket.sent.every((frame) => frame[0] === 1)).toBe(true);
}

export function inputText(socket: FixtureSocket) {
  return socket.sent
    .filter((frame) => frame[0] === 0)
    .map((frame) => new TextDecoder().decode(frame.subarray(1)))
    .join("");
}

export function geometry(socket: FixtureSocket) {
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

export function gestureStart(socket: FixtureSocket, column = 8, row = 12) {
  const { bounds, cell, row: height } = geometry(socket);
  return {
    x: bounds.left + (column - 0.5) * cell,
    y: bounds.top + (row - 0.5) * height,
  };
}

export async function swipe(
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

export function historyLine(line: number) {
  return `history-${String(line).padStart(4, "0")}`;
}

export function firstHistoryLine() {
  const text = host().querySelector(".xterm-rows > div")?.textContent ?? "";
  return Number(/history-(\d+)/.exec(text)?.[1] ?? NaN);
}

export async function populateHistory(socket: FixtureSocket) {
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

export function blurAndTrackFocus() {
  const textarea = host().querySelector<HTMLTextAreaElement>(
    ".xterm-helper-textarea",
  )!;
  textarea.blur();
  const focused = vi.fn();
  textarea.addEventListener("focus", focused);
  touchTeardowns.push(() => textarea.removeEventListener("focus", focused));
  return { textarea, focused };
}

export function pointer(
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

export function syntheticPointerCapture(target: HTMLElement) {
  // Synthetic pointer ids cannot be captured by Chromium. Only that browser
  // boundary is replaced; the real hook, fit, rendering and xterm protocol run.
  return {
    set: vi.spyOn(target, "setPointerCapture").mockImplementation(() => {}),
    release: vi
      .spyOn(target, "releasePointerCapture")
      .mockImplementation(() => {}),
  };
}

// Registers the per-test stubs and the teardown that asserts every socket,
// observer and resolution listener was released. Call once per test file.
export function installTerminalFixture() {
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
}
