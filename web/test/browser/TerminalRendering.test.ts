// Real xterm/search-addon behavior; transport is the existing framed fixture.
import { expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { Terminal } from "@xterm/xterm";
import { SearchAddon } from "@xterm/addon-search";
import {
  host,
  inputText,
  installTerminalFixture,
  observeGlyphPixels,
  openTerminalApi,
  reconnected,
} from "./terminal-fixture";

installTerminalFixture();
const options = { regex: false, wholeWord: false, caseSensitive: false };
const field = () =>
  page.getByRole("textbox", { name: "查找终端输出", exact: true });
const results = () => page.getByRole("status", { name: "查找结果" });
function key(target: HTMLElement, init: KeyboardEventInit) {
  const event = new KeyboardEvent("keydown", {
    bubbles: true,
    cancelable: true,
    ...init,
  });
  target.dispatchEvent(event);
  return event;
}
async function setup() {
  observeGlyphPixels(true);
  const resets = vi.spyOn(Terminal.prototype, "reset");
  const opened = await openTerminalApi();
  const term = resets.mock.contexts.at(-1)!;
  return { ...opened, term };
}
async function write(term: Terminal, value: string) {
  const { promise, resolve } = Promise.withResolvers<void>();
  term.write(value, resolve);
  await promise;
}
async function openFind() {
  await page.getByRole("button", { name: "Find terminal output" }).click();
  await expect.element(field()).toHaveFocus();
}

test("capture shortcut, navigation, copy and reopen never type or resize the PTY", async () => {
  const { socket, api, term } = await setup();
  await write(term, "ERROR first\r\nERROR second\r\n");
  term.focus();
  const sizes = socket.sent.filter((frame) => frame[0] === 1).length;
  expect(
    key(term.textarea!, {
      key: "F",
      code: "KeyF",
      keyCode: 70,
      ctrlKey: true,
      shiftKey: true,
    }).defaultPrevented,
  ).toBe(true);
  await expect.element(field()).toHaveFocus();
  await field().fill("ERROR");
  await expect.element(results()).toHaveTextContent("1/2");
  const first = term.getSelectionPosition();
  await userEvent.keyboard("{Enter}");
  await expect.element(results()).toHaveTextContent("2/2");
  expect(term.getSelectionPosition()?.start.y).toBeGreaterThan(first!.start.y);
  await userEvent.keyboard("{Shift>}{Enter}{/Shift}");
  await expect.element(results()).toHaveTextContent("1/2");
  await page.getByRole("button", { name: "复制选中", exact: true }).click();
  await expect
    .element(page.getByRole("status", { name: "Copied terminal text" }))
    .toHaveTextContent("ERROR");
  await page.getByRole("button", { name: "关闭查找" }).click();
  expect(api.findResults).toBeNull();
  expect(term.getSelection()).toBe("");
  await expect
    .poll(() => host().querySelectorAll(".xterm-decoration").length)
    .toBe(0);
  const viewport = term.buffer.active.viewportY;
  await openFind();
  await expect.element(field()).toHaveValue("ERROR");
  expect(api.findResults).toBeNull();
  expect(term.buffer.active.viewportY).toBe(viewport);
  expect(socket.sent.filter((frame) => frame[0] === 1)).toHaveLength(sizes);
  expect(inputText(socket)).toBe("");
  await userEvent.keyboard("{Escape}");
  term.focus();
  await userEvent.keyboard("{Control>}f{/Control}");
  expect(inputText(socket)).toBe("\x06");
});

test("macOS Cmd+F captures browser find without terminal input", async () => {
  const { term, socket } = await setup();
  vi.spyOn(navigator, "platform", "get").mockReturnValue("MacIntel");
  term.focus();
  expect(
    key(term.textarea!, { key: "f", metaKey: true }).defaultPrevented,
  ).toBe(true);
  await expect.element(field()).toHaveFocus();
  expect(inputText(socket)).toBe("");
});

test("draft capture keeps query and IME outside the draft input router", async () => {
  observeGlyphPixels(true);
  const insertText = vi.fn();
  const draftKey = vi.fn();
  const { socket, api } = await openTerminalApi({
    userInput: {
      draftEnabled: () => true,
      focusDraft: () => {},
      key: draftKey,
      insertText,
    },
  });
  socket.receive(0, "中文错误\r\n");
  const draft = document.querySelector<HTMLTextAreaElement>(
    '[aria-label="Fixture draft"]',
  )!;
  draft.focus();
  key(draft, { key: "F", ctrlKey: true, shiftKey: true });
  await expect.element(field()).toHaveFocus();
  const query = document.querySelector<HTMLInputElement>(
    'input[aria-label="查找终端输出"]',
  )!;
  query.dispatchEvent(
    new CompositionEvent("compositionstart", { bubbles: true }),
  );
  query.value = "zhong";
  query.dispatchEvent(
    new InputEvent("input", { bubbles: true, isComposing: true }),
  );
  expect(api.findResults).toBeNull();
  query.value = "中文";
  query.dispatchEvent(
    new CompositionEvent("compositionend", { bubbles: true, data: "中文" }),
  );
  await expect.poll(() => api.selection).toBe("中文");
  expect(insertText).not.toHaveBeenCalled();
  expect(draftKey).not.toHaveBeenCalled();
  await userEvent.keyboard("{Escape}");
  await expect.poll(() => document.activeElement).toBe(draft);
  expect(inputText(socket)).toBe("");
});

test("new searches start near viewport, handle wraps and wide characters, and invalid regex preserves selection", async () => {
  const { term, api, socket } = await setup();
  await write(
    term,
    Array.from({ length: 150 }, (_, i) => `ERROR ${i}\r\n`).join(""),
  );
  term.scrollToLine(70);
  await openFind();
  await field().fill("ERROR");
  expect(term.getSelectionPosition()?.start.y).toBe(70);
  await write(
    term,
    `\r\n${"x".repeat(term.cols - 2)}中文错误-tail\r\nerror ERROR errors\r\n`,
  );
  await field().fill("中文错误-tail");
  expect(api.selection).toBe("中文错误-tail");
  await field().fill("error");
  const all = api.findResults!.resultCount;
  await page.getByRole("button", { name: "全词匹配" }).click();
  expect(api.findResults!.resultCount).toBe(all - 1);
  await page.getByRole("button", { name: "区分大小写" }).click();
  expect(api.findResults!.resultCount).toBe(1);
  await page.getByRole("button", { name: "正则表达式" }).click();
  const selected = term.getSelectionPosition();
  const viewport = term.buffer.active.viewportY;
  const addonFind = vi.spyOn(SearchAddon.prototype, "findNext");
  await field().fill("[");
  await expect.element(results()).toHaveTextContent("正则表达式无效");
  expect(addonFind).not.toHaveBeenCalled();
  expect(term.getSelectionPosition()).toEqual(selected);
  expect(term.buffer.active.viewportY).toBe(viewport);
  await field().fill("missing-unique");
  await expect.element(results()).toHaveTextContent("无匹配");
  expect(api.selection).toBe("");
  await field().fill("");
  expect(api.findResults).toBeNull();
  expect(inputText(socket)).toBe("");
});

test("reselecting the only match keeps the controller selection synchronized", async () => {
  const { term, api, socket } = await setup();
  await write(term, "needle\r\n");
  await openFind();
  await field().fill("needle");
  const selected = term.getSelectionPosition();
  expect(api.selection).toBe("needle");
  for (const previous of [false, true]) {
    if (previous) api.findPrevious("needle", options);
    else api.findNext("needle", options);
    expect(term.getSelectionPosition()).toEqual(selected);
    expect(term.getSelection()).toBe("needle");
    expect(api.selection).toBe("needle");
    expect(api.findResults).toEqual({ resultIndex: 0, resultCount: 1 });
  }
  api.copySelection();
  await expect
    .element(page.getByRole("status", { name: "Copied terminal text" }))
    .toHaveTextContent("needle");
  expect(inputText(socket)).toBe("");
});

test("navigation passes the highlighted result limit", async () => {
  const { term, api } = await setup();
  await write(term, "needle\r\n".repeat(1001));
  term.scrollToTop();
  await openFind();
  await field().fill("needle");
  expect(api.findResults?.resultIndex).toBe(0);
  for (let i = 0; i < 1000; i++) api.findNext("needle", options);
  expect(api.selection).toBe("needle");
  expect(api.findResults?.resultIndex).toBe(-1);
});

test("alternate, clear and full replay reset results explicitly, retain query, and exited output remains searchable", async () => {
  const { term, api, socket } = await setup();
  await write(term, "needle normal\r\n");
  await openFind();
  await field().fill("needle");
  await write(term, "\x1b[?1049hneedle alternate\r\n");
  expect(api.bufferType).toBe("alternate");
  expect(api.findResults).toBeNull();
  expect(api.selection).toBe("");
  await userEvent.keyboard("{Enter}");
  expect(api.selection).toBe("needle");
  await write(term, "\x1b[?1049l");
  expect(api.bufferType).toBe("normal");
  expect(api.findResults).toBeNull();
  await userEvent.keyboard("{Enter}");
  api.clear();
  expect(api.findResults).toBeNull();
  expect(api.selection).toBe("");
  const originalWrite = term.write.bind(term);
  let finishReplay: (() => void) | undefined;
  const writes = vi
    .spyOn(term, "write")
    .mockImplementation((data, callback) => {
      // Hold only the snapshot completion, not parsing or the reset callback.
      if (data instanceof Uint8Array)
        originalWrite(data, () => {
          finishReplay = callback;
        });
      else originalWrite(data, callback);
    });
  socket.receive(1, "needle replay\r\n");
  await expect
    .element(page.getByRole("button", { name: "下一个匹配" }))
    .toBeDisabled();
  await expect.poll(() => finishReplay).toBeDefined();
  finishReplay!();
  writes.mockRestore();
  await expect.poll(() => api.restoring).toBe(false);
  expect(api.findResults).toBeNull();
  await expect.element(field()).toHaveValue("needle");
  socket.receive(3, JSON.stringify({ exitCode: 0 }));
  await userEvent.keyboard("{Enter}");
  expect(api.selection).toBe("needle");
  expect(api.ready).toBe(false);
  expect(api.canFind).toBe(true);
  expect(inputText(socket)).toBe("");
});

for (const tail of ["", "new missed output\r\n"]) {
  test(`incremental resume preserves the searched viewport (${tail ? "missing tail" : "empty append"})`, async () => {
    const { term, api, socket } = await setup();
    const generation = "0123456789abcdef";
    const text = "needle\r\n".repeat(140);
    const offset = new TextEncoder().encode(text).length;
    socket.receive(
      4,
      JSON.stringify({
        generation,
        mode: "reset",
        start: 0,
        end: offset,
        truncated: true,
      }),
    );
    socket.receive(1, text);
    await expect.poll(() => api.restoring).toBe(false);
    term.scrollToLine(20);
    await openFind();
    await field().fill("needle");
    const selected = term.getSelectionPosition();
    expect(term.getSelection()).toBe("needle");
    expect(api.selection).toBe("needle");
    const viewport = term.buffer.active.viewportY;
    socket.drop();
    const resumed = await reconnected(2);
    resumed.open();
    resumed.receive(
      4,
      JSON.stringify({
        generation,
        mode: "append",
        start: offset,
        end: offset + new TextEncoder().encode(tail).length,
        truncated: true,
      }),
    );
    const updates = vi.spyOn(SearchAddon.prototype, "findPrevious");
    if (tail) resumed.receive(0, tail);
    await write(term, "");
    await expect.poll(() => updates.mock.calls.length).toBeGreaterThan(0);
    expect(updates.mock.results.at(-1)?.value).toBe(true);
    expect(term.buffer.active.viewportY).toBe(viewport);
    expect(api.findResults?.resultCount).toBe(140);
    expect(term.getSelectionPosition()).toEqual(selected);
    expect(term.getSelection()).toBe("needle");
    expect(api.selection).toBe("needle");
    api.copySelection();
    await expect
      .element(page.getByRole("status", { name: "Copied terminal text" }))
      .toHaveTextContent("needle");
    expect(inputText(resumed)).toBe("");
  });
}

test("keys in a sibling code pane do not open terminal find", async () => {
  const { socket } = await setup();
  const code = document.querySelector<HTMLTextAreaElement>(
    '[aria-label="Fixture code pane"]',
  )!;
  code.focus();
  expect(
    key(code, { key: "F", ctrlKey: true, shiftKey: true }).defaultPrevented,
  ).toBe(false);
  expect(key(code, { key: "f", metaKey: true }).defaultPrevented).toBe(false);
  await expect.element(field()).not.toBeInTheDocument();
  expect(inputText(socket)).toBe("");
});
