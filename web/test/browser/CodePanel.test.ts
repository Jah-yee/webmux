import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import Harness from "./CodePanelHarness.svelte";
import { nodeId } from "../../src/code-reference";
const now = "2026-10-14T00:00:00Z";
function file(n: number, version = "original") {
  return {
    snapshotId: `snapshot-${n}-${version}`,
    checkedAt: now,
    document: {
      path: `file-${n}.txt`,
      absolutePath: `/root/file-${n}.txt`,
      source: "worktree",
      oid: "",
      contentVersion: version,
      kind: "text",
      text: `${version} file ${n}\n`,
      lineCount: 1,
      referenceable: true,
      reason: "",
      mediaType: "",
      size: `${version} file ${n}\n`.length,
      downloadable: true,
    },
  };
}
let state = "unchanged",
  version = "original",
  checkFailed = false,
  checks = 0;
function serve() {
  state = "unchanged";
  version = "original";
  checkFailed = false;
  checks = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const url = new URL(String(input), location.href);
      if (url.pathname.endsWith("/context"))
        return Response.json({
          contextId: "root",
          rootPath: "/root",
          rootKind: "directory",
          browseAvailable: true,
          diffAvailable: false,
          ignoreAvailable: false,
          reason: "",
        });
      if (url.pathname.endsWith("/tree"))
        return Response.json({
          children: Array.from({ length: 21 }, (_, n) => ({
            nodeId: btoa(`file-${n}.txt`),
            path: `file-${n}.txt`,
            name: `file-${n}.txt`,
            kind: "file",
            gitStatus: "",
            ignored: false,
            referenceable: true,
            mediaType: "",
          })),
          nextCursor: "",
          limited: false,
          complete: true,
          contentVersion: "tree",
          checkedAt: now,
        });
      if (url.pathname.endsWith("/file")) {
        const path = atob(url.searchParams.get("nodeId")!);
        return Response.json(file(Number(path.match(/\d+/)![0]), version));
      }
      if (url.pathname.endsWith("/check")) {
        checks++;
        if (checkFailed)
          return Response.json({ error: "check unavailable" }, { status: 503 });
        const req = JSON.parse(String(init?.body));
        return Response.json({
          snapshots: Object.fromEntries(
            req.snapshotIds.map((id: string) => [id, state]),
          ),
          directories: {},
          changes: "unchanged",
          checkedAt: now,
        });
      }
      throw new Error(`Unexpected ${url}`);
    }),
  );
}
afterEach(async () => {
  await cleanup();
  await userEvent.cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
test("updates only on refresh, blocks stale references and pauses hidden polling", async () => {
  serve();
  render(Harness);
  await page.getByRole("button", { name: "file-0.txt", exact: true }).click();
  await expect
    .element(page.getByLabelText("只读代码"))
    .toMatchTextContent(new RegExp("original file 0"));
  state = "updated";
  document.dispatchEvent(new Event("visibilitychange"));
  await expect
    .element(
      page.getByText("内容已更新，保留旧内容。刷新文档", { exact: false }),
    )
    .toMatchTextContent(new RegExp("内容已更新"));
  await expect
    .element(page.getByRole("button", { name: "发送位置到终端" }))
    .toBeDisabled();
  await expect
    .element(page.getByLabelText("只读代码"))
    .toMatchTextContent(new RegExp("original file 0"));
  version = "refreshed";
  state = "unchanged";
  await page.getByRole("button", { name: "刷新文档" }).click();
  await expect
    .element(page.getByLabelText("只读代码"))
    .toMatchTextContent(new RegExp("refreshed file 0"));
  await page.getByRole("button", { name: "发送位置到终端" }).click();
  await expect
    .element(page.getByLabelText("Inserted reference"))
    .toMatchTextContent(new RegExp("/root/file-0.txt"));
  checkFailed = true;
  document.dispatchEvent(new Event("visibilitychange"));
  await expect
    .element(page.getByRole("button", { name: "发送位置到终端" }))
    .toBeDisabled();
  await expect
    .element(page.getByLabelText("只读代码"))
    .toMatchTextContent(new RegExp("refreshed file 0"));
  await page.getByRole("button", { name: "Toggle code visibility" }).click();
  const previous = checks;
  document.dispatchEvent(new Event("visibilitychange"));
  await new Promise((resolve) => setTimeout(resolve, 100));
  expect(checks).toBe(previous);
});
test("caps open documents without evicting tabs and reuses existing paths", async () => {
  serve();
  render(Harness);
  for (let n = 0; n < 20; n++) {
    await page
      .getByRole("button", { name: `file-${n}.txt`, exact: true })
      .click();
    await expect
      .element(
        page.getByRole("tab", {
          name: `file-${n}.txt · worktree`,
          exact: true,
        }),
      )
      .toBeInTheDocument();
  }
  await page.getByRole("button", { name: "file-20.txt", exact: true }).click();
  await expect
    .element(page.getByRole("alert"))
    .toMatchTextContent(new RegExp("最多打开 20 个标签"));
  expect(
    document.querySelectorAll(
      '[role="tablist"][aria-label="文件标签"] [role="tab"]',
    ).length,
  ).toBe(20);
  await page.getByRole("button", { name: "file-0.txt", exact: true }).click();
  await expect
    .element(
      page.getByRole("tab", { name: "file-0.txt · worktree", exact: true }),
    )
    .toHaveAttribute("aria-selected", "true");
  expect(
    document.querySelectorAll(
      '[role="tablist"][aria-label="文件标签"] [role="tab"]',
    ).length,
  ).toBe(20);
});

test("a delayed file response cannot activate over a newer document choice", async () => {
  serve();
  const server = globalThis.fetch;
  const delayed = Promise.withResolvers<Response>();
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>((input, init) => {
      const url = new URL(String(input), location.href);
      if (
        url.pathname.endsWith("/file") &&
        atob(url.searchParams.get("nodeId")!) === "file-0.txt"
      )
        return delayed.promise;
      return server(input, init);
    }),
  );
  render(Harness);
  await page.getByRole("button", { name: "file-0.txt", exact: true }).click();
  await page.getByRole("button", { name: "file-1.txt", exact: true }).click();
  const latest = page.getByRole("tab", {
    name: "file-1.txt · worktree",
    exact: true,
  });
  await expect.element(latest).toHaveAttribute("aria-selected", "true");
  delayed.resolve(Response.json(file(0)));
  await expect
    .element(
      page.getByRole("tab", { name: "file-0.txt · worktree", exact: true }),
    )
    .toBeInTheDocument();
  await expect.element(latest).toHaveAttribute("aria-selected", "true");
  await expect
    .element(page.getByRole("tabpanel").getByLabelText("只读代码"))
    .toMatchTextContent("original file 1");
});

test("loads changes beyond 1000 lazily, virtualizes rows, filters and refreshes a late diff", async () => {
  serve();
  const server = globalThis.fetch;
  const entries = Array.from({ length: 1505 }, (_, n) => ({
    nodeId: btoa(`file-${n}.txt`),
    changeId: `change-${n}`,
    group: "untracked",
    oldPath: `file-${n}.txt`,
    newPath: `file-${n}.txt`,
    status: "A",
    oldOid: "",
    newOid: "",
    oldMode: "000000",
    newMode: "100644",
  }));
  let stale = false;
  const changeRequests: URL[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const url = new URL(String(input), location.href);
      if (url.pathname.endsWith("/context")) {
        const response = await server(input, init);
        return Response.json({
          ...(await response.json()),
          diffAvailable: true,
        });
      }
      if (url.pathname.endsWith("/changes")) {
        changeRequests.push(url);
        const cursor = url.searchParams.get("cursor") || "";
        if (stale && cursor)
          return Response.json(
            { error: "修改列表已改变，请刷新列表" },
            { status: 409 },
          );
        const id = url.searchParams.get("nodeId"),
          q = url.searchParams.get("q") || "";
        const group = url.searchParams.get("group");
        const matching = entries.filter(
          (c) =>
            (!id || c.nodeId === id) &&
            (!group || c.group === group) &&
            c.newPath.includes(q),
        );
        const offset = Number(cursor || "0"),
          end = Math.min(offset + 200, matching.length);
        return Response.json({
          entries: matching.slice(offset, end),
          nextCursor: end < matching.length ? String(end) : "",
          total: matching.length,
          contentVersion: stale ? "updated" : "original",
          checkedAt: now,
        });
      }
      if (url.pathname.endsWith("/diff")) {
        const n = Number(url.searchParams.get("changeId")!.split("-")[1]);
        const next = file(n).document;
        return Response.json({
          snapshotId: `diff-${n}`,
          checkedAt: now,
          kind: "text",
          reason: "",
          change: entries[n],
          old: {
            ...next,
            source: "empty",
            text: "",
            lineCount: 0,
            referenceable: false,
            size: 0,
            downloadable: false,
          },
          new: next,
        });
      }
      return server(input, init);
    }),
  );
  render(Harness);
  await page.getByRole("tab", { name: "修改", exact: true }).click();
  await expect
    .element(page.getByText("已加载 200 / 1505 项", { exact: true }))
    .toBeVisible();
  const list = document.querySelector<HTMLElement>(
    '[role="list"][aria-label="修改列表"]',
  )!;
  expect(list.querySelectorAll("button").length).toBeLessThan(50);
  const source = document.querySelector<HTMLSelectElement>(
    'select[aria-label="修改来源"]',
  )!;
  source.value = "staged";
  source.dispatchEvent(new Event("change", { bubbles: true }));
  await expect
    .element(page.getByText("已加载 0 / 0 项", { exact: true }))
    .toBeVisible();
  source.value = "untracked";
  source.dispatchEvent(new Event("change", { bubbles: true }));
  await expect
    .element(page.getByText("已加载 200 / 1505 项", { exact: true }))
    .toBeVisible();
  const calls = changeRequests.length;
  list.querySelector<HTMLButtonElement>("button")!.focus();
  await userEvent.keyboard("{ArrowUp}");
  expect(changeRequests.length).toBe(calls);
  await userEvent.keyboard("{End}");
  await expect
    .element(page.getByText("已加载 400 / 1505 项", { exact: true }))
    .toBeVisible();
  for (const loaded of [600, 800, 1000, 1200, 1400, 1505]) {
    await page
      .getByRole("button", { name: "加载更多修改", exact: true })
      .click();
    await expect
      .element(page.getByText(`已加载 ${loaded} / 1505 项`, { exact: true }))
      .toBeVisible();
  }
  await expect
    .element(page.getByText("已加载 1505 / 1505 项", { exact: true }))
    .toBeVisible();
  list.scrollTop = list.scrollHeight;
  list.dispatchEvent(new Event("scroll"));
  await page
    .getByRole("button", { name: "未跟踪 file-1504.txt", exact: true })
    .click();
  await expect
    .element(page.getByText("original file 1504", { exact: true }))
    .toBeVisible();
  expect(list.querySelectorAll("button").length).toBeLessThan(50);
  await page.getByRole("button", { name: "刷新全部", exact: true }).click();
  await expect
    .poll(() =>
      changeRequests.some(
        (u) => u.searchParams.get("nodeId") === btoa("file-1504.txt"),
      ),
    )
    .toBe(true);
  await page.getByLabelText("筛选修改路径").fill("file-1504.txt");
  await page.getByRole("button", { name: "筛选修改", exact: true }).click();
  await expect
    .element(page.getByText("已加载 1 / 1 项", { exact: true }))
    .toBeVisible();
  await page
    .getByRole("button", { name: "未跟踪 file-1504.txt", exact: true })
    .click();
  await page.getByLabelText("筛选修改路径").fill("");
  await page.getByRole("button", { name: "筛选修改", exact: true }).click();
  await expect
    .element(page.getByText("已加载 200 / 1505 项", { exact: true }))
    .toBeVisible();
  stale = true;
  await page.getByRole("button", { name: "加载更多修改", exact: true }).click();
  await expect
    .element(page.getByRole("alert"))
    .toMatchTextContent("修改列表已改变");
  await expect
    .element(page.getByText("已加载 200 / 1505 项", { exact: true }))
    .toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "加载更多修改", exact: true }))
    .toBeDisabled();
  await page.getByRole("button", { name: "刷新修改列表", exact: true }).click();
  await expect
    .element(page.getByRole("button", { name: "加载更多修改", exact: true }))
    .toBeEnabled();
});

