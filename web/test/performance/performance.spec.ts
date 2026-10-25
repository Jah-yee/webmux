import { setTimeout as delay } from "node:timers/promises";
import { execFileSync } from "node:child_process";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import {
  test,
  expect,
  createShell,
  runCommand,
  expectTerminalLine,
  expectConnected,
} from "../e2e/fixtures";
import { observePerformance, performanceReport, settled } from "./performance";

test.use({
  launchOptions: {
    args: ["--disable-webgl"],
    // Headless Chrome otherwise hides scrollbars and masks gutter shifts.
    ignoreDefaultArgs: ["--hide-scrollbars"],
  },
});

test.beforeEach(async ({ page }) => {
  await observePerformance(page);
});

for (const theme of ["light", "dark"] as const) {
  test(`4x CPU: cold ${theme} workspace stays stable with delayed startup and fonts`, async ({
    page,
  }, testInfo) => {
    if (theme === "light") {
      // Styled scrollbars force the classic, space-consuming desktop gutter
      // even on macOS, where overlay scrollbars otherwise mask this shift.
      await page.addInitScript(() => {
        const style = document.createElement("style");
        style.textContent = "html::-webkit-scrollbar{width:16px}";
        const observer = new MutationObserver(() => {
          if (document.head) {
            document.head.append(style);
            observer.disconnect();
          }
        });
        observer.observe(document, { childList: true, subtree: true });
      });
    }
    await page.addInitScript((theme) => {
      localStorage.setItem("webmux.theme", theme);
    }, theme);
    await page.route(
      /\/api\/(health|config|repositories|presets)$/,
      async (route) => {
        await delay(750);
        await route.continue();
      },
    );
    await page.route("**/fonts/*.woff2", async (route) => {
      await delay(650);
      await route.continue();
    });
    await page.goto("/");
    await expect(page.getByText("还没有会话", { exact: true })).toBeVisible();
    await expect(
      page.locator("header").getByRole("button", { name: "＋ 新建会话" }),
    ).toBeEnabled();
    await settled(page);
    await page.screenshot({
      path: testInfo.outputPath(`${theme}-workspace.png`),
      fullPage: true,
    });
    const scrollbarGutter = await page.evaluate(
      () => innerWidth - document.documentElement.clientWidth,
    );
    if (theme === "light" && !testInfo.project.use.isMobile)
      expect(
        scrollbarGutter,
        "The classic scrollbar scenario must reserve actual space",
      ).toBe(16);
    await performanceReport(page, testInfo, `${theme}-cold-start`, {
      scrollbarGutter,
    });
  });
}

test("4x CPU: background output and status updates do not move session cards", async ({
  page,
  request,
  workDir,
}, testInfo) => {
  // A blank initial preview becomes multiline output without user input. A
  // second card makes height changes and activity-based reordering observable.
  for (const name of ["background-output", "stable-neighbour"]) {
    const response = await request.post("/api/sessions", {
      data: {
        preset: "custom",
        name,
        cwd: workDir,
        cmd: [
          "/bin/bash",
          "-c",
          name === "background-output"
            ? "sleep 4; printf 'one\\ntwo\\nthree\\nfour\\n'; sleep 2"
            : "sleep 20",
        ],
      },
    });
    expect(response.status()).toBe(201);
  }
  await page.goto("/");
  await expect(
    page.getByRole("link", { name: "stable-neighbour", exact: true }),
  ).toBeVisible();
  await settled(page);
  await page.screenshot({
    path: testInfo.outputPath("before-output.png"),
  });
  const output = page.locator("[data-session-id]").filter({
    has: page.getByRole("link", { name: "background-output", exact: true }),
  });
  await expect(output.getByLabel("终端输出预览")).toContainText("three");
  await expect(output).toHaveAttribute("data-status", "exited");
  await page.screenshot({
    path: testInfo.outputPath("after-output-light.png"),
  });
  await page
    .getByRole("button", { name: "切换为黑色主题", exact: true })
    .click();
  await page.screenshot({
    path: testInfo.outputPath("after-output-dark.png"),
  });
  await performanceReport(page, testInfo, "background-updates");
});

