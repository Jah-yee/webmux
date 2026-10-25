import { describe, expect, test } from "vitest";
import {
  isTerminalFindShortcut,
  terminalFindError,
  terminalFindReplayText,
  terminalFindResultText,
  terminalFindScope,
} from "../src/terminal-search";

describe("terminal find contracts", () => {
  test("empty and literal queries do not produce regex errors", () => {
    expect(terminalFindError("", true)).toBe("");
    expect(terminalFindError("[", false)).toBe("");
    expect(terminalFindError("[", true)).toBe("正则表达式无效");
    expect(terminalFindError("错误.*", true)).toBe("");
  });
  test("cleared results differ from no matches and saturated counts never claim an exact total", () => {
    expect(terminalFindResultText(null)).toBe("");
    expect(terminalFindResultText({ resultIndex: -1, resultCount: 0 })).toBe(
      "无匹配",
    );
    expect(terminalFindResultText({ resultIndex: 1, resultCount: 3 })).toBe(
      "2/3",
    );
    for (const resultIndex of [-1, 0, 999]) {
      expect(terminalFindResultText({ resultIndex, resultCount: 1000 })).toBe(
        "至少 1000 个匹配（仅统计前 1000 个）",
      );
    }
  });
  test("scope separates client retention from server truncation and alternate screens", () => {
    expect(terminalFindScope(10000, "normal", false)).toBe(
      "当前终端缓冲（最多回看 10000 行）",
    );
    expect(terminalFindScope(250, "alternate", true)).toBe(
      "当前终端缓冲（最多回看 250 行） · 全屏程序：仅当前屏幕 · 服务端已覆盖更早输出",
    );
    expect(terminalFindReplayText(true, true)).toBe("正在回放，暂不能查找");
    expect(terminalFindReplayText(false, true)).toBe(
      "回放完成，按 Enter 重新查找",
    );
    expect(terminalFindReplayText(false, false)).toBe("");
  });
  test("only dedicated shortcuts are captured", () => {
    const key = {
      key: "f",
      ctrlKey: true,
      shiftKey: false,
      altKey: false,
      metaKey: false,
      isComposing: false,
    };
    expect(isTerminalFindShortcut(key, "Linux")).toBe(false);
    expect(isTerminalFindShortcut({ ...key, shiftKey: true }, "Linux")).toBe(
      true,
    );
    expect(
      isTerminalFindShortcut(
        { ...key, ctrlKey: false, metaKey: true },
        "MacIntel",
      ),
    ).toBe(true);
    expect(
      isTerminalFindShortcut(
        { ...key, ctrlKey: false, metaKey: true },
        "Linux",
      ),
    ).toBe(false);
    expect(
      isTerminalFindShortcut(
        { ...key, shiftKey: true, isComposing: true },
        "Linux",
      ),
    ).toBe(false);
    expect(
      isTerminalFindShortcut({ ...key, shiftKey: true, altKey: true }, "Linux"),
    ).toBe(false);
  });
});