// A same-origin iframe stands in for the pop-out window: a second document
// the panel moves into, closed through the window's pagehide.
function fakeWindow() {
  let frame: HTMLIFrameElement | undefined;
  return {
    get window() {
      return frame?.contentWindow ?? null;
    },
    open() {
      frame = document.createElement("iframe");
      frame.title = "popout";
      document.body.append(frame);
      const win = frame.contentWindow!;
      const close = () => {
        win.dispatchEvent(new Event("pagehide"));
        frame?.remove();
        frame = undefined;
      };
      Object.defineProperty(win, "close", { value: close });
      return win;
    },
  };
}
function fakePictureInPicture(refuse?: Error) {
  const fake = fakeWindow();
  const api = {
    get window() {
      return fake.window;
    },
    async requestWindow() {
      if (refuse) throw refuse;
      return fake.open();
    },
  };
  vi.stubGlobal("documentPictureInPicture", api);
  return api;
}
// Leaves every other media query to the real browser.
function pointer(fine: boolean) {
  const real = window.matchMedia.bind(window);
  vi.stubGlobal("matchMedia", (query: string) =>
    query === "(pointer: fine)"
      ? Object.defineProperty(real(query), "matches", { value: fine })
      : real(query),
  );
}
function popoutButton(win: Window, name: string) {
  return [...win.document.querySelectorAll("button")].find(
    (button) =>
      button.getAttribute("aria-label") === name ||
      button.textContent?.trim() === name,
  );
}

