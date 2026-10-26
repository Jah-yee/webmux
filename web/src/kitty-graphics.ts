const ESC = 0x1b;
const APC = 0x5f;
const GRAPHICS = 0x47;
const ST = 0x5c;
const SEMICOLON = 0x3b;
const latin1Decoder = new TextDecoder("latin1");
const base64ChunkChars = 4 * 1024 * 1024;

type FromBase64 = (input: string) => Uint8Array;
const fromBase64 = (Uint8Array as unknown as { fromBase64?: FromBase64 })
  .fromBase64;

function yieldToMainThread() {
  return new Promise<void>((resolve) => setTimeout(resolve, 0));
}

// Source budgets include anonymous placements and outstanding decode reservations.
// Saturation is explicit ENOSPC; live resources are never silently evicted.
const maxEncodedBytes = 48 * 1024 * 1024;
const maxImageBytes = 32 * 1024 * 1024;
const maxPixels = maxImageBytes / 4;
const maxImageDimension = 8192;
const maxTotalPixels = 64 * 1024 * 1024;
const maxImages = 256;
const maxPlacements = 1024;
const maxPendingEscapeBytes = 64 * 1024 * 1024;

class KittyCommandError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

interface KittyCellSize {
  width: number;
  height: number;
}

interface KittyCursor {
  column: number;
  line: number;
}

interface KittyViewport {
  top: number;
  bottom: number;
}

export interface KittyImage {
  width: number;
  height: number;
  source: CanvasImageSource;
  dispose?(): void;
}

export interface KittyPlacementSpec {
  sourceX: number;
  sourceY: number;
  sourceWidth: number;
  sourceHeight: number;
  offsetX: number;
  offsetY: number;
  drawWidth: number;
  drawHeight: number;
  columns: number;
  rows: number;
  column: number;
  line: number;
  z: number;
  relayout(cell: KittyCellSize): KittyLayout;
}

export interface KittyPlacement {
  dispose(): void;
  active?(): boolean;
  bounds(): { column: number; line: number; columns: number; rows: number };
}

interface KittyRenderer {
  place(
    image: KittyImage,
    spec: KittyPlacementSpec,
    dismissed: () => void,
  ): KittyPlacement;
}

interface KittyDecodeInput {
  bytes: Uint8Array;
  format: number;
  width?: number;
  height?: number;
}

type KittyDecode = (input: KittyDecodeInput) => Promise<KittyImage>;

export interface KittyGraphicsOptions {
  cellSize(): KittyCellSize | undefined;
  cursor(): KittyCursor;
  viewport(): KittyViewport;
  reply(text: string): void;
  write(data: Uint8Array, parsed: () => void): void;
  moveCursor(columns: number, rows: number): Promise<void> | void;
  renderer: KittyRenderer;
  decode?: KittyDecode;
}

export type KittyEvent =
  | { kind: "text"; data: Uint8Array }
  | { kind: "graphics"; control: string; payload: string };

export function createKittyStream() {
  let prefix = 0;
  let graphics = false;
  let discarded = false;
  let escaped = false;
  let pending = new Uint8Array(0);
  let length = 0;

  function append(bytes: Uint8Array) {
    if (discarded || bytes.length === 0) return;
    if (length + bytes.length > maxPendingEscapeBytes - 4) {
      discarded = true;
      pending = new Uint8Array(0);
      length = 0;
      return;
    }
    if (length + bytes.length > pending.length) {
      const capacity = Math.min(
        maxPendingEscapeBytes - 4,
        Math.max(256, length + bytes.length, pending.length * 2),
      );
      const grown = new Uint8Array(capacity);
      grown.set(pending.subarray(0, length));
      pending = grown;
    }
    pending.set(bytes, length);
    length += bytes.length;
  }

  return {
    write(data: Uint8Array, emit: (event: KittyEvent) => void) {
      let cursor = 0;
      let textStart = 0;
      const flushText = (end: number) => {
        if (end > textStart)
          emit({ kind: "text", data: data.subarray(textStart, end) });
      };
      const flushPrefix = () => {
        if (prefix === 1) emit({ kind: "text", data: new Uint8Array([ESC]) });
        else if (prefix === 2)
          emit({ kind: "text", data: new Uint8Array([ESC, APC]) });
        prefix = 0;
      };
      while (cursor < data.length) {
        if (graphics) {
          if (escaped) {
            escaped = false;
            if (data[cursor] === ST) {
              if (!discarded) emitGraphics(pending.subarray(0, length), emit);
              graphics = false;
              discarded = false;
              pending = new Uint8Array(0);
              length = 0;
              textStart = ++cursor;
              continue;
            }
            append(new Uint8Array([ESC]));
          }
          const stop = data.indexOf(ESC, cursor);
          const end = stop < 0 ? data.length : stop;
          append(data.subarray(cursor, end));
          cursor = end;
          if (stop >= 0) {
            escaped = true;
            cursor++;
          }
          textStart = cursor;
          continue;
        }
        if (prefix === 1) {
          if (data[cursor] === APC) {
            prefix = 2;
            cursor++;
            textStart = cursor;
            continue;
          }
          flushPrefix();
          textStart = cursor;
          continue;
        }
        if (prefix === 2) {
          if (data[cursor] === GRAPHICS) {
            prefix = 0;
            graphics = true;
            cursor++;
            textStart = cursor;
            continue;
          }
          flushPrefix();
          textStart = cursor;
          continue;
        }
        const escape = data.indexOf(ESC, cursor);
        if (escape < 0) {
          cursor = data.length;
          break;
        }
        if (escape + 1 >= data.length) {
          flushText(escape);
          prefix = 1;
          cursor = data.length;
          textStart = cursor;
          break;
        }
        if (data[escape + 1] === APC) {
          if (escape + 2 >= data.length) {
            flushText(escape);
            prefix = 2;
            cursor = data.length;
            textStart = cursor;
            break;
          }
          if (data[escape + 2] === GRAPHICS) {
            flushText(escape);
            graphics = true;
            cursor = escape + 3;
            textStart = cursor;
            continue;
          }
        }
        cursor = escape + 1;
      }
      if (!graphics) flushText(cursor);
    },
    reset() {
      prefix = 0;
      graphics = false;
      discarded = false;
      escaped = false;
      pending = new Uint8Array(0);
      length = 0;
    },
    get pendingBytes() {
      return graphics ? length + (escaped ? 1 : 0) + 3 : prefix;
    },
  };
}

