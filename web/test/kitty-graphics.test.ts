import { expect, test, vi } from "vitest";
import {
  createKittyGraphics,
  createKittyStream,
  parseKittyControl,
  type KittyImage,
  type KittyPlacement,
  type KittyPlacementSpec,
} from "../src/kitty-graphics";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function raw(width: number, height: number) {
  return btoa(String.fromCharCode(...new Uint8Array(width * height * 4)));
}

// Only the header is needed by the injected metadata-only test decoder.
function pngHeader(width: number, height: number) {
  const bytes = new Uint8Array(24);
  bytes.set([137, 80, 78, 71, 13, 10, 26, 10], 0);
  bytes.set([73, 72, 68, 82], 12);
  const view = new DataView(bytes.buffer);
  view.setUint32(16, width);
  view.setUint32(20, height);
  return btoa(String.fromCharCode(...bytes));
}

function harness(
  decode: (input: {
    bytes: Uint8Array;
    format: number;
    width?: number;
    height?: number;
  }) => Promise<KittyImage> = async ({ width, height }) =>
    fakeImage(width ?? 2, height ?? 2),
  afterMove?: () => Promise<void>,
) {
  const replies: string[] = [];
  const logs: string[] = [];
  const moves: [number, number][] = [];
  const placements: {
    image: KittyImage;
    spec: KittyPlacementSpec;
    disposed: boolean;
    active: boolean;
    handle: KittyPlacement;
  }[] = [];
  let cursor = { column: 0, line: 0 };
  const graphics = createKittyGraphics({
    cellSize: () => ({ width: 10, height: 20 }),
    cursor: () => cursor,
    viewport: () => ({ top: 0, bottom: 24 }),
    reply: (text) => {
      replies.push(text);
      logs.push(`reply:${text}`);
    },
    write: (data, parsed) => {
      logs.push(`text:${decoder.decode(data)}`);
      parsed();
    },
    moveCursor: (columns, rows) => {
      moves.push([columns, rows]);
      cursor = { column: cursor.column + columns, line: cursor.line + rows };
      return afterMove?.();
    },
    decode: vi.fn(decode),
    renderer: {
      place(image, spec, dismissed) {
        const placement = {
          image,
          spec,
          disposed: false,
          active: true,
          handle: undefined as unknown as KittyPlacement,
        };
        placements.push(placement);
        placement.handle = {
          active: () => placement.active,
          dispose() {
            if (placement.disposed) return;
            placement.disposed = true;
            dismissed();
          },
          bounds: () => ({
            column: spec.column,
            line: spec.line,
            columns: spec.columns,
            rows: spec.rows,
          }),
        };
        return placement.handle;
      },
    },
  });
  const send = (value: string) =>
    new Promise<void>((resolve) =>
      graphics.write(encoder.encode(value), resolve),
    );
  return {
    graphics,
    replies,
    logs,
    moves,
    placements,
    send,
    setCursor(column: number, line: number) {
      cursor = { column, line };
    },
  };
}

function fakeImage(width = 2, height = 2): KittyImage {
  return {
    width,
    height,
    source: {} as CanvasImageSource,
  };
}

test("the stream passes text through and buffers APC chunks split lazily", () => {
  const stream = createKittyStream();
  const events: { kind: string; value: string }[] = [];
  const emit = (event: {
    kind: string;
    data?: Uint8Array;
    control?: string;
    payload?: string;
  }) =>
    events.push(
      event.kind === "text"
        ? { kind: "text", value: decoder.decode(event.data) }
        : {
            kind: "graphics",
            value: `${event.control}:${event.payload}`,
          },
    );
  stream.write(encoder.encode("hello\x1b"), emit);
  stream.write(encoder.encode("_G"), emit);
  stream.write(encoder.encode("a=T,i=1;AAAA\x1b"), emit);
  stream.write(encoder.encode("\\world"), emit);
  expect(events).toEqual([
    { kind: "text", value: "hello" },
    { kind: "graphics", value: "a=T,i=1:AAAA" },
    { kind: "text", value: "world" },
  ]);
  expect(stream.pendingBytes).toBe(0);
});

