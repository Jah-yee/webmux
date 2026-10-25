export interface TerminalFindOptions {
  caseSensitive: boolean;
  wholeWord: boolean;
  regex: boolean;
}

export interface TerminalFindResults {
  resultIndex: number;
  resultCount: number;
}

export const terminalFindLimit = 1000;

export function terminalFindError(query: string, regex: boolean): string {
  if (!query || !regex) return "";
  try {
    new RegExp(query);
    return "";
  } catch {
    return "正则表达式无效";
  }
}

export function terminalFindResultText(
  results: TerminalFindResults | null,
): string {
  if (!results) return "";
  if (results.resultCount >= terminalFindLimit)
    return "至少 1000 个匹配（仅统计前 1000 个）";
  if (!results.resultCount) return "无匹配";
  if (results.resultIndex < 0) return `${results.resultCount} 个匹配`;
  return `${results.resultIndex + 1}/${results.resultCount}`;
}

export function terminalFindScope(
  scrollback: number,
  bufferType: "normal" | "alternate",
  truncated: boolean,
): string {
  const scope = `当前终端缓冲（最多回看 ${scrollback} 行）`;
  return [
    scope,
    bufferType === "alternate" ? "全屏程序：仅当前屏幕" : "",
    truncated ? "服务端已覆盖更早输出" : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

export function terminalFindReplayText(
  restoring: boolean,
  replayed: boolean,
): string {
  return restoring
    ? "正在回放，暂不能查找"
    : replayed
      ? "回放完成，按 Enter 重新查找"
      : "";
}

export function isTerminalFindShortcut(
  event: Pick<
    KeyboardEvent,
    "key" | "ctrlKey" | "shiftKey" | "altKey" | "metaKey" | "isComposing"
  >,
  platform: string,
): boolean {
  return (
    !event.isComposing &&
    !event.altKey &&
    event.key.toLowerCase() === "f" &&
    ((event.ctrlKey && event.shiftKey && !event.metaKey) ||
      (/Mac/.test(platform) &&
        event.metaKey &&
        !event.ctrlKey &&
        !event.shiftKey))
  );
}
