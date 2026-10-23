import { afterEach, expect, test, vi } from "vitest";
import { page } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import TunnelSettings from "../../src/components/TunnelSettings.svelte";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

test("copies the displayed tunnel template", async () => {
  const writeText = vi
    .spyOn(navigator.clipboard, "writeText")
    .mockResolvedValue();
  render(TunnelSettings);
  await page.getByRole("button", { name: "复制配置模板" }).click();
  await expect.element(page.getByText("已复制")).toBeVisible();
  expect(writeText).toHaveBeenCalledExactlyOnceWith(
    page.getByLabelText("Named Tunnel 配置模板").element().textContent,
  );
});

test("reports a failed copy", async () => {
  vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
    new Error("denied"),
  );
  vi.spyOn(document, "execCommand").mockReturnValue(false);
  render(TunnelSettings);
  await page.getByRole("button", { name: "复制配置模板" }).click();
  await expect.element(page.getByRole("alert")).toBeVisible();
  await expect.element(page.getByText("复制配置模板")).toBeVisible();
});