function emitGraphics(payload: Uint8Array, emit: (event: KittyEvent) => void) {
  const separator = payload.indexOf(SEMICOLON);
  emit({
    kind: "graphics",
    control:
      separator < 0
        ? latin1Decoder.decode(payload)
        : latin1Decoder.decode(payload.subarray(0, separator)),
    payload:
      separator < 0
        ? ""
        : latin1Decoder.decode(payload.subarray(separator + 1)),
  });
}

export function parseKittyControl(text: string): Record<string, string> {
  const params: Record<string, string> = {};
  for (const part of text.split(",")) {
    if (!part) continue;
    const equals = part.indexOf("=");
    if (equals <= 0) throw new KittyCommandError("EINVAL", "invalid control");
    const key = part.slice(0, equals);
    if (key.length !== 1 || key in params)
      throw new KittyCommandError("EINVAL", "invalid control");
    params[key] = part.slice(equals + 1);
  }
  return params;
}

function pngSize(bytes: Uint8Array) {
  if (bytes.length < 24) return undefined;
  const signature = [137, 80, 78, 71, 13, 10, 26, 10];
  if (signature.some((value, index) => bytes[index] !== value))
    return undefined;
  if (
    bytes[12] !== 73 ||
    bytes[13] !== 72 ||
    bytes[14] !== 68 ||
    bytes[15] !== 82
  )
    return undefined;
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const width = view.getUint32(16);
  const height = view.getUint32(20);
  return width > 0 && height > 0 ? { width, height } : undefined;
}

async function decodeKittyImage({
  bytes,
  format,
  width,
  height,
}: KittyDecodeInput): Promise<KittyImage> {
  if (format === 100) {
    const bitmap = await createImageBitmap(
      new Blob([asArrayBuffer(bytes)], { type: "image/png" }),
    );
    return {
      width: bitmap.width,
      height: bitmap.height,
      source: bitmap,
      dispose: () => bitmap.close(),
    };
  }
  if ((format !== 24 && format !== 32) || !width || !height)
    throw new KittyCommandError("EINVAL", "invalid image dimensions");
  const channels = format === 24 ? 3 : 4;
  if (bytes.length !== width * height * channels)
    throw new KittyCommandError("EINVAL", "invalid pixel data length");
  const rgba = new Uint8ClampedArray(width * height * 4);
  for (let source = 0, target = 0; target < rgba.length;) {
    rgba[target++] = bytes[source++];
    rgba[target++] = bytes[source++];
    rgba[target++] = bytes[source++];
    rgba[target++] = channels === 3 ? 255 : bytes[source++];
  }
  const bitmap = await createImageBitmap(new ImageData(rgba, width, height));
  return {
    width,
    height,
    source: bitmap,
    dispose: () => bitmap.close(),
  };
}

function asArrayBuffer(bytes: Uint8Array) {
  if (bytes.byteOffset === 0 && bytes.byteLength === bytes.buffer.byteLength)
    return bytes.buffer as ArrayBuffer;
  return bytes.buffer.slice(
    bytes.byteOffset,
    bytes.byteOffset + bytes.byteLength,
  ) as ArrayBuffer;
}

