// Keyboard, IME, clipboard shortcuts, soft modifiers and draft input routing.
import { expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import {
  expectDomText,
  geometry,
  host,
  inputText,
  installTerminalFixture,
  observeGlyphPixels,
  openTerminal,
  openTerminalApi,
} from "./terminal-fixture";

const inputSendError = "终端输入未发送：连接不可用或发送队列已满，请重试。";

function keydown(target: HTMLElement, init: KeyboardEventInit) {
  const event = new KeyboardEvent("keydown", {
    bubbles: true,
    cancelable: true,
    ...init,
  });
  target.dispatchEvent(event);
  return event;
}

installTerminalFixture();

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

test("draft mode routes keys, paste and IME commits to the draft while device replies keep the PTY path", async () => {
  observeGlyphPixels(true);
  let enabled = true;
  const userInput = {
    draftEnabled: () => enabled,
    focusDraft: vi.fn(),
    key: vi.fn(),
    insertText: vi.fn(),
  };
  const onPaste = vi.fn();
  const { socket, rendered } = await openTerminal({ userInput, onPaste });
  const target = host();
  const textarea = target.querySelector<HTMLTextAreaElement>(
    ".xterm-helper-textarea",
  )!;
  textarea.blur();
  userInput.focusDraft.mockClear();
  textarea.focus();
  expect(userInput.focusDraft).toHaveBeenCalledTimes(1);

  const typed = keydown(textarea, { key: "a", code: "KeyA", keyCode: 65 });
  expect(typed.defaultPrevented).toBe(true);
  expect(userInput.key).toHaveBeenCalledTimes(1);
  expect(userInput.key.mock.calls[0][0]).toBe(typed);
  // Plain Ctrl+V lets the native paste reach the focused draft; Shift asks
  // for the explicit clipboard action instead.
  userInput.focusDraft.mockClear();
  keydown(textarea, { key: "v", code: "KeyV", keyCode: 86, ctrlKey: true });
  expect(userInput.focusDraft).toHaveBeenCalledTimes(1);
  expect(onPaste).not.toHaveBeenCalled();
  const explicit = keydown(textarea, {
    key: "V",
    code: "KeyV",
    keyCode: 86,
    ctrlKey: true,
    shiftKey: true,
  });
  expect(explicit.defaultPrevented).toBe(true);
  expect(onPaste).toHaveBeenCalledTimes(1);
  expect(userInput.key).toHaveBeenCalledTimes(1);

  const clipboard = new DataTransfer();
  clipboard.setData("text/plain", "pasted 中文");
  const paste = new ClipboardEvent("paste", {
    clipboardData: clipboard,
    bubbles: true,
    cancelable: true,
  });
  textarea.dispatchEvent(paste);
  expect(paste.defaultPrevented).toBe(true);

  const before = new InputEvent("beforeinput", {
    inputType: "insertText",
    data: "x",
    bubbles: true,
    cancelable: true,
  });
  textarea.dispatchEvent(before);
  expect(before.defaultPrevented).toBe(true);
  // An uncancellable beforeinput is consumed through its input event, once.
  const init = { inputType: "insertText", data: "y", bubbles: true };
  textarea.dispatchEvent(new InputEvent("beforeinput", init));
  textarea.dispatchEvent(new InputEvent("input", init));

  textarea.dispatchEvent(new CompositionEvent("compositionstart"));
  textarea.dispatchEvent(
    new CompositionEvent("compositionupdate", { data: "ni" }),
  );
  textarea.dispatchEvent(
    new InputEvent("input", {
      inputType: "insertCompositionText",
      data: "ni",
      isComposing: true,
      bubbles: true,
    }),
  );
  textarea.dispatchEvent(
    new CompositionEvent("compositionend", { data: "你" }),
  );
  expect(userInput.insertText.mock.calls).toEqual([
    ["pasted 中文"],
    ["x"],
    ["y"],
    ["你"],
  ]);

  // Device replies are not user input and still answer the program.
  socket.receive(0, "\x1b[H\x1b[6n");
  await expect
    .poll(() =>
      socket.sent
        .filter((frame) => frame[0] === 6)
        .map((frame) => new TextDecoder().decode(frame.subarray(1))),
    )
    .toEqual(["\x1b[1;1R"]);
  expect(inputText(socket)).toBe("");

  enabled = false;
  textarea.focus();
  await userEvent.keyboard("z");
  await expect.poll(() => inputText(socket)).toBe("z");
  expect(userInput.key).toHaveBeenCalledTimes(1);
  expect(userInput.insertText).toHaveBeenCalledTimes(4);

  // Unmounting releases every capture listener on the host.
  enabled = true;
  const focusCalls = userInput.focusDraft.mock.calls.length;
  await rendered.unmount();
  target.dispatchEvent(new FocusEvent("focusin"));
  target.dispatchEvent(
    new ClipboardEvent("paste", {
      clipboardData: clipboard,
      bubbles: true,
      cancelable: true,
    }),
  );
  target.dispatchEvent(new CompositionEvent("compositionend", { data: "晚" }));
  expect(userInput.focusDraft).toHaveBeenCalledTimes(focusCalls);
  expect(userInput.insertText).toHaveBeenCalledTimes(4);
});

test("clipboard shortcuts copy the selection and request explicit paste while native Ctrl+V sends no control byte", async () => {
  observeGlyphPixels(true);
  const onPaste = vi.fn();
  const { socket } = await openTerminal({ onPaste });
  socket.receive(1, "copyable words\r\n");
  await expectDomText("copyable words");
  const { cell, row } = geometry(socket);
  await page
    .elementLocator(host().querySelector<HTMLElement>(".xterm-screen")!)
    .dblClick({ position: { x: cell * 2.5, y: row * 0.5 } });
  await expect
    .element(page.getByRole("status", { name: "Terminal selection" }))
    .toHaveTextContent("copyable");

  const textarea = host().querySelector<HTMLTextAreaElement>(
    ".xterm-helper-textarea",
  )!;
  textarea.focus();
  keydown(textarea, {
    key: "C",
    code: "KeyC",
    keyCode: 67,
    ctrlKey: true,
    shiftKey: true,
  });
  await expect
    .element(page.getByRole("status", { name: "Copied terminal text" }))
    .toHaveTextContent("copyable");
  const explicit = keydown(textarea, {
    key: "V",
    code: "KeyV",
    keyCode: 86,
    ctrlKey: true,
    shiftKey: true,
  });
  expect(explicit.defaultPrevented).toBe(true);
  expect(onPaste).toHaveBeenCalledTimes(1);
  // Native Ctrl+V stays uncancelled so the browser fires its paste event.
  const native = keydown(textarea, {
    key: "v",
    code: "KeyV",
    keyCode: 86,
    ctrlKey: true,
  });
  expect(native.defaultPrevented).toBe(false);
  expect(onPaste).toHaveBeenCalledTimes(1);
  // A later key arrives alone: no ^C or ^V byte preceded it.
  await userEvent.keyboard("q");
  await expect.poll(() => inputText(socket)).toBe("q");
});

test("soft modifiers apply to the next input once and paste reports whether it was sent", async () => {
  observeGlyphPixels(true);
  const { socket, api } = await openTerminalApi();
  const released = { ctrl: false, alt: false };
  api.toggleModifier("ctrl");
  expect(api.modifiers).toEqual({ ctrl: true, alt: false });
  expect(api.send("c")).toBe(true);
  expect(api.modifiers).toEqual(released);
  api.toggleModifier("alt");
  api.send("x");
  api.toggleModifier("ctrl");
  api.resetModifiers();
  api.send("d");
  expect(inputText(socket)).toBe("\x03\x1bxd");

  // Paste never applies a pending modifier.
  api.toggleModifier("alt");
  expect(api.paste("a\nb")).toBe(true);
  expect(api.modifiers).toEqual(released);
  socket.receive(0, "\x1b[?2004hbracketed-ready");
  await expectDomText("bracketed-ready");
  expect(api.paste("e\r\nf")).toBe(true);
  expect(inputText(socket)).toBe(
    "\x03\x1bxd" + "a\rb" + "\x1b[200~e\rf\x1b[201~",
  );

  socket.bufferedAmount = 1024 * 1024;
  expect(api.paste("g")).toBe(false);
  expect(api.error).toBe(inputSendError);
  socket.bufferedAmount = 0;
  expect(api.send("h")).toBe(true);
  expect(api.error).toBe("");
  expect(inputText(socket)).toBe(
    "\x03\x1bxd" + "a\rb" + "\x1b[200~e\rf\x1b[201~h",
  );
});
