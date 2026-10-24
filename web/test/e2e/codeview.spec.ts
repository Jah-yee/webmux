import { execFileSync } from "node:child_process";
import { mkdir, writeFile, readFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { join } from "node:path";
import type { Page } from "@playwright/test";
import {
  test,
  expect,
  createShell,
  runCommand,
  expectTerminalLine,
} from "./fixtures";
function git(root: string, ...args: string[]) {
  return execFileSync("git", ["-C", root, ...args], {
    env: {
      ...process.env,
      GIT_AUTHOR_NAME: "E2E",
      GIT_AUTHOR_EMAIL: "e2e@example.com",
      GIT_COMMITTER_NAME: "E2E",
      GIT_COMMITTER_EMAIL: "e2e@example.com",
    },
  }).toString();
}
async function showCode(page: import("@playwright/test").Page) {
  await page
    .locator(".terminal-header")
    .getByRole("button", { name: "代码", exact: true })
    .click();
  await expect(page.getByRole("region", { name: "代码工作区" })).toBeVisible();
}
async function visibleCode(page: import("@playwright/test").Page) {
  return (
    await page
      .locator(
        ".code-reader:visible .cm-content, .code-diff-reader:visible [data-code]",
      )
      .allTextContents()
  ).join("\n");
}
async function nav(page: import("@playwright/test").Page) {
  const button = page.getByRole("button", { name: "文件导航", exact: true });
  if (await button.isVisible()) await button.click();
}

test("read ordinary files, independent tabs, updates and insert a line without Enter", async ({
  page,
  request,
  workDir,
}, info) => {
  await writeFile(
    join(workDir, "clean.go"),
    "package main\n// unchanged line\nfunc main() {}\n",
  );
  await writeFile(join(workDir, "other.txt"), "independent document\n");
  const id = await createShell(page, "Code shell", workDir);
  await runCommand(page, "printf 'code-pty-ready\\n'");
  await expectTerminalLine(page, "code-pty-ready");
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type("printf 'reference: %s\\n' ");
  await showCode(page);
  await page.getByRole("button", { name: "clean.go", exact: true }).click();
  await expect.poll(() => visibleCode(page)).toContain("unchanged line");
  if (info.project.use.isMobile) {
    await page
      .locator(".code-reader:visible .cm-line")
      .filter({ hasText: /^package main$/ })
      .tap();
    await expect(
      page.getByRole("button", { name: "发送第 1 行位置到终端", exact: true }),
    ).toBeVisible();
    await page
      .locator(".code-reader:visible .cm-line")
      .filter({ hasText: /^package main$/ })
      .evaluate((row) => {
        const range = document.createRange();
        range.selectNodeContents(row);
        const selection = getSelection()!;
        selection.removeAllRanges();
        selection.addRange(range);
      });
    await expect(
      page.getByRole("button", { name: "发送第 1 行位置到终端", exact: true }),
    ).toHaveCount(0);
    expect(await page.evaluate(() => getSelection()?.toString())).toBe(
      "package main",
    );
    await page.evaluate(() => getSelection()?.removeAllRanges());
    await page
      .locator(".code-reader:visible .cm-line")
      .filter({ hasText: /^\/\/ unchanged line$/ })
      .tap();
    await expect(
      page.getByRole("button", { name: "发送第 1 行位置到终端", exact: true }),
    ).toHaveCount(0);
    await page
      .getByRole("button", { name: "发送第 2 行位置到终端", exact: true })
      .tap();
    await expect(page.locator(".terminal-host")).toBeVisible();
  } else {
    const navigation = page.getByRole("navigation", {
      name: "文件与修改导航",
    });
    await page.getByRole("button", { name: "clean.go", exact: true }).click();
    await expect(navigation).toBeVisible();
    const divider = page.getByRole("separator", { name: "调整导航宽度" });
    const before = (await navigation.boundingBox())!.width;
    await divider.focus();
    await page.keyboard.press("Shift+ArrowRight");
    await expect
      .poll(async () => (await navigation.boundingBox())!.width)
      .toBeCloseTo(before + 64, 0);
    const box = (await divider.boundingBox())!;
    await page.mouse.move(box.x + 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x - 38, box.y + box.height / 2);
    await page.mouse.up();
    await expect
      .poll(async () => (await navigation.boundingBox())!.width)
      .toBeCloseTo(before + 24, 0);
    await page.getByRole("button", { name: "收起导航", exact: true }).click();
    await expect(navigation).toHaveCount(0);
    await page.getByRole("button", { name: "文件导航", exact: true }).click();
    await page
      .locator(".code-reader:visible .cm-lineNumbers .cm-gutterElement")
      .filter({ hasText: /^2$/ })
      .click();
    await page.getByRole("button", { name: "发送位置到终端" }).click();
    await page
      .locator(".terminal-header")
      .getByRole("button", { name: "折叠代码" })
      .click();
  }
  const buffer = await (await request.get(`/api/sessions/${id}/buffer`)).text();
  expect(buffer).not.toContain(`reference: ${join(workDir, "clean.go")}:2\r\n`);
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.press("Enter");
  await expect
    .poll(
      async () =>
        await (await request.get(`/api/sessions/${id}/buffer`)).text(),
    )
    .toContain(`reference: ${join(workDir, "clean.go")}:2\r\n`);
  await showCode(page);
  await nav(page);
  await page.getByRole("button", { name: "other.txt", exact: true }).click();
  await expect.poll(() => visibleCode(page)).toContain("independent document");
  await page.getByRole("tab", { name: /clean.go · worktree/ }).click();
  await expect.poll(() => visibleCode(page)).toContain("unchanged line");
  await writeFile(join(workDir, "clean.go"), "new version\n");
  await expect(
    page.getByRole("status").filter({ hasText: "内容已更新" }),
  ).toBeVisible({ timeout: 12000 });
  await expect.poll(() => visibleCode(page)).toContain("unchanged line");
  await expect(
    page
      .getByRole("button", { name: "发送位置到终端" })
      .filter({ visible: true }),
  ).toBeDisabled();
  if (info.project.use.isMobile) {
    await page
      .locator(".code-reader:visible .cm-line")
      .filter({ hasText: /^\/\/ unchanged line$/ })
      .tap();
    await expect(
      page.getByRole("button", { name: "发送第 2 行位置到终端", exact: true }),
    ).toBeDisabled();
  }
  await page
    .getByRole("button", { name: "刷新文档" })
    .filter({ visible: true })
    .click();
  await expect.poll(() => visibleCode(page)).toContain("new version");
  await expect(
    page.getByRole("button", { name: /^发送第 \d+ 行位置到终端$/ }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "文件内查找" })
    .filter({ visible: true })
    .click();
  await expect(page.locator(".code-search:visible")).toBeVisible();
  expect(await readFile(join(workDir, "clean.go"), "utf8")).toBe(
    "new version\n",
  );
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
});

test("touch line actions infer the reference source from the clicked line", async ({
  page,
  request,
  workDir,
}, info) => {
  test.skip(!info.project.use.isMobile, "Touch line actions are mobile-only");
  git(workDir, "init", "-q");
  await writeFile(join(workDir, "code.txt"), "first\nremoved\nlast\n");
  git(workDir, "add", ".");
  git(workDir, "commit", "-qm", "initial");
  await writeFile(
    join(workDir, "code.txt"),
    "first\nadded one\nadded two\nlast\n",
  );
  const id = await createShell(page, "Touch Git references", workDir);
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type("printf 'old:%s\\n' ");
  await showCode(page);
  await page.getByRole("tab", { name: "修改", exact: true }).click();
  await page
    .getByRole("button", { name: "修改 code.txt", exact: true })
    .click();
  const lines = page.locator(".code-diff-reader:visible [data-line]");
  await lines.filter({ hasText: /^added two$/ }).tap();
  await expect(
    page.getByRole("button", { name: "发送第 3 行位置到终端", exact: true }),
  ).toBeVisible();
  await lines.filter({ hasText: /^removed$/ }).tap();
  await page
    .getByRole("button", { name: "发送第 2 行位置到终端", exact: true })
    .tap();
  await expect(page.locator(".terminal-host")).toBeVisible();
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.press("Enter");
  await expect
    .poll(async () => (await request.get(`/api/sessions/${id}/buffer`)).text())
    .toContain(`old:${join(workDir, "code.txt")}:2（索引版本）\r\n`);
  await page.keyboard.type("printf 'new:%s\\n' ");
  await showCode(page);
  await lines.filter({ hasText: /^added two$/ }).tap();
  await page
    .getByRole("button", { name: "发送第 3 行位置到终端", exact: true })
    .tap();
  await expect(page.locator(".terminal-host")).toBeVisible();
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.press("Enter");
  await expect
    .poll(async () => (await request.get(`/api/sessions/${id}/buffer`)).text())
    .toContain(`new:${join(workDir, "code.txt")}:3\r\n`);
});

test("frozen Git diff, ignored navigation and complete source documents", async ({
  page,
  workDir,
}) => {
  git(workDir, "init", "-q");
  const original =
    Array.from({ length: 120 }, (_, i) => `ordinary line ${i + 1}`).join("\n") +
    "\n";
  await writeFile(join(workDir, "code.txt"), original);
  await writeFile(join(workDir, ".gitignore"), ".env\nnode_modules/\n");
  await writeFile(join(workDir, ".env"), "hidden default");
  await mkdir(join(workDir, "node_modules"));
  await writeFile(join(workDir, "node_modules/dependency"), "ignored");
  git(workDir, "add", ".");
  git(workDir, "commit", "-qm", "initial");
  const indexed = original.replace("ordinary line 50\n", "indexed line 50\n");
  await writeFile(join(workDir, "code.txt"), indexed);
  git(workDir, "add", "code.txt");
  await writeFile(
    join(workDir, "code.txt"),
    indexed.replace("ordinary line 80\n", "working line 80\n"),
  );
  await writeFile(join(workDir, "untracked.txt"), "untracked content\n");
  await createShell(page, "Git code", workDir);
  await showCode(page);
  await expect(
    page.getByRole("button", { name: ".env", exact: true }),
  ).toHaveCount(0);
  await page.getByLabel("显示忽略项").check();
  await expect(page.getByRole("button", { name: /.env · 忽略/ })).toBeVisible();
  await page.getByRole("tab", { name: "修改", exact: true }).click();
  const captured = page.waitForResponse((response) =>
    new URL(response.url()).pathname.endsWith("/code/diff"),
  );
  const contextRequests: string[] = [];
  page.on("request", (request) => {
    if (/\/code\/snapshots\/[^/]+\/context/.test(request.url()))
      contextRequests.push(request.url());
  });
  await page
    .getByRole("button", { name: "修改 code.txt", exact: true })
    .first()
    .click();
  await expect.poll(() => visibleCode(page)).toContain("working line 80");
  const pair = await (await captured).json();
  expect(pair).not.toHaveProperty("blocks");
  expect(pair.old.text).toBe(indexed);
  expect(pair.new.text).toBe(
    indexed.replace("ordinary line 80\n", "working line 80\n"),
  );
  await writeFile(join(workDir, "code.txt"), "external replacement\n");
  for (let i = 0; i < 3; i++) {
    await page
      .getByRole("button", { name: "向下展开 30 行", exact: true })
      .first()
      .click();
  }
  await page.locator(".code-diff-reader:visible").evaluate((element) => {
    element.scrollTop = 0;
  });
  await expect.poll(() => visibleCode(page)).toContain("ordinary line 1");
  await expect
    .poll(() => visibleCode(page))
    .not.toContain("external replacement");
  expect(contextRequests).toEqual([]);
  // Keep a real asynchronous boundary: the old Diff stays mounted while
  // the captured full document loads, so actions must target the new tab.
  await page.route(
    /\/code\/snapshots\/[^/]+\/file\?side=old$/,
    async (route) => {
      const response = await route.fetch();
      await new Promise((resolve) => setTimeout(resolve, 200));
      await route.fulfill({ response });
    },
  );
  await page.getByRole("button", { name: "完整文件", exact: true }).click();
  await page.getByRole("menuitem", { name: /修改前的文件/ }).click();
  await expect(
    page.getByRole("tab", { name: /^code\.txt · index(?: ●)?$/ }),
  ).toHaveAttribute("aria-selected", "true");
  await page
    .getByRole("tabpanel")
    .filter({ visible: true })
    .getByRole("button", { name: "跳转行" })
    .filter({ visible: true })
    .click();
  await page.locator(".code-goto-line:visible input").fill("50");
  await page.locator(".code-goto-line:visible input").press("Enter");
  await expect.poll(() => visibleCode(page)).toContain("indexed line 50");
  await expect.poll(() => visibleCode(page)).not.toContain("working line 80");
  await nav(page);
  await page
    .getByRole("button", { name: "修改 code.txt", exact: true })
    .last()
    .click();
  await expect.poll(() => visibleCode(page)).toContain("indexed line 50");
  await page.getByRole("button", { name: "完整文件", exact: true }).click();
  await page.getByRole("menuitem", { name: /修改前的文件/ }).click();
  await expect(
    page.getByRole("tab", { name: /^code\.txt · head(?: ●)?$/ }),
  ).toHaveAttribute("aria-selected", "true");
  await expect.poll(() => visibleCode(page)).toContain("ordinary line 1");
  await expect.poll(() => visibleCode(page)).not.toContain("indexed line 50");
});

test("main and linked worktrees bind their own roots and reject cross-session snapshots", async ({
  page,
  request,
  workDir,
}) => {
  const main = join(workDir, "main"),
    linked = join(workDir, "linked");
  await mkdir(main);
  git(main, "init", "-q");
  await mkdir(join(main, "src"));
  await writeFile(join(main, "src/clean.txt"), "main source\n");
  git(main, "add", ".");
  git(main, "commit", "-qm", "main");
  git(main, "worktree", "add", "-qb", "linked", linked);
  await writeFile(join(linked, "src/clean.txt"), "linked source\n");
  const first = await createShell(page, "Main root", join(main, "src"));
  await showCode(page);
  await page.getByLabel("搜索文件路径").fill("clean");
  await page.getByRole("button", { name: "搜索路径", exact: true }).click();
  await page
    .getByRole("button", { name: "src/clean.txt", exact: true })
    .click();
  await expect.poll(() => visibleCode(page)).toContain("main source");
  const context = await (
    await request.get(`/api/sessions/${first}/code/context`)
  ).json();
  expect(context.rootPath).toBe(main);
  const file = await (
    await request.get(
      `/api/sessions/${first}/code/file?${new URLSearchParams({ contextId: context.contextId, nodeId: Buffer.from("src/clean.txt").toString("base64url") })}`,
    )
  ).json();
  const second = await createShell(page, "Linked root", join(linked, "src"));
  await showCode(page);
  await page.getByLabel("搜索文件路径").fill("clean");
  await page.getByRole("button", { name: "搜索路径", exact: true }).click();
  await page
    .getByRole("button", { name: "src/clean.txt", exact: false })
    .click();
  await expect.poll(() => visibleCode(page)).toContain("linked source");
  expect(
    (
      await request.get(
        `/api/sessions/${second}/code/snapshots/${file.snapshotId}/file?side=new`,
      )
    ).status(),
  ).toBe(400);
  await expect(page.locator(".xterm")).toHaveCount(1);
});

test("large change lists remain browsable and late files can compare, update and refresh", async ({
  page,
  request,
  workDir,
}) => {
  git(workDir, "init", "-q");
  await mkdir(join(workDir, "bulk"));
  const total = 1205;
  for (let offset = 0; offset < total; offset += 64) {
    await Promise.all(
      Array.from({ length: Math.min(64, total - offset) }, (_, n) => {
        const index = offset + n;
        return writeFile(
          join(workDir, "bulk", `file-${String(index).padStart(4, "0")}.txt`),
          `content ${index}\n`,
        );
      }),
    );
  }
  const id = await createShell(page, "Large changes", workDir);
  await showCode(page);
  await page.getByLabel("搜索文件路径").fill("file-1204");
  await page.getByRole("button", { name: "搜索路径", exact: true }).click();
  await page
    .getByRole("button", { name: /^bulk\/file-1204\.txt(?: \?\?)?$/ })
    .click();
  await expect.poll(() => visibleCode(page)).toContain("content 1204");
  await page.getByRole("button", { name: "查看 Diff", exact: true }).click();
  await expect(
    page.locator(".code-diff-reader:visible [data-gutter]").first(),
  ).toBeVisible();
  await expect.poll(() => visibleCode(page)).toContain("content 1204");
  await nav(page);
  await page.getByRole("tab", { name: "修改", exact: true }).click();
  await expect(
    page.getByText("已加载 200 / 1205 项", { exact: true }),
  ).toBeVisible();
  expect(
    await page
      .getByRole("list", { name: "修改列表" })
      .locator("button")
      .count(),
  ).toBeLessThan(50);
  await page.getByLabel("筛选修改路径").fill("file-1204");
  await page.getByRole("button", { name: "筛选修改", exact: true }).click();
  await expect(
    page.getByText("已加载 1 / 1 项", { exact: true }),
  ).toBeVisible();
  const bulkRow = page.getByRole("button", {
    name: "未跟踪 bulk/file-1204.txt",
    exact: true,
  });
  await expect(bulkRow).toHaveText("file-1204.txt");
  await bulkRow.click();
  await expect.poll(() => visibleCode(page)).toContain("content 1204");
  await expect(
    page.getByRole("tab", {
      name: /^bulk\/file-1204\.txt · untracked(?: ●)?$/,
    }),
  ).toHaveAttribute("aria-selected", "true");
  await expect.poll(() => visibleCode(page)).toContain("content 1204");
  await writeFile(join(workDir, "bulk/file-1204.txt"), "late updated\n");
  await expect(
    page.getByRole("status").filter({ hasText: "内容已更新", visible: true }),
  ).toBeVisible({ timeout: 12000 });
  await page
    .getByRole("button", { name: "刷新文档", exact: true })
    .filter({ visible: true })
    .click();
  await expect.poll(() => visibleCode(page)).toContain("late updated");
  await nav(page);
  await page.getByLabel("筛选修改路径").fill("");
  await page.getByRole("button", { name: "筛选修改", exact: true }).click();
  for (const loaded of [200, 400, 600, 800, 1000, 1200, 1205]) {
    await expect(
      page.getByText(`已加载 ${loaded} / 1205 项`, { exact: true }),
    ).toBeVisible();
    if (loaded < total)
      await page
        .getByRole("button", { name: "加载更多修改", exact: true })
        .click();
  }
  const list = page.getByRole("list", { name: "修改列表" });
  await list.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
    element.dispatchEvent(new Event("scroll"));
  });
  await page
    .getByRole("button", { name: "未跟踪 bulk/file-1204.txt", exact: true })
    .click();
  expect(await list.locator("button").count()).toBeLessThan(50);
  const context = await (
    await request.get(`/api/sessions/${id}/code/context`)
  ).json();
  let cursor = "";
  const paths = new Set<string>();
  do {
    const response = await request.get(
      `/api/sessions/${id}/code/changes?${new URLSearchParams({ contextId: context.contextId, cursor })}`,
    );
    expect(response.ok()).toBe(true);
    const page = await response.json();
    expect(page.total).toBe(total);
    expect(page.entries.length).toBeLessThanOrEqual(200);
    for (const entry of page.entries) {
      expect(paths.has(entry.newPath)).toBe(false);
      paths.add(entry.newPath);
    }
    cursor = page.nextCursor;
  } while (cursor);
  expect(paths.size).toBe(total);
});

