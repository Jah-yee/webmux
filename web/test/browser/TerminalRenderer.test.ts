// WebGL/DOM renderers, DPR changes, fit, fonts and theme colours.
import { expect, test } from "vitest";
import { page, userEvent } from "vitest/browser";
import {
  domText,
  dpr,
  expectDomText,
  expectGlyphs,
  expectNoSessionMutation,
  geometry,
  glyphPixels,
  host,
  hostObserver,
  installTerminalFixture,
  measuredEntry,
  observeGlyphPixels,
  openTerminal,
  openTerminalApi,
  prompt,
  resolutionQueries,
  setDpr,
  setMeasurement,
  sockets,
} from "./terminal-fixture";

installTerminalFixture();

test("mobile high-DPR CSS-pixel observations render the restored prompt and retain working input", async () => {
  setDpr(2.625);
  setMeasurement("css-pixels");
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
  setMeasurement("css-pixels");
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

for (const theme of ["light", "dark"]) {
  test(`the space below the last whole row shows the ${theme} terminal background`, async () => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    await openTerminal();
    const rows = host()
      .querySelector(".xterm-scrollable-element")!
      .getBoundingClientRect();
    const bottom = host().getBoundingClientRect().bottom;
    // The host is not a whole number of rows tall, leaving a strip.
    expect(bottom - rows.bottom).toBeGreaterThanOrEqual(2);
    let painted: Element | null = document.elementFromPoint(
      rows.left + rows.width / 2,
      (rows.bottom + bottom) / 2,
    );
    while (
      painted &&
      getComputedStyle(painted).backgroundColor === "rgba(0, 0, 0, 0)"
    )
      painted = painted.parentElement;
    const expected = document.createElement("div");
    expected.style.background = "var(--terminal-background)";
    document.body.append(expected);
    expect(painted && getComputedStyle(painted).backgroundColor).toBe(
      getComputedStyle(expected).backgroundColor,
    );
    expected.remove();
  });
}

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

test("font size is restored, clamped, persisted and re-fits the PTY grid", async () => {
  observeGlyphPixels(true);
  localStorage.setItem("webmux.fontSize", "18");
  const { socket, api } = await openTerminalApi();
  expect(api.fontSize).toBe(18);
  expect(getComputedStyle(host().querySelector(".xterm-rows")!).fontSize).toBe(
    "18px",
  );
  const initial = geometry(socket).cell;

  api.setFontSize(30);
  expect(api.fontSize).toBe(20);
  expect(localStorage.getItem("webmux.fontSize")).toBe("20");
  await expect.poll(() => geometry(socket).cell).toBeGreaterThan(initial);
  api.setFontSize(3);
  expect(api.fontSize).toBe(12);
  expect(localStorage.getItem("webmux.fontSize")).toBe("12");
  await expect.poll(() => geometry(socket).cell).toBeLessThan(initial);
  api.setFontSize(15.6);
  expect(api.fontSize).toBe(16);
  expect(localStorage.getItem("webmux.fontSize")).toBe("16");
  await expect
    .poll(() => getComputedStyle(host().querySelector(".xterm-rows")!).fontSize)
    .toBe("16px");
  expectNoSessionMutation(socket);
});
