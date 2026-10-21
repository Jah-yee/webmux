import { afterEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import SessionRow from "../../src/components/SessionRow.svelte";
import type { SessionInfo } from "../../src/schemas";

afterEach(() => cleanup());

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

test("status, connection and time read as one aligned, grouped line on a wide row", async () => {
  render(SessionRow, {
    session: { ...base, status: "exited", exitCode: 137 },
    now: Date.parse("2026-10-16T00:00:03Z"),
    onclose: () => {},
  });
  const labels = ["已退出 · code 137", "后台会话", "刚刚"];
  await expect.element(page.getByText(labels[0])).toBeVisible();
  const text = labels.map((label) => {
    const element = page.getByText(label, { exact: true }).element();
    const range = document.createRange();
    range.selectNodeContents(element);
    return range.getBoundingClientRect();
  });
  // The badge's border and padding must not drop its text below the others.
  for (const box of text.slice(1))
    expect(Math.abs(box.top - text[0].top)).toBeLessThanOrEqual(0.5);
  // Each item stays on one line beside the previous one (one 9rem track and
  // gap apart) instead of being spread across the row.
  expect(new Set(text.map((box) => Math.round(box.height))).size).toBe(1);
  for (let index = 1; index < text.length; index++)
    expect(text[index].left - text[index - 1].left).toBeLessThanOrEqual(180);
});
