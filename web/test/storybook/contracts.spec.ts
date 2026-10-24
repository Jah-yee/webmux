import { expect, test } from "@playwright/test";
import { overflowViolations } from "./overflow";
import { stories } from "./cases";
import { assertRequiredButtons } from "./controls";

test("the production diff bundles its highlighting worker", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/api/**", (route) => route.abort());
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(
    "/iframe.html?id=workspace-code-diff--review&viewMode=story&globals=theme:dark",
  );
  // The package's sideEffects metadata can silently strip a Worker wrapper.
  // A built preview must produce both actual tokens and inline word changes.
  const diff = page.locator(".code-diff-reader");
  await expect(diff.locator("[data-line] span[style]").first()).toBeVisible();
  await expect(diff.locator("[data-diff-span]").first()).toBeVisible();
  await expect(diff.locator("pre")).toHaveAttribute("data-diff-type", "split");
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(diff.locator("pre")).toHaveAttribute("data-diff-type", "single");
  await page.getByRole("button", { name: "自动换行" }).click();
  await expect(diff.locator("[data-line] span[style]").first()).toBeVisible();
  expect(errors).toEqual([]);
});

test("the overflow detector rejects content hidden by a clipped ancestor", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await page.setContent(
    '<style>body { margin:0; overflow:hidden } main { width:100%; overflow:hidden }</style><main><div style="width:2000px">layout regression</div></main>',
  );
  expect(await overflowViolations(page)).not.toEqual([]);
});

test("hiding a required action fails the reachability contract", async ({
  page,
}) => {
  await page.setContent(
    "<main><button>清空草稿</button><button>发送</button></main>",
  );
  const controls = page.locator("main");
  await assertRequiredButtons(controls, "draft-composer");
  await page
    .getByRole("button", { name: "发送", exact: true })
    .evaluate((button) => {
      button.style.display = "none";
    });
  await expect(
    assertRequiredButtons(controls, "draft-composer"),
  ).rejects.toThrow();
});

test("every overflow story has a regression case", async ({ request }) => {
  const response = await request.get("/index.json");
  expect(response.ok()).toBe(true);
  const index = await response.json();
  const ids = Object.values(
    index.entries as Record<string, { id: string; type: string }>,
  )
    .filter(
      (entry) => entry.type === "story" && entry.id.startsWith("ui-overflow--"),
    )
    .map((entry) => entry.id)
    .sort();
  expect(ids).toEqual(stories.map((name) => `ui-overflow--${name}`).sort());
});

test("the detector checks text spilling from a zero-width flex child", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await page.setContent(
    `<style>body { margin:0; overflow:hidden } main { display:flex; width:320px; overflow:hidden } span { width:0; flex:none }</style><main><span>${"unbroken_identifier_".repeat(40)}</span></main>`,
  );
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(
    320,
  );
  expect(await overflowViolations(page)).not.toEqual([]);
});

test("the detector rejects text clipped inside the viewport", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await page.setContent(
    '<main style="width:32px; overflow:hidden"><span>clipped_identifier</span></main>',
  );
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(
    320,
  );
  expect(await overflowViolations(page)).not.toEqual([]);
});

test("the detector rejects horizontal text clipping in a vertical scroller", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await page.setContent(
    '<style>body { margin:0 }</style><main style="width:32px; overflow-y:auto"><span>clipped_identifier</span></main>',
  );
  expect(
    await page
      .locator("main")
      .evaluate((element) => getComputedStyle(element).overflowX),
  ).toBe("auto");
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(
    320,
  );
  expect(await overflowViolations(page)).not.toEqual([]);
});