test("touch-only devices without Picture-in-Picture get no pop-out control", async () => {
  serve();
  vi.stubGlobal("documentPictureInPicture", undefined);
  pointer(false);
  render(Harness);
  await expect
    .element(page.getByRole("button", { name: "file-0.txt", exact: true }))
    .toBeVisible();
  expect(
    page.getByRole("button", { name: "在独立窗口中显示代码" }).elements(),
  ).toHaveLength(0);
});

test("desktop browsers without Picture-in-Picture pop out into an ordinary popup", async () => {
  serve();
  vi.stubGlobal("documentPictureInPicture", undefined);
  pointer(true);
  const popup = fakeWindow();
  const open = vi.spyOn(window, "open").mockImplementation(() => popup.open());
  render(Harness);
  await page.getByRole("button", { name: "file-0.txt", exact: true }).click();
  await page
    .getByRole("button", { name: "在独立窗口中显示代码", exact: true })
    .click();
  await expect.element(page.getByText("代码已在独立窗口中显示")).toBeVisible();
  expect(open).toHaveBeenCalledWith(
    "",
    "_blank",
    expect.stringMatching(/^popup,width=\d+,height=\d+$/),
  );
  const win = popup.window!;
  popoutButton(win, "file-1.txt")!.click();
  await expect
    .poll(
      () =>
        win.document.querySelector("[role=tabpanel]:not(.hidden) .cm-content")
          ?.textContent,
    )
    .toContain("original file 1");
  // Reloading or leaving the opener must not strand the panel in the popup.
  window.dispatchEvent(new Event("pagehide"));
  expect(popup.window).toBeNull();
  await expect
    .element(page.getByRole("tab", { name: /^file-1\.txt/ }))
    .toHaveAttribute("aria-selected", "true");
});