test("non-kitty APC sequences stay in the text stream", () => {
  const stream = createKittyStream();
  const events: { kind: string; value: string }[] = [];
  const emit = (event: {
    kind: string;
    data?: Uint8Array;
    control?: string;
    payload?: string;
  }) =>
    events.push(
      event.kind === "text"
        ? { kind: "text", value: decoder.decode(event.data) }
        : { kind: "graphics", value: `${event.control}:${event.payload}` },
    );
  stream.write(encoder.encode("a\x1b_X b\x1b\\c"), emit);
  expect(events).toEqual([{ kind: "text", value: "a\x1b_X b\x1b\\c" }]);
});

test("control data rejects malformed keys", () => {
  expect(parseKittyControl("a=T,f=100,i=7")).toEqual({
    a: "T",
    f: "100",
    i: "7",
  });
  expect(() => parseKittyControl("ab=1")).toThrow(/invalid control/);
  expect(() => parseKittyControl("a=T,a=p")).toThrow(/invalid control/);
});

test("a transmitted image is stored and displayed at the cursor", async () => {
  const board = harness();
  board.setCursor(3, 5);
  await board.send(`before\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\after`);
  expect(board.logs.filter((entry) => entry.startsWith("text:"))).toEqual([
    "text:before",
    "text:after",
  ]);
  expect(board.placements).toHaveLength(1);
  expect(board.placements[0].spec).toMatchObject({
    column: 3,
    line: 5,
    columns: 1,
    rows: 1,
    drawWidth: 2,
    drawHeight: 2,
    offsetX: 0,
    offsetY: 0,
    z: 0,
  });
  expect(board.moves).toEqual([[1, 0]]);
  expect(board.replies).toEqual(["\x1b_Gi=1;OK\x1b\\"]);
});

test("consecutive placements advance the cursor before the next one", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\`);
  await board.send(`\x1b_Ga=p,i=1\x1b\\\x1b_Ga=p,i=1\x1b\\`);
  expect(board.placements.map((placement) => placement.spec.column)).toEqual([
    0, 1, 2,
  ]);
});

test("source rectangles are clipped to the image", async () => {
  const board = harness();
  await board.send(
    `\x1b_Ga=T,f=32,s=4,v=4,i=1,x=2,y=2,w=100,h=100;${raw(4, 4)}\x1b\\`,
  );
  expect(board.placements[0].spec).toMatchObject({
    sourceX: 2,
    sourceY: 2,
    sourceWidth: 2,
    sourceHeight: 2,
  });
});

test("delete by screen position follows the placement's current line", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\`);
  board.placements[0].spec.line = 2;
  await board.send("\x1b_Ga=d,d=p,x=1,y=3\x1b\\");
  expect(board.placements[0].disposed).toBe(true);
});

test("delete by image-id range accepts full-width ids", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\`);
  await board.send("\x1b_Ga=d,d=r,x=0,y=4294967295\x1b\\");
  expect(board.placements[0].disposed).toBe(true);
});

test("a dismissed anonymous placement releases its image", async () => {
  const dispose = vi.fn();
  const board = harness(async ({ width, height }) => ({
    ...fakeImage(width, height),
    dispose,
  }));
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2;${raw(2, 2)}\x1b\\`);
  expect(board.placements).toHaveLength(1);
  board.placements[0].handle.dispose();
  expect(dispose).toHaveBeenCalledTimes(1);
});

test("placement ids are acknowledged with their resolved image id", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=t,f=32,s=2,v=2,I=13;${raw(2, 2)}\x1b\\`);
  await board.send("\x1b_Ga=p,I=13\x1b\\");
  expect(board.replies.at(-1)).toBe("\x1b_Gi=1,I=13;OK\x1b\\");
  await board.send("\x1b_Ga=p,p=7\x1b\\");
  expect(board.replies.at(-1)).toBe("\x1b_Gi=1,I=13;OK\x1b\\");
});

test("chunked transmission is assembled before display", async () => {
  const board = harness();
  const payload = raw(2, 2);
  await board.send(
    `\x1b_Ga=T,f=32,s=2,v=2,i=7,m=1;${payload.slice(0, 8)}\x1b\\`,
  );
  expect(board.placements).toHaveLength(0);
  await board.send(`\x1b_Gm=1;${payload.slice(8, 16)}\x1b\\`);
  expect(board.placements).toHaveLength(0);
  await board.send(`\x1b_Gm=0;${payload.slice(16)}\x1b\\`);
  expect(board.placements).toHaveLength(1);
  expect(board.replies).toEqual(["\x1b_Gi=7;OK\x1b\\"]);
});

test("the capability query answers before following text and skips decoding", async () => {
  const decode = vi.fn(async () => fakeImage());
  const board = harness(decode);
  await board.send("\x1b_Ga=q,i=9,f=24,s=1,v=1;AAAA\x1b\\after");
  expect(decode).not.toHaveBeenCalled();
  expect(board.logs).toEqual(["reply:\x1b_Gi=9;OK\x1b\\", "text:after"]);
});

test("the C=1 policy leaves the cursor in place", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1,C=1;${raw(2, 2)}\x1b\\`);
  expect(board.moves).toEqual([]);
});

