import { createTwoFilesPatch } from "diff";
import { processFile } from "@pierre/diffs";
import { FileDiffSchema, type DiffResult } from "./code-diff-model";

const DIFF_BUDGET_MS = 250;

function lineCount(text: string): number {
  return text ? text.split("\n").length - Number(text.endsWith("\n")) : 0;
}

// Only the Worker imports this module in the app. Parsing and the bounded
// jsdiff computation never block terminal input on the main thread.
export function computeDiff(
  oldText: string,
  newText: string,
  budget = DIFF_BUDGET_MS,
): DiffResult {
  if (
    lineCount(oldText) > 20000 ||
    lineCount(newText) > 20000 ||
    new TextEncoder().encode(oldText).length > 2 * 1024 * 1024 ||
    new TextEncoder().encode(newText).length > 2 * 1024 * 1024
  ) {
    return {
      kind: "limited",
      reason: "内容超过 2 MiB 或 20000 行，请查看两侧完整文件",
    };
  }
  // Fixed patch names keep tabs/newlines in real filenames out of patch
  // syntax. The captured document supplies the display name in the view.
  const patch = createTwoFilesPatch("file", "file", oldText, newText, "", "", {
    context: 3,
    stripTrailingCr: true,
    timeout: budget,
  });
  if (patch === undefined) {
    return {
      kind: "limited",
      reason: "差异计算超过时间预算，请查看两侧完整文件",
    };
  }
  const fileDiff = processFile(patch, {
    oldFile: { name: "file", contents: oldText },
    newFile: { name: "file", contents: newText },
    throwOnError: true,
  });
  return { kind: "ready", fileDiff: FileDiffSchema.parse(fileDiff) };
}
