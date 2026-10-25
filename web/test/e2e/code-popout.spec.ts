import { execFileSync } from "node:child_process";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import type { Locator, Page } from "@playwright/test";
import { test, expect, createShell } from "./fixtures";

function git(root: string, ...args: string[]) {
  execFileSync("git", ["-C", root, ...args], {
    env: {
      ...process.env,
      GIT_AUTHOR_NAME: "E2E",
      GIT_AUTHOR_EMAIL: "e2e@example.com",
      GIT_COMMITTER_NAME: "E2E",
      GIT_COMMITTER_EMAIL: "e2e@example.com",
    },
  });
}
// The diff library styles its shadow DOM with constructed stylesheets, which
// a move between documents drops; an unstyled diff has a zero-height gutter.
function diffStyled(diff: Locator) {
  return diff.evaluate((element) => ({
    sheets: element.shadowRoot!.adoptedStyleSheets.length,
    gutter:
      element.shadowRoot!.querySelector<HTMLElement>("[data-gutter]")
        ?.offsetWidth ?? 0,
  }));
}

test("the code panel pops out into its own window and returns intact", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "chromium-desktop",
    "Document Picture-in-Picture is a desktop browser feature",
  );
  git(workDir, "init", "-q");
  await writeFile(join(workDir, "code.txt"), "first line\nsecond line\n");
  await writeFile(join(workDir, "other.txt"), "other document\n");
  git(workDir, "add", ".");
  git(workDir, "commit", "-qm", "initial");
  await writeFile(join(workDir, "code.txt"), "first line\nchanged line\n");
  await createShell(page, "Popout code", workDir);
  await page
    .locator(".terminal-header")
    .getByRole("button", { name: "代码", exact: true })
    .click();
  const panel = page.getByRole("region", { name: "代码工作区" });
  await page.getByRole("button", { name: "other.txt", exact: true }).click();
  await expect(
    page.locator("[data-code-reader]:visible .cm-content"),
  ).toContainText("other document");

  const opened = page.context().waitForEvent("page");
  await page
    .getByRole("button", { name: "在独立窗口中显示代码", exact: true })
    .click();
  const pip = await opened;
  await expect(page.getByText("代码已在独立窗口中显示")).toBeVisible();
  await expect(panel).toHaveCount(0);
  const popped = pip.getByRole("region", { name: "代码工作区" });
  await expect(popped).toBeVisible();
  // The panel fills the window and keeps its styles and live editor.
  const size = await pip.evaluate(() => [innerWidth, innerHeight]);
  const box = (await popped.boundingBox())!;
  expect([box.width, box.height]).toEqual(size);
  await expect(pip.locator(".cm-editor").first()).toHaveCSS("display", "flex");
  await expect(
    pip.locator("[data-code-reader] .cm-content").first(),
  ).toContainText("other document");

  // The theme follows the opener.
  await page.evaluate(() => document.documentElement.classList.add("dark"));
  await expect(pip.locator("html")).toHaveClass(/\bdark\b/);
  await page.evaluate(() => document.documentElement.classList.remove("dark"));
  await expect(pip.locator("html")).not.toHaveClass(/\bdark\b/);

  // A diff opened inside the window is created by the opener's document and
  // still renders styled here.
  await pip.getByRole("tab", { name: "修改", exact: true }).click();
  await pip
    .getByRole("button", { name: "修改 code.txt", exact: true })
    .first()
    .click();
  await expect(pip.locator(".code-diff-reader").first()).toContainText(
    "changed line",
  );
  const poppedDiff = pip.locator("diffs-container").first();
  await expect.poll(async () => (await diffStyled(poppedDiff)).sheets).toBe(1);
  expect((await diffStyled(poppedDiff)).gutter).toBeGreaterThan(0);

  const closed = pip.waitForEvent("close");
  await pip.getByRole("button", { name: "收回代码窗口", exact: true }).click();
  await closed;
  await expect(page.getByText("代码已在独立窗口中显示")).toHaveCount(0);
  await expect(panel).toBeVisible();
  // Both documents survive the round trip with their styles.
  await expect(page.locator(".code-diff-reader:visible").first()).toContainText(
    "changed line",
  );
  const diff = page.locator("diffs-container:visible").first();
  await expect.poll(async () => (await diffStyled(diff)).sheets).toBe(1);
  expect((await diffStyled(diff)).gutter).toBeGreaterThan(0);
  await page.getByRole("tab", { name: /^other\.txt/ }).click();
  await expect(
    page.locator("[data-code-reader]:visible .cm-content"),
  ).toContainText("other document");
  // Editors rebound to the opener still respond to input.
  await page
    .locator("[data-code-reader]:visible .cm-lineNumbers .cm-gutterElement")
    .filter({ hasText: /^1$/ })
    .click();
  await expect(
    page.getByRole("button", { name: /^发送位置到/ }).filter({ visible: true }),
  ).toBeEnabled();
});

