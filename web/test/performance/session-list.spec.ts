import { test, expect } from "../e2e/fixtures";
import { observePerformance, performanceReport, settled } from "./performance";
import type { SessionInfo } from "../../src/schemas";

test("4x CPU: a busy session among 120 cards keeps order, filtering and input responsive", async ({
  page,
}, testInfo) => {
  await observePerformance(page);
  await page.route("**/api/config", async (route) => {
    const response = await route.fetch();
    const config = await response.json();
    await route.fulfill({
      response,
      json: { ...config, hostname: "demo-host" },
    });
  });
  const alphabet = "abcdefghijklmnopqrstuvwxyz234567";
  const sessions: SessionInfo[] = Array.from({ length: 120 }, (_, index) => ({
    id: "aaaaaa" + alphabet[Math.floor(index / 32)] + alphabet[index % 32],
    generation: "0123456789abcdef",
    title: `load-${index}`,
    cwd: "/work",
    preset: "shell",
    cmd: ["bash"],
    status: "running",
    exitCode: null,
    preview: `ready-${index}\nstatus`,
    createdAt: new Date(Date.UTC(2026, 9, 24, 0, 0, index)).toISOString(),
    lastActiveAt: new Date().toISOString(),
    cols: 80,
    rows: 24,
    clients: 0,
    workspace: null,
    attention: null,
    reportedCwd: null,
  }));
  let update: (session: SessionInfo) => void = () => {
    throw new Error("Missing list socket");
  };
  let remove: (id: string) => void = () => {
    throw new Error("Missing list socket");
  };
  await page.routeWebSocket("**/ws/sessions*", (socket) => {
    socket.send(JSON.stringify({ type: "snapshot", sessions }));
    remove = (id) => socket.send(JSON.stringify({ type: "removed", id }));
    update = (session) =>
      socket.send(JSON.stringify({ type: "updated", session }));
  });
  await page.goto("/");
  await expect(page.locator("[data-session-id]:visible")).toHaveCount(120);
  const order = () =>
    page
      .locator("[data-session-id]:visible")
      .evaluateAll((rows) =>
        rows.map((row) => row.getAttribute("data-session-id")),
      );
  const before = await order();
  const retained = page.locator(`[data-session-id="${sessions[0].id}"]`);
  await retained.evaluate((node) =>
    node.setAttribute("data-retention-probe", "same-row"),
  );
  await settled(page);
  // This scenario measures steady-state list updates. Cold startup and delayed
  // fonts are covered separately by performance.spec.ts using real sessions.
  await page.evaluate(() => {
    window.webmuxPerformance.shifts = [];
    window.webmuxPerformance.interactions = [];
    window.webmuxPerformance.interactionEvents = [];
    window.webmuxPerformance.longTasks = [];
  });
  const started = performance.now();
  for (let index = 0; index < 60; index++) {
    update({
      ...sessions[31],
      preview: `updated-${index}\nstatus`,
      status: "waiting",
    });
  }
  const row = page.locator(`[data-session-id="${sessions[31].id}"]`);
  await expect(row.getByLabel("终端输出预览")).toContainText("updated-59");
  const updateBurstMs = performance.now() - started;
  expect(await order()).toEqual(before);
  await expect(row).toHaveAttribute("data-status", "waiting");

  const filters = page.getByRole("group", { name: "按会话状态筛选" });
  await filters.getByRole("button", { name: /等待中/ }).click();
  await row.getByRole("button", { name: /会话菜单/ }).click();
  await expect(page.getByRole("menu")).toBeVisible();
  update({ ...sessions[31], preview: "updated-59\nstatus", status: "running" });
  await expect(row).toBeHidden();
  await expect(page.getByRole("menu")).toBeHidden();
  await filters.getByRole("button", { name: /全部/ }).click();
  await expect(row).toBeVisible();
  await expect(page.getByRole("menu")).toBeHidden();

  const search = page.getByRole("searchbox", { name: "搜索会话" });
  // Real key events carry the same recent-input attribution as manual typing.
  await search.click();
  await page.keyboard.type("load-31");
  await expect(page.locator("[data-session-id]:visible")).toHaveCount(1);
  await expect(
    page.getByRole("link", { name: "load-31", exact: true }),
  ).toBeVisible();
  await expect(retained).toBeHidden();
  await expect(page.locator("[data-session-id]")).toHaveCount(120);
  await search.press("Escape");
  await expect(page.locator("[data-session-id]:visible")).toHaveCount(120);
  expect(await order()).toEqual(before);
  await expect(retained).toHaveAttribute("data-retention-probe", "same-row");
  const report = await performanceReport(
    page,
    testInfo,
    "many-session-updates",
    {
      updateBurstMs,
    },
  );
  expect(report.maxInteractionMs).toBeLessThanOrEqual(200);

  await search.click();
  await page.keyboard.type("load-31");
  for (const theme of ["light", "dark"]) {
    await page.evaluate((theme) => {
      document.documentElement.classList.toggle("dark", theme === "dark");
    }, theme);
    await row.getByRole("button", { name: /会话菜单/ }).click();
    await page.screenshot({
      path: testInfo.outputPath(`${theme}-session-menu.png`),
    });
    await page.keyboard.press("Escape");
  }
  remove(sessions[0].id);
  await expect(retained).toHaveCount(0);
});