test("a blocked popup keeps the panel in place and says why", async () => {
  serve();
  vi.stubGlobal("documentPictureInPicture", undefined);
  pointer(true);
  vi.spyOn(window, "open").mockReturnValue(null);
  render(Harness);
  await page
    .getByRole("button", { name: "在独立窗口中显示代码", exact: true })
    .click();
  await expect
    .element(page.getByRole("alert"))
    .toMatchTextContent("弹出窗口被浏览器拦截");
  expect(page.getByText("代码已在独立窗口中显示").elements()).toHaveLength(0);
  expect(document.querySelector(".code-panel")).not.toBeNull();
});

test("a popped-out panel stays interactive in the other document and returns with its tabs", async () => {
  serve();
  const pip = fakePictureInPicture();
  render(Harness);
  await page.getByRole("button", { name: "file-0.txt", exact: true }).click();
  await page
    .getByRole("button", { name: "在独立窗口中显示代码", exact: true })
    .click();
  await expect.element(page.getByText("代码已在独立窗口中显示")).toBeVisible();
  const win = pip.window!;
  expect(document.querySelector(".code-panel")).toBeNull();
  expect(win.document.querySelector(".code-panel")).not.toBeNull();
  // Delegated Svelte handlers fire for clicks inside the other document.
  popoutButton(win, "file-1.txt")!.click();
  await expect
    .poll(() =>
      [
        ...win.document.querySelectorAll('[aria-label="文件标签"] [role=tab]'),
      ].map((tab) => tab.textContent),
    )
    .toEqual(["file-0.txt · worktree", "file-1.txt · worktree"]);
  await expect
    .poll(
      () =>
        win.document.querySelector("[role=tabpanel]:not(.hidden) .cm-content")
          ?.textContent,
    )
    .toContain("original file 1");
  // Hiding the opener's pane does not pause a popped-out panel.
  await page.getByRole("button", { name: "Toggle code visibility" }).click();
  const previous = checks;
  document.dispatchEvent(new Event("visibilitychange"));
  await expect.poll(() => checks).toBeGreaterThan(previous);

  popoutButton(win, "收回代码窗口")!.click();
  await expect
    .element(page.getByText("代码已在独立窗口中显示"))
    .not.toBeInTheDocument();
  expect(pip.window).toBeNull();
  await expect
    .element(page.getByRole("tab", { name: /^file-1\.txt/ }))
    .toHaveAttribute("aria-selected", "true");
  await expect
    .element(page.getByRole("tabpanel").getByLabelText("只读代码"))
    .toMatchTextContent(new RegExp("original file 1"));
  await page.getByRole("button", { name: "file-2.txt", exact: true }).click();
  await expect
    .element(page.getByRole("tab", { name: /^file-2\.txt/ }))
    .toHaveAttribute("aria-selected", "true");
});

