import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { test, expect } from "./fixtures";

test("workspace search shortcuts clear filters without capturing dialog input", async ({
  page,
}) => {
  await page.goto("/");
  const search = page.getByRole("searchbox", { name: "搜索会话" });
  await expect(search).toBeVisible();
  const filters = page.getByRole("group", { name: "按会话状态筛选" });
  await filters.getByRole("button", { name: /已退出/ }).click();
  await page.keyboard.press("/");
  await expect(search).toBeFocused();
  await page.keyboard.type("feature/path");
  await expect(search).toHaveValue("feature/path");
  await page.keyboard.press("Escape");
  await expect(search).toHaveValue("");
  await expect(filters.getByRole("button", { name: /全部/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await page
    .locator("header")
    .getByRole("button", { name: "＋ 新建会话" })
    .click();
  const name = page.getByLabel("会话名称", { exact: false });
  await name.fill("review");
  await page.keyboard.type("/branch");
  await expect(name).toHaveValue("review/branch");
  await expect(search).toHaveValue("");
});

test("an older config without hostname keeps creation available", async ({
  page,
}) => {
  await page.route("**/api/config", async (route) => {
    const response = await route.fetch();
    const config = await response.json();
    delete config.hostname;
    await route.fulfill({ response, json: config });
  });
  await page.goto("/");
  await expect(page.getByRole("button", { name: "添加仓库" })).toBeEnabled();
  await expect(
    page.locator("header").getByRole("button", { name: "＋ 新建会话" }),
  ).toBeEnabled();
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("invalid config can be retried without blocking loaded presets", async ({
  page,
}) => {
  let invalid = true;
  await page.route("**/api/config", async (route) => {
    if (invalid) await route.fulfill({ json: {} });
    else await route.continue();
  });
  await page.goto("/");
  await expect(
    page.getByText("部分启动配置加载失败，相关创建操作暂不可用。"),
  ).toBeVisible();
  await expect(page.getByText("invalid_type", { exact: false })).toHaveCount(0);
  await expect(
    page.locator("header").getByRole("button", { name: "＋ 新建会话" }),
  ).toBeEnabled();
  await expect(page.getByRole("button", { name: "添加仓库" })).toBeDisabled();
  invalid = false;
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(page.getByRole("button", { name: "添加仓库" })).toBeEnabled();
  await expect(
    page.getByText("部分启动配置加载失败，相关创建操作暂不可用。"),
  ).toHaveCount(0);
});

test("workspace controls stay reachable at 320px with doubled text", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await page.goto("/");
  await expect(page.getByRole("searchbox", { name: "搜索会话" })).toBeVisible();
  await page.evaluate(() => {
    document.documentElement.style.fontSize = "200%";
  });
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(321);
  for (const button of await page.locator("header button, main button").all()) {
    await button.scrollIntoViewIfNeeded();
    const box = await button.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(-1);
    expect(box!.x + box!.width).toBeLessThanOrEqual(321);
    await expect(button).toBeInViewport({ ratio: 1 });
    if (await button.isEnabled())
      await button.click({ trial: true, timeout: 2_000 });
  }
});

test("search and status filters compose and follow live session changes", async ({
  page,
  request,
  workDir,
}) => {
  const created = [];
  for (const name of ["Alpha build", "Beta review", "中文会话"]) {
    const response = await request.post("/api/sessions", {
      data: {
        cwd: workDir,
        preset: "custom",
        // Keep activity deterministic: an idle shell prompt can become
        // "waiting" while a slower browser is still exercising the filters.
        cmd: [
          "/bin/sh",
          "-c",
          "while true; do printf 'working\\n'; sleep 0.2; done",
        ],
        name,
      },
    });
    expect(response.status()).toBe(201);
    created.push(await response.json());
  }
  await page.goto("/");
  const rows = page.locator("[data-session-id]");
  await expect(rows).toHaveCount(3);
  const search = page.getByRole("searchbox", { name: "搜索会话" });
  const filters = page.getByRole("group", { name: "按会话状态筛选" });
  await search.fill("  ALPHA  ");
  await expect(rows).toHaveCount(1);
  await expect(page.getByRole("link", { name: "Alpha build" })).toBeVisible();
  await filters.getByRole("button", { name: /已退出/ }).click();
  await expect(page.getByText("没有匹配的会话", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "清除筛选" }).click();
  await expect(search).toHaveValue("");
  await expect(rows).toHaveCount(3);
  await expect(filters.getByRole("button", { name: /全部/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await search.fill("中文");
  await expect(rows).toHaveCount(1);
  await search.fill(workDir);
  await expect(rows).toHaveCount(3);
  await search.fill("");
  await filters.getByRole("button", { name: /已退出/ }).click();
  await expect(rows).toHaveCount(0);
  // The WebSocket must update the filtered list without a page reload.
  expect(
    (await request.delete(`/api/sessions/${created[1].id}`)).status(),
  ).toBe(204);
  await expect(rows).toHaveCount(1);
  await expect(page.getByRole("link", { name: "Beta review" })).toBeVisible();
  await page.getByRole("button", { name: "会话菜单 Beta review" }).click();
  await expect(page.getByRole("menuitem", { name: "移除会话" })).toBeVisible();
  await page.keyboard.press("Escape");
  await filters.getByRole("button", { name: /运行中/ }).click();
  await expect(rows).toHaveCount(2);
  await filters.getByRole("button", { name: /等待中/ }).click();
  await expect(rows).toHaveCount(0);
  // Keyboard navigation can reach and clear search without requiring a pointer.
  await search.focus();
  await page.keyboard.type("nothing");
  await page.keyboard.press("Tab");
  await expect(filters.getByRole("button", { name: /全部/ })).toBeFocused();
});

test("branch search reveals a collapsed repository and preserves its layout", async ({
  page,
  request,
  workDir,
}) => {
  const exec = promisify(execFile);
  await exec("git", ["-C", workDir, "init", "-b", "feature/discovery"]);
  await exec("git", [
    "-C",
    workDir,
    "-c",
    "user.name=WebMux QA",
    "-c",
    "user.email=qa@example.invalid",
    "commit",
    "--allow-empty",
    "-m",
    "initial",
  ]);
  const registered = await request.post("/api/repositories", {
    data: { path: workDir },
  });
  expect(registered.status()).toBe(201);
  const repositories = await (await request.get("/api/repositories")).json();
  const repo = repositories[0];
  const created = await request.post("/api/sessions", {
    data: {
      preset: "shell",
      name: "Repository agent",
      workspace: { repositoryId: repo.id, worktreeId: repo.worktrees[0].id },
    },
  });
  expect(created.status()).toBe(201);
  await page.goto("/");
  const region = page.locator(`[data-repository-id="${repo.id}"]`);
  const toggle = region.locator('[data-slot="collapsible-trigger"]');
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  const search = page.getByRole("searchbox", { name: "搜索会话" });
  await search.fill("FEATURE/DISCOVERY");
  await expect(
    page.getByRole("link", { name: "Repository agent" }),
  ).toBeVisible();
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await search.fill(repo.name);
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await search.fill("");
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
});
