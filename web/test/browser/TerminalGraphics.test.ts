import { expect, test, vi } from "vitest";
import { kittyScrollFrame, kittyTextRedraw } from "../fixtures/kitty-scroll";
import {
  domText,
  expectDomText,
  geometry,
  host,
  installTerminalFixture,
  observeGlyphPixels,
  openTerminal,
  resolutionQueries,
  openTerminalApi,
  setDpr,
  type FixtureSocket,
} from "./terminal-fixture";

installTerminalFixture();

const pixels = "/wAA/wD/AP8AAP///////w==";

function canvases() {
  return Array.from(
    host().querySelectorAll<HTMLCanvasElement>(
      ".xterm-graphics-layer .xterm-graphics-image",
    ),
  );
}

function replies(socket: FixtureSocket) {
  return socket.sent
    .filter((frame) => frame[0] === 6)
    .map((frame) => new TextDecoder().decode(frame.subarray(1)))
    .join("");
}

test("a kitty image displays above the grid and is acknowledged", async () => {
  const { socket } = await openTerminal();
  socket.receive(0, `\x1b_Ga=T,f=32,s=2,v=2,i=1;${pixels}\x1b\\`);
  await expect.poll(() => canvases().length).toBe(1);
  const canvas = canvases()[0];
  expect(canvas.style.width).not.toBe("");
  expect(canvas.style.height).not.toBe("");
  expect(
    Array.from(canvas.getContext("2d")!.getImageData(0, 0, 1, 1).data),
  ).toEqual([255, 0, 0, 255]);
  await expect.poll(() => replies(socket)).toContain("\x1b_Gi=1;OK\x1b\\");
});

test("chunks split across socket frames display one image", async () => {
  const { socket } = await openTerminal();
  socket.receive(
    0,
    `\x1b_Ga=T,f=32,s=2,v=2,i=3,m=1;${pixels.slice(0, 8)}\x1b\\`,
  );
  expect(canvases()).toHaveLength(0);
  socket.receive(0, `\x1b_Gm=0;${pixels.slice(8)}\x1b\\`);
  await expect.poll(() => canvases().length).toBe(1);
  await expect.poll(() => replies(socket).split("i=3;OK").length - 1).toBe(1);
});

test("deleting and replaying a session clear kitty placements", async () => {
  const { socket } = await openTerminal();
  socket.receive(0, `\x1b_Ga=T,f=32,s=2,v=2,i=1;${pixels}\x1b\\`);
  await expect.poll(() => canvases().length).toBe(1);
  socket.receive(0, "\x1b_Ga=d,d=i,i=1\x1b\\");
  await expect.poll(() => canvases().length).toBe(0);
  socket.receive(0, `\x1b_Ga=T,f=32,s=2,v=2,i=2;${pixels}\x1b\\`);
  await expect.poll(() => canvases().length).toBe(1);
  socket.receive(1, "replayed");
  await expect.poll(() => canvases().length).toBe(0);
});

test("the graphics query is answered without an image", async () => {
  const { socket } = await openTerminal();
  socket.receive(0, "\x1b_Ga=q,i=7,f=24,s=1,v=1;AAAA\x1b\\");
  await expect.poll(() => replies(socket)).toContain("\x1b_Gi=7;OK\x1b\\");
  expect(canvases()).toHaveLength(0);
});

test("normal placements are hidden in alternate screens and restored without alternate-image bleed", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  socket.receive(0, `\x1b_Ga=T,f=32,s=2,v=2,i=11,C=1;${pixels}\x1b\\`);
  await expect.poll(() => canvases().length).toBe(1);
  const normal = canvases()[0];
  socket.receive(
    0,
    `\x1b[?1049h\x1b[2Jalternate screen\x1b_Ga=T,f=32,s=2,v=2,i=12,C=1;${pixels}\x1b\\`,
  );
  await expectDomText("alternate screen");
  await expect.poll(() => canvases().length).toBe(2);
  const alternate = canvases().find((canvas) => canvas !== normal)!;
  await expect.poll(() => normal.style.display).toBe("none");
  await expect.poll(() => alternate.style.display).not.toBe("none");

  socket.receive(0, "\x1b_Ga=d,d=a\x1b\\");
  await expect.poll(() => canvases().length).toBe(1);
  expect(canvases()[0]).toBe(normal);
  expect(alternate.width).toBe(0);
  socket.receive(0, "\x1b_Ga=p,i=12,C=1\x1b\\");
  await expect.poll(() => canvases().length).toBe(2);
  const replacement = canvases().find((canvas) => canvas !== normal)!;
  socket.receive(0, "\x1b[?1049l");
  await expect.poll(() => canvases().length).toBe(1);
  await expect.poll(() => normal.style.display).not.toBe("none");
  expect(replacement.width).toBe(0);
  socket.receive(0, "\x1b[?1049hsecond alternate screen");
  await expectDomText("second alternate screen");
  await expect.poll(() => normal.style.display).toBe("none");
  expect(canvases()).toEqual([normal]);
  socket.receive(0, "\x1b[?1049l");
  await expect.poll(() => normal.style.display).not.toBe("none");
});

