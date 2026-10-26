import type { IBuffer, IMarker, Terminal } from "@xterm/xterm";
import {
  createKittyGraphics,
  type KittyImage,
  type KittyPlacement,
  type KittyPlacementSpec,
} from "../kitty-graphics";
import { observeDevicePixelRatio } from "./terminal-dpr";

export interface TerminalGraphics {
  write(data: Uint8Array, parsed: () => void): void;
  reset(): void;
  dispose(): void;
}

// Backing stores, including hidden-buffer placements, are limited to 32 MiB
// each and 64 MiB in total. Keep each axis within conservative browser limits.
const maxCanvasDimension = 8192;
const maxCanvasPixels = 8 * 1024 * 1024;
const maxTotalCanvasPixels = 16 * 1024 * 1024;

interface RenderedPlacement {
  canvas: HTMLCanvasElement;
  image: KittyImage;
  spec: KittyPlacementSpec;
  buffer: IBuffer;
  pixels: number;
  marker?: IMarker;
  dismissed: () => void;
  closed: boolean;
}

export function attachTerminalGraphics(
  term: Terminal,
  reply: (text: string) => void,
): TerminalGraphics {
  let disposed = false;
  const screen = term.element?.querySelector<HTMLElement>(".xterm-screen");
  if (!screen)
    return {
      write: (data, parsed) => {
        if (!disposed)
          term.write(data, () => {
            if (!disposed) parsed();
          });
      },
      reset() {},
      dispose() {
        disposed = true;
      },
    };
  const target = screen;
  const layer = document.createElement("div");
  layer.className = "xterm-graphics-layer";
  layer.setAttribute("aria-hidden", "true");
  screen.appendChild(layer);
  let frame = 0;
  let lastDpr = 0;
  let lastCellWidth = 0;
  let lastCellHeight = 0;
  let totalCanvasPixels = 0;
  const rendered = new Set<RenderedPlacement>();

  function cellSize() {
    if (term.cols <= 0 || term.rows <= 0) return undefined;
    const bounds = target.getBoundingClientRect();
    if (bounds.width <= 0 || bounds.height <= 0) return undefined;
    return {
      width: bounds.width / term.cols,
      height: bounds.height / term.rows,
    };
  }

  function cursor() {
    const buffer = term.buffer.active;
    return { column: buffer.cursorX, line: buffer.baseY + buffer.cursorY };
  }

  function viewport() {
    const buffer = term.buffer.active;
    return { top: buffer.viewportY, bottom: buffer.viewportY + term.rows };
  }

  function backingSize(spec: KittyPlacementSpec, previousPixels = 0) {
    const dpr = window.devicePixelRatio || 1;
    const width = Math.max(1, Math.round(spec.drawWidth * dpr));
    const height = Math.max(1, Math.round(spec.drawHeight * dpr));
    const pixels = width * height;
    if (
      !Number.isFinite(width) ||
      !Number.isFinite(height) ||
      width > maxCanvasDimension ||
      height > maxCanvasDimension ||
      pixels > maxCanvasPixels
    )
      throw new Error("graphics canvas size limit exceeded");
    if (totalCanvasPixels - previousPixels + pixels > maxTotalCanvasPixels)
      throw new Error("graphics canvas total pixel limit exceeded");
    return { width, height, pixels };
  }

  function draw(placement: RenderedPlacement) {
    const { canvas, image, spec } = placement;
    const { width, height, pixels } = backingSize(spec, placement.pixels);
    if (canvas.width !== width || canvas.height !== height) {
      // Release the old store first: changing one axis at a time must not
      // temporarily allocate the new width times the old height.
      canvas.width = 0;
      canvas.height = 0;
      canvas.width = width;
      canvas.height = height;
    }
    totalCanvasPixels += pixels - placement.pixels;
    placement.pixels = pixels;
    canvas.style.width = `${spec.drawWidth}px`;
    canvas.style.height = `${spec.drawHeight}px`;
    const context = canvas.getContext("2d");
    if (!context) throw new Error("graphics canvas context is unavailable");
    context.clearRect(0, 0, width, height);
    context.imageSmoothingEnabled = true;
    context.drawImage(
      image.source,
      spec.sourceX,
      spec.sourceY,
      spec.sourceWidth,
      spec.sourceHeight,
      0,
      0,
      width,
      height,
    );
  }

  function update() {
    if (disposed) return;
    if (rendered.size === 0) return;
    const cell = cellSize();
    if (!cell) return;
    const dpr = window.devicePixelRatio || 1;
    const redraw = dpr !== lastDpr;
    const resized =
      cell.width !== lastCellWidth || cell.height !== lastCellHeight;
    lastDpr = dpr;
    lastCellWidth = cell.width;
    lastCellHeight = cell.height;
    const buffer = term.buffer.active;
    const viewportY = buffer.viewportY;
    for (const placement of rendered) {
      if (placement.closed) continue;
      try {
        if (resized)
          Object.assign(placement.spec, placement.spec.relayout(cell));
        if (redraw || resized) draw(placement);
      } catch {
        // A later resize/DPR change has no protocol command to reject.
        // Dismiss the placement rather than retain an invalid backing store.
        remove(placement);
        continue;
      }
      const marker = placement.marker;
      const line =
        marker && !marker.isDisposed ? marker.line : placement.spec.line;
      const left = placement.spec.column * cell.width + placement.spec.offsetX;
      const top = (line - viewportY) * cell.height + placement.spec.offsetY;
      const visible =
        placement.buffer === buffer &&
        line + placement.spec.rows > viewportY &&
        line < viewportY + term.rows;
      placement.canvas.style.display = visible ? "" : "none";
      placement.canvas.style.transform = `translate(${left}px, ${top}px)`;
    }
  }

  function schedule() {
    if (disposed || frame) return;
    frame = requestAnimationFrame(() => {
      frame = 0;
      update();
    });
  }

  function remove(placement: RenderedPlacement) {
    if (placement.closed) return;
    placement.closed = true;
    rendered.delete(placement);
    placement.marker?.dispose();
    totalCanvasPixels -= placement.pixels;
    placement.pixels = 0;
    placement.canvas.width = 0;
    placement.canvas.height = 0;
    placement.canvas.remove();
    placement.dismissed();
    schedule();
  }

  const graphics = createKittyGraphics({
    cellSize,
    cursor,
    viewport,
    reply: (text) => {
      if (!disposed) reply(text);
    },
    write: (data, parsed) => {
      if (!disposed) term.write(data, parsed);
    },
    moveCursor(columns, rows) {
      if (disposed) return;
      let sequence = "";
      if (rows > 0) sequence += `\x1b[${rows}B`;
      if (columns > 0) sequence += `\x1b[${columns}C`;
      if (!sequence) return;
      const { promise, resolve } = Promise.withResolvers<void>();
      term.write(sequence, resolve);
      return promise;
    },
    renderer: {
      place(image, spec, dismissed) {
        if (disposed) throw new Error("terminal graphics is disposed");
        backingSize(spec);
        const canvas = document.createElement("canvas");
        canvas.width = 0;
        canvas.height = 0;
        canvas.className = "xterm-graphics-image";
        canvas.style.zIndex = String(spec.z);
        const placement: RenderedPlacement = {
          canvas,
          image,
          spec,
          buffer: term.buffer.active,
          pixels: 0,
          dismissed,
          closed: false,
        };
        try {
          draw(placement);
          const marker = term.registerMarker(0);
          if (marker) {
            placement.marker = marker;
            marker.onDispose(() => remove(placement));
          }
          layer.appendChild(canvas);
          rendered.add(placement);
          schedule();
        } catch (error) {
          remove(placement);
          throw error;
        }
        const handle: KittyPlacement = {
          dispose: () => remove(placement),
          active: () =>
            !placement.closed && placement.buffer === term.buffer.active,
          bounds: () => ({
            column: placement.spec.column,
            line:
              placement.marker && !placement.marker.isDisposed
                ? placement.marker.line
                : placement.spec.line,
            columns: placement.spec.columns,
            rows: placement.spec.rows,
          }),
        };
        return handle;
      },
    },
  });

  const scroll = term.onScroll(schedule);
  const bufferChange = term.buffer.onBufferChange((buffer) => {
    if (disposed) return;
    for (const placement of rendered) {
      if (placement.buffer.type === "alternate" && placement.buffer !== buffer)
        remove(placement);
      else if (placement.buffer !== buffer)
        placement.canvas.style.display = "none";
    }
    schedule();
  });
  const eraseDisplay = term.parser.registerCsiHandler(
    { final: "J" },
    (params) => {
      if (!disposed && typeof params[0] === "number")
        graphics.eraseDisplay(params[0]);
      return false;
    },
  );
  const resize = term.onResize(schedule);
  const parsed = term.onWriteParsed(schedule);
  const observer = new ResizeObserver(schedule);
  observer.observe(screen);
  const stopDpr = observeDevicePixelRatio(schedule);
  schedule();

  return {
    write: (data, parsedOutput) => {
      if (!disposed)
        graphics.write(data, () => {
          if (!disposed) parsedOutput();
        });
    },
    reset() {
      if (!disposed) graphics.reset();
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      graphics.dispose();
      for (const placement of Array.from(rendered)) remove(placement);
      cancelAnimationFrame(frame);
      scroll.dispose();
      bufferChange.dispose();
      eraseDisplay.dispose();
      resize.dispose();
      parsed.dispose();
      observer.disconnect();
      stopDpr();
      layer.remove();
    },
  };
}
