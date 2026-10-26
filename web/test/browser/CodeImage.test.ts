import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import Harness from "./CodeImageHarness.svelte";
import {
  CodeDiffSchema,
  CodeFileSchema,
  type CodeDiff,
  type CodeDocument,
  type CodeFile,
} from "../../src/schemas";
import { formatBytes } from "../../src/code-image";

const now = "2026-10-24T00:00:00Z";
const svgText =
  '<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="red"/></svg>\n';

afterEach(async () => {
  await cleanup();
  await userEvent.cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem("webmux.token");
});

function imageDocument(overrides: Partial<CodeDocument> = {}): CodeDocument {
  return {
    nodeId: "YXNzZXRzL3Bob3RvLnBuZw",
    path: "assets/photo.png",
    absolutePath: "/repo/assets/photo.png",
    source: "worktree",
    oid: "",
    contentVersion: "v1",
    kind: "image",
    text: "",
    lineCount: 0,
    referenceable: true,
    reason: "",
    mediaType: "image/png",
    size: 1234,
    downloadable: true,
    ...overrides,
  };
}
const fileOf = (document: CodeDocument, snapshotId = "snap-1"): CodeFile =>
  CodeFileSchema.parse({ snapshotId, checkedAt: now, document });
function imageDiff(
  old: CodeDocument,
  next: CodeDocument,
  snapshotId = "diff-1",
): CodeDiff {
  return CodeDiffSchema.parse({
    snapshotId,
    checkedAt: now,
    kind: "image",
    reason: "",
    change: {
      nodeId: next.nodeId,
      changeId: "change",
      group: "unstaged",
      oldPath: old.path || next.path,
      newPath: next.path || old.path,
      status: next.source === "empty" ? "D" : "M",
      oldOid: "",
      newOid: "",
      oldMode: "100644",
      newMode: next.source === "empty" ? "000000" : "100644",
    },
    old,
    new: next,
  });
}
async function png(width: number, height: number) {
  const canvas = new OffscreenCanvas(width, height);
  const context = canvas.getContext("2d")!;
  context.fillStyle = "#c33";
  context.fillRect(0, 0, width, height);
  return canvas.convertToBlob({ type: "image/png" });
}
type Handler = (
  url: URL,
  init: RequestInit | undefined,
) => Response | Promise<Response>;
function serve(handler: Handler) {
  const requests: { url: URL; init?: RequestInit }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const url = new URL(String(input), location.href);
      requests.push({ url, init });
      return handler(url, init);
    }),
  );
  return requests;
}
const raw = (blob: Blob) =>
  new Response(blob, { headers: { "Content-Type": blob.type } });
const viewport = () =>
  document.querySelector<HTMLElement>('[aria-label="图片预览"]')!;
const image = () => viewport().querySelector("img");
const percent = () =>
  document.querySelector('[aria-label="缩放比例"]')?.textContent?.trim();
const fitPercent = (width: number, height: number) =>
  `${Math.round(
    Math.min(
      1,
      viewport().clientWidth / width,
      viewport().clientHeight / height,
    ) * 100,
  )}%`;
function pointer(
  type: string,
  id: number,
  x: number,
  y: number,
  pointerType = "touch",
) {
  const rect = viewport().getBoundingClientRect();
  return viewport().dispatchEvent(
    new PointerEvent(type, {
      pointerId: id,
      pointerType,
      clientX: rect.left + x,
      clientY: rect.top + y,
      button: 0,
      bubbles: true,
      cancelable: true,
    }),
  );
}