test("ordered native clear erases overlays and text but preserves the image for following placements", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  socket.receive(
    0,
    `\x1b[Hbefore clear\x1b_Ga=T,f=32,s=2,v=2,i=21,C=1;${pixels}\x1b\\`,
  );
  await expectDomText("before clear");
  await expect.poll(() => canvases().length).toBe(1);
  const erased = canvases()[0];
  socket.receive(
    0,
    "\x1b[2J\x1b[Hafter clear\r\n\x1b_Ga=p,i=21,p=2,C=1\x1b\\following placement",
  );
  await expectDomText("following placement");
  expect(domText()).toContain("after clear");
  expect(domText()).not.toContain("before clear");
  await expect.poll(() => canvases().length).toBe(1);
  expect(canvases()[0]).not.toBe(erased);
  expect(erased.width).toBe(0);
  expect(erased.height).toBe(0);
  expect(replies(socket)).toContain("\x1b_Gi=21,p=2;OK\x1b\\");
});

test.each([0, 1, 3])(
  "native erase-display mode %i preserves graphics",
  async (mode) => {
    observeGlyphPixels(true);
    const { socket } = await openTerminal();
    socket.receive(
      0,
      `\x1b[H\x1b_Ga=T,f=32,s=2,v=2,i=71,C=1;${pixels}\x1b\\` +
        `\x1b[3;1H\x1b_Ga=T,f=32,s=2,v=2,i=72,C=1;${pixels}\x1b\\`,
    );
    await expect.poll(() => canvases().length).toBe(2);
    const [upper, lower] = canvases();
    socket.receive(0, `\x1b[2;1H\x1b[${mode}Jtext after partial clear`);
    await expectDomText("text after partial clear");
    expect(canvases()).toEqual([upper, lower]);
    expect(upper.width).toBeGreaterThan(0);
    expect(lower.width).toBeGreaterThan(0);
  },
);

test("omp redraw and top-cropped placements survive partial text erasure", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  for (const step of [0, 1, 2, 1, 0]) {
    socket.receive(0, kittyScrollFrame(step));
    await expectDomText(`Kitty scroll step ${step}:`);
    await expect.poll(() => canvases().length).toBe(1);
    const canvas = canvases()[0];
    const colors = [
      [255, 0, 0, 255],
      [0, 255, 0, 255],
      [0, 0, 255, 255],
    ];
    await expect
      .poll(() =>
        Array.from(
          canvas
            .getContext("2d")!
            .getImageData(
              Math.floor(canvas.width / 2),
              Math.floor(canvas.height / (8 - step * 2)),
              1,
              1,
            ).data,
        ),
      )
      .toEqual(colors[step]);
    expect(canvas.style.display).not.toBe("none");
    socket.receive(0, kittyTextRedraw(step));
    await expectDomText(`Kitty redraw step ${step}:`);
    expect(canvases()).toEqual([canvas]);
  }
});

test("images follow output into scrollback, return with history, and release on trim", async () => {
  observeGlyphPixels(true);
  const { socket, api } = await openTerminalApi();
  socket.receive(
    0,
    `\x1b[Himage-anchor\x1b_Ga=T,f=32,s=2,v=2,i=82,C=1;${pixels}\x1b\\`,
  );
  await expect.poll(() => canvases().length).toBe(1);
  const canvas = canvases()[0];
  socket.receive(0, "\r\nhistory output".repeat(100));
  await expect.poll(() => canvas.style.display).toBe("none");
  api.findNext("image-anchor", {
    caseSensitive: true,
    wholeWord: false,
    regex: false,
  });
  await expectDomText("image-anchor");
  await expect.poll(() => canvas.style.display).not.toBe("none");
  api.scrollToBottom();
  await expect.poll(() => canvas.style.display).toBe("none");
  socket.receive(0, "\x1b[3J");
  await expect.poll(() => canvases().length).toBe(0);
  expect(canvas.width).toBe(0);
});