test("placement geometry letterboxes explicit cell rectangles", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=T,f=32,s=4,v=4,i=1,c=2,r=2;${raw(4, 4)}\x1b\\`);
  expect(board.placements[0].spec).toMatchObject({
    columns: 2,
    rows: 2,
    drawWidth: 20,
    drawHeight: 20,
    offsetX: 0,
    offsetY: 10,
  });
});

test("deleting placements keeps image data until an uppercase delete frees it", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=5;${raw(2, 2)}\x1b\\`);
  await board.send("\x1b_Ga=d,d=i,i=5\x1b\\");
  expect(board.placements[0].disposed).toBe(true);
  await board.send("\x1b_Ga=p,i=5\x1b\\");
  expect(board.placements).toHaveLength(2);
  expect(board.placements[1].disposed).toBe(false);
  await board.send("\x1b_Ga=d,d=I,i=5\x1b\\");
  expect(board.placements[1].disposed).toBe(true);
  await board.send("\x1b_Ga=p,i=5\x1b\\");
  expect(board.replies.at(-1)).toBe("\x1b_Gi=5;ENOENT:image not found\x1b\\");
});

test("image numbers allocate ids and report the newest image", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=t,f=32,s=2,v=2,I=13;${raw(2, 2)}\x1b\\`);
  expect(board.replies).toEqual(["\x1b_Gi=1,I=13;OK\x1b\\"]);
  await board.send("\x1b_Ga=p,I=13\x1b\\");
  expect(board.placements).toHaveLength(1);
});

test("unsupported transmission mediums and animation report errors", async () => {
  const board = harness();
  await board.send("\x1b_Ga=t,i=2,t=f;ZmlsZQ==\x1b\\");
  expect(board.replies.at(-1)).toContain("ENOTSUP");
  await board.send("\x1b_Ga=f,i=2;AAAA\x1b\\");
  expect(board.replies.at(-1)).toContain("ENOTSUP");
});

test("reset disposes placements and stored images", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\`);
  board.graphics.reset();
  expect(board.placements[0].disposed).toBe(true);
  await board.send("\x1b_Ga=p,i=1\x1b\\");
  expect(board.replies.at(-1)).toContain("ENOENT");
});

test.each(["reset", "dispose"] as const)(
  "%s invalidates a deferred decode and every previously queued event",
  async (lifecycle) => {
    const { promise: decoded, resolve: finish } =
      Promise.withResolvers<KittyImage>();
    const { promise: started, resolve: began } = Promise.withResolvers<void>();
    const decode = vi.fn(() => {
      began();
      return decoded;
    });
    const board = harness(decode);
    const first = board.send(
      `\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\after`,
    );
    const queued = board.send("queued");
    await started;
    board.graphics[lifecycle]();
    const dispose = vi.fn();
    finish({ ...fakeImage(), dispose });
    await Promise.all([first, queued]);
    expect(dispose).toHaveBeenCalledTimes(1);
    expect(board.placements).toEqual([]);
    expect(board.moves).toEqual([]);
    expect(board.replies).toEqual([]);
    expect(board.logs).toEqual([]);
    if (lifecycle === "dispose") {
      await board.send("ignored");
      expect(board.logs).toEqual([]);
    }
  },
);

test("reset permits new commands without waiting for the stale decoder", async () => {
  const { promise: decoded, resolve: finish } =
    Promise.withResolvers<KittyImage>();
  const { promise: started, resolve: began } = Promise.withResolvers<void>();
  const decode = vi
    .fn()
    .mockImplementationOnce(() => {
      began();
      return decoded;
    })
    .mockImplementation(async () => fakeImage());
  const board = harness(decode);
  const old = board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\stale`);
  await started;
  board.graphics.reset();
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\fresh`);
  const dispose = vi.fn();
  finish({ ...fakeImage(), dispose });
  await old;
  expect(dispose).toHaveBeenCalledTimes(1);
  expect(board.placements).toHaveLength(1);
  expect(board.placements[0].disposed).toBe(false);
  expect(board.replies).toEqual(["\x1b_Gi=1;OK\x1b\\"]);
  expect(board.logs.filter((line) => line.startsWith("text:"))).toEqual([
    "text:fresh",
  ]);
});

test.each(["reset", "dispose"] as const)(
  "%s suppresses a late decoder rejection",
  async (lifecycle) => {
    const { promise: decoded, reject: fail } =
      Promise.withResolvers<KittyImage>();
    const { promise: started, resolve: began } = Promise.withResolvers<void>();
    const board = harness(() => {
      began();
      return decoded;
    });
    const pending = board.send(
      `\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\after`,
    );
    await started;
    board.graphics[lifecycle]();
    fail(new Error("late decoder failure"));
    await pending;
    expect(board.replies).toEqual([]);
    expect(board.logs).toEqual([]);
  },
);

test("a stale cursor completion cannot reply or delete the replacement image", async () => {
  const { promise: moved, reject: fail } = Promise.withResolvers<void>();
  const { promise: started, resolve: began } = Promise.withResolvers<void>();
  const afterMove = vi
    .fn()
    .mockImplementationOnce(() => {
      began();
      return moved;
    })
    .mockResolvedValue(undefined);
  const board = harness(undefined, afterMove);
  const old = board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\old`);
  await started;
  board.graphics.reset();
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\`);
  fail(new Error("late cursor failure"));
  await old;
  expect(board.placements.map((item) => item.disposed)).toEqual([true, false]);
  expect(board.replies).toEqual(["\x1b_Gi=1;OK\x1b\\"]);
  await board.send("\x1b_Ga=p,i=1,C=1\x1b\\");
  expect(board.replies.at(-1)).toBe("\x1b_Gi=1;OK\x1b\\");
});

test("source dimensions accept the exact limits and reject oversized headers before decode", async () => {
  const decode = vi.fn(
    async ({ width, height }: { width?: number; height?: number }) =>
      fakeImage(width, height),
  );
  const board = harness(decode);
  await board.send(`\x1b_Ga=t,f=100,i=1;${pngHeader(8192, 1024)}\x1b\\`);
  expect(board.replies.at(-1)).toBe("\x1b_Gi=1;OK\x1b\\");
  for (const [width, height] of [
    [8193, 1],
    [8192, 1025],
    [1, 8193],
  ]) {
    await board.send(`\x1b_Ga=T,f=100,i=2;${pngHeader(width, height)}\x1b\\`);
    expect(board.replies.at(-1)).toContain(
      "EINVAL:image dimensions exceed source limits",
    );
  }
  expect(decode).toHaveBeenCalledTimes(1);
  expect(board.placements).toHaveLength(0);
});

test("anonymous placements consume aggregate source pixels and release them on dismissal", async () => {
  const dispose = vi.fn();
  const decode = vi.fn(
    async ({ width, height }: { width?: number; height?: number }) => ({
      ...fakeImage(width, height),
      dispose,
    }),
  );
  const board = harness(decode);
  const payload = pngHeader(4096, 2048);
  for (let index = 0; index < 8; index++)
    await board.send(`\x1b_Ga=T,f=100,C=1;${payload}\x1b\\`);
  await board.send(`\x1b_Ga=T,f=100,i=9,C=1;${payload}\x1b\\`);
  expect(board.replies.at(-1)).toContain(
    "ENOSPC:decoded image resource limit exceeded",
  );
  expect(decode).toHaveBeenCalledTimes(8);
  expect(board.placements).toHaveLength(8);
  expect(dispose).not.toHaveBeenCalled();
  board.placements[0].handle.dispose();
  await board.send(`\x1b_Ga=T,f=100,i=9,C=1;${payload}\x1b\\`);
  expect(board.replies.at(-1)).toBe("\x1b_Gi=9;OK\x1b\\");
  expect(decode).toHaveBeenCalledTimes(9);
  expect(dispose).toHaveBeenCalledTimes(1);
  board.graphics.dispose();
  expect(dispose).toHaveBeenCalledTimes(9);
});

test.each([false, true])(
  "the 256-image count includes anonymous=%s and rejects before decoding without evicting live images",
  async (anonymous) => {
    const dispose = vi.fn();
    const decode = vi.fn(async () => ({ ...fakeImage(), dispose }));
    const board = harness(decode);
    for (let index = 1; index <= 256; index++)
      await board.send(
        `\x1b_Ga=T,f=32,s=2,v=2,C=1${anonymous ? "" : `,i=${index}`};${raw(2, 2)}\x1b\\`,
      );
    await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=257,C=1;${raw(2, 2)}\x1b\\`);
    expect(board.replies.at(-1)).toContain(
      "ENOSPC:decoded image resource limit exceeded",
    );
    expect(decode).toHaveBeenCalledTimes(256);
    expect(board.placements).toHaveLength(256);
    expect(dispose).not.toHaveBeenCalled();
    if (!anonymous) {
      await board.send("\x1b_Ga=p,i=1,C=1\x1b\\");
      expect(board.replies.at(-1)).toBe("\x1b_Gi=1;OK\x1b\\");
      expect(board.placements.at(-1)?.image).toBe(board.placements[0].image);
    }
    board.graphics.dispose();
    expect(dispose).toHaveBeenCalledTimes(256);
  },
);