test("image documents load an authenticated Blob, fit, zoom, pan and release it", async () => {
  localStorage.setItem("webmux.token", "secret");
  const blob = await png(800, 600);
  const requests = serve(() => raw(blob));
  const created = vi.spyOn(URL, "createObjectURL");
  const revoked = vi.spyOn(URL, "revokeObjectURL");
  const rendered = await render(Harness, {
    file: fileOf(imageDocument({ size: blob.size })),
  });
  await expect
    .element(page.getByLabelText("图片信息").first())
    .toHaveTextContent(`PNG · 800 × 600 · ${formatBytes(blob.size)}`);
  expect(requests[0].url.pathname).toBe(
    "/api/sessions/session/code/snapshots/snap-1/raw",
  );
  expect(requests[0].url.search).toBe("");
  expect(new Headers(requests[0].init?.headers).get("Authorization")).toBe(
    "Bearer secret",
  );
  expect(getComputedStyle(viewport()).touchAction).toBe("none");
  // Preview replaces the line reader; whole-file references stay available.
  expect(document.querySelector(".cm-editor")).toBeNull();
  await expect.poll(percent).toBe(fitPercent(800, 600));
  const frame = viewport().getBoundingClientRect();
  const fitted = image()!.getBoundingClientRect();
  expect(fitted.left).toBeGreaterThanOrEqual(frame.left - 0.5);
  expect(fitted.right).toBeLessThanOrEqual(frame.right + 0.5);
  expect(fitted.bottom).toBeLessThanOrEqual(frame.bottom + 0.5);

  await page.getByRole("button", { name: "原始尺寸 1:1" }).click();
  await expect.poll(percent).toBe("100%");
  expect(image()!.getBoundingClientRect().width).toBeCloseTo(800, 0);
  const before = image()!.getBoundingClientRect().left;
  pointer("pointerdown", 1, 300, 200, "mouse");
  pointer("pointermove", 1, 340, 200, "mouse");
  pointer("pointerup", 1, 340, 200, "mouse");
  await expect
    .poll(() => image()!.getBoundingClientRect().left)
    .toBeCloseTo(before + 40, 0);

  await page.getByRole("button", { name: "放大" }).click();
  await expect.poll(percent).toBe("125%");
  await page.getByRole("button", { name: "缩小" }).click();
  await expect.poll(percent).toBe("100%");
  await page.getByRole("button", { name: "适应窗口" }).click();
  await expect.poll(percent).toBe(fitPercent(800, 600));

  // A narrower panel refits while in fit mode.
  document.querySelector<HTMLElement>('[data-testid="frame"]')!.style.width =
    "400px";
  await expect.poll(percent).toBe(fitPercent(800, 600));
  expect(percent()).toBe(
    `${Math.round((viewport().clientWidth / 800) * 100)}%`,
  );

  await page.getByRole("button", { name: /发送位置到终端/ }).click();
  await expect
    .element(page.getByLabelText("Inserted reference"))
    .toHaveTextContent("/repo/assets/photo.png");

  const url = created.mock.results[0].value as string;
  expect(image()!.getAttribute("src")).toBe(url);
  await rendered.unmount();
  expect(revoked).toHaveBeenCalledWith(url);
});

test("pinch and ctrl+wheel zoom stay inside the preview", async () => {
  const blob = await png(800, 600);
  serve(() => raw(blob));
  await render(Harness, { file: fileOf(imageDocument({ size: blob.size })) });
  await expect.poll(percent).toBe(fitPercent(800, 600));
  const fit =
    Math.min(1, viewport().clientWidth / 800, viewport().clientHeight / 600) *
    100;
  // Only gestures that start inside the preview matter; toolbar clicks bubble.
  const escaped: string[] = [];
  const outside = (event: Event) => {
    if (viewport().contains(event.target as Node)) escaped.push(event.type);
  };
  for (const type of ["pointerdown", "pointermove", "wheel", "touchstart"])
    document.addEventListener(type, outside);
  try {
    expect(
      viewport().dispatchEvent(
        new WheelEvent("wheel", {
          ctrlKey: true,
          deltaY: -100,
          clientX: viewport().getBoundingClientRect().left + 100,
          clientY: viewport().getBoundingClientRect().top + 100,
          bubbles: true,
          cancelable: true,
        }),
      ),
    ).toBe(false);
    await expect.poll(percent).toBe(`${Math.round(fit * 1.25)}%`);
    await page.getByRole("button", { name: "适应窗口" }).click();

    pointer("pointerdown", 1, 100, 100);
    pointer("pointerdown", 2, 200, 100);
    expect(pointer("pointermove", 2, 300, 100)).toBe(false);
    await expect.poll(percent).toBe(`${Math.round(fit * 2)}%`);
    pointer("pointerup", 2, 300, 100);
    pointer("pointerup", 1, 100, 100);
    viewport().dispatchEvent(
      new TouchEvent("touchstart", { bubbles: true, cancelable: true }),
    );
    expect(escaped).toEqual([]);
  } finally {
    for (const type of ["pointerdown", "pointermove", "wheel", "touchstart"])
      document.removeEventListener(type, outside);
  }
});

