import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdir, writeFile, access, rename } from "node:fs/promises";
import { join } from "node:path";
import type { Page, Locator } from "@playwright/test";
import {
  test,
  expect,
  runCommand,
  expectTerminalLine,
  expectConnected,
} from "./fixtures";
import { startE2EServer } from "./server.mjs";

const exec = promisify(execFile);
async function git(path: string, ...args: string[]) {
  return (await exec("git", ["-C", path, ...args])).stdout.trim();
}
async function repository(root: string, name = "project with spaces") {
  const path = join(root, name);
  await mkdir(path);
  await git(path, "init", "-b", "main");
  await writeFile(join(path, ".gitignore"), ".env\n");
  await writeFile(join(path, "tracked.txt"), "original\n");
  await git(path, "add", ".");
  await git(
    path,
    "-c",
    "user.name=WebMux QA",
    "-c",
    "user.email=qa@example.invalid",
    "commit",
    "-m",
    "initial",
  );
  return path;
}
async function register(page: Page, path: string) {
  await page.getByRole("button", { name: "添加仓库", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("仓库目录").fill(path);
  await dialog.getByRole("button", { name: "添加仓库", exact: true }).click();
  await expect(dialog).toBeHidden();
}
async function expand(group: Locator) {
  const toggle = group.locator("button[aria-expanded]").first();
  if ((await toggle.getAttribute("aria-expanded")) === "false")
    await toggle.click();
}
async function createTree(page: Page, branch: string, name: string) {
  await page
    .getByRole("button", { name: "新建 worktree", exact: true })
    .click();
  const branchInput = page.getByLabel("新分支名称", { exact: true });
  // The default name arrives asynchronously. Wait before fill selects the
  // input, so a late default cannot be inserted ahead of the intended name.
  await expect(branchInput).not.toHaveValue("");
  await branchInput.fill(branch);
  await expect(branchInput).toHaveValue(branch);
  await expect(page.getByLabel("起始基线", { exact: true })).not.toHaveValue(
    "",
  );
  const path = await page
    .getByLabel("新工作树完整路径", { exact: true })
    .inputValue();
  await page.getByRole("button", { name: "Shell", exact: true }).click();
  await page.getByLabel("会话名称", { exact: false }).fill(name);
  await expect(branchInput).toHaveValue(branch);
  await page.getByRole("button", { name: "创建并进入 →", exact: true }).click();
  await expect(page).toHaveURL(/\/s\/[a-z0-9]+$/);
  await expectConnected(page);
  return { path, id: new URL(page.url()).pathname.split("/").at(-1)! };
}

test("repository → two worktrees → shared PTYs → guarded force removal retains branch and replay", async ({
  page,
  request,
  workDir,
}) => {
  const root = await repository(workDir);
  await page.goto("/");
  await register(page, root);
  const repos = await (await request.get("/api/repositories")).json();
  const repo = page.locator(`[data-repository-id="${repos[0].id}"]`);
  await expect(repo).toBeVisible();
  await register(page, root);
  expect(await (await request.get("/api/repositories")).json()).toHaveLength(1);
  const first = await createTree(page, "feature/alpha", "Alpha agent");
  await runCommand(page, "printf '\\n%s%s\\n' 'worktree-' 'replay-proof'");
  await expectTerminalLine(page, "worktree-replay-proof");
  await page.reload();
  await expectTerminalLine(page, "worktree-replay-proof");
  await page.goto("/");
  const second = await createTree(page, "feature/beta", "Beta agent");
  await page.goto("/");
  await expand(repo);
  const trees = (
    await (
      await request.get(`/api/repositories/${repos[0].id}/worktrees`)
    ).json()
  ).worktrees;
  const alpha = trees.find(
    (tree: { path: string }) => tree.path === first.path,
  );
  const alphaGroup = page.locator(`[data-worktree-id="${alpha.id}"]`);
  await alphaGroup
    .getByRole("button", { name: "启动会话", exact: true })
    .click();
  await page.getByRole("button", { name: "Shell", exact: true }).click();
  await page.getByLabel("会话名称", { exact: false }).fill("Alpha helper");
  await page.getByRole("button", { name: "创建并进入 →", exact: true }).click();
  await expect(page).toHaveURL(/\/s\/[a-z0-9]+$/);
  const helperId = new URL(page.url()).pathname.split("/").at(-1)!;
  await runCommand(page, "printf '\\n%s%s\\n' 'independent-' 'helper'");
  await expectTerminalLine(page, "independent-helper");
  expect(
    await (await request.get(`/api/sessions/${first.id}/buffer`)).text(),
  ).not.toContain("independent-helper");
  await writeFile(join(first.path, "tracked.txt"), "changed\n");
  await writeFile(join(first.path, "untracked.txt"), "untracked\n");
  await writeFile(join(first.path, ".env"), "SECRET=local\n");
  await page.goto("/");
  await expand(repo);
  await expect(page.locator("[data-session-link]")).toHaveCount(3);
  const route = `/api/repositories/${repos[0].id}/worktrees/${alpha.id}`;
  const preview = await (
    await request.post(route + "/removal-preview", { data: {} })
  ).json();
  expect(
    (
      await request.post(route + "/remove", {
        data: { confirmationId: preview.confirmationId, mode: "safe" },
      })
    ).status(),
  ).toBe(409);
  await alphaGroup.getByRole("button", { name: /工作树菜单/ }).click();
  await page
    .getByRole("menuitem", { name: "删除 worktree", exact: true })
    .click();
  await expect(page.getByText("安全删除被阻止", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await access(first.path);
  expect(
    (await (await request.get(`/api/sessions/${first.id}`)).json()).status,
  ).not.toBe("exited");
  await alphaGroup.getByRole("button", { name: /工作树菜单/ }).click();
  await page
    .getByRole("menuitem", { name: "删除 worktree", exact: true })
    .click();
  await page
    .getByRole("button", { name: "进入强制删除确认", exact: true })
    .click();
  const force = page.getByRole("button", {
    name: "关闭会话并强制删除",
    exact: true,
  });
  await expect(force).toBeDisabled();
  await page.getByLabel("输入完整路径以确认删除").fill(first.path);
  await expect(force).toBeDisabled();
  await page.getByRole("checkbox").check();
  await force.click();
  await expect(page.getByRole("dialog")).toBeHidden();
  await expect(alphaGroup).toHaveCount(0);
  await expect(repo.getByText("已不可用工作区", { exact: true })).toBeVisible();
  for (const id of [first.id, helperId])
    expect(
      (await (await request.get(`/api/sessions/${id}`)).json()).status,
    ).toBe("exited");
  expect(
    (await (await request.get(`/api/sessions/${second.id}`)).json()).status,
  ).not.toBe("exited");
  expect(await git(root, "branch", "--list", "feature/alpha")).toContain(
    "feature/alpha",
  );
  await expect(access(first.path)).rejects.toThrow();
  expect(
    await (await request.get(`/api/sessions/${first.id}/buffer`)).text(),
  ).toContain("\r\nworktree-replay-proof\r\n");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("failed agent start retains new worktree and retry does not create again; unregister keeps terminal", async ({
  page,
  request,
  workDir,
}) => {
  const root = await repository(workDir);
  await page.goto("/");
  await register(page, root);
  await page
    .getByRole("button", { name: "新建 worktree", exact: true })
    .click();
  const branchInput = page.getByLabel("新分支名称", { exact: true });
  await expect(branchInput).not.toHaveValue("");
  await branchInput.fill("retry-agent");
  await expect(branchInput).toHaveValue("retry-agent");
  await page
    .getByRole("button", { name: "临时命令（不保存）", exact: true })
    .click();
  await page.getByRole("button", { name: "自定义", exact: true }).click();
  await page.getByLabel("自定义启动命令").fill("/webmux-missing-agent");
  await expect(branchInput).toHaveValue("retry-agent");
  await page.getByRole("button", { name: "创建并进入 →", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("webmux-missing-agent");
  await expect(
    page.getByText(
      "工作树已创建。再次提交只启动会话，不会重复创建分支或目录。",
    ),
  ).toBeVisible();
  await page.getByRole("button", { name: "Shell", exact: true }).click();
  await page.getByLabel("会话名称", { exact: false }).fill("Retained shell");
  await page.getByRole("button", { name: "创建并进入 →", exact: true }).click();
  await expect(page).toHaveURL(/\/s\/[a-z0-9]+$/);
  const sessionURL = page.url();
  await page.goto("/");
  const repos = await (await request.get("/api/repositories")).json();
  expect(repos[0].worktrees).toHaveLength(2);
  await page.getByRole("button", { name: /仓库菜单/ }).click();
  await page.getByRole("menuitem", { name: "从列表移除仓库" }).click();
  await page.getByRole("button", { name: "确认移除登记" }).click();
  await expect(
    page.getByRole("region", { name: "直接会话", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Retained shell", exact: true }).click();
  await expect(page).toHaveURL(sessionURL);
  await runCommand(page, "printf '\\n%s%s\\n' 'still-' 'running'");
  await expectTerminalLine(page, "still-running");
  expect(await git(root, "branch", "--list", "retry-agent")).toContain(
    "retry-agent",
  );
});

test("external changes refresh and restart retains registry but not sessions", async ({
  page,
  request,
  workDir,
  server,
}) => {
  const root = await repository(workDir);
  await page.goto("/");
  await register(page, root);
  const before = await (await request.get("/api/repositories")).json();
  const external = join(workDir, "external worktree");
  await git(root, "worktree", "add", "-b", "external", external);
  await page.getByRole("button", { name: "刷新 Git 状态" }).click();
  const repo = page.locator(`[data-repository-id="${before[0].id}"]`);
  await expand(repo);
  // Externally created worktrees stay off the home page until chosen.
  await expect(repo.getByText("主页 1 / 共 2 工作区")).toBeVisible();
  await expect(repo.getByText("external", { exact: true })).toHaveCount(0);
  await repo
    .getByRole("button", { name: "添加已有 worktree", exact: true })
    .click();
  const curate = page.getByRole("dialog");
  await curate.getByRole("option", { name: "external", exact: true }).click();
  await curate
    .getByRole("button", { name: "添加 1 个到主页", exact: true })
    .click();
  await expect(curate).toBeHidden();
  await expect(repo.getByText("external", { exact: true })).toBeVisible();
  const session = await (
    await request.post("/api/sessions", {
      data: { cwd: external, preset: "shell" },
    })
  ).json();
  expect(session.workspace.repositoryId).toBe(before[0].id);
  await server.restart();
  expect(await (await request.get("/api/sessions")).json()).toEqual([]);
  const after = await (await request.get("/api/repositories")).json();
  expect(after[0].id).toBe(before[0].id);
  expect(
    after[0].worktrees.map((tree: { path: string }) => tree.path),
  ).toContain(external);
  await page.reload();
  await expect(repo).toBeVisible();
  await expand(repo);
  // The home-page choice survives the restart; removing it keeps the worktree.
  const externalGroup = repo.locator(
    `[data-worktree-id="${after[0].worktrees.find((tree: { path: string }) => tree.path === external).id}"]`,
  );
  await expect(externalGroup).toBeVisible();
  await externalGroup.getByRole("button", { name: /工作树菜单/ }).click();
  await page.getByRole("menuitem", { name: "从主页移除", exact: true }).click();
  await expect(externalGroup).toHaveCount(0);
  await access(external);
});

test("without Git an isolated real service still starts and runs a direct shell", async ({
  page,
}) => {
  const isolated = await startE2EServer({ noGit: true });
  try {
    await page.goto(isolated.baseURL);
    await expect(
      page.getByRole("button", { name: "添加仓库", exact: true }),
    ).toBeDisabled();
    const config = await (await fetch(isolated.apiURL + "/api/config")).json();
    expect(config.workspace.gitAvailable).toBe(false);
    expect(config).not.toHaveProperty("token");
    await page
      .locator("header")
      .getByRole("button", { name: "＋ 新建会话", exact: true })
      .click();
    await page.getByRole("button", { name: "Shell", exact: true }).click();
    await page
      .getByRole("button", { name: "创建并进入 →", exact: true })
      .click();
    await expect(page).toHaveURL(/\/s\/[a-z0-9]+$/);
    await runCommand(page, "printf '\\n%s%s\\n' 'no-git-' 'shell-proof'");
    await expectTerminalLine(page, "no-git-shell-proof");
  } finally {
    await isolated.close();
  }
});

test("multiple repositories keep every session visible through local failure, collapse and long paths", async ({
  page,
  request,
  workDir,
}) => {
  const root = await repository(
    workDir,
    "long-project-" + "segment-".repeat(16),
  );
  const other = await repository(workDir, "second-project");
  const registered = [];
  for (const path of [root, other]) {
    const response = await request.post("/api/repositories", {
      data: { path },
    });
    expect(response.status()).toBe(201);
    registered.push(await response.json());
  }
  const entries = [];
  for (const [cwd, name] of [
    [root, "First shell"],
    [root, "Shared shell"],
    [other, "Second shell"],
    [workDir, "Direct shell"],
  ]) {
    const response = await request.post("/api/sessions", {
      data: { cwd, preset: "shell", name },
    });
    expect(response.status()).toBe(201);
    entries.push(await response.json());
  }
  await page.goto("/");
  await expect(page.locator("[data-session-link]")).toHaveCount(4);
  for (const item of entries)
    await expect(
      page.getByRole("link", { name: item.title, exact: true }),
    ).toHaveCount(1);
  const firstGroup = page.locator(`[data-repository-id="${registered[0].id}"]`);
  await expect(page.locator("[data-repository-id]")).toHaveCount(2);
  const order = await page
    .locator("[data-repository-id]")
    .evaluateAll((nodes) =>
      nodes.map((node) => node.getAttribute("data-repository-id")),
    );
  const toggle = firstGroup.locator("button[aria-expanded]").first();
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await page.getByRole("button", { name: "刷新 Git 状态" }).click();
  await expect(
    page.getByRole("button", { name: "刷新 Git 状态" }),
  ).toBeEnabled();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  expect(
    await page
      .locator("[data-repository-id]")
      .evaluateAll((nodes) =>
        nodes.map((node) => node.getAttribute("data-repository-id")),
      ),
  ).toEqual(order);
  await toggle.focus();
  await page.keyboard.press("Enter");
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await rename(other, other + "-moved");
  await page.getByRole("button", { name: "刷新 Git 状态" }).click();
  const failed = page.locator(`[data-repository-id="${registered[1].id}"]`);
  await expect(
    failed.getByText("已不可用工作区", { exact: true }),
  ).toBeVisible();
  await expect(page.locator("[data-session-link]")).toHaveCount(4);
  await failed.getByRole("button", { name: /仓库菜单/ }).click();
  await page.getByRole("menuitem", { name: "从列表移除仓库" }).click();
  await page.getByRole("button", { name: "确认移除登记" }).click();
  await expect(failed).toHaveCount(0);
  await expect(
    page
      .getByRole("region", { name: "直接会话", exact: true })
      .getByRole("link", { name: "Second shell", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "添加仓库", exact: true }).click();
  await page.getByLabel("仓库目录").fill(workDir);
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "添加仓库", exact: true })
    .click();
  await expect(page.getByRole("dialog").getByRole("alert")).toBeVisible();
  await expect(access(join(workDir, ".git"))).rejects.toThrow();
  await page.getByRole("button", { name: "取消", exact: true }).click();
});

test("existing branch creation, safe removal and stale destructive confirmation require fresh consent", async ({
  page,
  request,
  workDir,
}) => {
  const root = await repository(workDir);
  await git(root, "branch", "spare");
  await page.goto("/");
  await register(page, root);
  await page
    .getByRole("button", { name: "新建 worktree", exact: true })
    .click();
  await page.getByRole("button", { name: "使用已有分支", exact: true }).click();
  await page.getByLabel("已有本地分支", { exact: true }).fill("spare");
  await page.getByRole("option", { name: "spare", exact: true }).click();
  await expect(page.getByLabel("新工作树完整路径")).toHaveValue(/\/spare$/);
  const path = await page.getByLabel("新工作树完整路径").inputValue();
  await page.getByRole("button", { name: "仅创建工作树", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeHidden();
  const repo = (await (await request.get("/api/repositories")).json())[0];
  const tree = repo.worktrees.find(
    (item: { path: string }) => item.path === path,
  );
  const group = page.locator(`[data-worktree-id="${tree.id}"]`);
  await expand(page.locator(`[data-repository-id="${repo.id}"]`));
  await group.getByRole("button", { name: /工作树菜单/ }).click();
  await page
    .getByRole("menuitem", { name: "删除 worktree", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "确认安全删除", exact: true }),
  ).toBeVisible();
  const started = await (
    await request.post("/api/sessions", {
      data: { cwd: path, preset: "shell", name: "New arrival" },
    })
  ).json();
  await page.getByRole("button", { name: "确认安全删除", exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("alert")).toContainText(
    "重新",
  );
  expect(
    (await (await request.get(`/api/sessions/${started.id}`)).json()).status,
  ).not.toBe("exited");
  await access(path);
  await request.delete(`/api/sessions/${started.id}`);
  await expect
    .poll(
      async () =>
        (await (await request.get(`/api/sessions/${started.id}`)).json())
          .status,
    )
    .toBe("exited");
  await page.getByRole("button", { name: "重新读取预览", exact: true }).click();
  await page.getByRole("button", { name: "确认安全删除", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeHidden();
  await expect(access(path)).rejects.toThrow();
  expect(await git(root, "branch", "--list", "spare")).toContain("spare");
});

test("late Git response cannot replace a newer refresh", async ({
  page,
  request,
  workDir,
}) => {
  const root = await repository(workDir);
  await request.post("/api/repositories", { data: { path: root } });
  const old = await (await request.get("/api/repositories")).json();
  await page.goto("/");
  await expect(page.locator("[data-repository-id]")).toHaveCount(1);
  let release!: () => void;
  const delayed = new Promise<void>((resolve) => {
    release = resolve;
  });
  let captured!: () => void;
  const capture = new Promise<void>((resolve) => {
    captured = resolve;
  });
  let intercepted = false;
  await page.route("**/api/repositories", async (route) => {
    if (intercepted) {
      await route.continue();
      return;
    }
    intercepted = true;
    captured();
    await delayed;
    await route.fulfill({ status: 200, json: old });
  });
  await page.getByRole("button", { name: "刷新 Git 状态" }).click();
  await capture;
  const external = join(workDir, "newer-snapshot");
  await git(root, "worktree", "add", "-b", "newer-snapshot", external);
  await page.evaluate(() =>
    document.dispatchEvent(new Event("visibilitychange")),
  );
  const repo = page.locator("[data-repository-id]");
  await expand(repo);
  await expect(repo.getByText("主页 1 / 共 2 工作区")).toBeVisible();
  const lateResponse = page.waitForResponse((response) =>
    response.url().endsWith("/api/repositories"),
  );
  release();
  await (await lateResponse).finished();
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
  await expect(
    page.getByRole("button", { name: "刷新 Git 状态" }),
  ).toBeEnabled();
  await expect(repo.getByText("主页 1 / 共 2 工作区")).toBeVisible();
});

test("direct launch still offers explicit directory creation without registering a repository", async ({
  page,
  request,
  workDir,
}) => {
  const path = join(workDir, "new direct directory");
  await page.goto("/");
  await page
    .locator("header")
    .getByRole("button", { name: "＋ 新建会话", exact: true })
    .click();
  await page.getByLabel("工作目录", { exact: true }).fill(path);
  await page.getByRole("button", { name: "Shell", exact: true }).click();
  await page.getByRole("button", { name: "创建并进入 →", exact: true }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  await expect(access(path)).rejects.toThrow();
  await page.getByRole("button", { name: "创建并进入 →", exact: true }).click();
  await page
    .getByRole("button", { name: "创建目录并启动", exact: true })
    .click();
  await expect(page).toHaveURL(/\/s\/[a-z0-9]+$/);
  const sessions = await (await request.get("/api/sessions")).json();
  expect(sessions).toHaveLength(1);
  expect(sessions[0]).toMatchObject({ cwd: path, workspace: null });
  expect(await (await request.get("/api/repositories")).json()).toEqual([]);
});
