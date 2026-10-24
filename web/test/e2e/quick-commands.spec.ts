import { readFile } from "node:fs/promises";
import type { Locator, Page } from "@playwright/test";
import {
  test,
  expect,
  createShell,
  expectTerminalLine,
  recordInput,
  expectConnected,
} from "./fixtures";

// Only one entry is rendered visibly at a time; role queries skip the hidden one.
const quick = (page: Page) =>
  page.getByRole("button", { name: "快捷指令", exact: true });
const menu = (page: Page) => page.getByRole("menu");
const item = (page: Page, name: string) =>
  menu(page).getByRole("menuitem", { name, exact: true });
const draft = (page: Page) =>
  page.getByRole("textbox", { name: "草稿内容", exact: true });
const card = (page: Page) => page.locator("#quick-commands");
const commandNames = (scope: Locator) =>
  scope
    .getByRole("menuitem")
    .evaluateAll((items) =>
      items.map((item) => item.getAttribute("aria-label")).filter(Boolean),
    );

async function addCommand(page: Page, command: string, enter: boolean) {
  await card(page)
    .getByRole("button", { name: "添加快捷指令", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "添加快捷指令" });
  const body = dialog.getByLabel("指令", { exact: true });
  await expect(body).toBeFocused();
  await body.fill(command);
  const toggle = dialog.getByRole("switch", { name: "发送后自动回车" });
  await expect(toggle).not.toBeChecked();
  if (enter) await toggle.click();
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toBeHidden();
}

