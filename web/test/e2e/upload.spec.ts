import { readFile } from "node:fs/promises";
import {
  test,
  expect,
  createShell,
  expectTerminalLine,
  expectConnected,
  headerAction,
} from "./fixtures";

test("file chooser shows progress, survives foreground replay, and pastes a shell-safe path without Enter", async ({
  page,
  request,
  workDir,
}, info) => {
  const id = await createShell(page, "File upload", workDir);
  const contents = Buffer.from("UPLOADED_FILE_CONTENT\n");
  const uploadGate = Promise.withResolvers<void>();
  const replayGate = Promise.withResolvers<void>();
  let requests = 0;
  await page.route(`**/api/sessions/${id}/upload`, async (route) => {
    requests++;
    await uploadGate.promise;
    await route.continue();
  });
  if (info.project.use.isMobile) {
    await page.routeWebSocket(`**/ws/s/${id}**`, async (socket) => {
      await replayGate.promise;
      socket.connectToServer();
    });
  }
  try {
    await page.locator(".xterm-helper-textarea").focus();
    await page.keyboard.type("cat ");
    const response = page.waitForResponse((response) =>
      response.url().endsWith(`/api/sessions/${id}/upload`),
    );
    const chooser = page.waitForEvent("filechooser");
    await (await headerAction(page, "上传文件")).click();
    if (info.project.use.isMobile) {
      await page.evaluate(() => {
        Object.defineProperty(document, "hidden", {
          configurable: true,
          value: true,
        });
        document.dispatchEvent(new Event("visibilitychange"));
        Reflect.deleteProperty(document, "hidden");
        document.dispatchEvent(new Event("visibilitychange"));
      });
    }
    await (
      await chooser
    ).setFiles({
      name: "notes 'quoted'.txt",
      mimeType: "text/plain",
      buffer: contents,
    });
    await expect(
      page.getByRole("progressbar", { name: "文件上传进度" }),
    ).toBeVisible();
    // Narrow screens reach upload from the terminal menu, disabled alike.
    await expect(await headerAction(page, "上传文件")).toBeDisabled();
    if (info.project.use.isMobile) await page.keyboard.press("Escape");
    uploadGate.resolve();
    const uploaded = await response;
    expect(uploaded.status()).toBe(201);
    const { path } = await uploaded.json();
    expect(path).toContain("webmux-upload-");
    expect(path.startsWith(workDir)).toBe(false);
    expect(await readFile(path)).toEqual(contents);
    if (info.project.use.isMobile) {
      await expect(
        page
          .getByRole("status")
          .filter({ hasText: "文件已上传，等待终端恢复" }),
      ).toBeVisible();
      expect(
        await (await request.get(`/api/sessions/${id}/buffer`)).text(),
      ).not.toContain("notes");
      replayGate.resolve();
    }
    await expect(
      page.getByRole("progressbar", { name: "文件上传进度" }),
    ).toBeHidden();
    await expect(page.locator(".xterm-rows")).not.toContainText(
      "UPLOADED_FILE_CONTENT",
    );
    expect(requests).toBe(1);
    await page.locator(".xterm-helper-textarea").press("Enter");
    await expectTerminalLine(page, "UPLOADED_FILE_CONTENT");
  } finally {
    uploadGate.resolve();
    replayGate.resolve();
  }
});

test("file chooser uploads every selected file and reports batch progress", async ({
  page,
  workDir,
}) => {
  const id = await createShell(page, "Multi-file upload", workDir);
  const gates = [Promise.withResolvers<void>(), Promise.withResolvers<void>()];
  const bodies: string[] = [];
  await page.route(`**/api/sessions/${id}/upload`, async (route) => {
    const index = bodies.length;
    bodies.push(route.request().postDataBuffer()?.toString("utf8") ?? "");
    await (gates[index]?.promise ?? Promise.resolve());
    await route.continue();
  });
  try {
    await page.locator(".xterm-helper-textarea").focus();
    await page.keyboard.type("cat ");
    const chooser = page.waitForEvent("filechooser");
    await (await headerAction(page, "上传文件")).click();
    await (
      await chooser
    ).setFiles([
      {
        name: "first.txt",
        mimeType: "text/plain",
        buffer: Buffer.from("FIRST_FILE_CONTENT\n"),
      },
      {
        name: "second.txt",
        mimeType: "text/plain",
        buffer: Buffer.from("SECOND_FILE_CONTENT\n"),
      },
    ]);
    // The picker's batch runs serially: the second request only starts after
    // the first upload finished and its path was inserted.
    await expect(
      page.getByRole("status").filter({ hasText: "first.txt（1/2）" }),
    ).toBeVisible();
    await expect.poll(() => bodies.length).toBe(1);
    gates[0]!.resolve();
    await expect(
      page.getByRole("status").filter({ hasText: "second.txt（2/2）" }),
    ).toBeVisible();
    await expect.poll(() => bodies.length).toBe(2);
    gates[1]!.resolve();
    await expect(
      page.getByRole("progressbar", { name: "文件上传进度" }),
    ).toBeHidden();
    await expect(page.locator(".xterm-rows")).toContainText("first.txt");
    await expect(page.locator(".xterm-rows")).toContainText("second.txt");
    expect(bodies[0]).toContain("first.txt");
    expect(bodies[1]).toContain("second.txt");
    await page.locator(".xterm-helper-textarea").press("Enter");
    await expectTerminalLine(page, "FIRST_FILE_CONTENT");
    await expectTerminalLine(page, "SECOND_FILE_CONTENT");
  } finally {
    for (const gate of gates) gate.resolve();
  }
});

