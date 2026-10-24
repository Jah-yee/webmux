import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import CodeLineAction from "../../src/components/CodeLineAction.svelte";

afterEach(async () => {
  await cleanup();
  await userEvent.cleanup();
});

test("names the line and the insertion target; tooltip only on request", async () => {
  const onclick = vi.fn();
  const view = await render(CodeLineAction, {
    line: 12,
    draftEnabled: false,
    disabled: false,
    onclick,
  });
  const terminal = page.getByRole("button", { name: "发送第 12 行位置到终端" });
  await expect.element(terminal).not.toHaveAttribute("title");
  await terminal.click();
  expect(onclick).toHaveBeenCalledOnce();
  await view.rerender({ draftEnabled: true, tooltip: true });
  await expect
    .element(page.getByRole("button", { name: "发送第 12 行位置到草稿" }))
    .toHaveAttribute("title", "发送第 12 行位置到草稿");
});