test("margin scrolling moves images with text and leaves outside rows fixed", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  socket.receive(
    0,
    `\x1b[H\x1b_Ga=T,f=32,s=2,v=2,i=83,C=1;${pixels}\x1b\\` +
      `\x1b[5;1H\x1b_Ga=T,f=32,s=2,v=2,i=84,C=1;${pixels}\x1b\\`,
  );
  await expect.poll(() => canvases().length).toBe(2);
  const [outside, inside] = canvases();
  const top = () => inside.getBoundingClientRect().top;
  await expect.poll(() => inside.style.transform).not.toBe("");
  const initial = top();
  const fixed = outside.getBoundingClientRect().top;
  const height = geometry(socket).row;
  socket.receive(0, "\x1b[2;10r\x1b[10;1H\n");
  await expect.poll(() => top()).toBeCloseTo(initial - height, 0);
  expect(outside.getBoundingClientRect().top).toBe(fixed);
  socket.receive(0, "\x1b[2;1H\x1bM");
  await expect.poll(() => top()).toBeCloseTo(initial, 0);
  socket.receive(0, "\x1b[3;1H\x1b[L");
  await expect.poll(() => top()).toBeCloseTo(initial + height, 0);
  socket.receive(0, "\x1b[M\x1b[J");
  await expect.poll(() => top()).toBeCloseTo(initial, 0);
  expect(canvases()).toEqual([outside, inside]);
});

test("line erasure and resizing preserve graphics, while native reset removes old placements", async () => {
  observeGlyphPixels(true);
  const { socket, api } = await openTerminalApi();
  socket.receive(0, `\x1b[H\x1b_Ga=T,f=32,s=2,v=2,i=85,C=1;${pixels}\x1b\\`);
  await expect.poll(() => canvases().length).toBe(1);
  const canvas = canvases()[0];
  socket.receive(0, "\x1b[2K\x1b[Xerased line");
  await expectDomText("erased line");
  expect(canvases()).toEqual([canvas]);
  api.setFontSize(18);
  await expect.poll(() => geometry(socket).row).toBeGreaterThan(20);
  expect(canvases()).toEqual([canvas]);
  socket.receive(
    0,
    `\x1bc\x1b_Ga=T,f=32,s=2,v=2,i=86,C=1;${pixels}\x1b\\after reset`,
  );
  await expectDomText("after reset");
  await expect.poll(() => canvases().length).toBe(1);
  expect(canvases()[0]).not.toBe(canvas);
  expect(canvas.width).toBe(0);
});

test.each([
  { name: "per-axis", edge: 100_000 },
  { name: "per-canvas pixels", edge: 3000 },
])(
  "oversized $name scaling fails explicitly before allocating a backing canvas",
  async ({ edge }) => {
    const { socket } = await openTerminal();
    const columns = Math.ceil(edge / geometry(socket).cell);
    socket.receive(
      0,
      `\x1b_Ga=t,f=32,s=2,v=2,i=31;${pixels}\x1b\\\x1b_Ga=p,i=31,c=${columns},C=1\x1b\\`,
    );
    await expect
      .poll(() => replies(socket))
      .toContain("\x1b_Gi=31;EINVAL:graphics canvas size limit exceeded\x1b\\");
    expect(canvases()).toHaveLength(0);
    socket.receive(0, "\x1b_Ga=p,i=31,C=1\x1b\\");
    await expect.poll(() => canvases().length).toBe(1);
  },
);

