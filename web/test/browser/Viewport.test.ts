import { afterEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import Harness from "./ViewportHarness.svelte";

afterEach(async () => {
  await cleanup();
  window.scrollTo(0, 0);
});

const frames = () =>
  new Promise<void>((resolve) =>
    requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
  );

const shownHeight = () =>
  Number(
    page.getByRole("status", { name: "Viewport height" }).element().textContent,
  );

const shownKeyboard = () =>
  page.getByRole("status", { name: "Keyboard visibility" }).element()
    .textContent;

const shownDismissed = () =>
  page.getByRole("status", { name: "Keyboard dismissed" }).element()
    .textContent;

// Scroll snapping waits for composition to end, but keyboard height changes
// apply immediately: Android keyboards compose every word and can hide
// without ending the composition.
test("composition defers scroll snapping but never the viewport height", async () => {
  await page.viewport(400, 700);
  await render(Harness);
  await expect.poll(shownHeight).toBe(700);
  expect(document.documentElement.style.overflow).toBe("hidden");

  const input = page.getByRole("textbox", { name: "Composition input" });
  const field = input.element() as HTMLInputElement;
  field.focus();
  field.dispatchEvent(
    new CompositionEvent("compositionstart", { bubbles: true, data: "" }),
  );
  window.scrollTo(0, 50);
  window.dispatchEvent(new Event("resize"));
  await frames();
  expect(window.scrollY).toBe(50);

  await page.viewport(400, 420);
  await expect.poll(shownHeight).toBe(420);
  expect(window.scrollY).toBe(50);

  field.dispatchEvent(
    new CompositionEvent("compositionend", { bubbles: true, data: "你" }),
  );
  await expect.poll(() => window.scrollY).toBe(0);

  // An abandoned composition is released by blur.
  field.dispatchEvent(
    new CompositionEvent("compositionstart", { bubbles: true, data: "" }),
  );
  window.scrollTo(0, 50);
  window.dispatchEvent(new Event("resize"));
  await frames();
  expect(window.scrollY).toBe(50);
  field.blur();
  await expect.poll(() => window.scrollY).toBe(0);
});

test("unmount restores the document overflow", async () => {
  document.documentElement.style.overflow = "clip";
  const rendered = await render(Harness);
  expect(document.documentElement.style.overflow).toBe("hidden");
  await rendered.unmount();
  expect(document.documentElement.style.overflow).toBe("clip");
  document.documentElement.style.overflow = "";
});

// Android hides the IME with Back without blurring its editable, so touch
// handlers need to distinguish that state from an open keyboard. Browser chrome
// and rotation must not masquerade as an IME-sized viewport shrink.
test("keyboard visibility follows IME-sized height changes, not chrome or rotation", async () => {
  await page.viewport(400, 700);
  await render(Harness);
  await expect.poll(shownKeyboard).toBe("hidden");
  expect(await shownDismissed()).toBe("false");

  await page.viewport(400, 420);
  await expect.poll(shownKeyboard).toBe("visible");
  expect(await shownDismissed()).toBe("false");
  await page.viewport(400, 700);
  await expect.poll(shownKeyboard).toBe("hidden");
  // Android Back hides the IME but leaves focus in the editable.
  expect(await shownDismissed()).toBe("true");

  await page.viewport(400, 500);
  await expect.poll(shownKeyboard).toBe("visible");
  expect(await shownDismissed()).toBe("false");
  await page.viewport(400, 700);
  await expect.poll(shownKeyboard).toBe("hidden");
  expect(await shownDismissed()).toBe("true");

  // A browser toolbar appearing is smaller than an IME.
  await page.viewport(400, 630);
  await expect.poll(shownKeyboard).toBe("hidden");
  await page.viewport(400, 700);

  // Rotation and split-screen resizes change the width, so they stay unknown
  // until a later resize gives a comparable height.
  await page.viewport(700, 400);
  await expect.poll(shownKeyboard).toBe("unknown");
  await page.viewport(700, 700);
  await expect.poll(shownKeyboard).toBe("hidden");

  // Rotating while the IME is open invalidates the height baseline. Do not
  // fabricate a dismissal from the first post-rotation height.
  await page.viewport(700, 500);
  await expect.poll(shownKeyboard).toBe("visible");
  await page.viewport(400, 500);
  await expect.poll(shownKeyboard).toBe("unknown");
  await page.viewport(400, 800);
  await expect.poll(shownKeyboard).toBe("hidden");
  expect(await shownDismissed()).toBe("false");
});
