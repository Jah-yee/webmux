import { afterEach, expect, test, vi } from "vitest";
import { page } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import WorkspaceGroup from "../../src/components/WorkspaceGroup.svelte";
import type { Worktree } from "../../src/schemas";

const previousDark = document.documentElement.classList.contains("dark");
afterEach(() => {
  cleanup();
  document.documentElement.classList.toggle("dark", previousDark);
});

const worktree: Worktree = {
  id: "main",
  repositoryId: "repo-1",
  path: "/repos/project",
  kind: "main",
  branch: "main",
  head: "a".repeat(40),
  detached: false,
  locked: false,
  lockReason: "",
  prunable: false,
  available: true,
  dirty: false,
  hasUntracked: false,
  hasIgnored: false,
  error: "",
};

const transparent = "rgba(0, 0, 0, 0)";

for (const theme of ["light", "dark"])
  for (const available of [true, false])
    test(`${available ? "an available" : "an unavailable"} worktree's 启动会话 reads as a button before hover (${theme})`, async () => {
      document.documentElement.classList.toggle("dark", theme === "dark");
      const onstart = vi.fn();
      render(WorkspaceGroup, {
        title: "main",
        path: worktree.path,
        worktree: { ...worktree, available },
        sessions: [],
        now: Date.now(),
        onclose: () => {},
        onstart,
      });
      const button = page.getByRole("button", { name: "启动会话" });
      await expect.element(button).toBeVisible();
      // Touch screens never hover: the boundary must show at rest.
      const style = getComputedStyle(button.element());
      expect(
        (parseFloat(style.borderTopWidth) > 0 &&
          style.borderTopColor !== transparent) ||
          style.backgroundColor !== transparent,
      ).toBe(true);
      if (available) {
        await button.click();
        expect(onstart).toHaveBeenCalledOnce();
      } else await expect.element(button).toBeDisabled();
    });
