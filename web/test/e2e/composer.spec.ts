import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import type { Page } from "@playwright/test";
import {
  test,
  expect,
  createShell,
  headerAction,
  quote,
  recordInput,
} from "./fixtures";

const mode = (page: Page) => page.getByRole("button", { name: /^草稿模式/ });
const draft = (page: Page) =>
  page.getByRole("textbox", {
    name: "草稿内容",
    exact: true,
    includeHidden: true,
  });
const send = (page: Page) =>
  page.getByRole("button", { name: "发送", exact: true });
const scratch = (page: Page) => page.getByRole("button", { name: /打开暂存/ });
const scratchEntry = (page: Page, index: number) =>
  page.getByRole("textbox", { name: `暂存第 ${index} 条`, exact: true });

for (const bracketed of [true, false]) {
  test(`draft holds editable text until explicit send, bracketed=${bracketed}`, async ({
    page,
    workDir,
  }) => {
    await createShell(page, "Draft recorder", workDir);
    const received = await recordInput(page, workDir, bracketed);
    await mode(page).click();
    await expect(draft(page)).toBeVisible();
    await expect(send(page)).toBeDisabled();
    await draft(page).fill("  中文🙂 first\nsecond  ");
    await draft(page).press("End");
    await draft(page).press("Enter");
    await page.keyboard.type("third");
    const text = await draft(page).inputValue();
    expect(text).toBe("  中文🙂 first\nsecond  \nthird");
    await draft(page).press("Control+Enter");
    await expect(draft(page)).toHaveValue(text);
    await draft(page).press("Escape");
    await expect(draft(page)).toBeVisible();
    expect(await readFile(received, "utf8")).toBe("");
    await mode(page).click();
    await expect(draft(page)).toBeHidden();
    await mode(page).click();
    await expect(draft(page)).toHaveValue(text);
    await send(page).click();
    const wire = text.replaceAll("\n", "\r");
    await expect
      .poll(() => readFile(received, "utf8"))
      .toBe(bracketed ? `\x1b[200~${wire}\x1b[201~\r` : `${wire}\r`);
    await expect(draft(page)).toBeHidden();
    await expect(mode(page)).toHaveAttribute("aria-pressed", "false");
    await mode(page).click();
    await expect(draft(page)).toHaveValue("");
    await expect(send(page)).toBeDisabled();
    await draft(page).fill(text);
    await page.getByRole("button", { name: "清空草稿", exact: true }).click();
    await page.getByRole("button", { name: "取消", exact: true }).click();
    await expect(draft(page)).toHaveValue(text);
    await page.getByRole("button", { name: "清空草稿", exact: true }).click();
    await page.getByRole("button", { name: "确认清空", exact: true }).click();
    await expect(page.getByRole("alertdialog")).toBeHidden();
    await expect(draft(page)).toHaveValue("");
    await expect(send(page)).toBeDisabled();
    expect(await readFile(received, "utf8")).toBe(
      bracketed ? `\x1b[200~${wire}\x1b[201~\r` : `${wire}\r`,
    );
    await draft(page).fill("Next draft remains editable");
    await expect(draft(page)).toBeFocused();
    await expect(mode(page)).toHaveAttribute("aria-pressed", "true");
  });
}

