import type { Terminal } from "@xterm/xterm";

type TouchPoint = { x: number; y: number };

// Touch and stylus scrolling, pinch font sizing and long-press selection on
// the terminal host. Pens are handled like fingers so a stylus tap cannot focus
// xterm and raise the virtual keyboard. Returns the cleanup that releases
// captured pointers and listeners.
export function attachTouchGestures(
  term: Terminal,
  parent: HTMLElement,
  options: {
    fontSize: () => number;
    setFontSize: (value: number) => void;
    hasSelection: () => boolean;
    keyboardDismissed: () => boolean;
  },
) {
  const terminalElement = term.element!;
  const screen = terminalElement.querySelector<HTMLElement>(".xterm-screen")!;
  let disposed = false;
  const pointers = new Map<number, TouchPoint>();
  let firstPoint: TouchPoint | undefined;
  let secondPoint: TouchPoint | undefined;
  let touchX = 0;
  let panX = 0;
  let touchY = 0;
  let wheelY = 0;
  let remainder = 0;
  let pinchDistance = 0;
  let pinchFont = options.fontSize();
  let longPress: number | undefined;
  let selecting = false;
  let selectionAnchor: { start: number; end: number } | undefined;
  let lastTouch = -Infinity;
  const markTouch = () => {
    lastTouch = performance.now();
  };
  // Android hides the IME with the system Back button but keeps the focused
  // editable, so the next page-consumed touch can summon the keyboard again.
  // Drop that stale focus only after an observed dismissal; an open keyboard,
  // a hardware keyboard and a page that never showed an IME keep their focus.
  const releaseDismissedKeyboard = () => {
    if (!options.keyboardDismissed()) return;
    const active = document.activeElement;
    if (
      active instanceof HTMLInputElement ||
      active instanceof HTMLTextAreaElement ||
      (active instanceof HTMLElement && active.isContentEditable)
    )
      active.blur();
  };
  const cancelLongPress = () => {
    clearTimeout(longPress);
    longPress = undefined;
  };
  const cellAt = (point: TouchPoint) => {
    const bounds = screen.getBoundingClientRect();
    const column = Math.max(
      0,
      Math.min(
        term.cols - 1,
        Math.floor(((point.x - bounds.left) / bounds.width) * term.cols),
      ),
    );
    const row =
      term.buffer.active.viewportY +
      Math.max(
        0,
        Math.min(
          term.rows - 1,
          Math.floor(((point.y - bounds.top) / bounds.height) * term.rows),
        ),
      );
    const line = term.buffer.active.getLine(row);
    // The trailing cell of a wide CJK/emoji glyph belongs to its leading cell.
    const start = line?.getCell(column)?.getWidth() === 0 ? column - 1 : column;
    const width = line?.getCell(start)?.getWidth() || 1;
    return { row, column: start, width, line };
  };
  const startSelection = (point: TouchPoint) => {
    const { row, column, width, line } = cellAt(point);
    if (!line) return;
    let start = column;
    let end = column + width;
    const isWord = (col: number) =>
      /[\p{L}\p{N}\p{M}_]/u.test(line.getCell(col)?.getChars() ?? "");
    if (isWord(column)) {
      while (start > 0) {
        const previous =
          line.getCell(start - 1)?.getWidth() === 0 ? start - 2 : start - 1;
        if (!isWord(previous)) break;
        start = previous;
      }
      while (end < term.cols && isWord(end))
        end += line.getCell(end)?.getWidth() || 1;
    }
    selectionAnchor = {
      start: row * term.cols + start,
      end: row * term.cols + end,
    };
    selecting = true;
    term.blur();
    term.select(start, row, end - start);
  };
  const extendSelection = (point: TouchPoint) => {
    if (!selectionAnchor) return;
    const { row, column, width } = cellAt(point);
    const position = row * term.cols + column;
    const start = Math.min(selectionAnchor.start, position);
    const end = Math.max(selectionAnchor.end, position + width);
    term.select(start % term.cols, Math.floor(start / term.cols), end - start);
  };
  const distance = () =>
    Math.hypot(firstPoint!.x - secondPoint!.x, firstPoint!.y - secondPoint!.y);
  const rebasePointers = () => {
    const points = pointers.values();
    firstPoint = points.next().value;
    secondPoint = points.next().value;
    touchX = firstPoint?.x ?? 0;
    panX = touchX;
    touchY = firstPoint?.y ?? 0;
    wheelY = touchY;
    remainder = 0;
    pinchDistance = pointers.size === 2 ? distance() : 0;
    pinchFont = options.fontSize();
  };
  const clearPointers = () => {
    cancelLongPress();
    selecting = false;
    selectionAnchor = undefined;
    for (const id of pointers.keys()) {
      pointers.delete(id);
      if (parent.hasPointerCapture(id)) parent.releasePointerCapture(id);
    }
    firstPoint = undefined;
    secondPoint = undefined;
    remainder = 0;
    pinchDistance = 0;
  };
  const pointerDown = (event: PointerEvent) => {
    if (disposed || event.pointerType === "mouse") return;
    markTouch();
    releaseDismissedKeyboard();
    // Suppress compatibility mousedown events that would refocus xterm after a swipe.
    event.preventDefault();
    event.stopPropagation();
    if (!pointers.size) term.clearSelection();
    pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
    if (pointers.size > 1) {
      cancelLongPress();
      selecting = false;
      selectionAnchor = undefined;
    } else {
      const point = { x: event.clientX, y: event.clientY };
      longPress = window.setTimeout(() => {
        if (!disposed && pointers.size === 1 && longPress !== undefined)
          startSelection(point);
      }, 500);
    }
    // DOM terminal rows are replaced while scrolling or repainting a TUI.
    // Capture to the stable host, not the row originally touched.
    parent.setPointerCapture(event.pointerId);
    rebasePointers();
  };
  const pointerMove = (event: PointerEvent) => {
    if (disposed || event.pointerType === "mouse") return;
    const point = pointers.get(event.pointerId);
    if (!point) return;
    event.preventDefault();
    event.stopPropagation();
    if (point.x === event.clientX && point.y === event.clientY) return;
    point.x = event.clientX;
    point.y = event.clientY;
    if (selecting) {
      extendSelection(point);
      return;
    }
    // Preserve sub-row scrolling, but tolerate finger jitter before long press.
    if (Math.hypot(point.x - touchX, point.y - wheelY) >= 8) cancelLongPress();
    if (pointers.size === 2 && pinchDistance > 0) {
      options.setFontSize((pinchFont * distance()) / pinchDistance);
    } else if (pointers.size === 1) {
      // An observer's grid can exceed the host. Pan the host first, so the
      // rows above the visible part and history stay reachable in order.
      if (parent.classList.contains("observing")) {
        parent.scrollLeft += panX - point.x;
        panX = point.x;
        const delta = touchY - point.y;
        const buffer = term.buffer.active;
        if (
          (delta < 0 && parent.scrollTop > 0) ||
          (delta > 0 &&
            buffer.viewportY >= buffer.baseY &&
            parent.scrollTop + parent.clientHeight < parent.scrollHeight)
        ) {
          parent.scrollTop += delta;
          touchY = point.y;
          return;
        }
      }
      const rowHeight = screen.getBoundingClientRect().height / term.rows;
      if (rowHeight <= 0) return;
      remainder += (touchY - point.y) / rowHeight;
      const lines = Math.trunc(remainder);
      if (
        term.buffer.active.type === "normal" &&
        term.modes.mouseTrackingMode === "none"
      ) {
        if (lines !== 0) term.scrollLines(lines);
      } else
        for (let count = Math.abs(lines); count > 0; count--) {
          // Bubble through xterm 6's inner viewport before its protocol handler.
          screen.dispatchEvent(
            new WheelEvent("wheel", {
              bubbles: true,
              cancelable: true,
              deltaMode: WheelEvent.DOM_DELTA_LINE,
              deltaY: lines > 0 ? 1 : -1,
              clientX: touchX,
              clientY: wheelY,
            }),
          );
        }
      remainder -= lines;
      touchY = point.y;
    }
  };
  const pointerUp = (event: PointerEvent) => {
    if (disposed || !pointers.has(event.pointerId)) return;
    markTouch();
    event.stopPropagation();
    cancelLongPress();
    event.preventDefault();
    pointers.delete(event.pointerId);
    if (parent.hasPointerCapture(event.pointerId))
      parent.releasePointerCapture(event.pointerId);
    if (pointers.size) rebasePointers();
    else clearPointers();
  };
  const pointerCancel = (event: PointerEvent) => {
    if (disposed || !pointers.has(event.pointerId)) return;
    event.stopPropagation();
    clearPointers();
  };
  const blockNativeTouch = (event: TouchEvent) => {
    markTouch();
    event.stopPropagation();
    if (selecting) event.preventDefault();
    if (event.type === "touchmove" || event.type === "touchend")
      event.preventDefault();
  };
  // Some Android browsers still deliver the compatibility mousedown after a
  // canceled touch pointerdown. Chromium marks it via sourceCapabilities;
  // others fall back to the recent native touch. Never swallow a real mouse
  // click, which must keep focusing xterm for hardware-keyboard users.
  const mouseDown = (event: MouseEvent) => {
    if (disposed || event.button !== 0) return;
    const source = (
      event as MouseEvent & {
        sourceCapabilities?: { firesTouchEvents?: boolean } | null;
      }
    ).sourceCapabilities;
    const touchDerived = source
      ? source.firesTouchEvents === true
      : performance.now() - lastTouch < 700;
    if (!touchDerived) return;
    releaseDismissedKeyboard();
    event.preventDefault();
    event.stopPropagation();
  };
  const contextMenu = (event: MouseEvent) => {
    // Native touch menus target xterm's hidden input, not its rendered buffer.
    if (pointers.size || selecting || options.hasSelection())
      event.preventDefault();
  };
  parent.addEventListener("pointerdown", pointerDown, true);
  parent.addEventListener("pointermove", pointerMove, true);
  parent.addEventListener("pointerup", pointerUp, true);
  parent.addEventListener("pointercancel", pointerCancel, true);
  parent.addEventListener("lostpointercapture", pointerCancel, true);
  parent.addEventListener("touchstart", blockNativeTouch, {
    capture: true,
    passive: true,
  });
  parent.addEventListener("touchmove", blockNativeTouch, {
    capture: true,
    passive: false,
  });
  parent.addEventListener("touchend", blockNativeTouch, {
    capture: true,
    passive: false,
  });
  parent.addEventListener("touchcancel", blockNativeTouch, {
    capture: true,
    passive: true,
  });
  parent.addEventListener("mousedown", mouseDown, true);
  parent.addEventListener("contextmenu", contextMenu);

  return () => {
    disposed = true;
    clearPointers();
    parent.removeEventListener("pointerdown", pointerDown, true);
    parent.removeEventListener("pointermove", pointerMove, true);
    parent.removeEventListener("pointerup", pointerUp, true);
    parent.removeEventListener("pointercancel", pointerCancel, true);
    parent.removeEventListener("lostpointercapture", pointerCancel, true);
    parent.removeEventListener("touchstart", blockNativeTouch, true);
    parent.removeEventListener("touchmove", blockNativeTouch, true);
    parent.removeEventListener("touchend", blockNativeTouch, true);
    parent.removeEventListener("touchcancel", blockNativeTouch, true);
    parent.removeEventListener("mousedown", mouseDown, true);
    parent.removeEventListener("contextmenu", contextMenu);
  };
}