test("4x CPU: create, type, replay, switch, filter and open dialogs remain usable", async ({
  page,
  workDir,
}, testInfo) => {
  await page.addInitScript(() => {
    // Keep native transport and events. Closing both ends of a Playwright
    // WebSocket route races its in-flight forwarding on Linux Chromium.
    const NativeWebSocket = window.WebSocket;
    let disconnect = () => {};
    window.WebSocket = class extends NativeWebSocket {
      constructor(url: string | URL, protocols?: string | string[]) {
        super(url, protocols);
        if (new URL(url, location.href).pathname.startsWith("/ws/s/"))
          disconnect = () => this.close();
      }
    };
    window.addEventListener("webmux:test-disconnect", () => disconnect());
  });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.stack ?? error.message));
  await createShell(page, "CPU 4x shell", workDir);
  await runCommand(page, "printf '\\n%s%s\\n' 'cpu-four-' 'ready'");
  await expectTerminalLine(page, "cpu-four-ready");
  await page.screenshot({ path: testInfo.outputPath("terminal-4x.png") });
  await performanceReport(page, testInfo, "create-and-type");

  await page.reload();
  await expectConnected(page);
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await expectTerminalLine(page, "cpu-four-ready");
  await settled(page);
  await page.evaluate(() =>
    window.dispatchEvent(new Event("webmux:test-disconnect")),
  );
  await expect(page.locator(".terminal-header").getByRole("status")).toHaveText(
    "重连中",
  );
  await expectConnected(page);
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await runCommand(page, "printf '\\n%s%s\\n' 'reconnected-' 'ready'");
  await expectTerminalLine(page, "reconnected-ready");

  await runCommand(
    page,
    "for ((i=0;i<1000;i++)); do printf 'load-%s\\n' \"$i\"; done; printf '%s%s\\n' 'burst-' 'complete'",
  );
  await expectTerminalLine(page, "burst-complete");
  const input = page.locator(".xterm-helper-textarea");
  await input.focus();
  await page.keyboard.type("printf '\\n%s%s\\n' 'responsive-' 'input'");
  const start = performance.now();
  await page.keyboard.press("Enter");
  await expectTerminalLine(page, "responsive-input");
  const inputRoundTripMs = performance.now() - start;
  await testInfo.attach("terminal-response.json", {
    body: JSON.stringify({ inputRoundTripMs }),
    contentType: "application/json",
  });
  expect(inputRoundTripMs).toBeLessThan(1500);
  await page.getByRole("button", { name: "草稿模式", exact: true }).click();
  await expect(page.locator(".terminal-composer textarea")).toBeVisible();
  await page.getByRole("button", { name: "草稿模式", exact: true }).click();
  await page.getByRole("button", { name: "终端菜单", exact: true }).click();
  await page.getByRole("menuitem", { name: "字体大小", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await performanceReport(page, testInfo, "replay-reconnect-and-burst", {
    inputRoundTripMs,
  });

  await page.getByRole("button", { name: "返回会话列表", exact: true }).click();
  const search = page.getByRole("searchbox", { name: "搜索会话" });
  await search.fill("no matching session");
  await expect(page.getByText("没有匹配的会话", { exact: true })).toBeVisible();
  await search.press("Escape");
  const session = page.getByRole("link", { name: "CPU 4x shell", exact: true });
  await expect(session).toBeVisible();
  await session.click();
  await expectTerminalLine(page, "responsive-input");
  await performanceReport(page, testInfo, "switch-and-filter");
  expect(errors).toEqual([]);
});