test("1024 placements of one reusable image are bounded and replacement remains possible", async () => {
  const dispose = vi.fn();
  const board = harness(async () => ({ ...fakeImage(), dispose }));
  await board.send(`\x1b_Ga=t,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\`);
  for (let id = 1; id <= 1024; id++)
    await board.send(`\x1b_Ga=p,i=1,p=${id},C=1\x1b\\`);
  await board.send("\x1b_Ga=p,i=1,C=1\x1b\\");
  expect(board.replies.at(-1)).toContain("ENOSPC:placement limit exceeded");
  expect(board.placements).toHaveLength(1024);
  expect(board.moves).toEqual([]);
  expect(dispose).not.toHaveBeenCalled();
  await board.send("\x1b_Ga=p,i=1,p=1,C=1\x1b\\");
  expect(board.replies.at(-1)).toBe("\x1b_Gi=1,p=1;OK\x1b\\");
  expect(board.placements[0].disposed).toBe(true);
  expect(board.placements.filter((item) => !item.disposed)).toHaveLength(1024);
  board.placements[1].handle.dispose();
  await board.send("\x1b_Ga=p,i=1,C=1\x1b\\");
  expect(board.replies.at(-1)).toBe("\x1b_Gi=1;OK\x1b\\");
  expect(dispose).not.toHaveBeenCalled();
});

test.each([0, 1, 2, 3])(
  "eraseDisplay(%s) intersects the visible erased cells and preserves stored images",
  async (mode) => {
    const dispose = vi.fn();
    const board = harness(async () => ({ ...fakeImage(), dispose }));
    await board.send(`\x1b_Ga=t,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\`);
    const positions = [
      [1, 4],
      [1, 5],
      [3, 5],
      [7, 5],
      [1, 6],
      [1, -2],
      [1, 24],
      [1, 6],
    ];
    for (const [column, line] of positions) {
      board.setCursor(column, line);
      await board.send("\x1b_Ga=p,i=1,c=2,r=2,C=1\x1b\\");
    }
    board.placements[7].active = false;
    board.setCursor(4, 5);
    board.graphics.eraseDisplay(mode);
    const expected =
      mode === 0
        ? [false, true, true, true, true, false, false, false]
        : mode === 1
          ? [true, true, true, false, false, false, false, false]
          : [true, true, true, true, true, false, false, false];
    expect(board.placements.map((item) => item.disposed)).toEqual(expected);
    expect(dispose).not.toHaveBeenCalled();
    await board.send("\x1b_Ga=p,i=1,C=1\x1b\\");
    expect(board.replies.at(-1)).toBe("\x1b_Gi=1;OK\x1b\\");
  },
);

test("viewport and positional deletion ignore inactive placements but explicit removal still works", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1,C=1;${raw(2, 2)}\x1b\\`);
  board.placements[0].active = false;
  for (const control of ["d=a", "d=c", "d=p,x=1,y=1", "d=x,x=1", "d=y,y=1"])
    await board.send(`\x1b_Ga=d,${control}\x1b\\`);
  expect(board.placements[0].disposed).toBe(false);
  await board.send("\x1b_Ga=d,d=I,i=1\x1b\\");
  expect(board.placements[0].disposed).toBe(true);
});