test("closing the opener's code view keeps the popped-out panel live", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "chromium-desktop",
    "Document Picture-in-Picture is a desktop browser feature",
  );
  await writeFile(join(workDir, "live.txt"), "before\n");
  await createShell(page, "Popout live", workDir);
  const toggle = page
    .locator(".terminal-header")
    .getByRole("button", { name: /^(代码|折叠代码)$/ });
  await toggle.click();
  const opened = page.context().waitForEvent("page");
  await page
    .getByRole("button", { name: "在独立窗口中显示代码", exact: true })
    .click();
  const pip = await opened;
  await pip.getByRole("button", { name: "live.txt", exact: true }).click();
  await expect(pip.locator(".cm-content")).toContainText("before");
  // Hiding the code pane in the opener must not pause the popped-out panel's
  // update checks.
  await toggle.click();
  await writeFile(join(workDir, "live.txt"), "after\n");
  await expect(
    pip.getByRole("tab", { name: /^live\.txt · worktree ●$/ }),
  ).toBeVisible({ timeout: 15_000 });
  await pip.close();
});

// Safari, Firefox and plain-HTTP origins have no Document Picture-in-Picture.
function withoutPictureInPicture(page: Page) {
  return page.addInitScript(() => {
    Reflect.deleteProperty(window, "documentPictureInPicture");
  });
}

test("without Picture-in-Picture a desktop browser pops out into an ordinary popup", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "chromium-desktop",
    "mobile browsers get no pop-out control without Picture-in-Picture",
  );
  await withoutPictureInPicture(page);
  await writeFile(join(workDir, "popup.txt"), "popup document\n");
  await createShell(page, "Popup code", workDir);
  await page
    .locator(".terminal-header")
    .getByRole("button", { name: "代码", exact: true })
    .click();
  expect(await page.evaluate(() => "documentPictureInPicture" in window)).toBe(
    false,
  );
  const opened = page.waitForEvent("popup");
  await page
    .getByRole("button", { name: "在独立窗口中显示代码", exact: true })
    .click();
  const popup = await opened;
  await expect(page.getByText("代码已在独立窗口中显示")).toBeVisible();
  await popup.getByRole("button", { name: "popup.txt", exact: true }).click();
  await expect(popup.locator(".cm-content")).toContainText("popup document");
  await expect(popup.locator(".cm-editor")).toHaveCSS("display", "flex");

  // Closing the popup with the window control returns the panel.
  await popup.close();
  await expect(page.getByText("代码已在独立窗口中显示")).toHaveCount(0);
  await expect(
    page.locator("[data-code-reader]:visible .cm-content"),
  ).toContainText("popup document");

  // Reloading the opener closes a popup instead of stranding the panel there.
  const again = page.waitForEvent("popup");
  await page
    .getByRole("button", { name: "在独立窗口中显示代码", exact: true })
    .click();
  const second = await again;
  const closed = second.waitForEvent("close");
  await page.reload();
  await closed;
});

test("mobile browsers without Picture-in-Picture offer no pop-out", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(testInfo.project.name === "chromium-desktop", "touch-only device");
  await withoutPictureInPicture(page);
  await writeFile(join(workDir, "mobile.txt"), "mobile document\n");
  await createShell(page, "Mobile code", workDir);
  await page.getByRole("button", { name: "代码", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "mobile.txt", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "在独立窗口中显示代码" }),
  ).toHaveCount(0);
});