test("cancelling an uploaded path during replay never pastes it after reconnect", async ({
  page,
  request,
  workDir,
}, info) => {
  test.skip(!info.project.use.isMobile, "Mobile file picker recovery");
  const id = await createShell(page, "Cancel pending upload", workDir);
  const replayGate = Promise.withResolvers<void>();
  await page.routeWebSocket(`**/ws/s/${id}**`, async (socket) => {
    await replayGate.promise;
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
    const response = page.waitForResponse((response) =>
      response.url().endsWith(`/api/sessions/${id}/upload`),
    );
    await (
      await chooser
    ).setFiles({
      name: "cancel-pending.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("CANCELLED_PATH_CONTENT\n"),
    });
    const uploaded = await response;
    expect(uploaded.status()).toBe(201);
    const { path } = await uploaded.json();
    await expect(
      page.getByRole("status").filter({ hasText: "文件已上传，等待终端恢复" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "取消", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText(path);
    await expect(page.getByRole("alert")).toContainText("操作已取消");
    replayGate.resolve();
    await expectConnected(page);
    await expect(page.locator(".restore-overlay")).toBeHidden();
    expect(
      await (await request.get(`/api/sessions/${id}/buffer`)).text(),
    ).not.toContain("cancel-pending.txt");
  } finally {
    replayGate.resolve();
  }
});

test("dragging a file into the terminal uploads actual bytes and pastes the path", async ({
  page,
  workDir,
}) => {
  const id = await createShell(page, "Drop upload", workDir);
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type("cat ");
  const data = await page.evaluateHandle(() => {
    const transfer = new DataTransfer();
    transfer.items.add(
      new File(["DROPPED_FILE_CONTENT\n"], "dragged.txt", {
        type: "text/plain",
      }),
    );
    return transfer;
  });
  const host = page.locator(".terminal-host");
  await host.dispatchEvent("dragenter", { dataTransfer: data });
  await expect(
    page.getByText("松开以上传文件到临时目录", { exact: true }),
  ).toBeVisible();
  await host.dispatchEvent("dragover", { dataTransfer: data });
  const response = page.waitForResponse((response) =>
    response.url().endsWith(`/api/sessions/${id}/upload`),
  );
  await host.dispatchEvent("drop", { dataTransfer: data });
  const uploaded = await response;
  expect(uploaded.status()).toBe(201);
  const { path } = await uploaded.json();
  expect(await readFile(path, "utf8")).toBe("DROPPED_FILE_CONTENT\n");
  await expect(
    page.getByText("松开以上传文件到临时目录", { exact: true }),
  ).toBeHidden();
  await expect(
    page.getByRole("progressbar", { name: "文件上传进度" }),
  ).toBeHidden();
  await expect(page.locator(".xterm-rows")).toContainText("dragged.txt");
  await page.locator(".xterm-helper-textarea").press("Enter");
  await expectTerminalLine(page, "DROPPED_FILE_CONTENT");
  await data.dispose();
});

test("desktop clipboard image uploads while ordinary text paste remains terminal input", async ({
  page,
  context,
  workDir,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "chromium-desktop",
    "Desktop clipboard integration",
  );
  const id = await createShell(page, "Clipboard image", workDir);
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const clipboardBytes = await page.evaluate(async () => {
    const canvas = new OffscreenCanvas(2, 2);
    const context = canvas.getContext("2d")!;
    context.fillStyle = "#00aa66";
    context.fillRect(0, 0, 2, 2);
    const image = await canvas.convertToBlob({ type: "image/png" });
    await navigator.clipboard.write([
      new ClipboardItem({ "image/png": image }),
    ]);
    const [item] = await navigator.clipboard.read();
    return Array.from(
      new Uint8Array(await (await item.getType("image/png")).arrayBuffer()),
    );
  });
  await page.locator(".xterm-helper-textarea").focus();
  const response = page.waitForResponse((response) =>
    response.url().endsWith(`/api/sessions/${id}/upload`),
  );
  await page.keyboard.press("Control+Shift+V");
  const uploaded = await response;
  expect(uploaded.status()).toBe(201);
  const { path } = await uploaded.json();
  expect(path).toMatch(/\.png$/);
  expect(await readFile(path)).toEqual(Buffer.from(clipboardBytes));
  await expect(page.locator(".xterm-rows")).toContainText("clipboard-");
  await expect(
    page.getByRole("progressbar", { name: "文件上传进度" }),
  ).toBeHidden();
  await page.keyboard.press("Control+U");
  await page.evaluate(() =>
    navigator.clipboard.writeText("printf 'NORMAL_PASTE_CONTENT\\n'"),
  );
  await page.keyboard.press(
    process.platform === "darwin" ? "Meta+V" : "Control+V",
  );
  await page.waitForFunction(() =>
    document
      .querySelector(".xterm-rows")
      ?.textContent?.includes("NORMAL_PASTE_CONTENT"),
  );
  await page.keyboard.press("Enter");
  await expectTerminalLine(page, "NORMAL_PASTE_CONTENT");
});

test("desktop native file paste takes precedence over the clipboard's text", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "chromium-desktop",
    "Desktop file clipboard event",
  );
  const id = await createShell(page, "Clipboard file", workDir);
  const response = page.waitForResponse((response) =>
    response.url().endsWith(`/api/sessions/${id}/upload`),
  );
  await page.locator(".xterm-helper-textarea").evaluate((input) => {
    const transfer = new DataTransfer();
    transfer.items.add(
      new File(["CLIPBOARD_FILE_CONTENT"], "clipboard-file.txt", {
        type: "text/plain",
      }),
    );
    transfer.setData("text/plain", "DO_NOT_PASTE_FILENAME_TEXT");
    input.dispatchEvent(
      new ClipboardEvent("paste", {
        bubbles: true,
        cancelable: true,
        clipboardData: transfer,
      }),
    );
  });
  const uploaded = await response;
  expect(uploaded.status()).toBe(201);
  const { path } = await uploaded.json();
  expect(await readFile(path, "utf8")).toBe("CLIPBOARD_FILE_CONTENT");
  await expect(page.locator(".xterm-rows")).toContainText("clipboard-file.txt");
  await expect(page.locator(".xterm-rows")).not.toContainText(
    "DO_NOT_PASTE_FILENAME_TEXT",
  );
});

test("413, 500 and interrupted uploads report failure and allow a successful retry", async ({
  page,
  workDir,
}, info) => {
  const id = await createShell(page, "Retry failed uploads", workDir);
  let attempt = 0;
  await page.route(`**/api/sessions/${id}/upload`, async (route) => {
    attempt++;
    if (attempt <= 2)
      await route.fulfill({
        status: attempt === 1 ? 413 : 500,
        json: { error: `upload rejected ${attempt}` },
      });
    else if (attempt === 3) await route.abort();
    else await route.continue();
  });
  async function selectFile() {
    const chooser = page.waitForEvent("filechooser");
    await (await headerAction(page, "上传文件")).click();
    await (
      await chooser
    ).setFiles({
      name: "retry.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("RETRY_UPLOAD_CONTENT\n"),
    });
    if (info.project.use.isMobile) await page.keyboard.press("Escape");
  }
  for (const message of [
    "upload rejected 1",
    "upload rejected 2",
    "上传失败：网络连接异常",
  ]) {
    await selectFile();
    await expect(page.getByRole("alert")).toContainText(message);
    await expect(
      page.getByRole("progressbar", { name: "文件上传进度" }),
    ).toBeHidden();
  }
  await selectFile();
  await expect(
    page.getByRole("progressbar", { name: "文件上传进度" }),
  ).toBeHidden();
  await expect(page.locator(".xterm-rows")).toContainText("retry.txt");
  expect(attempt).toBe(4);
});