test("code references and uploaded paths join the same draft without PTY input", async ({
  page,
  request,
  workDir,
}, info) => {
  await writeFile(join(workDir, "reference.txt"), "reference body\n");
  const id = await createShell(page, "Mixed draft", workDir);
  const received = await recordInput(page, workDir);
  await mode(page).click();
  await draft(page).fill("Please inspect ");
  await draft(page).press("End");
  await page
    .locator(".terminal-header")
    .getByRole("button", { name: "代码", exact: true })
    .click();
  await page
    .getByRole("button", { name: "reference.txt", exact: true })
    .click();
  await page
    .getByRole("button", { name: /位置.*草稿/ })
    .filter({ visible: true })
    .click();
  await expect(draft(page)).toHaveValue(
    `Please inspect ${join(workDir, "reference.txt")} `,
  );
  await expect(page.getByRole("region", { name: "代码工作区" })).toBeVisible();
  const response = page.waitForResponse((r) =>
    r.url().endsWith(`/api/sessions/${id}/upload`),
  );
  const chooser = page.waitForEvent("filechooser");
  await (await headerAction(page, "上传文件")).click();
  await (
    await chooser
  ).setFiles({
    name: "sample 'quoted'.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("real upload bytes"),
  });
  const result = await response;
  expect(result.status()).toBe(201);
  const { path } = await result.json();
  expect(await readFile(path, "utf8")).toBe("real upload bytes");
  const expected = `Please inspect ${join(workDir, "reference.txt")} ${quote(path)} `;
  await expect(draft(page)).toHaveValue(expected);
  expect(await readFile(received, "utf8")).toBe("");
  if (info.project.use.isMobile) {
    await expect(draft(page)).toBeHidden();
    await page
      .locator(".terminal-header")
      .getByRole("button", { name: "终端", exact: true })
      .click();
  }
  await send(page).click();
  await expect
    .poll(() => readFile(received, "utf8"))
    .toBe(`\x1b[200~${expected}\x1b[201~\r`);
  if (info.project.use.isMobile)
    await page
      .locator(".terminal-header")
      .getByRole("button", { name: "代码", exact: true })
      .click();
  await expect(page.getByRole("region", { name: "代码工作区" })).toBeVisible();
  await expect(draft(page)).toBeHidden();
  await expect(mode(page)).toHaveAttribute("aria-pressed", "false");
  await mode(page).click();
  await expect(draft(page)).toHaveValue("");
  await info.attach("composer-code.png", {
    body: await page.screenshot(),
    contentType: "image/png",
  });
  expect((await request.get(`/api/sessions/${id}`)).ok()).toBeTruthy();
});

test("draft splits the terminal column while code stays full height", async ({
  page,
  workDir,
}, info) => {
  await writeFile(join(workDir, "panel.txt"), "code stays readable\n");
  await createShell(page, "Resizable draft", workDir);
  const terminal = page.locator(".terminal-host");
  const before = await terminal.boundingBox();
  const host = await terminal.elementHandle();
  await mode(page).click();
  await draft(page).fill("Keep this draft");
  const handle = page.getByRole("separator", { name: "调整草稿面板高度" });
  const panel = page.getByRole("region", { name: "终端输入草稿" });
  const original = (await panel.boundingBox())!;
  const grip = (await handle.boundingBox())!;
  await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2);
  await page.mouse.down();
  await page.mouse.move(grip.x + grip.width / 2, grip.y - 80, { steps: 8 });
  await page.mouse.up();
  expect((await panel.boundingBox())!.height).toBeGreaterThan(
    original.height + 50,
  );
  const shrunk = (await terminal.boundingBox())!;
  const enlarged = (await panel.boundingBox())!;
  expect(shrunk.height).toBeLessThan(before!.height - 50);
  expect(shrunk.y + shrunk.height).toBeLessThanOrEqual(enlarged.y);
  await mode(page).click();
  await expect.poll(() => terminal.boundingBox()).toEqual(before);
  await mode(page).click();
  expect(
    await host!.evaluate(
      (element) => element === document.querySelector(".terminal-host"),
    ),
  ).toBe(true);
  await expect(draft(page)).toHaveValue("Keep this draft");
  await page
    .locator(".terminal-header")
    .getByRole("button", { name: "代码", exact: true })
    .click();
  await page.getByRole("button", { name: "panel.txt", exact: true }).click();
  const code = page.getByRole("region", { name: "代码工作区" });
  const codeBounds = (await code.boundingBox())!;
  if (info.project.use.isMobile) {
    await expect(panel).toBeHidden();
  } else {
    const bounds = (await panel.boundingBox())!;
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(codeBounds.x);
    await handle.focus();
    await handle.press("ArrowUp");
    expect((await panel.boundingBox())!.height).toBeGreaterThan(bounds.height);
    expect(await code.boundingBox()).toEqual(codeBounds);
    const resizedTerminal = (await terminal.boundingBox())!;
    const resizedDraft = (await panel.boundingBox())!;
    expect(resizedTerminal.y + resizedTerminal.height).toBeLessThanOrEqual(
      resizedDraft.y,
    );
    expect(codeBounds.height).toBeGreaterThan(
      resizedTerminal.height + resizedDraft.height,
    );
  }
  await mode(page).click();
  expect(await code.boundingBox()).toEqual(codeBounds);
  await mode(page).click();
  expect(await code.boundingBox()).toEqual(codeBounds);
  if (info.project.use.isMobile)
    await page
      .locator(".terminal-header")
      .getByRole("button", { name: "终端", exact: true })
      .click();
  await expect(draft(page)).toHaveValue("Keep this draft");
  await expect(panel).toBeVisible();
});

