import { afterEach, beforeEach, expect, test, vi, type Mock } from "vitest";
import { page } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import Harness from "./FullscreenHarness.svelte";
import type { Fullscreen } from "../../src/hooks/fullscreen.svelte";

// The test iframe cannot enter real fullscreen, so the document API is
// replaced by a model that dispatches fullscreenchange like a browser.
let element: Element | null;
let enabled: boolean;
let request: Mock<(options?: FullscreenOptions) => Promise<void>>;
let exit: Mock<() => Promise<void>>;

function change(next: Element | null) {
  element = next;
  document.dispatchEvent(new Event("fullscreenchange"));
}

beforeEach(() => {
  element = null;
  enabled = true;
  request = vi.fn(async () => change(document.documentElement));
  exit = vi.fn(async () => change(null));
  Object.defineProperty(document, "fullscreenElement", {
    configurable: true,
    get: () => element,
  });
  Object.defineProperty(document, "fullscreenEnabled", {
    configurable: true,
    get: () => enabled,
  });
  Object.defineProperty(document, "exitFullscreen", {
    configurable: true,
    value: exit,
  });
  Object.defineProperty(document.documentElement, "requestFullscreen", {
    configurable: true,
    value: request,
  });
});

afterEach(async () => {
  await cleanup();
  for (const name of [
    "fullscreenElement",
    "fullscreenEnabled",
    "exitFullscreen",
  ])
    Reflect.deleteProperty(document, name);
  Reflect.deleteProperty(document.documentElement, "requestFullscreen");
});

async function mount() {
  const errors: string[] = [];
  let fullscreen: Fullscreen | undefined;
  const screen = await render(Harness, {
    errors,
    expose: (value) => (fullscreen = value),
  });
  return { errors, fullscreen: fullscreen!, unmount: () => screen.unmount() };
}

const state = () => page.getByRole("status", { name: "Fullscreen state" });

test("enters and leaves document fullscreen, following fullscreenchange", async () => {
  const { errors, fullscreen } = await mount();
  await fullscreen.toggle();
  expect(request).toHaveBeenCalledWith({ navigationUI: "hide" });
  await expect.element(state()).toHaveTextContent("fullscreen idle");
  await fullscreen.toggle();
  expect(exit).toHaveBeenCalledOnce();
  await expect.element(state()).toHaveTextContent("windowed idle");
  expect(errors).toEqual(["", ""]);

  // Fullscreen entered elsewhere is reflected too.
  change(document.documentElement);
  await expect.element(state()).toHaveTextContent("fullscreen idle");
});

test("unmounting leaves only fullscreen the page entered itself", async () => {
  const own = await mount();
  await own.fullscreen.toggle();
  await own.unmount();
  expect(exit).toHaveBeenCalledOnce();

  change(document.documentElement);
  const other = await mount();
  await other.unmount();
  expect(exit).toHaveBeenCalledOnce();
});

test("a request still pending at unmount is undone when it resolves", async () => {
  const pending = Promise.withResolvers<void>();
  request.mockImplementation(async () => {
    await pending.promise;
    change(document.documentElement);
  });
  const { errors, fullscreen, unmount } = await mount();
  const toggled = fullscreen.toggle();
  await unmount();
  expect(exit).not.toHaveBeenCalled();
  pending.resolve();
  await toggled;
  expect(exit).toHaveBeenCalledOnce();
  expect(element).toBeNull();
  expect(errors).toEqual([""]);
});

test("reports unsupported fullscreen and failed requests", async () => {
  enabled = false;
  const { errors, fullscreen, unmount } = await mount();
  await fullscreen.toggle();
  expect(request).not.toHaveBeenCalled();
  expect(errors.at(-1)).toMatch(/当前浏览器环境未开放网页全屏/);
  await expect.element(state()).toHaveTextContent("windowed idle");

  enabled = true;
  request.mockRejectedValueOnce(new Error("denied"));
  await fullscreen.toggle();
  expect(errors.at(-1)).toBe("切换全屏失败：denied");
  await expect.element(state()).toHaveTextContent("windowed idle");

  // A failed request does not count as fullscreen this page owns.
  change(document.documentElement);
  await unmount();
  expect(exit).not.toHaveBeenCalled();
});
