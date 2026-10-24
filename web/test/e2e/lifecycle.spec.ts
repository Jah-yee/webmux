import { randomUUID } from "node:crypto";
import { hostname } from "node:os";
import {
  test,
  expect,
  createShell,
  runCommand,
  expectTerminalLine,
  headerAction,
  expectConnected,
} from "./fixtures";

for (const initialization of ["connected", "font-pending"] as const) {
  test(`top-left back restores the live list from a ${initialization} terminal`, async ({
    page,
    request,
    workDir,
  }) => {
    const errors: string[] = [];
    let opened = 0;
    let closed = 0;
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("websocket", (socket) => {
      const path = new URL(socket.url()).pathname;
      if (path.startsWith("/ws/s/")) {
        opened++;
        socket.on("close", () => closed++);
      }
    });
    if (initialization === "font-pending") {
      // Hold the real font load until after leaving, exercising the delayed
      // terminal mount without substituting terminal output or server frames.
      await page.addInitScript(() => {
        const load = document.fonts.load.bind(document.fonts);
        const released = Promise.withResolvers<void>();
        window.addEventListener(
          "webmux:e2e-release-font",
          () => released.resolve(),
          { once: true },
        );
        document.fonts.load = (font, text) =>
          released.promise.then(() => load(font, text));
      });
    }
    const name = `Back navigation ${initialization}`;
    const created = await request.post("/api/sessions", {
      data: { cwd: workDir, preset: "shell", name },
    });
    expect(created.status()).toBe(201);
    const { id } = await created.json();
    await page.goto("/");
    const timeOrigin = await page.evaluate(() => performance.timeOrigin);
    const session = page.getByRole("link", { name, exact: true });
    await session.click();
    const back = page.getByRole("button", {
      name: "返回会话列表",
      exact: true,
    });
    await expect(back).toBeVisible();
    if (initialization === "connected") {
      await expectConnected(page);
      await expect(page.locator(".restore-overlay")).toBeHidden();
    } else {
      await expect(page.locator(".xterm-helper-textarea")).toHaveCount(0);
      expect(opened).toBe(0);
    }

    const returnToList = async () => {
      await back.click();
      await expect(page).toHaveURL(/\/$/);
      await expect(page.locator("header")).toContainText(`${hostname()} 在线`);
      await expect(
        page.getByLabel("正在加载会话", { exact: true }),
      ).toBeHidden();
      await expect(session).toBeVisible();
      const create = page
        .locator("header")
        .getByRole("button", { name: "＋ 新建会话", exact: true });
      await expect(create).toBeEnabled();
      await create.click();
      await expect(
        page.getByRole("button", { name: "Shell", exact: true }),
      ).toBeVisible();
      await page.getByRole("button", { name: "取消", exact: true }).click();
      await page
        .getByRole("button", { name: `会话菜单 ${name}`, exact: true })
        .click();
      await expect(
        page.getByRole("menuitem", { name: "关闭会话", exact: true }),
      ).toBeVisible();
      await page.keyboard.press("Escape");
      await expect.poll(() => closed).toBe(opened);
      await expect
        .poll(async () => {
          const response = await request.get(`/api/sessions/${id}`);
          const current = await response.json();
          expect(current.status).not.toBe("exited");
          return current.clients;
        })
        .toBe(0);
      expect(await page.evaluate(() => performance.timeOrigin)).toBe(
        timeOrigin,
      );
      expect(errors).toEqual([]);
    };
    await returnToList();
    if (initialization === "font-pending") {
      await page.evaluate(async () => {
        window.dispatchEvent(new Event("webmux:e2e-release-font"));
        await document.fonts.load('14px "Maple Mono NF CN"', "A");
        const frame = Promise.withResolvers<void>();
        requestAnimationFrame(() =>
          requestAnimationFrame(() => frame.resolve()),
        );
        await frame.promise;
      });
      expect(opened).toBe(0);
      await expect(page.locator(".xterm")).toHaveCount(0);
    }

    // Re-enter the same PTY, execute a real command, then exercise teardown again.
    await session.click();
    await expectConnected(page);
    await expect(page.locator(".restore-overlay")).toBeHidden();
    await runCommand(page, "printf '\\n%s%s\\n' 'back-navigation-' 'alive'");
    await expectTerminalLine(page, "back-navigation-alive");
    await returnToList();
  });
}