test("drafts survive navigation and reload, remain session-local and stop persisting when disabled", async ({
  page,
  request,
  workDir,
}) => {
  const a = await createShell(page, "Draft A", workDir);
  await mode(page).click();
  await draft(page).fill("A private draft");
  const response = await request.post("/api/sessions", {
    data: { cwd: workDir, preset: "shell", name: "Draft B" },
  });
  expect(response.status()).toBe(201);
  const back = () =>
    page.getByRole("button", { name: "返回会话列表", exact: true }).click();
  await back();
  await page.getByRole("link", { name: "Draft B", exact: true }).click();
  await expect(mode(page)).toHaveAttribute("aria-pressed", "false");
  await mode(page).click();
  await expect(draft(page)).toHaveValue("");
  await draft(page).fill("B private draft");
  await back();
  await page.getByRole("link", { name: "Draft A", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/s/${a}$`));
  await expect(draft(page)).toHaveValue("A private draft");
  // A reload restores the saved draft visibly, with its target, and sends nothing.
  await page.reload();
  await expect(mode(page)).toHaveAttribute("aria-pressed", "true");
  await expect(draft(page)).toHaveValue("A private draft");
  await expect(page.getByText(/已恢复本机草稿.*目标：Draft A/)).toBeVisible();
  await expect(send(page)).toBeEnabled();
  await page.goto("/settings");
  await expect(
    page.getByText("本机保存了 2 个会话的草稿，0 条暂存"),
  ).toBeVisible();
  await page.getByRole("switch", { name: "在本机保存草稿" }).click();
  await expect(
    page.getByText("本机保存了 0 个会话的草稿，0 条暂存"),
  ).toBeVisible();
  await page.goto(`/s/${a}`);
  await expect(mode(page)).toHaveAttribute("aria-pressed", "false");
  await mode(page).click();
  await expect(draft(page)).toHaveValue("");
});

test("scratch notes persist and move into the draft only on take", async ({
  page,
  workDir,
}) => {
  const id = await createShell(page, "Scratch notes", workDir);
  const received = await recordInput(page, workDir);
  await mode(page).click();
  await scratch(page).click();
  await page.getByLabel("新建暂存", { exact: true }).fill("First note");
  await page.getByRole("button", { name: "添加暂存", exact: true }).click();
  await page.getByLabel("新建暂存", { exact: true }).fill("Second note");
  await page.getByRole("button", { name: "添加暂存", exact: true }).click();
  await expect(scratchEntry(page, 1)).toHaveValue("First note");
  await expect(scratchEntry(page, 2)).toHaveValue("Second note");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "暂存" })).toBeHidden();
  await mode(page).click();
  // The closed draft button still exposes the scratch count.
  await expect(mode(page)).toContainText("2");
  expect(await readFile(received, "utf8")).toBe("");

  // The terminal menu reaches scratch without opening the draft; an update is
  // announced visibly but does not open the draft or send anything.
  await page.getByRole("button", { name: "终端菜单", exact: true }).click();
  await page.getByRole("menuitem", { name: "暂存 (2)", exact: true }).click();
  await page.getByLabel("新建暂存", { exact: true }).fill("Third note");
  await page.getByRole("button", { name: "添加暂存", exact: true }).click();
  await page.keyboard.press("Escape");
  await expect(
    page.getByText(/暂存已更新 · 共 3 条，不会自动发送/),
  ).toBeVisible();
  await page.getByRole("button", { name: "知道了", exact: true }).click();
  await expect(page.getByText(/暂存已更新 · 共 3 条/)).toBeHidden();
  expect(await readFile(received, "utf8")).toBe("");

  await page.goto("/settings");
  await expect(
    page.getByText("本机保存了 0 个会话的草稿，3 条暂存"),
  ).toBeVisible();
  await page.goto(`/s/${id}`);
  await mode(page).click();
  await scratch(page).click();
  await expect(scratchEntry(page, 1)).toHaveValue("First note");
  await expect(scratchEntry(page, 2)).toHaveValue("Second note");
  await page
    .getByRole("button", { name: "取用暂存第 1 条", exact: true })
    .click();
  await expect(page.getByRole("dialog", { name: "暂存" })).toBeHidden();
  await expect(draft(page)).toHaveValue("First note");
  expect(await readFile(received, "utf8")).toBe("");
});

for (const initialDraft of [true, false]) {
  test(`in-flight upload stays in draft after mode changes, initialDraft=${initialDraft}`, async ({
    page,
    workDir,
  }) => {
    const id = await createShell(page, "Pending draft upload", workDir);
    const received = await recordInput(page, workDir);
    if (initialDraft) await mode(page).click();
    const gate = Promise.withResolvers<void>();
    const started = Promise.withResolvers<void>();
    await page.route(`**/api/sessions/${id}/upload`, async (route) => {
      started.resolve();
      await gate.promise;
      await route.continue();
    });
    try {
      const response = page.waitForResponse((r) =>
        r.url().endsWith(`/api/sessions/${id}/upload`),
      );
      const chooser = page.waitForEvent("filechooser");
      await (await headerAction(page, "上传文件")).click();
      await (
        await chooser
      ).setFiles({
        name: "pending.txt",
        mimeType: "text/plain",
        buffer: Buffer.from("pending"),
      });
      await started.promise;
      if (!initialDraft) await mode(page).click();
      await mode(page).click();
      gate.resolve();
      const { path } = await (await response).json();
      await expect(
        page.getByRole("progressbar", { name: "文件上传进度" }),
      ).toBeHidden();
      expect(await readFile(received, "utf8")).toBe("");
      await expect(draft(page)).toBeHidden();
      await mode(page).click();
      await expect(draft(page)).toHaveValue(`${quote(path)} `);
    } finally {
      gate.resolve();
    }
  });
}

test("native text and file paste target the draft, while exited sessions retain editable text", async ({
  page,
  request,
  workDir,
}) => {
  const id = await createShell(page, "Paste draft", workDir);
  const received = await recordInput(page, workDir);
  await mode(page).click();
  await draft(page).fill("before after");
  await draft(page).evaluate((element: HTMLTextAreaElement) => {
    element.setSelectionRange(7, 12);
    element.dispatchEvent(new Event("select", { bubbles: true }));
  });
  await page.locator(".xterm-helper-textarea").evaluate((element) => {
    const data = new DataTransfer();
    data.setData("text/plain", "replacement🙂");
    element.dispatchEvent(
      new ClipboardEvent("paste", {
        clipboardData: data,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  await expect(draft(page)).toHaveValue("before replacement🙂");
  const response = page.waitForResponse((r) =>
    r.url().endsWith(`/api/sessions/${id}/upload`),
  );
  await draft(page).evaluate((element) => {
    const data = new DataTransfer();
    data.items.add(
      new File(["image bytes"], "image.png", { type: "image/png" }),
    );
    data.setData("text/plain", "DO_NOT_INSERT");
    element.dispatchEvent(
      new ClipboardEvent("paste", {
        clipboardData: data,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  const { path } = await (await response).json();
  await expect(draft(page)).toHaveValue(`before replacement🙂${quote(path)} `);
  expect(await readFile(received, "utf8")).toBe("");
  await request.delete(`/api/sessions/${id}`);
  await expect(send(page)).toBeDisabled();
  await draft(page).fill("Retained after exit");
  await expect(draft(page)).toHaveValue("Retained after exit");
});

test("composition, direct keyboard entry and disconnect never bypass explicit send", async ({
  page,
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
  await createShell(page, "Disconnected draft", workDir);
  const received = await recordInput(page, workDir);
  await mode(page).click();
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type("first key");
  await expect(draft(page)).toHaveValue("first key");
  await draft(page).dispatchEvent("compositionstart", { data: "" });
  await draft(page).fill("first key中文");
  await expect(send(page)).toBeDisabled();
  await draft(page).dispatchEvent("compositionend", { data: "中文" });
  await expect(send(page)).toBeEnabled();
  blockReconnect = true;
  expect(disconnect).toBeDefined();
  await disconnect!();
  await expect.poll(() => blockedReconnects).toBeGreaterThan(0);
  await expect(page.locator(".terminal-header").getByRole("status")).toHaveText(
    "重连中",
  );
  await expect(send(page)).toBeDisabled();
  await draft(page).fill("Keep after disconnect");
  expect(await readFile(received, "utf8")).toBe("");
  await expect(draft(page)).toHaveValue("Keep after disconnect");
  blockReconnect = false;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(send(page)).toBeEnabled();
  expect(await readFile(received, "utf8")).toBe("");
  await send(page).click();
  await expect
    .poll(() => readFile(received, "utf8"))
    .toBe("\x1b[200~Keep after disconnect\x1b[201~\r");
});

test("an uploaded path waiting for terminal recovery moves to draft immediately", async ({
  page,
  workDir,
}) => {
  const id = await createShell(page, "Promote waiting upload", workDir);
  const received = await recordInput(page, workDir);
  const replay = Promise.withResolvers<void>();
  await page.routeWebSocket(`**/ws/s/${id}**`, async (socket) => {
    await replay.promise;
    socket.connectToServer();
  });
  try {
    const chooser = page.waitForEvent("filechooser");
    await (await headerAction(page, "上传文件")).click();
    await page.evaluate(() => {
      Object.defineProperty(document, "hidden", {
        configurable: true,
        value: true,
      });
      document.dispatchEvent(new Event("visibilitychange"));
      Reflect.deleteProperty(document, "hidden");
      document.dispatchEvent(new Event("visibilitychange"));
    });
    const response = page.waitForResponse((r) =>
      r.url().endsWith(`/api/sessions/${id}/upload`),
    );
    await (
      await chooser
    ).setFiles({
      name: "waiting.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("waiting"),
    });
    const { path } = await (await response).json();
    await expect(
      page.getByRole("status").filter({ hasText: "文件已上传，等待终端恢复" }),
    ).toBeVisible();
    await mode(page).click();
    await expect(draft(page)).toHaveValue(`${quote(path)} `);
    await expect(send(page)).toBeDisabled();
    expect(await readFile(received, "utf8")).toBe("");
    replay.resolve();
    await expect(send(page)).toBeEnabled();
    expect(await readFile(received, "utf8")).toBe("");
  } finally {
    replay.resolve();
  }
});

test("partial upload failure retains only real paths in the draft", async ({
  page,
  workDir,
}) => {
  const id = await createShell(page, "Partial upload", workDir);
  const received = await recordInput(page, workDir);
  await mode(page).click();
  let count = 0;
  await page.route(`**/api/sessions/${id}/upload`, async (route) => {
    if (++count === 2) await route.abort("failed");
    else await route.continue();
  });
  const first = page.waitForResponse((r) =>
    r.url().endsWith(`/api/sessions/${id}/upload`),
  );
  await draft(page).evaluate((element) => {
    const transfer = new DataTransfer();
    transfer.items.add(
      new File(["success bytes"], "success.txt", { type: "text/plain" }),
    );
    transfer.items.add(
      new File(["never uploaded"], "failure.txt", { type: "text/plain" }),
    );
    element.dispatchEvent(
      new ClipboardEvent("paste", {
        clipboardData: transfer,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  const { path } = await (await first).json();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(draft(page)).toHaveValue(`${quote(path)} `);
  expect(await readFile(path, "utf8")).toBe("success bytes");
  expect(await readFile(received, "utf8")).toBe("");
});
