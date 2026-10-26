import type { Page } from "@playwright/test";
import { test, expect, expectTerminalLine, runCommand } from "./fixtures";

const card = (page: Page) => page.locator("#launch-configs");
const secret = "e2e-launch-secret-42";

async function createConfig(page: Page, name: string) {
  await page.goto("/settings#launch-configs");
  await card(page)
    .getByRole("button", { name: "新建启动配置", exact: true })
    .first()
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("名称", { exact: true }).fill(name);
  await dialog
    .getByLabel("启动命令", { exact: true })
    .fill("/bin/bash --noprofile --norc -i");
  await dialog.getByRole("combobox", { name: "图标", exact: true }).click();
  await page.getByRole("option", { name: "Codex", exact: true }).click();
  await dialog
    .getByRole("button", { name: "添加环境变量", exact: true })
    .click();
  await dialog.getByLabel("变量名 1", { exact: true }).fill("E2E_LAUNCH_VAR");
  const value = dialog.getByLabel("变量值 1", { exact: true });
  await value.fill(secret);
  await expect(value).toHaveAttribute("type", "password");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toBeHidden();
}

async function startFromConfig(page: Page, name: string, cwd: string) {
  await page.goto("/");
  await page
    .locator("header")
    .getByRole("button", { name: "＋ 新建会话", exact: true })
    .click();
  await page.getByRole("tab", { name: "预先输入", exact: true }).click();
  await page
    .getByRole("combobox", { name: "选择启动配置", exact: true })
    .click();
  await page.getByRole("option", { name, exact: true }).click();
  await page.getByLabel("工作目录", { exact: true }).fill(cwd);
  await page.getByLabel("会话名称", { exact: false }).fill("E2E config");
  const request = page.waitForRequest(
    (r) =>
      r.method() === "POST" && new URL(r.url()).pathname === "/api/sessions",
  );
  await page.getByRole("button", { name: "创建并进入 →", exact: true }).click();
  const body = (await request).postDataJSON();
  expect(Object.keys(body).sort()).toEqual(["configId", "cwd", "name"]);
  await expect(page).toHaveURL(/\/s\/[a-z0-9]+$/);
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await expect(page.locator(".xterm-helper-textarea")).toBeAttached();
  return new URL(page.url()).pathname.split("/").at(-1)!;
}

test("a saved launch config starts a session with its environment and survives reloads", async ({
  page,
  request,
  workDir,
}) => {
  await createConfig(page, "E2E env shell");
  const row = card(page).getByText("E2E env shell", { exact: true });
  await expect(row).toBeVisible();
  await expect(card(page)).toContainText("1 个环境变量");
  await expect(card(page)).not.toContainText(secret);

  const list = await request.get("/api/launch-configs");
  expect(await list.text()).not.toContain(secret);

  const id = await startFromConfig(page, "E2E env shell", workDir);
  await runCommand(page, 'printf "VAR=%s\\n" "$E2E_LAUNCH_VAR"');
  await expectTerminalLine(page, `VAR=${secret}`);
  const info = await (await request.get(`/api/sessions/${id}`)).json();
  expect(info.cmd).toEqual(["/bin/bash", "--noprofile", "--norc", "-i"]);

  // Deleting the config leaves the running session alone.
  await page.goto("/settings#launch-configs");
  await card(page)
    .getByRole("button", { name: "删除 E2E env shell", exact: true })
    .click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "删除", exact: true })
    .click();
  await expect(row).toBeHidden();
  await page.reload();
  await expect(
    card(page).getByText("E2E env shell", { exact: true }),
  ).toBeHidden();
  await page.goto(`/s/${id}`);
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await runCommand(page, "printf 'STILL=%s\\n' alive");
  await expectTerminalLine(page, "STILL=alive");
  expect(
    (await (await request.get(`/api/sessions/${id}`)).json()).status,
  ).not.toBe("exited");
});

test("configs persist across a settings reload and remain selectable alongside manual commands", async ({
  page,
}) => {
  await createConfig(page, "E2E persisted");
  await page.reload();
  await expect(
    card(page).getByText("E2E persisted", { exact: true }),
  ).toBeVisible();
  await expect(
    card(page)
      .getByRole("button", { name: "编辑 E2E persisted", exact: true })
      .locator('[data-launch-icon="codex"] svg'),
  ).toBeVisible();
  await page.goto("/");
  await page
    .locator("header")
    .getByRole("button", { name: "＋ 新建会话", exact: true })
    .click();
  await page.getByRole("tab", { name: "预先输入", exact: true }).click();
  const select = page.getByRole("combobox", {
    name: "选择启动配置",
    exact: true,
  });
  await expect(select).toContainText("E2E persisted");
  await expect(select.locator('[data-launch-icon="codex"] svg')).toBeVisible();
  await expect(select).toContainText("/bin/bash --noprofile --norc -i");
  await select.click();
  await expect(
    page.getByRole("option", { name: "E2E persisted", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("tab", { name: "手动填写", exact: true }).click();
  await expect(
    page.getByLabel("自定义启动命令", { exact: true }),
  ).toBeVisible();
});
