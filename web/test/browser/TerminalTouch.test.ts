// Native touch scrolling, wheel reports, pinch and long-press selection.
import { expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import {
  blurAndTrackFocus,
  expectDomText,
  expectNoSessionMutation,
  firstHistoryLine,
  geometry,
  gestureStart,
  historyLine,
  host,
  inputText,
  installTerminalFixture,
  observeGlyphPixels,
  openTerminal,
  pointer,
  populateHistory,
  prompt,
  swipe,
  syntheticPointerCapture,
  touchTeardowns,
} from "./terminal-fixture";

installTerminalFixture();

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

test("viewing history is reported until the terminal returns to the latest output", async () => {
  observeGlyphPixels(true);
  const { socket } = await openTerminal();
  await populateHistory(socket);
  const position = page.getByRole("status", { name: "Terminal position" });
  await expect.element(position).toHaveTextContent("latest");
  await swipe(socket, 4.25);
  await expect.element(position).toHaveTextContent("history");
  // Output arriving while reading history does not pull the reader down.
  socket.receive(0, "\r\nlater output");
  await expect.element(position).toHaveTextContent("history");
  await page.getByRole("button", { name: "Scroll to latest" }).click();
  await expect.element(position).toHaveTextContent("latest");
  await expectDomText("later output");
  socket.receive(0, "\r\nnewest output");
  await expectDomText("newest output");
  await expect.element(position).toHaveTextContent("latest");
});