test("aggregate backing pixels are bounded and deletion and replacement reclaim their allocation", async () => {
  const { socket } = await openTerminal();
  const columns = Math.floor(2048 / geometry(socket).cell);
  socket.receive(
    0,
    `\x1b_Ga=T,f=32,s=2,v=2,i=41,p=1,c=${columns},C=1;${pixels}\x1b\\`,
  );
  await expect.poll(() => canvases().length).toBe(1);
  const first = canvases()[0];
  const perCanvasPixels = first.width * first.height;
  const capacity = Math.floor((16 * 1024 * 1024) / perCanvasPixels);
  expect(capacity).toBeGreaterThan(1);
  for (let placement = 2; placement <= capacity + 1; placement++)
    socket.receive(0, `\x1b_Ga=p,i=41,p=${placement},c=${columns},C=1\x1b\\`);
  await expect
    .poll(() => replies(socket))
    .toContain(
      `\x1b_Gi=41,p=${capacity + 1};EINVAL:graphics canvas total pixel limit exceeded\x1b\\`,
    );
  expect(canvases()).toHaveLength(capacity);
  expect(
    canvases().reduce(
      (total, canvas) => total + canvas.width * canvas.height,
      0,
    ),
  ).toBeLessThanOrEqual(16 * 1024 * 1024);

  socket.receive(0, `\x1b_Ga=p,i=41,p=1,c=${columns},C=1\x1b\\`);
  await expect.poll(() => first.width).toBe(0);
  await expect
    .poll(() => replies(socket).split("i=41,p=1;OK").length - 1)
    .toBe(2);
  expect(canvases()).toHaveLength(capacity);
  socket.receive(0, `\x1b[?1049h\x1b_Ga=p,i=41,p=98,c=${columns},C=1\x1b\\`);
  await expect
    .poll(() => replies(socket))
    .toContain(
      "\x1b_Gi=41,p=98;EINVAL:graphics canvas total pixel limit exceeded\x1b\\",
    );
  expect(canvases()).toHaveLength(capacity);
  expect(canvases().every((canvas) => canvas.style.display === "none")).toBe(
    true,
  );
  socket.receive(0, "\x1b[?1049l");
  await expect.poll(() => canvases()[0].style.display).not.toBe("none");
  const previous = canvases();
  socket.receive(0, "\x1b_Ga=d,d=i,i=41\x1b\\");
  await expect.poll(() => canvases().length).toBe(0);
  expect(
    previous.every((canvas) => canvas.width === 0 && canvas.height === 0),
  ).toBe(true);
  socket.receive(0, `\x1b_Ga=p,i=41,p=99,c=${columns},C=1\x1b\\`);
  await expect.poll(() => canvases().length).toBe(1);
});

test("DPR changes redraw within the aggregate budget and release placements that no longer fit", async () => {
  const { socket } = await openTerminal();
  const columns = Math.floor(1000 / geometry(socket).cell);
  socket.receive(
    0,
    `\x1b_Ga=T,f=32,s=2,v=2,i=51,p=1,c=${columns},C=1;${pixels}\x1b\\`,
  );
  for (let placement = 2; placement <= 5; placement++)
    socket.receive(0, `\x1b_Ga=p,i=51,p=${placement},c=${columns},C=1\x1b\\`);
  await expect.poll(() => canvases().length).toBe(5);
  const previous = canvases();
  const initialWidth = previous[0].width;
  setDpr(2);
  await expect.poll(() => canvases().length).toBeLessThan(5);
  expect(canvases().length).toBeGreaterThan(0);
  expect(
    canvases().every(
      (canvas) => Math.abs(canvas.width - initialWidth * 2) <= 1,
    ),
  ).toBe(true);
  expect(
    canvases().reduce(
      (total, canvas) => total + canvas.width * canvas.height,
      0,
    ),
  ).toBeLessThanOrEqual(16 * 1024 * 1024);
  expect(
    previous
      .filter((canvas) => !canvas.isConnected)
      .every((canvas) => canvas.width === 0),
  ).toBe(true);
});

test("teardown releases canvases and listeners and ignores a late image decode", async () => {
  const { socket, rendered } = await openTerminal();
  socket.receive(0, `\x1b_Ga=T,f=32,s=2,v=2,i=61,C=1;${pixels}\x1b\\`);
  await expect.poll(() => canvases().length).toBe(1);
  const canvas = canvases()[0];
  const source = document.createElement("canvas");
  source.width = 2;
  source.height = 2;
  const bitmap = await createImageBitmap(source);
  const closed = vi.spyOn(bitmap, "close");
  const { promise, resolve } = Promise.withResolvers<ImageBitmap>();
  const decode = vi
    .spyOn(globalThis, "createImageBitmap")
    .mockReturnValueOnce(promise);
  socket.receive(0, `\x1b_Ga=T,f=32,s=2,v=2,i=62,C=1;${pixels}\x1b\\`);
  await expect.poll(() => decode.mock.calls.length).toBe(1);
  const layer = canvas.parentElement!;
  const queries = [...resolutionQueries];
  await rendered.unmount();
  const sent = socket.sent.length;
  expect(canvas.width).toBe(0);
  expect(canvas.height).toBe(0);
  expect(layer.isConnected).toBe(false);
  for (const query of queries) query.dispatchEvent(new Event("change"));
  resolve(bitmap);
  await expect.poll(() => closed.mock.calls.length).toBe(1);
  expect(layer.childElementCount).toBe(0);
  expect(socket.sent).toHaveLength(sent);
  expect(queries.every((query) => query.listeners.size === 0)).toBe(true);
});