async function backToTerminal(page: Page, id: string) {
  await page.getByRole("button", { name: "返回终端", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/s/${id}$`));
  await expectConnected(page);
  await expect(page.locator(".restore-overlay")).toBeHidden();
}

test("presets, settings round trip, direct sends and draft insertion", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(
    !testInfo.project.use.isMobile,
    "Exercises the soft-key entry on touch devices",
  );
  const id = await createShell(page, "Quick commands", workDir);
  const bar = page.getByRole("complementary", { name: "终端软键" });
  await expect(bar.getByRole("button", { name: "快捷指令" })).toBeVisible();

  await quick(page).click();
  await expect(menu(page)).toHaveAttribute("data-side", "top");
  await expect(
    menu(page).getByText("发送到终端", { exact: true }),
  ).toBeVisible();
  expect(await commandNames(menu(page))).toEqual([
    "/resume（并回车）",
    "/new（并回车）",
  ]);
  await page.keyboard.press("Escape");

  const other = await page.context().newPage();
  await other.goto(`/s/${id}`);
  await expect(quick(other)).toBeEnabled();

  await quick(page).click();
  await item(page, "管理快捷指令…").click();
  await expect(page).toHaveURL(
    new RegExp(`/settings\\?session=${id}#quick-commands$`),
  );
  await expect(
    page.getByRole("link", { name: "返回工作区", exact: true }),
  ).toHaveCount(0);
  await addCommand(page, "echo quick-ok\n\n", true);
  await addCommand(page, "echo pending", false);
  await expect(
    card(page).getByRole("button", { name: "编辑 echo pending" }),
  ).not.toContainText("回车");
  await expect(
    card(page).getByRole("button", { name: "编辑 echo quick-ok" }),
  ).toContainText("回车");

  // Another open tab picks up the saved list without a reload.
  await quick(other).click();
  await expect(item(other, "echo quick-ok（并回车）")).toBeVisible();
  await other.close();

  await backToTerminal(page, id);
  await quick(page).click();
  await item(page, "echo quick-ok（并回车）").click();
  await expectTerminalLine(page, "quick-ok");

  const received = await recordInput(page, workDir);
  const ctrl = bar.getByRole("button", { name: "Ctrl", exact: true });
  await ctrl.click();
  await expect(ctrl).toHaveAttribute("aria-pressed", "true");
  await quick(page).click();
  await item(page, "echo pending").click();
  await expect
    .poll(() => readFile(received, "utf8"))
    .toBe("\x1b[200~echo pending\x1b[201~");
  await expect(ctrl).toHaveAttribute("aria-pressed", "false");

  await page.getByRole("button", { name: "草稿模式", exact: true }).click();
  await draft(page).fill("请先 然后提交");
  await draft(page).evaluate((element: HTMLTextAreaElement) => {
    element.setSelectionRange(3, 3);
    element.dispatchEvent(new Event("select", { bubbles: true }));
  });
  await quick(page).click();
  await expect(menu(page).getByText("插入草稿", { exact: true })).toBeVisible();
  await item(page, "echo quick-ok").click();
  await expect(draft(page)).toHaveValue("请先 echo quick-ok然后提交");
  expect(await readFile(received, "utf8")).toBe(
    "\x1b[200~echo pending\x1b[201~",
  );

  await quick(page).click();
  await item(page, "管理快捷指令…").click();
  await backToTerminal(page, id);
  await expect(
    page.getByRole("button", { name: "草稿模式", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(draft(page)).toHaveValue("请先 echo quick-ok然后提交");
  expect(await readFile(received, "utf8")).toBe(
    "\x1b[200~echo pending\x1b[201~",
  );
});

test("commands are disabled while the terminal cannot send, but drafts still accept them", async ({
  page,
  request,
  workDir,
}) => {
  let blockReconnect = false;
  let blockedReconnects = 0;
  let disconnect: (() => Promise<void>) | undefined;
  await page.routeWebSocket("**/ws/s/*", async (socket) => {
    if (blockReconnect) {
      blockedReconnects++;
      // Returning alone opens a mocked connection instead of staying offline.
      await socket.close();
      return;
    }
    socket.connectToServer();
    disconnect = () => socket.close();
  });
  const id = await createShell(page, "Quick unavailable", workDir);
  blockReconnect = true;
  await disconnect!();
  await expect.poll(() => blockedReconnects).toBeGreaterThan(0);
  await expect(page.locator(".terminal-header").getByRole("status")).toHaveText(
    "重连中",
  );
  await quick(page).click();
  await expect(menu(page).getByText("终端未就绪，暂不能发送")).toBeVisible();
  await expect(item(page, "/resume（并回车）")).toBeDisabled();
  await expect(item(page, "管理快捷指令…")).toBeEnabled();
  await page.keyboard.press("Escape");

  blockReconnect = false;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expectConnected(page);
  await request.delete(`/api/sessions/${id}`);
  await expect(page.locator(".terminal-header").getByRole("status")).toHaveText(
    "已退出",
  );
  await quick(page).click();
  await expect(menu(page).getByText("会话已退出，无法发送")).toBeVisible();
  await expect(item(page, "/new（并回车）")).toBeDisabled();
  await page.keyboard.press("Escape");

  await page.getByRole("button", { name: "草稿模式", exact: true }).click();
  await quick(page).click();
  await item(page, "/new").click();
  await expect(draft(page)).toHaveValue("/new");
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("settings reorder, edit, confirm deletion and keep an emptied list empty", async ({
  page,
  workDir,
}) => {
  await page.goto("/settings");
  await expect(
    page.getByRole("link", { name: "返回工作区", exact: true }),
  ).toBeVisible();
  const list = card(page).getByRole("list", { name: "快捷指令列表" });
  const labels = () =>
    list
      .getByRole("button", { name: /^编辑 / })
      .evaluateAll((buttons) =>
        buttons.map((button) => button.getAttribute("aria-label")),
      );
  expect(await labels()).toEqual(["编辑 /resume", "编辑 /new"]);
  await expect(
    card(page).getByRole("button", { name: "上移 /resume" }),
  ).toBeDisabled();
  await expect(
    card(page).getByRole("button", { name: "下移 /new" }),
  ).toBeDisabled();
  await card(page).getByRole("button", { name: "上移 /new" }).click();
  expect(await labels()).toEqual(["编辑 /new", "编辑 /resume"]);

  const id = await createShell(page, "Reordered quick commands", workDir);
  await quick(page).click();
  expect(await commandNames(menu(page))).toEqual([
    "/new（并回车）",
    "/resume（并回车）",
  ]);
  await item(page, "管理快捷指令…").click();
  await expect(page).toHaveURL(
    new RegExp(`/settings\\?session=${id}#quick-commands$`),
  );

  await card(page).getByRole("button", { name: "编辑 /new" }).click();
  const dialog = page.getByRole("dialog", { name: "编辑快捷指令" });
  await expect(dialog.getByRole("switch")).toBeChecked();
  await dialog.getByLabel("名称（可选）").fill("新会话");
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  expect(await labels()).toEqual(["编辑 /new", "编辑 /resume"]);

  await card(page).getByRole("button", { name: "删除 /new" }).click();
  const confirm = page.getByRole("alertdialog", { name: "删除快捷指令？" });
  await expect(confirm).toContainText("/new");
  await confirm.getByRole("button", { name: "取消", exact: true }).click();
  expect(await labels()).toEqual(["编辑 /new", "编辑 /resume"]);
  for (const name of ["/new", "/resume"]) {
    await card(page)
      .getByRole("button", { name: `删除 ${name}` })
      .click();
    await confirm.getByRole("button", { name: "删除", exact: true }).click();
    await expect(confirm).toBeHidden();
  }
  await expect(card(page).getByText("还没有快捷指令")).toBeVisible();
  await page.reload();
  await expect(card(page).getByText("还没有快捷指令")).toBeVisible();

  await backToTerminal(page, id);
  await quick(page).click();
  await expect(menu(page).getByText("还没有快捷指令")).toBeVisible();
  expect(await commandNames(menu(page))).toEqual([]);
  await item(page, "添加快捷指令…").click();
  await expect(page).toHaveURL(
    new RegExp(`/settings\\?session=${id}#quick-commands$`),
  );
  await backToTerminal(page, id);
});

test("corrupt stored commands degrade to an empty list", async ({ page }) => {
  await page.goto("/settings");
  await page.evaluate(() =>
    localStorage.setItem("webmux.quickCommands", "{broken"),
  );
  await page.reload();
  await expect(card(page).getByText("还没有快捷指令")).toBeVisible();
  await expect(page.getByRole("heading", { name: "设置" })).toBeVisible();
});

test("an open add dialog preserves input when another tab fills the list", async ({
  page,
}) => {
  await page.goto("/settings");
  await page.evaluate(() =>
    localStorage.setItem(
      "webmux.quickCommands",
      JSON.stringify(
        Array.from({ length: 49 }, (_, index) => ({
          id: `command-${index}`,
          name: "",
          command: `echo ${index}`,
          enter: false,
        })),
      ),
    ),
  );
  await page.reload();
  await card(page).getByRole("button", { name: "添加快捷指令" }).click();
  const dialog = page.getByRole("dialog", { name: "添加快捷指令" });
  const body = dialog.getByLabel("指令", { exact: true });
  await body.fill("echo tab-a");
  const other = await page.context().newPage();
  try {
    await other.goto("/settings");
    await addCommand(other, "echo tab-b", false);
    await expect(
      card(page).getByRole("button", { name: "编辑 echo tab-b" }),
    ).toBeAttached();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog.getByRole("alert")).toBeVisible();
    await expect(body).toHaveValue("echo tab-a");
    expect(
      await page.evaluate(() =>
        JSON.parse(localStorage.getItem("webmux.quickCommands")!).map(
          (cmd: { command: string }) => cmd.command,
        ),
      ),
    ).toEqual([
      ...Array.from({ length: 49 }, (_, index) => `echo ${index}`),
      "echo tab-b",
    ]);

    await card(other)
      .getByRole("button", { name: "删除 echo 0", exact: true })
      .click();
    await other
      .getByRole("alertdialog")
      .getByRole("button", { name: "删除", exact: true })
      .click();
    await expect(
      card(page).getByRole("button", { name: "编辑 echo 0", exact: true }),
    ).toHaveCount(0);
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(
      card(other).getByRole("button", { name: "编辑 echo tab-a" }),
    ).toBeVisible();
    await card(page).getByRole("button", { name: "编辑 echo tab-a" }).click();
    const edit = page.getByRole("dialog", { name: "编辑快捷指令" });
    await edit.getByLabel("指令", { exact: true }).fill("echo edited");
    await edit.getByRole("button", { name: "保存", exact: true }).click();
    await expect(edit).toBeHidden();
    await expect(
      card(other).getByRole("button", { name: "编辑 echo edited" }),
    ).toBeVisible();
  } finally {
    await other.close();
  }
});

test("narrow code view keeps the quick-command entry accessible", async ({
  page,
  workDir,
}, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const id = await createShell(page, "Code view quick commands", workDir);
  if (!testInfo.project.use.isMobile) {
    await page.getByRole("button", { name: "终端菜单", exact: true }).click();
    await page
      .getByRole("menuitemcheckbox", { name: "强制显示底部工具栏" })
      .click();
    await page.keyboard.press("Escape");
  }
  const headerEntry = page
    .locator(".terminal-header")
    .getByRole("button", { name: "快捷指令", exact: true });
  const bar = page.getByRole("complementary", { name: "终端软键" });
  await expect(headerEntry).toBeHidden();
  await page.getByRole("button", { name: "代码", exact: true }).click();
  await expect(bar).toBeHidden();
  await expect(headerEntry).toBeVisible();
  await expect(quick(page)).toHaveCount(1);
  await headerEntry.click();
  await expect(menu(page)).toHaveAttribute("data-side", "bottom");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "终端", exact: true }).click();
  await expect(headerEntry).toBeHidden();
  await expect(bar.getByRole("button", { name: "快捷指令" })).toBeVisible();
  await page.getByRole("button", { name: "代码", exact: true }).click();
  await headerEntry.click();
  await item(page, "管理快捷指令…").click();
  await expect(page).toHaveURL(
    new RegExp(`/settings\\?session=${id}#quick-commands$`),
  );
});

test("desktop shows the entry beside the draft button until the toolbar is forced", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(testInfo.project.use.isMobile, "Exercises a non-coarse pointer");
  await page.goto("/");
  await page.evaluate(() =>
    localStorage.setItem(
      "webmux.quickCommands",
      JSON.stringify([
        { id: "a", name: "打招呼", command: "echo desk-ok", enter: true },
      ]),
    ),
  );
  await createShell(page, "Desktop quick commands", workDir);
  const header = page.locator(".terminal-header");
  const bar = page.getByRole("complementary", { name: "终端软键" });
  const entry = header.getByRole("button", { name: "快捷指令", exact: true });
  await expect(bar).toBeHidden();
  await expect(entry).toBeVisible();
  const [entryBox, draftBox] = await Promise.all([
    entry.boundingBox(),
    header.getByRole("button", { name: "草稿模式", exact: true }).boundingBox(),
  ]);
  expect(entryBox!.x + entryBox!.width).toBeLessThanOrEqual(draftBox!.x);
  expect(draftBox!.x - (entryBox!.x + entryBox!.width)).toBeLessThan(12);

  await entry.click();
  await expect(menu(page)).toHaveAttribute("data-side", "bottom");
  await item(page, "打招呼：echo desk-ok（并回车）").click();
  await expectTerminalLine(page, "desk-ok");
  await expect(page.locator(".xterm-helper-textarea")).toBeFocused();

  await page.getByRole("button", { name: "终端菜单", exact: true }).click();
  await page
    .getByRole("menuitemcheckbox", { name: "强制显示底部工具栏" })
    .click();
  await page.keyboard.press("Escape");
  await expect(bar).toBeVisible();
  await expect(entry).toBeHidden();
  await expect(quick(page)).toHaveCount(1);
  await bar.getByRole("button", { name: "快捷指令" }).click();
  await expect(menu(page)).toHaveAttribute("data-side", "top");
});
