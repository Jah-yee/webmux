import type { IBuffer, IDisposable } from "@xterm/xterm";

interface LineList {
  get(index: number): object | undefined;
  onTrim(listener: (amount: number) => void): IDisposable;
  onInsert(
    listener: (event: { index: number; amount: number }) => void,
  ): IDisposable;
  onDelete(
    listener: (event: { index: number; amount: number }) => void,
  ): IDisposable;
}

interface XtermBuffer extends IBuffer {
  _buffer: { lines: LineList };
}

// xterm 6's public markers are text markers: ED/EL dispose them even though
// Kitty graphics must survive text erasure. Subscribe to actual line movement
// instead. Keep the pinned xterm internal API isolated here; browser regressions
// exercise erase, scrolling, scrollback trimming and resize against real xterm.
export function graphicsAnchor(
  buffer: IBuffer,
  initialLine: number,
  removed: () => void,
) {
  // BufferApiView in the pinned xterm version wraps this line list.
  const internal = buffer as XtermBuffer;
  const lines = internal._buffer.lines;
  let line = initialLine;
  let identity = lines.get(line);
  let disposed = false;
  const listeners = [
    lines.onTrim((amount) => {
      line -= amount;
      if (line < 0) removed();
    }),
    lines.onInsert((event) => {
      if (line >= event.index) line += event.amount;
    }),
    lines.onDelete((event) => {
      if (line >= event.index && line < event.index + event.amount) removed();
      else if (line >= event.index + event.amount) line -= event.amount;
    }),
  ];
  return {
    get line() {
      return line;
    },
    sync() {
      if (internal._buffer.lines !== lines) {
        removed();
        return;
      }
      if (disposed || lines.get(line) === identity) return;
      // DECSTBM index scrolling shifts BufferLine objects without emitting
      // insert/delete events. Search only the active screen, not scrollback.
      for (let row = buffer.baseY; row < buffer.length; row++) {
        if (lines.get(row) === identity) {
          line = row;
          return;
        }
      }
      removed();
    },
    resized() {
      // Reflow reports insert/delete movement, but can replace line objects.
      identity = lines.get(line);
    },
    dispose() {
      disposed = true;
      for (const listener of listeners) listener.dispose();
    },
  };
}
