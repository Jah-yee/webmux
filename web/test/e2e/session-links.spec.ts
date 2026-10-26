import { randomUUID } from "node:crypto";
import {
  test,
  expect,
  expectConnected,
  expectTerminalLine,
  openSessionLink,
  runCommand,
} from "./fixtures";
import { startE2EServer } from "./server.mjs";

test("session links open the terminal in a new tab and keep the list", async ({
  page,
  request,
  workDir,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.context().on("page", (popup) => {
    popup.on("pageerror", (error) => errors.push(error.message));
  });
  const name = `New tab ${randomUUID().slice(0, 8)}`;
  const created = await request.post("/api/sessions", {
    data: {
      cwd: workDir,
      cmd: ["/bin/bash", "--noprofile", "--norc", "-i"],
      name,
    },
  });
  expect(created.status()).toBe(201);
  const { id } = await created.json();

  await page.goto("/");
  const timeOrigin = await page.evaluate(() => performance.timeOrigin);
  const terminal = await openSessionLink(page, name);

  expect(new URL(terminal.url()).pathname).toBe(`/s/${id}`);
  await expectConnected(terminal);
  await expect(terminal.locator(".xterm-helper-textarea")).toBeAttached();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("link", { name, exact: true })).toBeVisible();
  expect(await terminal.evaluate(() => window.opener)).toBeNull();
  await expect(terminal.locator(".restore-overlay")).toBeHidden();
  await expectTerminalLine(terminal, "webmux-e2e$");
  await runCommand(terminal, "printf '\\n%s%s\\n' 'new-tab-' 'live-output'");
  await expectTerminalLine(terminal, "new-tab-live-output");
  const row = page.locator(`[data-session-id="${id}"]`);
  await expect(row.getByLabel("终端输出预览")).toContainText(
    "new-tab-live-output",
  );
  expect((await request.delete(`/api/sessions/${id}`)).status()).toBe(204);
  await expect(row).toHaveAttribute("data-status", "exited");
  await terminal
    .getByRole("button", { name: "返回会话列表", exact: true })
    .click();
  await expect(terminal).toHaveURL(/\/$/);
  await expect(terminal.getByRole("link", { name, exact: true })).toBeVisible();
  expect(await page.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
  expect(errors).toEqual([]);
});

test("Home Screen session links keep authenticated navigation inside the app", async ({
  page,
}) => {
  const token = `session-links-${randomUUID()}`;
  const server = await startE2EServer({ token });
  try {
    const created = await page.request.post(`${server.baseURL}/api/sessions`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        cwd: server.workDir,
        cmd: ["/bin/bash", "--noprofile", "--norc", "-i"],
        name: "Home Screen shell",
      },
    });
    expect(created.status()).toBe(201);
    const { id } = await created.json();
    // Emulate only the display-mode capability; routing, storage, authentication
    // and the PTY connection remain real.
    await page.addInitScript(() => {
      const matchMedia = window.matchMedia.bind(window);
      window.matchMedia = (query) => {
        const result = matchMedia(query);
        if (query === "(display-mode: standalone)")
          Object.defineProperty(result, "matches", { value: true });
        return result;
      };
    });
    await page.goto(`${server.baseURL}/#token=${token}`);
    const link = page.getByRole("link", {
      name: "Home Screen shell",
      exact: true,
    });
    await expect(link).toBeVisible();
    const timeOrigin = await page.evaluate(() => performance.timeOrigin);
    const pages = page.context().pages().length;
    await link.click();
    await expect(page).toHaveURL(`${server.baseURL}/s/${id}`);
    await expectConnected(page);
    await expect(page.locator(".restore-overlay")).toBeHidden();
    await expectTerminalLine(page, "webmux-e2e$");
    await runCommand(
      page,
      "printf '\\n%s%s\\n' 'home-screen-' 'authenticated'",
    );
    await expectTerminalLine(page, "home-screen-authenticated");
    expect(page.context().pages()).toHaveLength(pages);
    expect(await page.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
    expect(
      await page.evaluate(() => localStorage.getItem("webmux.token")),
    ).toBe(token);
    await page
      .getByRole("button", { name: "返回会话列表", exact: true })
      .click();
    await expect(page).toHaveURL(`${server.baseURL}/`);
    await expect(link).toBeVisible();
    expect(await page.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
  } finally {
    await page.goto("about:blank");
    await server.close();
  }
});
