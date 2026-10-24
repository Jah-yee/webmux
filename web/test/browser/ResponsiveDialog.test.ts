import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import Harness from "./ResponsiveDialogHarness.svelte";

afterEach(async () => {
  try {
    await cleanup();
    await userEvent.cleanup();
  } finally {
    await page.viewport(1024, 900);
  }
});

const body = () => page.getByRole("button", { name: "表单操作" });
const title = () => page.getByText("编辑工作区", { exact: true });
const slot = (name: string) => body().element().closest(`[data-slot=${name}]`);

test("desktop renders a Dialog with the full description", async () => {
  const onclose = vi.fn();
  render(Harness, { onclose });
  await expect.element(body()).toBeVisible();
  expect(slot("dialog-content")).toBeTruthy();
  expect(slot("drawer-content")).toBeNull();
  await expect.element(page.getByText("桌面说明")).toBeVisible();
  await expect.element(page.getByText("手机说明")).not.toBeInTheDocument();
  await userEvent.keyboard("{Escape}");
  await vi.waitFor(() => expect(onclose).toHaveBeenCalledOnce());
});

test("narrow screens render a Drawer with the drawer description", async () => {
  await page.viewport(390, 844);
  const onclose = vi.fn();
  render(Harness, { onclose });
  await expect.element(body()).toBeVisible();
  expect(slot("drawer-content")).toBeTruthy();
  expect(slot("dialog-content")).toBeNull();
  await expect.element(page.getByText("手机说明")).toBeVisible();
  await expect.element(page.getByText("桌面说明")).not.toBeInTheDocument();
  await userEvent.keyboard("{Escape}");
  await vi.waitFor(() => expect(onclose).toHaveBeenCalledOnce());
});

test("the drawer header scrolls with the body unless pinned", async () => {
  await page.viewport(390, 844);
  const view = await render(Harness, { onclose: () => {} });
  await expect.element(body()).toBeVisible();
  let scroller = body().element().closest(".overflow-y-auto")!;
  expect(scroller.contains(title().element())).toBe(true);
  await view.rerender({ pinDrawerHeader: true });
  await vi.waitFor(() => {
    scroller = body().element().closest(".overflow-y-auto")!;
    expect(scroller).toBeTruthy();
    expect(scroller.contains(title().element())).toBe(false);
  });
});

test("busy blocks every desktop dismissal until the work ends", async () => {
  const onclose = vi.fn();
  const view = await render(Harness, { busy: true, onclose });
  await expect.element(body()).toBeVisible();
  await userEvent.keyboard("{Escape}");
  const overlay = document.querySelector<HTMLElement>(
    "[data-slot=dialog-overlay]",
  )!;
  await page.elementLocator(overlay).click({ position: { x: 4, y: 4 } });
  await page.getByRole("button", { name: "Close" }).click();
  await expect.element(body()).toBeVisible();
  expect(onclose).not.toHaveBeenCalled();
  await view.rerender({ busy: false });
  await userEvent.keyboard("{Escape}");
  await vi.waitFor(() => expect(onclose).toHaveBeenCalledOnce());
});

test("busy blocks drawer dismissal until the work ends", async () => {
  await page.viewport(390, 844);
  const onclose = vi.fn();
  const view = await render(Harness, { busy: true, onclose });
  await expect.element(body()).toBeVisible();
  await userEvent.keyboard("{Escape}");
  await expect.element(body()).toBeVisible();
  expect(onclose).not.toHaveBeenCalled();
  await view.rerender({ busy: false });
  await userEvent.keyboard("{Escape}");
  await vi.waitFor(() => expect(onclose).toHaveBeenCalledOnce());
});
