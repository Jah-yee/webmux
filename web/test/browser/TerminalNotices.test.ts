import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import TerminalNotices from "../../src/components/TerminalNotices.svelte";
import { createTerminalDrafts } from "../../src/terminal-draft.svelte";

afterEach(cleanup);

test("dismissed terminal alerts stay hidden without suppressing later alerts", async () => {
  const terminal = {
    control: undefined,
    controlling: false,
    connection: "connected" as const,
    exitCode: null,
    error: "History was discarded",
    claim: vi.fn(),
  };
  const view = await render(TerminalNotices, {
    terminal,
    uploads: { progress: null, cancel: vi.fn() },
    draft: createTerminalDrafts().get("notice-test"),
    draftSaved: "",
    error: "",
    notice: "",
    showScratch: vi.fn(),
    dismissScratch: vi.fn(),
    remove: vi.fn(),
  });
  const alert = page.getByRole("alert");
  const close = page.getByRole("button", { name: "关闭提示" });
  await expect.element(alert).toBeVisible();
  await close.click();
  await expect.element(alert).not.toBeInTheDocument();

  await view.rerender({ terminal: { ...terminal, error: "Connection lost" } });
  await expect.element(alert).toBeVisible();
  await userEvent.keyboard("{Tab}");
  await expect.element(close).toHaveFocus();
  await userEvent.keyboard("{Enter}");
  await expect.element(alert).not.toBeInTheDocument();

  await view.rerender({ terminal: { ...terminal, error: "" } });
  await view.rerender({ terminal });
  await expect.element(alert).toBeVisible();
});