interface KittyLayout {
  sourceX: number;
  sourceY: number;
  sourceWidth: number;
  sourceHeight: number;
  offsetX: number;
  offsetY: number;
  drawWidth: number;
  drawHeight: number;
  columns: number;
  rows: number;
}

function kittyLayout(
  params: Record<string, string>,
  image: KittyImage,
  cell: KittyCellSize,
): KittyLayout {
  const sourceX = optionalInteger(
    params,
    "x",
    0,
    Math.max(0, image.width - 1),
    0,
  );
  const sourceY = optionalInteger(
    params,
    "y",
    0,
    Math.max(0, image.height - 1),
    0,
  );
  const availableWidth = image.width - sourceX;
  const availableHeight = image.height - sourceY;
  const sourceWidth = Math.min(
    optionalInteger(params, "w", 1, 4_294_967_295, availableWidth),
    availableWidth,
  );
  const sourceHeight = Math.min(
    optionalInteger(params, "h", 1, 4_294_967_295, availableHeight),
    availableHeight,
  );
  if (sourceWidth <= 0 || sourceHeight <= 0)
    throw new KittyCommandError("EINVAL", "invalid source rectangle");
  const columns = optionalInteger(params, "c", 1, 1_000_000);
  const rows = optionalInteger(params, "r", 1, 1_000_000);
  const offsetX = optionalInteger(params, "X", 0, cell.width, 0);
  const offsetY = optionalInteger(params, "Y", 0, cell.height, 0);
  const aspect = sourceWidth / sourceHeight;
  let drawWidth: number;
  let drawHeight: number;
  let usedColumns: number;
  let usedRows: number;
  let letterboxX = 0;
  let letterboxY = 0;
  if (columns !== undefined && rows !== undefined) {
    const boxWidth = columns * cell.width;
    const boxHeight = rows * cell.height;
    const scale = Math.min(boxWidth / sourceWidth, boxHeight / sourceHeight);
    drawWidth = sourceWidth * scale;
    drawHeight = sourceHeight * scale;
    letterboxX = (boxWidth - drawWidth) / 2;
    letterboxY = (boxHeight - drawHeight) / 2;
    usedColumns = columns;
    usedRows = rows;
  } else if (columns !== undefined) {
    drawWidth = columns * cell.width;
    drawHeight = drawWidth / aspect;
    usedColumns = columns;
    usedRows = Math.ceil(drawHeight / cell.height);
  } else if (rows !== undefined) {
    drawHeight = rows * cell.height;
    drawWidth = drawHeight * aspect;
    usedColumns = Math.ceil(drawWidth / cell.width);
    usedRows = rows;
  } else {
    drawWidth = sourceWidth;
    drawHeight = sourceHeight;
    usedColumns = Math.ceil(drawWidth / cell.width);
    usedRows = Math.ceil(drawHeight / cell.height);
  }
  return {
    sourceX,
    sourceY,
    sourceWidth,
    sourceHeight,
    offsetX: offsetX + letterboxX,
    offsetY: offsetY + letterboxY,
    drawWidth,
    drawHeight,
    columns: usedColumns,
    rows: usedRows,
  };
}

interface StoredImage {
  id: number;
  number?: number;
  image: KittyImage;
}

interface StoredPlacement {
  image: KittyImage;
  imageId: number;
  placementId: number;
  z: number;
  handle: KittyPlacement;
}

