import { expect, test } from "@playwright/test";
import { overflowViolations } from "./overflow";
import { assertRequiredButtons } from "./controls";

import { stories } from "./cases";
import { layouts } from "./layouts";

for (const { name, viewport, theme, textScale } of layouts) {
  test.describe(name, () => {
    test.use({ viewport, colorScheme: theme });

    test("worktree heading keeps its width above mobile actions", async ({
      page,
    }) => {
      await page.goto(
        `/iframe.html?id=workspace-flat-layout--worktree-actions&viewMode=story&globals=theme:${theme}`,
      );
      const heading = page.getByRole("heading", {
        name: "codex/flatten-workspace-session-layout-for-mobile",
      });
      const start = page.getByRole("button", { name: "启动会话", exact: true });
      const menu = page.getByRole("button", { name: /^工作树菜单 / });
      await expect(heading).toBeVisible();
      await expect(start).toBeVisible();
      await expect(menu).toBeVisible();
      await page.evaluate(async (scale) => {
        document.documentElement.style.fontSize = `${Number(scale) * 100}%`;
        await document.fonts.ready;
      }, textScale);

      const layout = await heading.evaluate((element) => {
        const title = element.parentElement!.getBoundingClientRect();
        const group = element.closest("section")!.getBoundingClientRect();
        const actions =
          element.parentElement!.nextElementSibling!.getBoundingClientRect();
        return {
          title: { width: title.width, top: title.top, bottom: title.bottom },
          groupWidth: group.width,
          actions: { top: actions.top, bottom: actions.bottom },
          mobile: !matchMedia("(min-width: 40rem)").matches,
        };
      });
      if (layout.mobile) {
        expect(layout.title.width).toBeCloseTo(layout.groupWidth, 0);
        expect(layout.actions.top).toBeGreaterThanOrEqual(layout.title.bottom);
      } else {
        expect(layout.actions.top).toBeLessThan(layout.title.bottom);
        expect(layout.actions.bottom).toBeGreaterThan(layout.title.top);
      }
      await expect.poll(() => overflowViolations(page)).toEqual([]);
      await menu.click();
      await expect(
        page.getByRole("menuitem", { name: "从主页移除" }),
      ).toBeVisible();
    });

    for (const story of stories) {
      test(`${story} fits and keeps its controls reachable`, async ({
        page,
      }) => {
        const failures: string[] = [];
        page.on("pageerror", (error) => failures.push(error.message));
        await page.route("**/api/**", (route) => route.abort());
        await page.goto(
          `/iframe.html?id=ui-overflow--${story}&viewMode=story&globals=theme:${theme}`,
        );
        await expect(page.getByTestId("overflow-story")).toBeVisible();
        await page.evaluate(async (scale) => {
          document.documentElement.style.fontSize = `${Number(scale) * 100}%`;
          await document.fonts.ready;
        }, textScale);
        await expect(page.locator("html")).toHaveClass(
          theme === "dark" ? /dark/ : /^(?!.*\bdark\b)/,
        );
        if (story === "quick-command-error") {
          await page.getByRole("button", { name: "保存", exact: true }).click();
          await expect(page.getByRole("alert")).toBeVisible();
        }
        await page.evaluate(async () => {
          await Promise.all(
            document
              .getAnimations()
              .filter(
                (animation) =>
                  animation.effect?.getComputedTiming().endTime !== Infinity,
              )
              .map((animation) => animation.finished.catch(() => {})),
          );
        });
        await expect.poll(() => overflowViolations(page)).toEqual([]);

        const dialogs = page.locator('[role="dialog"], [role="alertdialog"]');
        if (await dialogs.count()) {
          const dialog = dialogs.first();
          const bounds = await dialog.boundingBox();
          expect(bounds).not.toBeNull();
          expect(bounds!.y).toBeGreaterThanOrEqual(-1);
          expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(
            page.viewportSize()!.height + 1,
          );
        }
        const controls = (await dialogs.count())
          ? dialogs.first()
          : page.getByTestId("overflow-story");
        await assertRequiredButtons(controls, story);
        for (const button of await controls.getByRole("button").all()) {
          await expect(button).toBeVisible();
          // Trial clicks already scroll and check stability. Keep an explicit
          // scroll for disabled controls, then assert full visibility in both cases.
          if (await button.isEnabled()) await button.click({ trial: true });
          else await button.scrollIntoViewIfNeeded();
          await expect(button).toBeInViewport({ ratio: 1 });
        }
        if (story === "workspace-dialog") {
          const consent = page.getByRole("checkbox", {
            name: "已检查工作区路径",
          });
          await consent.check();
          await expect(consent).toBeChecked();
        }
        if (story === "session-row")
          await expect(page.getByRole("link")).toHaveAccessibleName(
            /unbroken_identifier/,
          );
        if (story === "code-changes")
          await expect(page.getByRole("button").first()).toHaveAttribute(
            "title",
            /unbroken_identifier/,
          );
        expect(failures).toEqual([]);
      });
    }
  });
}

for (const { name, viewport, theme, textScale } of layouts) {
  test.describe(`scratch ${name}`, () => {
    test.use({ viewport, colorScheme: theme });

    test("keeps a full scratch list and its composer reachable", async ({
      page,
    }) => {
      await page.goto(
        `/iframe.html?id=workspace-scratch-notes--full&viewMode=story&globals=theme:${theme}`,
      );
      const dialog = page.getByRole("dialog", { name: "暂存", exact: true });
      await expect(dialog).toBeVisible();
      await page.evaluate(async (scale) => {
        document.documentElement.style.fontSize = `${scale * 100}%`;
        await document.fonts.ready;
      }, textScale);
      await expect.poll(() => overflowViolations(page)).toEqual([]);
      const add = page.getByRole("button", { name: "添加暂存", exact: true });
      await add.scrollIntoViewIfNeeded();
      await expect(add).toBeInViewport();
      await expect(add).toBeDisabled();
      await expect(page.getByText(/已达 20 条上限/)).toBeVisible();
      const last = page.getByRole("textbox", {
        name: "暂存第 20 条",
        exact: true,
      });
      await last.scrollIntoViewIfNeeded();
      await expect(last).toBeInViewport();
      await expect(add).toBeInViewport();
      await page
        .getByRole("button", { name: "暂存第 20 条的更多操作", exact: true })
        .click();
      await expect(
        page.getByRole("menuitem", { name: "下移暂存第 20 条", exact: true }),
      ).toBeDisabled();
      await page
        .getByRole("menuitem", { name: "删除暂存第 20 条", exact: true })
        .click();
      await page
        .getByRole("textbox", { name: "新建暂存", exact: true })
        .fill("稍后检查移动端");
      await expect(add).toBeEnabled();
      await add.click();
      await expect(last).toHaveValue("稍后检查移动端");
      await expect.poll(() => overflowViolations(page)).toEqual([]);
    });
  });
}