test("screen clear disposes anonymous images but preserves offscreen placements", async () => {
  const dispose = vi.fn();
  const board = harness(async () => ({ ...fakeImage(), dispose }));
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,C=1;${raw(2, 2)}\x1b\\`);
  board.setCursor(0, 24);
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,C=1;${raw(2, 2)}\x1b\\`);
  board.graphics.eraseDisplay(2);
  expect(board.placements.map((item) => item.disposed)).toEqual([true, false]);
  expect(dispose).toHaveBeenCalledTimes(1);
});

test("fragmented APC payloads are retained once and oversized payloads drain through ST", () => {
  const stream = createKittyStream();
  const events: unknown[] = [];
  const emit = (event: unknown) => events.push(event);
  stream.write(encoder.encode("\x1b_Ga=T;"), emit);
  for (let index = 0; index < 4096; index++)
    stream.write(encoder.encode("AAAA"), emit);
  expect(stream.pendingBytes).toBe(3 + 4 + 4096 * 4);
  stream.write(encoder.encode("\x1b"), emit);
  stream.write(encoder.encode("\\text"), emit);
  expect(events).toHaveLength(2);
  expect(events[0]).toEqual({
    kind: "graphics",
    control: "a=T",
    payload: "AAAA".repeat(4096),
  });
  expect(events[1]).toEqual({ kind: "text", data: encoder.encode("text") });

  events.length = 0;
  stream.write(encoder.encode("\x1b_Ga=T;"), emit);
  const chunk = new Uint8Array(1024 * 1024).fill(65);
  for (let index = 0; index < 65; index++) {
    stream.write(chunk, emit);
    expect(stream.pendingBytes).toBeLessThanOrEqual(64 * 1024 * 1024);
  }
  stream.write(encoder.encode("hidden\x1b"), emit);
  stream.write(encoder.encode("\\after\x1b_Ga=d\x1b\\"), emit);
  expect(events).toEqual([
    { kind: "text", data: encoder.encode("after") },
    { kind: "graphics", control: "a=d", payload: "" },
  ]);
  expect(stream.pendingBytes).toBe(0);
});