test("download saves the original name, shows progress and aborts on switch", async () => {
  const blob = await png(10, 10);
  let pending: AbortSignal | undefined;
  let hang = false;
  const requests = serve((url, init) => {
    if (!url.searchParams.has("download") || !hang) return raw(blob);
    const signal = init!.signal!;
    pending = signal;
    const { promise, reject } = Promise.withResolvers<Response>();
    signal.addEventListener("abort", () =>
      reject(new DOMException("aborted", "AbortError")),
    );
    return promise;
  });
  const clicks: { download: string; href: string }[] = [];
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    clicks.push({ download: this.download, href: this.href });
  });
  const revoked = vi.spyOn(URL, "revokeObjectURL");
  const rendered = await render(Harness, {
    file: fileOf(imageDocument({ size: blob.size })),
  });
  await page.getByRole("button", { name: "下载", exact: true }).click();
  await expect.poll(() => clicks.length).toBe(1);
  expect(clicks[0].download).toBe("photo.png");
  expect(
    requests.find((r) => r.url.searchParams.get("download") === "1")?.url
      .pathname,
  ).toBe("/api/sessions/session/code/snapshots/snap-1/raw");
  await expect.poll(() => revoked.mock.calls.flat()).toContain(clicks[0].href);
  expect(document.querySelector("a[download]")).toBeNull();

  hang = true;
  await page.getByRole("button", { name: "下载", exact: true }).click();
  await expect
    .element(page.getByRole("button", { name: "下载中…" }))
    .toBeDisabled();
  await rendered.rerender({
    file: fileOf(
      imageDocument({ path: "assets/other.png", size: blob.size }),
      "snap-2",
    ),
  });
  await expect.poll(() => pending?.aborted).toBe(true);
  await expect
    .element(page.getByRole("button", { name: "下载", exact: true }))
    .toBeEnabled();
  expect(clicks).toHaveLength(1);
  expect(document.querySelector('[role="alert"]')).toBeNull();
});

test("download failures explain the cause and expired or changed snapshots ask for refresh", async () => {
  const blob = await png(10, 10);
  let failure: () => Response | Promise<Response> = () =>
    Response.json({ error: "x" }, { status: 500 });
  serve((url) => (url.searchParams.has("download") ? failure() : raw(blob)));
  await render(Harness, { file: fileOf(imageDocument({ size: blob.size })) });
  const download = page.getByRole("button", { name: "下载", exact: true });
  const cases: [() => Response | Promise<Response>, string][] = [
    [
      () => Response.json({ error: "too large" }, { status: 413 }),
      "下载失败：文件超过 100 MiB 下载上限",
    ],
    [
      () => Response.json({ error: "busy" }, { status: 429 }),
      "下载失败：同时进行的传输过多，请稍后重试",
    ],
    [
      () => Response.json({ error: "unauthorized" }, { status: 401 }),
      "下载失败：认证失败，请重新输入令牌",
    ],
    [
      () => Promise.reject(new TypeError("Failed to fetch")),
      "下载失败：网络连接异常",
    ],
    [
      () => Response.json({ error: "内容已变化，请刷新" }, { status: 409 }),
      "下载失败：内容已变化，请刷新文档",
    ],
    [
      () => Response.json({ error: "expired" }, { status: 410 }),
      "下载失败：快照已过期，请刷新文档",
    ],
  ];
  for (const [response, message] of cases) {
    failure = response;
    await download.click();
    await expect.element(page.getByRole("alert")).toHaveTextContent(message);
  }
  await expect.element(page.getByText(/快照已过期，保留旧内容/)).toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "刷新文档" }))
    .toBeVisible();
});

test("an undecodable image keeps its metadata and download", async () => {
  serve(
    () =>
      new Response(new Blob(["\x89PNGbroken"], { type: "image/png" }), {
        headers: { "Content-Type": "image/png" },
      }),
  );
  await render(Harness, { file: fileOf(imageDocument({ size: 10 })) });
  await expect
    .element(page.getByRole("alert"))
    .toHaveTextContent(
      "图片无法解码：文件可能已损坏，或当前浏览器不支持该格式",
    );
  expect(image()).toBeNull();
  await expect
    .element(page.getByLabelText("图片信息"))
    .toHaveTextContent("PNG · 尺寸未知 · 10 B");
  await expect
    .element(page.getByRole("button", { name: "下载", exact: true }))
    .toBeEnabled();
  await expect
    .element(page.getByRole("button", { name: "适应窗口" }))
    .toBeDisabled();
});

test("over-limit images show metadata and download without loading pixels", async () => {
  const requests = serve(() => Response.json({}));
  const size = 30 * 1024 * 1024;
  const rendered = await render(Harness, {
    file: fileOf(
      imageDocument({
        kind: "too-large",
        reason: "超过 20 MiB 预览上限",
        size,
      }),
    ),
  });
  await expect.element(page.getByText("超过 20 MiB 预览上限")).toBeVisible();
  await expect
    .element(page.getByLabelText("图片信息"))
    .toHaveTextContent("PNG · 30 MiB");
  await expect
    .element(page.getByRole("button", { name: "下载", exact: true }))
    .toBeEnabled();
  expect(document.querySelector('[aria-label="图片预览"]')).toBeNull();
  expect(requests).toHaveLength(0);

  await rendered.rerender({
    file: fileOf(
      imageDocument({
        kind: "too-large",
        reason: "超过 20 MiB 预览上限",
        size: 120 * 1024 * 1024,
        downloadable: false,
      }),
      "snap-2",
    ),
  });
  await expect
    .element(page.getByRole("button", { name: "下载", exact: true }))
    .toBeDisabled();
  await expect
    .element(page.getByText("超过 100 MiB 下载上限，不可下载"))
    .toBeVisible();
  expect(requests).toHaveLength(0);
});