test("4x CPU: delayed repository discovery and Git refresh stay stable", async ({
  page,
  request,
  workDir,
}, testInfo) => {
  const git = (...args: string[]) =>
    execFileSync("git", ["-C", workDir, ...args]);
  git("init", "-b", "main");
  await writeFile(join(workDir, "tracked.txt"), "original\n");
  git("add", ".");
  git(
    "-c",
    "user.name=WebMux QA",
    "-c",
    "user.email=qa@example.invalid",
    "commit",
    "-m",
    "initial",
  );
  const registered = await request.post("/api/repositories", {
    data: { path: workDir },
  });
  expect(registered.status()).toBe(201);
  const [repository] = await (await request.get("/api/repositories")).json();
  const created = await request.post("/api/sessions", {
    data: {
      preset: "shell",
      name: "Repository CPU 4x",
      workspace: {
        repositoryId: repository.id,
        worktreeId: repository.worktrees[0].id,
      },
    },
  });
  expect(created.status()).toBe(201);
  await page.route("**/api/repositories", async (route) => {
    await delay(750);
    await route.continue();
  });
  await page.goto("/");
  await expect(
    page.getByRole("link", { name: "Repository CPU 4x", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("已跟踪文件干净", { exact: true })).toBeVisible();
  await settled(page);
  await writeFile(join(workDir, "tracked.txt"), "changed\n");
  await page
    .getByRole("button", { name: "刷新 Git 状态", exact: true })
    .click();
  await expect(page.getByText("有修改", { exact: true })).toBeVisible();
  await performanceReport(page, testInfo, "repository-and-refresh");
});

test("4x CPU: a stalled repository request cannot block existing terminals", async ({
  page,
  request,
  workDir,
}, testInfo) => {
  const created = await request.post("/api/sessions", {
    data: { preset: "shell", name: "Always reachable", cwd: workDir },
  });
  expect(created.status()).toBe(201);
  const held = Promise.withResolvers<void>();
  let pendingRequests = 0;
  await page.route("**/api/repositories", async (route) => {
    if (++pendingRequests === 1) {
      // A lifecycle refresh must reuse the initial bounded request, rather
      // than superseding it with another discovery that outlives its timeout.
      await page.evaluate(() => window.dispatchEvent(new Event("pageshow")));
    }
    await held.promise;
    await route.continue().catch(() => {});
  });
  try {
    await page.goto("/");
    const session = page.getByRole("link", {
      name: "Always reachable",
      exact: true,
    });
    await expect(session).toBeVisible({ timeout: 5000 });
    await expect(page.getByText(/初次读取超时，请重试刷新/)).toBeVisible();
    expect(pendingRequests).toBe(1);
    await expect(
      page.getByRole("button", { name: "刷新 Git 状态", exact: true }),
    ).toBeEnabled();
    await performanceReport(page, testInfo, "repository-timeout");
    // The repository response remains held while the real PTY is opened and
    // typed into. A late response must not be needed for terminal access.
    await session.click();
    await expect(page.locator(".restore-overlay")).toBeHidden();
    await runCommand(page, "printf '\\n%s%s\\n' 'still-' 'reachable'");
    await expectTerminalLine(page, "still-reachable");
  } finally {
    held.resolve();
  }
});

test("4x CPU: settings and a large code file stay usable and stable", async ({
  page,
  workDir,
}, testInfo) => {
  await page.goto("/settings");
  await expect(
    page.getByRole("heading", { name: "设置", exact: true }),
  ).toBeVisible();
  await performanceReport(page, testInfo, "settings-start");
  await page.getByRole("button", { name: "添加快捷指令", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await writeFile(
    join(workDir, "large.ts"),
    Array.from(
      { length: 3000 },
      (_, i) => `export const value${i} = ${i};`,
    ).join("\n"),
  );
  await createShell(page, "Code CPU 4x", workDir);
  await page
    .locator(".terminal-header")
    .getByRole("button", { name: "代码", exact: true })
    .click();
  await expect(page.getByRole("region", { name: "代码工作区" })).toBeVisible();
  await page.getByRole("button", { name: "large.ts", exact: true }).click();
  await expect(
    page.locator("[data-code-reader]:visible .cm-content"),
  ).toContainText("export const value0");
  const editor = page.locator("[data-code-reader]:visible .cm-scroller");
  await editor.evaluate(
    (element) => (element.scrollTop = element.scrollHeight),
  );
  await expect(
    page.locator("[data-code-reader]:visible .cm-content"),
  ).toContainText("export const value2999");
  await performanceReport(page, testInfo, "large-code-file");
});