export function createKittyGraphics(options: KittyGraphicsOptions) {
  const stream = createKittyStream();
  const images = new Map<number, StoredImage>();
  const numbers = new Map<number, number>();
  const placements = new Set<StoredPlacement>();
  const decoder = options.decode ?? decodeKittyImage;
  // Includes anonymous images retained only by a live placement.
  const resources = new Map<KittyImage, number>();
  let resourcePixels = 0;
  // Reservations outlive reset until their non-cancellable decoder settles.
  let pendingImages = 0;
  let pendingPixels = 0;
  let upload:
    | { control: Record<string, string>; parts: string[]; size: number }
    | undefined;
  let nextId = 1;
  let chain: Promise<void> = Promise.resolve();
  let chainId = 0;
  let chainBusy = false;
  let pendingWrites = 0;
  let writeWaiters: (() => void)[] = [];
  let disposed = false;
  let generation = 0;

  function current(expected: number) {
    return !disposed && expected === generation;
  }

  function cellSize() {
    const size = options.cellSize();
    if (
      !size ||
      !Number.isFinite(size.width) ||
      !Number.isFinite(size.height) ||
      size.width <= 0 ||
      size.height <= 0
    )
      throw new KittyCommandError("EINVAL", "terminal is not ready");
    return size;
  }

  function respond(
    params: Record<string, string>,
    message: string,
    assigned?: { id?: number; number?: number },
  ) {
    const quiet = optionalInteger(params, "q", 0, 1_000_000, 0) ?? 0;
    if (quiet >= 2) return;
    if (message === "OK" && quiet >= 1) return;
    const parts: string[] = [];
    const id = assigned?.id ?? optionalId(params, "i");
    const number = assigned?.number ?? optionalId(params, "I");
    if (id === undefined && number === undefined) return;
    if (id !== undefined) parts.push(`i=${id}`);
    if (number !== undefined) parts.push(`I=${number}`);
    const placement = optionalId(params, "p");
    if (placement !== undefined) parts.push(`p=${placement}`);
    options.reply(`\x1b_G${parts.join(",")};${message}\x1b\\`);
  }

  function respondError(params: Record<string, string>, error: unknown) {
    const command =
      error instanceof KittyCommandError
        ? error
        : new KittyCommandError(
            "EINVAL",
            error instanceof Error ? error.message : "graphics command failed",
          );
    respond(params, `${command.code}:${command.message}`);
  }

  function checkSourceDimensions(width: number, height: number) {
    if (
      !Number.isInteger(width) ||
      !Number.isInteger(height) ||
      width < 1 ||
      height < 1 ||
      width > maxImageDimension ||
      height > maxImageDimension ||
      width * height > maxPixels
    )
      throw new KittyCommandError(
        "EINVAL",
        "image dimensions exceed source limits",
      );
  }

  function checkAdmission(
    width: number,
    height: number,
    params: Record<string, string>,
  ) {
    checkSourceDimensions(width, height);
    const id = optionalId(params, "i");
    const number = optionalId(params, "I");
    if (id !== undefined && number !== undefined)
      throw new KittyCommandError("EINVAL", "specify either i or I");
    if (
      resources.size + pendingImages + 1 > maxImages ||
      resourcePixels + pendingPixels + width * height > maxTotalPixels
    )
      throw new KittyCommandError(
        "ENOSPC",
        "decoded image resource limit exceeded",
      );
  }

  function releaseImage(image: KittyImage) {
    const pixels = resources.get(image);
    if (pixels === undefined) return;
    resources.delete(image);
    resourcePixels -= pixels;
    image.dispose?.();
  }

  function storeImage(image: KittyImage, params: Record<string, string>) {
    const id = optionalId(params, "i");
    const number = optionalId(params, "I");
    const assigned = id ?? (number === undefined ? 0 : allocateId());
    if (id !== undefined) removeImage(id);
    const stored = { id: assigned, number, image };
    if (assigned !== 0) images.set(assigned, stored);
    if (number !== undefined) numbers.set(number, assigned);
    return stored;
  }

  function allocateId() {
    for (let attempts = 0; attempts < 1_000_000; attempts++) {
      const id = nextId;
      nextId = nextId >= 4_294_967_295 ? 1 : nextId + 1;
      if (!images.has(id)) return id;
    }
    throw new KittyCommandError("ENOSPC", "no image ids available");
  }

  function hasPlacements(imageId: number) {
    for (const placement of placements)
      if (placement.imageId === imageId) return true;
    return false;
  }

  function releaseAnonymousImage(image: KittyImage) {
    for (const placement of placements) if (placement.image === image) return;
    releaseImage(image);
  }

  function forgetPlacement(placement: StoredPlacement) {
    if (!placements.delete(placement)) return;
    if (placement.imageId === 0) releaseAnonymousImage(placement.image);
  }

  function removeImage(imageId: number) {
    for (const placement of Array.from(placements)) {
      if (placement.imageId !== imageId) continue;
      placement.handle.dispose();
      forgetPlacement(placement);
    }
    const stored = images.get(imageId);
    if (stored) releaseImage(stored.image);
    images.delete(imageId);
    if (stored?.number !== undefined && numbers.get(stored.number) === imageId)
      numbers.delete(stored.number);
  }

  function resolveImage(params: Record<string, string>) {
    const id = optionalId(params, "i");
    const number = optionalId(params, "I");
    if (id !== undefined && number !== undefined)
      throw new KittyCommandError("EINVAL", "specify either i or I");
    const resolved =
      id ?? (number === undefined ? undefined : numbers.get(number));
    if (resolved === undefined || resolved === 0) return undefined;
    return images.get(resolved);
  }

  async function addPlacement(
    stored: StoredImage,
    params: Record<string, string>,
    expected: number,
  ) {
    if (!current(expected)) return;
    if (params.U === "1")
      throw new KittyCommandError(
        "ENOTSUP",
        "unicode placeholder placements are not supported",
      );
    const cell = cellSize();
    const layout = kittyLayout(params, stored.image, cell);
    const cursor = options.cursor();
    const placementId = optionalId(params, "p") ?? 0;
    const z =
      optionalInteger(params, "z", -2_147_483_648, 2_147_483_647, 0) ?? 0;
    if (stored.id !== 0 && placementId !== 0) {
      for (const placement of Array.from(placements)) {
        if (
          placement.imageId === stored.id &&
          placement.placementId === placementId
        ) {
          placement.handle.dispose();
          forgetPlacement(placement);
        }
      }
    }
    if (placements.size >= maxPlacements)
      throw new KittyCommandError("ENOSPC", "placement limit exceeded");
    const placement: StoredPlacement = {
      image: stored.image,
      imageId: stored.id,
      placementId: stored.id === 0 ? 0 : placementId,
      z,
      handle: undefined as unknown as KittyPlacement,
    };
    placement.handle = options.renderer.place(
      stored.image,
      {
        ...layout,
        column: cursor.column,
        line: cursor.line,
        z,
        relayout: (next) => kittyLayout(params, stored.image, next),
      },
      () => forgetPlacement(placement),
    );
    placements.add(placement);
    if (params.C !== "1")
      await options.moveCursor(layout.columns, Math.max(0, layout.rows - 1));
    return placement;
  }

  async function inflate(bytes: Uint8Array, expected: number) {
    const reader = new Blob([asArrayBuffer(bytes)])
      .stream()
      .pipeThrough(new DecompressionStream("deflate"))
      .getReader();
    const chunks: Uint8Array[] = [];
    let total = 0;
    for (;;) {
      const { done, value } = await reader.read();
      if (!current(expected)) {
        await reader.cancel();
        return;
      }
      if (done) break;
      total += value.length;
      if (total > maxImageBytes) {
        await reader.cancel();
        throw new KittyCommandError("EINVAL", "image is too large");
      }
      chunks.push(value);
    }
    const merged = new Uint8Array(total);
    let offset = 0;
    for (const chunk of chunks) {
      merged.set(chunk, offset);
      offset += chunk.length;
    }
    return merged;
  }

  async function decodeBase64(payload: string) {
    if (payload.length > maxEncodedBytes)
      throw new KittyCommandError("EINVAL", "image is too large");
    if (/[\t\n\f\r ]/.test(payload))
      payload = payload.replace(/[\t\n\f\r ]/g, "");
    if (payload.length === 0)
      throw new KittyCommandError("EINVAL", "empty image payload");
    if (payload.length % 4 !== 0)
      throw new KittyCommandError("EINVAL", "invalid base64 payload");
    let padding = 0;
    if (payload.endsWith("==")) padding = 2;
    else if (payload.endsWith("=")) padding = 1;
    const paddingStart = payload.indexOf("=");
    if (paddingStart !== -1 && paddingStart < payload.length - padding)
      throw new KittyCommandError("EINVAL", "invalid base64 payload");
    const size = (payload.length / 4) * 3 - padding;
    if (size > maxImageBytes)
      throw new KittyCommandError("EINVAL", "image is too large");
    const bytes = new Uint8Array(size);
    let target = 0;
    for (let offset = 0; offset < payload.length; offset += base64ChunkChars) {
      const end = Math.min(payload.length, offset + base64ChunkChars);
      let part: Uint8Array;
      if (fromBase64) {
        try {
          part = fromBase64(payload.slice(offset, end));
        } catch {
          throw new KittyCommandError("EINVAL", "invalid base64 payload");
        }
        bytes.set(part, target);
        target += part.length;
      } else {
        let binary: string;
        try {
          binary = atob(payload.slice(offset, end));
        } catch {
          throw new KittyCommandError("EINVAL", "invalid base64 payload");
        }
        for (let index = 0; index < binary.length; index++)
          bytes[target++] = binary.charCodeAt(index);
      }
      if (end < payload.length) await yieldToMainThread();
    }
    return bytes;
  }

  function transferFormat(params: Record<string, string>) {
    if ((params.t ?? "d") !== "d")
      throw new KittyCommandError(
        "ENOTSUP",
        "only direct image transmission is supported",
      );
    const format = optionalInteger(params, "f", 0, 1_000_000, 32) ?? 32;
    if (format !== 24 && format !== 32 && format !== 100)
      throw new KittyCommandError("EINVAL", "unsupported image format");
    return format;
  }

  async function decode(
    params: Record<string, string>,
    payload: string,
    expected: number,
  ) {
    const format = transferFormat(params);
    let bytes = await decodeBase64(payload);
    if (params.o !== undefined) {
      if (params.o !== "z")
        throw new KittyCommandError("ENOTSUP", "unsupported compression");
      const inflated = await inflate(bytes, expected);
      if (!inflated) return;
      bytes = inflated;
    }
    const width = optionalInteger(params, "s", 1, 1_000_000);
    const height = optionalInteger(params, "v", 1, 1_000_000);
    const size =
      format === 100
        ? pngSize(bytes)
        : width && height
          ? { width, height }
          : undefined;
    if (!size)
      throw new KittyCommandError(
        "EINVAL",
        format === 100 ? "invalid PNG data" : "image dimensions are required",
      );
    checkAdmission(size.width, size.height, params);
    if (format !== 100) {
      const expected = size.width * size.height * (format === 24 ? 3 : 4);
      if (bytes.length !== expected)
        throw new KittyCommandError("EINVAL", "invalid pixel data length");
    }
    pendingImages++;
    pendingPixels += size.width * size.height;
    try {
      const image = await decoder({
        bytes,
        format,
        width: size.width,
        height: size.height,
      });
      if (!current(expected)) {
        image.dispose?.();
        return;
      }
      if (image.width !== size.width || image.height !== size.height) {
        image.dispose?.();
        throw new KittyCommandError(
          "EINVAL",
          "decoded image dimensions do not match",
        );
      }
      resources.set(image, image.width * image.height);
      resourcePixels += image.width * image.height;
      return image;
    } finally {
      pendingImages--;
      pendingPixels -= size.width * size.height;
    }
  }

  async function transmit(
    params: Record<string, string>,
    payload: string,
    display: boolean,
    expected: number,
  ) {
    const image = await decode(params, payload, expected);
    if (!image) return;
    if (!current(expected)) {
      releaseImage(image);
      return;
    }
    let stored: StoredImage;
    try {
      stored = storeImage(image, params);
    } catch (error) {
      releaseImage(image);
      throw error;
    }
    if (display) {
      try {
        await addPlacement(stored, params, expected);
      } catch (error) {
        if (!current(expected)) return;
        if (stored.id === 0) releaseImage(image);
        else removeImage(stored.id);
        throw error;
      }
    } else if (stored.id === 0) {
      releaseImage(image);
    }
    if (!current(expected)) return;
    respond(params, "OK", {
      id: stored.id === 0 ? undefined : stored.id,
      number: stored.number,
    });
  }

  async function inspectImage(params: Record<string, string>, payload: string) {
    const format = transferFormat(params);
    const bytes = await decodeBase64(payload);
    if (params.o !== undefined) {
      if (params.o !== "z")
        throw new KittyCommandError("ENOTSUP", "unsupported compression");
      return;
    }
    if (format === 100) {
      const size = pngSize(bytes);
      if (!size) throw new KittyCommandError("EINVAL", "invalid PNG data");
      checkSourceDimensions(size.width, size.height);
      return;
    }
    const width = optionalInteger(params, "s", 1, 1_000_000);
    const height = optionalInteger(params, "v", 1, 1_000_000);
    if (!width || !height)
      throw new KittyCommandError("EINVAL", "image dimensions are required");
    checkSourceDimensions(width, height);
    if (bytes.length !== width * height * (format === 24 ? 3 : 4))
      throw new KittyCommandError("EINVAL", "invalid pixel data length");
  }

  async function execute(
    params: Record<string, string>,
    payload: string,
    expected: number,
  ) {
    const action = params.a ?? "t";
    if (action === "q") {
      await inspectImage(params, payload);
      if (!current(expected)) return;
      respond(params, "OK");
      return;
    }
    if (
      params.P !== undefined ||
      params.Q !== undefined ||
      params.H !== undefined ||
      params.V !== undefined
    )
      throw new KittyCommandError(
        "ENOTSUP",
        "relative placements are not supported",
      );
    if (params.U === "1")
      throw new KittyCommandError(
        "ENOTSUP",
        "unicode placeholder placements are not supported",
      );
    switch (action) {
      case "t":
        await transmit(params, payload, false, expected);
        return;
      case "T":
        await transmit(params, payload, true, expected);
        return;
      case "p": {
        const stored = resolveImage(params);
        if (!stored) throw new KittyCommandError("ENOENT", "image not found");
        await addPlacement(stored, params, expected);
        if (!current(expected)) return;
        respond(params, "OK", { id: stored.id, number: stored.number });
        return;
      }
      case "d":
        remove(params);
        return;
      case "f":
      case "a":
      case "c":
        throw new KittyCommandError("ENOTSUP", "animation is not supported");
      default:
        throw new KittyCommandError("EINVAL", "unknown action");
    }
  }

  function remove(params: Record<string, string>) {
    const d = params.d ?? "a";
    if (d === "f" || d === "F") return;
    const kind = d.toLowerCase();
    const hard = d === d.toUpperCase();
    const viewport = options.viewport();
    const cursor = options.cursor();
    const z = optionalInteger(params, "z", -2_147_483_648, 2_147_483_647);
    const placement = optionalId(params, "p");
    const imageId = optionalId(params, "i");
    const number = optionalId(params, "I");
    const usesCell =
      kind === "p" || kind === "q" || kind === "x" || kind === "y";
    const targetColumn = usesCell
      ? optionalInteger(params, "x", 1, 4_294_967_295)
      : undefined;
    const targetRow = usesCell
      ? optionalInteger(params, "y", 1, 4_294_967_295)
      : undefined;
    const targetCellColumn =
      targetColumn === undefined ? undefined : targetColumn - 1;
    const targetCellLine =
      targetRow === undefined ? undefined : viewport.top + targetRow - 1;
    const rangeLow =
      kind === "r" ? optionalInteger(params, "x", 0, 4_294_967_295) : undefined;
    const rangeHigh =
      kind === "r" ? optionalInteger(params, "y", 0, 4_294_967_295) : undefined;
    const numberId = number === undefined ? undefined : numbers.get(number);
    const removedImages = new Set<number>();
    for (const item of Array.from(placements)) {
      if (
        kind !== "i" &&
        kind !== "n" &&
        kind !== "r" &&
        kind !== "z" &&
        item.handle.active?.() === false
      )
        continue;
      const bounds = item.handle.bounds();
      let match = false;
      switch (kind) {
        case "a":
          match =
            bounds.line + bounds.rows > viewport.top &&
            bounds.line < viewport.bottom;
          break;
        case "i":
          match =
            imageId !== undefined &&
            item.imageId === imageId &&
            (placement === undefined || item.placementId === placement);
          break;
        case "n":
          match =
            numberId !== undefined &&
            item.imageId === numberId &&
            (placement === undefined || item.placementId === placement);
          break;
        case "c":
          match = intersects(item, cursor.column, cursor.line);
          break;
        case "p":
          match =
            targetCellColumn !== undefined &&
            targetCellLine !== undefined &&
            intersects(item, targetCellColumn, targetCellLine);
          break;
        case "q":
          match =
            targetCellColumn !== undefined &&
            targetCellLine !== undefined &&
            intersects(item, targetCellColumn, targetCellLine) &&
            (z === undefined || item.z === z);
          break;
        case "r":
          match =
            rangeLow !== undefined &&
            rangeHigh !== undefined &&
            item.imageId >= rangeLow &&
            item.imageId <= rangeHigh;
          break;
        case "x":
          match =
            targetCellColumn !== undefined &&
            targetCellColumn >= bounds.column &&
            targetCellColumn < bounds.column + bounds.columns;
          break;
        case "y":
          match =
            targetCellLine !== undefined &&
            targetCellLine >= bounds.line &&
            targetCellLine < bounds.line + bounds.rows;
          break;
        case "z":
          match = z !== undefined && item.z === z;
          break;
        default:
          throw new KittyCommandError("EINVAL", "unknown delete operation");
      }
      if (!match) continue;
      item.handle.dispose();
      forgetPlacement(item);
      if (item.imageId !== 0) removedImages.add(item.imageId);
    }
    if (hard) {
      if (numberId !== undefined) removedImages.add(numberId);
      if (imageId !== undefined) removedImages.add(imageId);
      for (const id of removedImages) if (!hasPlacements(id)) removeImage(id);
    }
  }

  function eraseDisplay(mode: number) {
    if (disposed || mode < 0 || mode > 3) return;
    const viewport = options.viewport();
    const cursor = options.cursor();
    for (const item of Array.from(placements)) {
      if (item.handle.active?.() === false) continue;
      const bounds = item.handle.bounds();
      const firstLine = Math.max(bounds.line, viewport.top);
      const lastLine = Math.min(bounds.line + bounds.rows, viewport.bottom) - 1;
      if (firstLine > lastLine) continue;
      const match =
        mode >= 2 ||
        (mode === 0 &&
          (lastLine > cursor.line ||
            (lastLine === cursor.line &&
              bounds.column + bounds.columns > cursor.column))) ||
        (mode === 1 &&
          (firstLine < cursor.line ||
            (firstLine === cursor.line && bounds.column <= cursor.column)));
      if (!match) continue;
      item.handle.dispose();
      forgetPlacement(item);
    }
  }

  function intersects(
    placement: StoredPlacement,
    column: number,
    line: number,
  ) {
    const bounds = placement.handle.bounds();
    return (
      column >= bounds.column &&
      column < bounds.column + bounds.columns &&
      line >= bounds.line &&
      line < bounds.line + bounds.rows
    );
  }

  async function handle(control: string, payload: string, expected: number) {
    let params: Record<string, string>;
    try {
      params = parseKittyControl(control);
    } catch {
      return;
    }
    const action = params.a;
    if (upload) {
      if (action === undefined) {
        upload.parts.push(payload);
        upload.size += payload.length;
        if (upload.size > maxEncodedBytes) {
          const failed = upload.control;
          upload = undefined;
          respondError(
            failed,
            new KittyCommandError("EINVAL", "image is too large"),
          );
          return;
        }
        if (params.m === "1") return;
        const finished = upload;
        upload = undefined;
        try {
          await execute(finished.control, finished.parts.join(""), expected);
        } catch (error) {
          if (current(expected)) respondError(finished.control, error);
        }
        return;
      }
      upload = undefined;
    }
    if (action === "f" || action === "a" || action === "c") {
      respondError(
        params,
        new KittyCommandError("ENOTSUP", "animation is not supported"),
      );
      return;
    }
    if (action === undefined && params.m !== undefined) {
      respondError(
        params,
        new KittyCommandError("EINVAL", "unexpected image chunk"),
      );
      return;
    }
    if (params.m === "1") {
      if (payload.length > maxEncodedBytes) {
        respondError(
          params,
          new KittyCommandError("EINVAL", "image is too large"),
        );
        return;
      }
      upload = { control: params, parts: [payload], size: payload.length };
      return;
    }
    try {
      await execute(params, payload, expected);
    } catch (error) {
      if (current(expected)) respondError(params, error);
    }
  }

  function writeText(data: Uint8Array, parsed?: () => void) {
    pendingWrites++;
    options.write(data, () => {
      pendingWrites--;
      if (pendingWrites === 0 && writeWaiters.length > 0) {
        const waiters = writeWaiters;
        writeWaiters = [];
        for (const resolve of waiters) resolve();
      }
      parsed?.();
    });
  }

  function drainWrites() {
    if (pendingWrites === 0) return Promise.resolve();
    return new Promise<void>((resolve) => writeWaiters.push(resolve));
  }

  function writeDirect(events: KittyEvent[], parsed: () => void) {
    let last = -1;
    for (let index = 0; index < events.length; index++) {
      const event = events[index];
      if (event.kind === "text" && event.data.length > 0) last = index;
    }
    if (last < 0) {
      parsed();
      return;
    }
    for (let index = 0; index <= last; index++) {
      const event = events[index];
      if (event.kind !== "text" || event.data.length === 0) continue;
      writeText(event.data, index === last ? parsed : undefined);
    }
  }

  function process(data: Uint8Array, parsed: () => void) {
    if (disposed) {
      parsed();
      return;
    }
    const expected = generation;
    const events: KittyEvent[] = [];
    stream.write(data, (event) => events.push(event));
    const hasGraphics = events.some((event) => event.kind === "graphics");
    if (!hasGraphics && !chainBusy) {
      writeDirect(events, parsed);
      return;
    }
    const id = ++chainId;
    chainBusy = true;
    chain = chain
      .then(async () => {
        if (!current(expected)) return;
        for (const event of events) {
          if (!current(expected)) return;
          if (event.kind === "text") {
            if (event.data.length > 0) {
              const { promise, resolve } = Promise.withResolvers<void>();
              options.write(event.data, resolve);
              await promise;
            }
          } else {
            await drainWrites();
            await handle(event.control, event.payload, expected);
          }
        }
      })
      .catch(() => {})
      .finally(() => {
        if (id === chainId) chainBusy = false;
        parsed();
      });
  }

  function clear() {
    generation++;
    chainId++;
    chainBusy = false;
    chain = Promise.resolve();
    upload = undefined;
    stream.reset();
    for (const placement of Array.from(placements)) {
      placement.handle.dispose();
      forgetPlacement(placement);
    }
    placements.clear();
    for (const image of resources.keys()) releaseImage(image);
    images.clear();
    numbers.clear();
  }

  return {
    write: process,
    eraseDisplay,
    reset() {
      clear();
      nextId = 1;
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      clear();
    },
  };
}

function optionalId(params: Record<string, string>, key: string) {
  const value = optionalInteger(params, key, 0, 4_294_967_295);
  return value === 0 ? undefined : value;
}

function optionalInteger(
  params: Record<string, string>,
  key: string,
  min: number,
  max: number,
): number | undefined;
function optionalInteger(
  params: Record<string, string>,
  key: string,
  min: number,
  max: number,
  fallback: number,
): number;
function optionalInteger(
  params: Record<string, string>,
  key: string,
  min: number,
  max: number,
  fallback?: number,
) {
  const raw = params[key];
  if (raw === undefined || raw === "") return fallback;
  if (!/^-?\d+$/.test(raw))
    throw new KittyCommandError("EINVAL", `invalid ${key}`);
  const value = Number(raw);
  if (!Number.isSafeInteger(value) || value < min || value > max)
    throw new KittyCommandError("EINVAL", `invalid ${key}`);
  return value;
}