test("decode reservations remain bounded across reset and release after late settlement", async () => {
  const gates = Array.from({ length: 8 }, () => ({
    decoded: Promise.withResolvers<KittyImage>(),
    started: Promise.withResolvers<void>(),
  }));
  let calls = 0;
  const board = harness(() => {
    const gate = gates[calls++];
    if (!gate) return Promise.resolve(fakeImage(4096, 2048));
    gate.started.resolve();
    return gate.decoded.promise;
  });
  const sends: Promise<void>[] = [];
  const payload = pngHeader(4096, 2048);
  for (const gate of gates) {
    sends.push(board.send(`\x1b_Ga=T,f=100,i=1,C=1;${payload}\x1b\\stale`));
    await gate.started.promise;
    board.graphics.reset();
  }
  await board.send(`\x1b_Ga=T,f=100,i=9,C=1;${payload}\x1b\\`);
  expect(calls).toBe(8);
  expect(board.replies).toEqual([
    "\x1b_Gi=9;ENOSPC:decoded image resource limit exceeded\x1b\\",
  ]);
  const dispose = vi.fn();
  for (const gate of gates)
    gate.decoded.resolve({ ...fakeImage(4096, 2048), dispose });
  await Promise.all(sends);
  expect(dispose).toHaveBeenCalledTimes(8);
  expect(board.placements).toEqual([]);
  expect(board.moves).toEqual([]);
  expect(board.logs.some((line) => line.startsWith("text:"))).toBe(false);
  await board.send(`\x1b_Ga=T,f=100,i=9,C=1;${payload}\x1b\\`);
  expect(calls).toBe(9);
  expect(board.replies.at(-1)).toBe("\x1b_Gi=9;OK\x1b\\");
  expect(board.placements).toHaveLength(1);
  board.graphics.dispose();
});