test("real shell output survives reload, then closes and is removed", async ({
  page,
  request,
  workDir,
}) => {
  const marker = `e2e-${randomUUID().slice(0, 8)}`;
  const id = await createShell(page, marker, workDir);
  const sessionURL = page.url();
  const created = await request.get(`/api/sessions/${id}`);
  expect(created.ok()).toBeTruthy();
  expect(await created.json()).toMatchObject({
    id,
    title: marker,
    cwd: workDir,
    preset: "shell",
    cmd: ["/bin/bash", "--noprofile", "--norc", "-i"],
  });

  // The whole marker never occurs in the typed command: only executing printf
  // inside the real PTY can join these halves into the asserted output line.
  await runCommand(
    page,
    `printf '\\n%s%s\\n' '${marker.slice(0, 6)}' '${marker.slice(6)}'`,
  );
  await expectTerminalLine(page, marker);
  const beforeReload = await request.get(`/api/sessions/${id}/buffer`);
  expect(await beforeReload.text()).toContain(`\r\n${marker}\r\n`);

  await page.reload();
  await expect(page).toHaveURL(sessionURL);
  await expectConnected(page);
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await expectTerminalLine(page, marker);
  const restored = await request.get("/api/sessions");
  expect(
    (await restored.json()).map((session: { id: string }) => session.id),
  ).toEqual([id]);

  await page.getByRole("button", { name: "终端菜单", exact: true }).click();
  await page.getByRole("menuitem", { name: "关闭会话", exact: true }).click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "确认关闭", exact: true })
    .click();
  await expect(page.locator(".terminal-header").getByRole("status")).toHaveText(
    "已退出",
  );
  await expect
    .poll(async () => {
      const response = await request.get(`/api/sessions/${id}`);
      return (await response.json()).status;
    })
    .toBe("exited");
  await expectTerminalLine(page, marker);
  const afterClose = await request.get(`/api/sessions/${id}/buffer`);
  expect(await afterClose.text()).toContain(`\r\n${marker}\r\n`);

  await page.getByRole("button", { name: "移除会话", exact: true }).click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "确认移除", exact: true })
    .click();
  await expect(page).toHaveURL(new URL("/", sessionURL).href);
  await expect(
    page.getByRole("link", { name: marker, exact: true }),
  ).toHaveCount(0);
  const removed = await request.get(`/api/sessions/${id}`);
  expect(removed.status()).toBe(404);
  expect(await (await request.get("/api/sessions")).json()).toEqual([]);
});

