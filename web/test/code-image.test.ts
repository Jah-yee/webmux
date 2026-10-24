import { describe, expect, it } from "vitest";
import { APIError } from "../src/api";
import {
  basename,
  codeRawPath,
  formatBytes,
  imageFormat,
  isImageType,
  rawError,
} from "../src/code-image";

describe("code image helpers", () => {
  it("builds raw snapshot paths without tokens", () => {
    expect(codeRawPath("s", "snap")).toBe(
      "/sessions/s/code/snapshots/snap/raw",
    );
    expect(codeRawPath("s", "diff", "old")).toBe(
      "/sessions/s/code/snapshots/diff/raw?side=old",
    );
    expect(codeRawPath("s", "diff", "new", true)).toBe(
      "/sessions/s/code/snapshots/diff/raw?side=new&download=1",
    );
  });

  it("formats sizes, formats and names", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1023)).toBe("1023 B");
    expect(formatBytes(1536)).toBe("1.5 KiB");
    expect(formatBytes(30 * 1024 * 1024)).toBe("30 MiB");
    expect(imageFormat("image/x-icon")).toBe("ICO");
    expect(imageFormat("image/svg+xml")).toBe("SVG");
    expect(isImageType("image/png")).toBe(true);
    expect(isImageType("")).toBe(false);
    expect(basename("assets/图 片.png")).toBe("图 片.png");
  });

  it("explains raw transfer failures by status", () => {
    expect(rawError(new APIError(409, "x"), "下载")).toBe(
      "下载失败：内容已变化，请刷新文档",
    );
    expect(rawError(new APIError(410, "x"), "预览")).toBe(
      "预览失败：快照已过期，请刷新文档",
    );
    expect(rawError(new APIError(413, "x"), "下载")).toMatch(/100 MiB/);
    expect(rawError(new APIError(429, "x"), "下载")).toMatch(/传输过多/);
    expect(rawError(new APIError(401, "x"), "下载")).toMatch(/认证失败/);
    expect(rawError(new TypeError("Failed to fetch"), "下载")).toBe(
      "下载失败：网络连接异常",
    );
    expect(rawError(new APIError(500, "boom"), "下载")).toBe("下载失败：boom");
  });
});