test("a decoded size mismatch is disposed and never reaches the renderer", async () => {
  const dispose = vi.fn();
  const board = harness(async () => ({ ...fakeImage(8193, 8193), dispose }));
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\`);
  expect(board.replies.at(-1)).toContain(
    "EINVAL:decoded image dimensions do not match",
  );
  expect(dispose).toHaveBeenCalledTimes(1);
  expect(board.placements).toEqual([]);
  expect(board.moves).toEqual([]);
});

test("full storage rejects an incoming displayed image without closing any existing image", async () => {
  const dispose = vi.fn();
  const decode = vi.fn(async () => ({ ...fakeImage(), dispose }));
  const board = harness(decode);
  for (let id = 1; id <= 256; id++)
    await board.send(`\x1b_Ga=t,f=32,s=2,v=2,i=${id};${raw(2, 2)}\x1b\\`);
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=257,C=1;${raw(2, 2)}\x1b\\`);
  expect(board.replies.at(-1)).toContain(
    "ENOSPC:decoded image resource limit exceeded",
  );
  expect(decode).toHaveBeenCalledTimes(256);
  expect(dispose).not.toHaveBeenCalled();
  expect(board.placements).toEqual([]);
  await board.send("\x1b_Ga=p,i=1,C=1\x1b\\");
  expect(board.replies.at(-1)).toBe("\x1b_Gi=1;OK\x1b\\");
  await board.send("\x1b_Ga=d,d=I,i=256\x1b\\");
  await board.send(`\x1b_Ga=T,f=32,s=2,v=2,i=257,C=1;${raw(2, 2)}\x1b\\`);
  expect(board.replies.at(-1)).toBe("\x1b_Gi=257;OK\x1b\\");
  expect(decode).toHaveBeenCalledTimes(257);
  expect(dispose).toHaveBeenCalledTimes(1);
  expect(board.placements.at(-1)?.disposed).toBe(false);
  board.graphics.dispose();
  expect(dispose).toHaveBeenCalledTimes(257);
});

test("relative placements fail explicitly without moving or displaying the stored image", async () => {
  const board = harness();
  await board.send(`\x1b_Ga=t,f=32,s=2,v=2,i=1;${raw(2, 2)}\x1b\\`);
  await board.send("\x1b_Ga=p,i=1,P=2,Q=3,H=1,V=1\x1b\\");
  expect(board.replies.at(-1)).toContain(
    "ENOTSUP:relative placements are not supported",
  );
  expect(board.placements).toEqual([]);
  expect(board.moves).toEqual([]);
  await board.send("\x1b_Ga=p,i=1,C=1\x1b\\");
  expect(board.placements[0].image).toMatchObject({ width: 2, height: 2 });
});

test("base64 whitespace neither changes decoded pixel length nor fabricates pixels", async () => {
  const board = harness();
  await board.send("\x1b_Ga=q,f=24,s=1,v=1,i=1;AQID    \x1b\\");
  expect(board.replies.at(-1)).toBe("\x1b_Gi=1;OK\x1b\\");
  await board.send("\x1b_Ga=q,f=24,s=2,v=1,i=2;AQID    \x1b\\");
  expect(board.replies.at(-1)).toContain("EINVAL:invalid pixel data length");
});

test("base64 padding before a decoding chunk boundary is rejected", async () => {
  const board = harness();
  const payload = "AAAA".repeat(1048575) + "AA==AAAA";
  await board.send(`\x1b_Ga=q,f=32,o=z,i=1;${payload}\x1b\\`);
  expect(board.replies.at(-1)).toContain("EINVAL:invalid base64 payload");
});

test("reset during asynchronous query validation prevents a late reply", async () => {
  vi.useFakeTimers();
  const board = harness();
  try {
    const payload = "AAAA".repeat(2048 * 1025);
    const pending = board.send(
      `\x1b_Ga=q,f=24,s=2048,v=1025,i=1;${payload}\x1b\\`,
    );
    await vi.advanceTimersByTimeAsync(0);
    expect(board.replies).toEqual([]);
    board.graphics.reset();
    await vi.runAllTimersAsync();
    await pending;
    expect(board.replies).toEqual([]);
    await board.send("\x1b_Ga=q,f=24,s=1,v=1,i=2;AQID\x1b\\");
    expect(board.replies).toEqual(["\x1b_Gi=2;OK\x1b\\"]);
  } finally {
    board.graphics.dispose();
    vi.useRealTimers();
  }
});