test("SVG previews by default, toggles to referenceable source and falls back when rendering fails", async () => {
  let body = svgText;
  serve(() => raw(new Blob([body], { type: "image/svg+xml" })));
  const svg = imageDocument({
    path: "icon.svg",
    absolutePath: "/repo/icon.svg",
    kind: "text",
    text: svgText,
    lineCount: 1,
    mediaType: "image/svg+xml",
    size: svgText.length,
  });
  const rendered = await render(Harness, { file: fileOf(svg) });
  await expect
    .element(page.getByLabelText("图片信息").first())
    .toHaveTextContent("SVG · 40 × 20 · 111 B");
  await expect
    .element(page.getByRole("button", { name: "预览", exact: true }))
    .toHaveAttribute("aria-pressed", "true");
  expect(document.querySelector(".cm-editor")).toBeNull();

  await page.getByRole("button", { name: "源码", exact: true }).click();
  await expect
    .poll(() => document.querySelector(".cm-content")?.textContent)
    .toContain("<svg");
  expect(document.querySelector('[aria-label="图片预览"]')).toBeNull();
  await page.getByRole("button", { name: "预览", exact: true }).click();
  await expect
    .element(page.getByLabelText("图片信息").first())
    .toHaveTextContent("SVG · 40 × 20 · 111 B");

  body = "<svg><broken";
  await rendered.rerender({ file: fileOf(svg, "snap-2") });
  await expect
    .element(page.getByText(/SVG 预览不可用，已切换到源码/))
    .toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "源码", exact: true }))
    .toHaveAttribute("aria-pressed", "true");
  await expect
    .poll(() => document.querySelector(".cm-content")?.textContent)
    .toContain("<svg");
});

test("image diffs show the change, sizes and preview the selected side", async () => {
  const older = await png(10, 10);
  const newer = await png(20, 20);
  const requests = serve((url) =>
    raw(url.searchParams.get("side") === "old" ? older : newer),
  );
  const rendered = await render(Harness, {
    diff: imageDiff(
      imageDocument({ source: "index", size: 1024 }),
      imageDocument({ size: 2048 }),
    ),
  });
  await expect.element(page.getByText("图片已修改")).toBeVisible();
  await expect
    .element(page.getByLabelText("大小变化"))
    .toHaveTextContent("1 KiB → 2 KiB");
  await expect
    .element(page.getByLabelText("图片信息").first())
    .toHaveTextContent("PNG · 20 × 20 · 2 KiB");
  expect(requests.at(-1)!.url.searchParams.get("side")).toBe("new");
  await page.getByRole("combobox", { name: "预览来源" }).selectOptions("old");
  await expect
    .element(page.getByLabelText("图片信息").first())
    .toHaveTextContent("PNG · 10 × 10 · 1 KiB");
  expect(requests.at(-1)!.url.pathname).toBe(
    "/api/sessions/session/code/snapshots/diff-1/raw",
  );
  expect(requests.at(-1)!.url.searchParams.get("side")).toBe("old");

  // A deleted image opens on the old side even if the new side was selected.
  await page.getByRole("combobox", { name: "预览来源" }).selectOptions("new");
  await rendered.rerender({
    diff: imageDiff(
      imageDocument({ source: "index", size: 1024 }),
      imageDocument({
        path: "",
        source: "empty",
        kind: "text",
        mediaType: "",
        size: 0,
        downloadable: false,
        referenceable: false,
      }),
      "diff-2",
    ),
  });
  await expect.element(page.getByText("图片已删除")).toBeVisible();
  await expect
    .element(page.getByLabelText("大小变化"))
    .toHaveTextContent("1 KiB → 不存在");
  await expect
    .element(page.getByRole("combobox", { name: "预览来源" }))
    .toHaveValue("old");
  await page.getByRole("combobox", { name: "预览来源" }).selectOptions("new");
  await expect.element(page.getByText("新侧不存在")).toBeVisible();
  expect(document.querySelector('[aria-label="图片预览"]')).toBeNull();
  await expect
    .element(page.getByRole("button", { name: "下载", exact: true }))
    .not.toBeInTheDocument();
});
