import { afterEach, describe, expect, test, vi } from "vitest";
import { clipboardFiles } from "../src/clipboard";

function item(entries: Record<string, string>): ClipboardItem {
  return {
    types: Object.keys(entries),
    getType: async (type: string) => new Blob([entries[type]], { type }),
  } as unknown as ClipboardItem;
}

afterEach(() => {
  vi.useRealTimers();
});

describe("clipboard files", () => {
  test("skips text-only items and keeps each file's item index", async () => {
    vi.useFakeTimers({ now: 1_700_000_000_000 });
    const files = await clipboardFiles([
      item({ "text/plain": "hello" }),
      item({ "text/html": "<b>hi</b>", "image/png": "png-bytes" }),
    ]);
    expect(files).toHaveLength(1);
    expect(files[0].name).toBe("clipboard-1700000000000-1.png");
    expect(files[0].type).toBe("image/png");
    expect(await files[0].text()).toBe("png-bytes");
  });

  test("names extensions after the subtype with a jpg and bin fallback", async () => {
    vi.useFakeTimers({ now: 5 });
    const files = await clipboardFiles([
      item({ "image/jpeg": "a" }),
      item({ "image/svg+xml": "b" }),
      item({ "application/+": "c" }),
    ]);
    expect(files.map((file) => file.name)).toEqual([
      "clipboard-5-0.jpg",
      "clipboard-5-1.svgxml",
      "clipboard-5-2.bin",
    ]);
  });

  test("returns no files when every item is text", async () => {
    expect(await clipboardFiles([item({ "text/plain": "x" })])).toEqual([]);
    expect(await clipboardFiles([])).toEqual([]);
  });
});