test("replay and the header action return to latest output without typing", async ({
  page,
  request,
  workDir,
}, info) => {
  const marker = "scroll-latest-output";
  const id = await createShell(page, "Scroll restoration", workDir);
  await runCommand(
    page,
    "for ((i=1;i<=200;i++)); do printf 'history-%03d\\n' \"$i\"; done; printf 'scroll-latest-%s\\n' output",
  );
  await expectTerminalLine(page, marker);
  await expectTerminalLine(page, "webmux-e2e$");
  const narrow = info.project.use.isMobile;
  // Wide headers show the button; narrow ones keep it in the terminal menu.
  const latestEnabled = async () => {
    await expect(await headerAction(page, "滚动到最新位置")).toBeEnabled();
    if (narrow) await page.keyboard.press("Escape");
  };
  const floating = page.getByRole("button", { name: "回到最新", exact: true });
  const scrollBack = async () => {
    await expect(page.locator(".restore-overlay")).toBeHidden();
    // A fit after a layout change (the exit banner, a reload) resets xterm's
    // viewport scrollTop and ignores the pending scroll event, discarding a
    // gesture that lands in that window. Repeat the gesture until it sticks.
    await expect(async () => {
      if (info.project.use.isMobile) {
        const bounds = (await page.locator(".xterm-screen").boundingBox())!;
        const cdp = await page.context().newCDPSession(page);
        try {
          const x = bounds.x + bounds.width / 2;
          const y = bounds.y + bounds.height / 4;
          await cdp.send("Input.dispatchTouchEvent", {
            type: "touchStart",
            touchPoints: [{ x, y, id: 1 }],
          });
          for (let step = 1; step <= 6; step++)
            await cdp.send("Input.dispatchTouchEvent", {
              type: "touchMove",
              touchPoints: [{ x, y: y + (bounds.height * step) / 12, id: 1 }],
            });
          await cdp.send("Input.dispatchTouchEvent", {
            type: "touchEnd",
            touchPoints: [],
          });
        } finally {
          await cdp.detach();
        }
      } else {
        await page.locator(".xterm-screen").hover();
        await page.mouse.wheel(0, -10000);
      }
      await expect(page.locator(".xterm-rows")).not.toContainText(marker, {
        timeout: 1000,
      });
    }).toPass();
    if (narrow) await expect(floating).toBeVisible();
    else await expect(floating).toBeHidden();
  };
  await scrollBack();
  await page
    .locator(".xterm-helper-textarea")
    .evaluate((element) => element.blur());
  const before = await (await request.get(`/api/sessions/${id}/buffer`)).text();
  if (narrow) {
    await floating.click();
    await expect(floating).toBeHidden();
  } else await (await headerAction(page, "滚动到最新位置")).click();
  await expectTerminalLine(page, marker);
  await expect(page.locator(".xterm-helper-textarea")).not.toBeFocused();
  expect(await (await request.get(`/api/sessions/${id}/buffer`)).text()).toBe(
    before,
  );

  await scrollBack();
  await page.reload();
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await expectTerminalLine(page, marker);

  await scrollBack();
  await page.getByRole("button", { name: "终端菜单", exact: true }).click();
  await page.getByRole("menuitem", { name: "重新同步", exact: true }).click();
  await expectTerminalLine(page, marker);

  await scrollBack();
  if (info.project.use.isMobile) {
    await page
      .locator(".terminal-header")
      .getByRole("button", { name: "代码", exact: true })
      .click();
    await expect(
      page.getByRole("region", { name: "代码工作区" }),
    ).toBeVisible();
  }
  // A controlled visibility transition exercises the real reattached WebSocket,
  // which resumes from the rendered offset instead of replaying a snapshot;
  // it is not a real-device suspend/resume test.
  const replay = page.waitForEvent("websocket").then((socket) =>
    socket.waitForEvent("framereceived", {
      predicate: ({ payload }) =>
        typeof payload !== "string" && payload[0] === 4,
    }),
  );
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
    Reflect.deleteProperty(document, "hidden");
    document.dispatchEvent(new Event("visibilitychange"));
  });
  const position = (await replay).payload as Buffer;
  expect(JSON.parse(position.subarray(1).toString())).toMatchObject({
    mode: "append",
  });
  await latestEnabled();
  if (info.project.use.isMobile)
    await page
      .locator(".terminal-header")
      .getByRole("button", { name: "终端", exact: true })
      .click();
  await expectTerminalLine(page, marker);

  if (info.project.use.isMobile) {
    await scrollBack();
    await page
      .locator(".terminal-header")
      .getByRole("button", { name: "代码", exact: true })
      .click();
    // From the narrow code view, the menu entry switches back to the terminal.
    await (await headerAction(page, "滚动到最新位置")).click();
    await expect(page.locator(".terminal-host")).toBeVisible();
    await expectTerminalLine(page, marker);
  }
  await runCommand(page, "exit");
  await expect(page.locator(".terminal-header").getByRole("status")).toHaveText(
    "已退出",
  );
  await scrollBack();
  await page.getByRole("button", { name: "终端菜单", exact: true }).click();
  await page.getByRole("menuitem", { name: "重新同步", exact: true }).click();
  await expectTerminalLine(page, marker);
  await latestEnabled();
});

test("shell exit reports its real code and remains removable", async ({
  page,
  request,
  workDir,
}) => {
  const id = await createShell(page, "E2E exit code", workDir);
  await runCommand(page, "exit 7");
  await expect(
    page.getByText("进程已退出（code 7）", { exact: true }),
  ).toBeVisible();
  const exited = await request.get(`/api/sessions/${id}`);
  expect(await exited.json()).toMatchObject({ status: "exited", exitCode: 7 });
  await page.getByRole("button", { name: "移除会话", exact: true }).click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "确认移除", exact: true })
    .click();
  await expect(page).toHaveURL(/\/$/);
  expect(await (await request.get("/api/sessions")).json()).toEqual([]);
});