test("a refused pop-out window keeps the panel in place and says why", async () => {
  serve();
  fakePictureInPicture(
    new DOMException(
      "Document PiP requires user activation",
      "NotAllowedError",
    ),
  );
  render(Harness);
  await page
    .getByRole("button", { name: "在独立窗口中显示代码", exact: true })
    .click();
  await expect
    .element(page.getByRole("alert"))
    .toMatchTextContent("Document PiP requires user activation");
  expect(page.getByText("代码已在独立窗口中显示").elements()).toHaveLength(0);
  await page.getByRole("button", { name: "file-0.txt", exact: true }).click();
  await expect
    .element(page.getByLabelText("只读代码"))
    .toMatchTextContent(new RegExp("original file 0"));
});

test("checks all loaded tree branches within the server's 30-directory limit", async () => {
  serve();
  const server = globalThis.fetch;
  const names = Array.from({ length: 31 }, (_, n) => `folder-${n}`);
  const batches: string[][] = [];
  const loaded = new Set<string>();
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const url = new URL(String(input), location.href);
      if (url.pathname.endsWith("/tree")) {
        const path = atob(url.searchParams.get("nodeId") ?? "");
        loaded.add(path);
        return Response.json({
          children: path
            ? []
            : names.map((name) => ({
                nodeId: btoa(name),
                path: name,
                name,
                kind: "directory",
                gitStatus: "",
                ignored: false,
                referenceable: false,
                mediaType: "",
              })),
          nextCursor: "",
          limited: false,
          complete: true,
          contentVersion: "tree",
          checkedAt: now,
        });
      }
      if (url.pathname.endsWith("/check")) {
        const body = JSON.parse(String(init?.body));
        const directories = Object.keys(body.directories);
        batches.push(directories);
        if (directories.length > 30)
          return Response.json(
            { error: "too many directories" },
            { status: 400 },
          );
      }
      return server(input, init);
    }),
  );
  await render(Harness);
  for (const name of names)
    await page.getByRole("button", { name, exact: true }).click();
  await expect.poll(() => loaded.size).toBe(32);
  batches.length = 0;
  document.dispatchEvent(new Event("visibilitychange"));
  await expect.poll(() => new Set(batches.flat()).size).toBe(32);
  expect(batches.every((batch) => batch.length <= 30)).toBe(true);
});

test("collapsing during a path search cannot replace the root with search hits", async () => {
  serve();
  const server = globalThis.fetch;
  const delayed = Promise.withResolvers<Response>();
  let searched = false;
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>((input, init) => {
      if (new URL(String(input), location.href).pathname.endsWith("/files")) {
        searched = true;
        return delayed.promise;
      }
      return server(input, init);
    }),
  );
  render(Harness);
  await page.getByRole("button", { name: "file-0.txt", exact: true }).query();
  await page.getByRole("button", { name: "切换路径搜索" }).click();
  await page.getByRole("textbox", { name: "搜索文件路径" }).fill("nested");
  await page.getByRole("button", { name: "搜索路径", exact: true }).click();
  await expect.poll(() => searched).toBe(true);
  await expect
    .element(page.getByRole("button", { name: "刷新文件树", exact: true }))
    .toBeDisabled();
  await page.getByRole("button", { name: "全部折叠", exact: true }).click();
  delayed.resolve(
    Response.json({
      children: [
        {
          nodeId: btoa("nested/hit.txt"),
          path: "nested/hit.txt",
          name: "hit.txt",
          kind: "file",
          gitStatus: "",
          ignored: false,
          referenceable: true,
          mediaType: "",
        },
      ],
      nextCursor: "",
      limited: false,
      complete: true,
      contentVersion: "search",
      checkedAt: now,
    }),
  );
  await expect
    .element(page.getByRole("button", { name: "刷新文件树", exact: true }))
    .toBeEnabled();
  await expect
    .element(page.getByRole("button", { name: "file-0.txt", exact: true }))
    .toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "hit.txt", exact: true }))
    .not.toBeInTheDocument();
});

