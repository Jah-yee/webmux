import { createShell, expect, test } from "./fixtures";

test("the saved dark theme is applied before the application module loads", async ({
  page,
}) => {
  const moduleGate = Promise.withResolvers<void>();
  let delayed = false;
  await page.addInitScript(() => {
    localStorage.setItem("webmux.theme", "dark");
  });
  await page.route("**/assets/*.js", async (route) => {
    if (!delayed) {
      delayed = true;
      await moduleGate.promise;
    }
    await route.continue();
  });
  try {
    await page.goto("/", { waitUntil: "commit" });
    await expect(page.locator("html")).toHaveClass(/(?:^|\s)dark(?:\s|$)/);
    await expect(
      page.locator("header").getByRole("button", { name: "＋ 新建会话" }),
    ).toHaveCount(0);
  } finally {
    moduleGate.resolve();
  }
});

test("reduced motion disables skeleton and dialog animation", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  const repositories = Promise.withResolvers<void>();
  await page.route("**/api/repositories", async (route) => {
    await repositories.promise;
    await route.continue();
  });
  try {
    await page.goto("/");
    const skeleton = page.locator('[data-slot="skeleton"]').first();
    await expect(skeleton).toBeVisible();
    expect(
      await skeleton.evaluate(
        (node) => getComputedStyle(node).animationIterationCount,
      ),
    ).toBe("1");
    expect(
      await skeleton.evaluate((node) =>
        parseFloat(getComputedStyle(node).animationDuration),
      ),
    ).toBeLessThanOrEqual(0.001);
    await page
      .locator("header")
      .getByRole("button", { name: "＋ 新建会话" })
      .click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    expect(
      await dialog.evaluate((node) =>
        parseFloat(getComputedStyle(node).transitionDuration),
      ),
    ).toBeLessThanOrEqual(0.001);
  } finally {
    repositories.resolve();
  }
});

test("returning to the workspace focuses its main region", async ({
  page,
  workDir,
}) => {
  await createShell(page, "Route focus", workDir);
  await page.getByRole("button", { name: "返回会话列表", exact: true }).click();
  await expect(page.getByRole("searchbox", { name: "搜索会话" })).toBeVisible();
  await expect(page.locator("main")).toBeFocused();
});