async function canvasImage(
  page: Page,
  type: string,
  width: number,
  height: number,
  color: string,
) {
  const bytes = await page.evaluate(
    async ({ type, width, height, color }) => {
      const canvas = new OffscreenCanvas(width, height);
      const context = canvas.getContext("2d")!;
      context.fillStyle = color;
      context.fillRect(0, 0, width, height);
      const blob = await canvas.convertToBlob({ type });
      return [...new Uint8Array(await blob.arrayBuffer())];
    },
    { type, width, height, color },
  );
  return Buffer.from(bytes);
}
const sha256 = (bytes: Buffer) =>
  createHash("sha256").update(bytes).digest("hex");

test("images preview, download byte-identical, isolate SVG and compare Git sides", async ({
  page,
  workDir,
}) => {
  const committed = await canvasImage(page, "image/png", 64, 48, "#d33");
  const working = await canvasImage(page, "image/png", 32, 32, "#33d");
  const jpeg = await canvasImage(page, "image/jpeg", 40, 30, "#3a3");
  const webp = await canvasImage(page, "image/webp", 24, 12, "#aa3");
  const gif = Buffer.from(
    "R0lGODlhAQABAIAAAP///wAAACH5BAEAAAAALAAAAAABAAEAAAICRAEAOw==",
    "base64",
  );
  const svg =
    '<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20" onload="fetch(\'http://svg-leak.invalid/onload\')">\n' +
    "<script>fetch('http://svg-leak.invalid/script')</script>\n" +
    '<image href="http://svg-leak.invalid/image.png" width="10" height="10"/>\n' +
    '<rect width="40" height="20" fill="teal"/>\n' +
    "</svg>\n";
  git(workDir, "init", "-q");
  await writeFile(join(workDir, "pic.png"), committed);
  git(workDir, "add", ".");
  git(workDir, "commit", "-qm", "image");
  await writeFile(join(workDir, "pic.png"), working);
  await writeFile(join(workDir, "photo.jpg"), jpeg);
  await writeFile(join(workDir, "tiny.webp"), webp);
  await writeFile(join(workDir, "dot.gif"), gif);
  await writeFile(join(workDir, "evil.svg"), svg);
  await writeFile(join(workDir, "archive.zip"), Buffer.from("PK\x03\x04\0\0"));
  const leaks: string[] = [];
  page.on("request", (request) => {
    if (request.url().includes("svg-leak.invalid")) leaks.push(request.url());
  });
  await createShell(page, "Image code", workDir);
  await showCode(page);
  const info = page.getByLabel("图片信息").filter({ visible: true });
  // File buttons append the Git status ("pic.png M", "photo.jpg ??").
  const file = (name: string) =>
    page.getByRole("button", {
      name: new RegExp(`^${name.replace(".", "\\.")}(?: |$)`),
    });
  const open = async (name: string) => {
    await nav(page);
    await file(name).click();
  };

  await file("pic.png").click();
  await expect(info).toContainText("PNG · 32 × 32");
  await expect(
    page.getByRole("group", { name: "图片预览" }).filter({ visible: true }),
  ).toBeVisible();
  const downloading = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "下载", exact: true })
    .filter({ visible: true })
    .click();
  const download = await downloading;
  expect(download.suggestedFilename()).toBe("pic.png");
  expect(sha256(await readFile((await download.path())!))).toBe(
    sha256(working),
  );

  for (const [name, text] of [
    ["photo.jpg", "JPEG · 40 × 30"],
    ["tiny.webp", "WebP · 24 × 12"],
    ["dot.gif", "GIF · 1 × 1"],
  ]) {
    await open(name);
    await expect(info).toContainText(text);
  }
  await open("archive.zip");
  await expect(
    page.getByText("二进制内容").filter({ visible: true }),
  ).toBeVisible();

  await open("evil.svg");
  await expect(info).toContainText("SVG · 40 × 20");
  await page
    .getByRole("group", { name: "SVG 显示方式" })
    .getByRole("button", { name: "源码", exact: true })
    .click();
  await expect.poll(() => visibleCode(page)).toContain("<script>");
  expect(leaks).toEqual([]);

  await nav(page);
  await page.getByRole("tab", { name: "修改", exact: true }).click();
  await page
    .getByRole("button", { name: "修改 pic.png", exact: true })
    .first()
    .click();
  await expect(
    page.getByText("图片已修改").filter({ visible: true }),
  ).toBeVisible();
  await expect(
    page.getByLabel("大小变化").filter({ visible: true }),
  ).toHaveText(`${committed.length} B → ${working.length} B`);
  await expect(info).toContainText("PNG · 32 × 32");
  const source = page.getByLabel("预览来源").filter({ visible: true });
  await source.selectOption("old");
  await expect(info).toContainText("PNG · 64 × 48");
  const old = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "下载", exact: true })
    .filter({ visible: true })
    .click();
  expect(sha256(await readFile((await (await old).path())!))).toBe(
    sha256(committed),
  );
  await source.selectOption("new");
  await expect(info).toContainText("PNG · 32 × 32");
  expect(git(workDir, "status", "--porcelain")).toContain(" M pic.png");
  expect(leaks).toEqual([]);
});