function serveExplorer() {
  serve();
  const server = globalThis.fetch;
  let revision = "v1";
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const url = new URL(String(input), location.href);
      if (url.pathname.endsWith("/context")) {
        const response = await server(input, init);
        return Response.json({
          ...(await response.json()),
          diffAvailable: true,
          ignoreAvailable: true,
        });
      }
      if (url.pathname.endsWith("/changes"))
        return Response.json({
          entries: [],
          nextCursor: "",
          total: 0,
          contentVersion: "changes",
          checkedAt: now,
          limited: false,
          complete: true,
        });
      if (url.pathname.endsWith("/tree")) {
        const path = atob(url.searchParams.get("nodeId") ?? "");
        const names = path ? [`docs/${revision}.txt`] : ["docs"];
        return Response.json({
          children: names.map((name) => ({
            nodeId: btoa(name),
            path: name,
            name: name.split("/").at(-1),
            kind: path ? "file" : "directory",
            gitStatus: "",
            ignored: false,
            referenceable: !!path,
            mediaType: "",
          })),
          nextCursor: "",
          limited: false,
          complete: true,
          contentVersion: path ? revision : "root",
          checkedAt: now,
        });
      }
      return server(input, init);
    }),
  );
  return {
    refresh: () => {
      revision = "v2";
    },
  };
}

test("expanded branches survive hiding navigation, changing tabs and ignore scope", async () => {
  serveExplorer();
  render(Harness);
  await page.getByRole("button", { name: "docs", exact: true }).click();
  const child = page.getByRole("button", {
    name: "v1.txt",
    exact: true,
    includeHidden: true,
  });
  await expect.element(child).toBeVisible();
  await page.getByRole("button", { name: "收起导航", exact: true }).click();
  await expect.element(child).not.toBeVisible();
  await page.getByRole("button", { name: "文件导航", exact: true }).click();
  await expect.element(child).toBeVisible();
  await page.getByRole("tab", { name: "修改", exact: true }).click();
  await expect.element(child).not.toBeVisible();
  await page.getByRole("tab", { name: "文件", exact: true }).click();
  await expect.element(child).toBeVisible();
  await page.getByRole("button", { name: "文件导航选项", exact: true }).click();
  await page.getByRole("menuitemcheckbox", { name: "显示忽略项" }).click();
  await expect.element(child).toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "docs", exact: true }))
    .toHaveAttribute("aria-expanded", "true");
});

test("a directory check from before refresh cannot restore a stale update warning", async () => {
  const explorer = serveExplorer();
  const server = globalThis.fetch;
  const delayed = Promise.withResolvers<Response>();
  let checking = false;
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>((input, init) => {
      const url = new URL(String(input), location.href);
      if (
        url.pathname.endsWith("/check") &&
        JSON.parse(String(init?.body)).directories[nodeId("docs")] === "v1"
      ) {
        checking = true;
        return delayed.promise;
      }
      return server(input, init);
    }),
  );
  render(Harness);
  await page.getByRole("button", { name: "docs", exact: true }).click();
  await expect
    .element(page.getByRole("button", { name: "v1.txt", exact: true }))
    .toBeVisible();
  document.dispatchEvent(new Event("visibilitychange"));
  await expect.poll(() => checking).toBe(true);
  explorer.refresh();
  await page.getByRole("button", { name: "刷新文件树", exact: true }).click();
  await expect
    .element(page.getByRole("button", { name: "v2.txt", exact: true }))
    .toBeVisible();
  delayed.resolve(
    Response.json({
      snapshots: {},
      directories: { [nodeId("docs")]: "updated" },
      changes: "unchanged",
      checkedAt: now,
    }),
  );
  const rendered = Promise.withResolvers<void>();
  requestAnimationFrame(() => requestAnimationFrame(() => rendered.resolve()));
  await rendered.promise;
  await expect
    .element(page.getByText("目录已更新或检测失败", { exact: true }))
    .not.toBeInTheDocument();
});
