import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import SessionRow from "../../src/components/SessionRow.svelte";
import type { SessionInfo } from "../../src/schemas";

afterEach(async () => {
  await cleanup();
  await userEvent.cleanup();
});

const base: SessionInfo = {
  id: "abcdefgh",
  generation: "0123456789abcdef",
  title: "omp agent",
  cwd: "/work",
  preset: "omp",
  cmd: ["omp"],
  status: "waiting",
  exitCode: null,
  preview: "⠋ Working",
  createdAt: "2026-10-16T00:00:00Z",
  lastActiveAt: "2026-10-16T00:00:01Z",
  cols: 80,
  rows: 24,
  clients: 0,
  workspace: null,
  attention: null,
  reportedCwd: null,
};

test("an agent permission request is labelled precisely", async () => {
  render(SessionRow, {
    session: {
      ...base,
      attention: {
        kind: "permission",
        source: "agent",
        since: "2026-10-16T00:00:02Z",
      },
    },
    now: Date.parse("2026-10-16T00:00:03Z"),
    onclose: () => {},
  });
  await expect.element(page.getByText("等待授权")).toBeVisible();
  expect(page.getByText("可能在等你").elements()).toHaveLength(0);
  expect(
    document.querySelector(
      '[data-status="waiting"] [data-slot="session-status"][data-tone="warning"]',
    ),
  ).not.toBeNull();
});

test("heuristic waiting keeps the cautious label", async () => {
  render(SessionRow, {
    session: base,
    now: Date.parse("2026-10-16T00:00:03Z"),
    onclose: () => {},
  });
  await expect.element(page.getByText("可能在等你")).toBeVisible();
});

test("shows the shell directory without repeating the group path", async () => {
  render(SessionRow, {
    session: {
      ...base,
      cwd: "/work/main",
      reportedCwd: {
        path: "/work/main/src",
        host: "",
        remote: false,
        since: "2026-10-16T00:00:02Z",
      },
    },
    groupPath: "/work/main",
    now: Date.parse("2026-10-16T00:00:03Z"),
    onclose: () => {},
  });
  await expect.element(page.getByText("Shell 报告目录")).toBeVisible();
  await expect.element(page.getByText("/work/main/src")).toBeVisible();
  expect(page.getByText("启动目录").elements()).toHaveLength(0);
});

test("offers a copy action for the shell directory", async () => {
  render(SessionRow, {
    session: {
      ...base,
      cwd: "/work/main",
      reportedCwd: {
        path: "/work/main/src",
        host: "",
        remote: false,
        since: "2026-10-16T00:00:02Z",
      },
    },
    groupPath: "/work/main",
    now: Date.parse("2026-10-16T00:00:03Z"),
    onclose: () => {},
  });
  await page.getByRole("button", { name: /会话菜单/ }).click();
  await expect
    .element(page.getByRole("menuitem", { name: "复制 Shell 报告目录" }))
    .toBeVisible();
});

test("shows a launch directory below the worktree root", async () => {
  render(SessionRow, {
    session: { ...base, cwd: "/work/main/sub" },
    groupPath: "/work/main",
    now: Date.parse("2026-10-16T00:00:03Z"),
    onclose: () => {},
  });
  await expect.element(page.getByText("启动目录")).toBeVisible();
  await expect.element(page.getByText("/work/main/sub")).toBeVisible();
  expect(page.getByText("Shell 报告目录").elements()).toHaveLength(0);
});

test("marks a remote shell directory", async () => {
  render(SessionRow, {
    session: {
      ...base,
      cwd: "/work/main",
      reportedCwd: {
        path: "/srv/agent",
        host: "build.example",
        remote: true,
        since: "2026-10-16T00:00:02Z",
      },
    },
    groupPath: "/work/main",
    now: Date.parse("2026-10-16T00:00:03Z"),
    onclose: () => {},
  });
  await expect.element(page.getByText("Shell 报告目录")).toBeVisible();
  await expect.element(page.getByText("/srv/agent")).toBeVisible();
  await expect.element(page.getByText("远端 build.example")).toBeVisible();
});

test("status, connection and time read as one aligned, grouped line on a wide row", async () => {
  render(SessionRow, {
    session: { ...base, status: "exited", exitCode: 137 },
    now: Date.parse("2026-10-16T00:00:03Z"),
    onclose: () => {},
  });
  const labels = ["已退出 · code 137", "后台会话 · 刚刚"];
  await expect.element(page.getByText(labels[0])).toBeVisible();
  const text = labels.map((label) => {
    const element = page.getByText(label, { exact: true }).element();
    const range = document.createRange();
    range.selectNodeContents(element);
    return range.getBoundingClientRect();
  });
  // The badge's border and padding must not drop its text below the others.
  expect(Math.abs(text[1].top - text[0].top)).toBeLessThanOrEqual(0.5);
  // Both stay on one line; the trailing text sits right after the status
  // slot (sized for exit labels) instead of being spread across the row.
  expect(Math.round(text[1].height)).toBe(Math.round(text[0].height));
  expect(text[1].left).toBeGreaterThan(text[0].right);
  expect(text[1].left - text[0].left).toBeLessThanOrEqual(160);
});

test("a deferred menu keeps mouse, keyboard, Escape focus and close actions", async () => {
  const onclose = vi.fn();
  render(SessionRow, {
    session: base,
    now: Date.parse("2026-10-16T00:00:03Z"),
    onclose,
  });
  const trigger = page.getByRole("button", { name: "会话菜单 omp agent" });
  const copy = page.getByRole("menuitem", { name: "复制启动目录" });
  await trigger.click();
  await expect.element(copy).toBeVisible();
  await userEvent.keyboard("{Escape}");
  await expect.element(trigger).toHaveFocus();
  await expect.poll(() => copy.elements()).toHaveLength(0);
  await userEvent.keyboard("{ArrowDown}");
  await expect.element(copy).toHaveFocus();
  await userEvent.keyboard("{End}{Enter}");
  expect(onclose).toHaveBeenCalledExactlyOnceWith(base);
  await expect.element(trigger).toHaveFocus();
});
