import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import { tick } from "svelte";
import Harness from "./DirectoryPickerHarness.svelte";

const manyDirectories = Array.from(
  { length: 60 },
  (_, index) =>
    `/many/dir-${String(index).padStart(2, "0")}${index === 59 ? "-a-very-long-directory-name".repeat(5) : ""}`,
);
const lastDirectory = manyDirectories[manyDirectories.length - 1];
const basename = (path: string) => path.split("/").pop()!;
type Reply = string[] | { status: number; error: string } | Promise<Response>;

const fixtures: Record<string, Reply> = {
  "/": ["/workspace"],
  "~/": ["~/home-project"],
  "/workspace/": [
    "/workspace/projects",
    "/workspace/visible",
    "/workspace/.private",
  ],
  "/workspace/projects/": ["/workspace/projects/src"],
  "/workspace/projects/src/": [],
  "/workspace/.": ["/workspace/.private"],
  "/many/": manyDirectories,
  [`${lastDirectory}/`]: [],
  "/denied/": { status: 403, error: "Permission denied by fixture" },
  "/recovered/": ["/recovered/available"],
  "/latest/": ["/latest/current-result"],
};
let unexpectedRequests: string[];

// Only the HTTP transport is replaced: the production API/schema, debounce,
// filtering, keyboard handlers, bindings and shadcn primitives all execute.
function serveDirectories(overrides: Record<string, Reply> = {}) {
  const replies = { ...fixtures, ...overrides };
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const url = new URL(
        input instanceof Request ? input.url : String(input),
        location.href,
      );
      const prefix = url.searchParams.get("prefix");
      if (
        url.pathname !== "/api/fs/dirs" ||
        (init?.method ?? "GET") !== "GET" ||
        prefix === null ||
        !Object.hasOwn(replies, prefix)
      ) {
        unexpectedRequests.push(url.pathname + url.search);
        throw new Error(`Unexpected directory request: ${url}`);
      }
      requests.push(prefix);
      const reply = replies[prefix];
      if (reply instanceof Promise) return reply;
      if (Array.isArray(reply)) return Response.json(reply);
      return Response.json({ error: reply.error }, { status: reply.status });
    }),
  );
  return requests;
}

const input = () => page.getByRole("combobox", { name: "目录路径" });
const confirm = () => page.getByRole("button", { name: "选择当前目录" });
const committed = () => page.getByRole("status", { name: "已确认目录" });
const option = (name: string) =>
  page.getByRole("option", { name, exact: true });

function element(selector: string): HTMLElement {
  const result = document.querySelector<HTMLElement>(selector);
  if (!result) throw new Error(`Missing picker element: ${selector}`);
  return result;
}
const slot = (name: string) => element(`[data-slot="${name}"]`);

async function openPicker(initialValue = "/workspace", anchorTop = 0) {
  await render(Harness, { initialValue, anchorTop });
  await page.getByRole("button", { name: "浏览目录" }).click();
  await expect
    .element(input())
    .toHaveValue(`${initialValue.replace(/\/+$/, "")}/`);
  await expect.element(input()).toHaveFocus();
  await expect.element(confirm()).toBeEnabled();
}

function within(inner: DOMRect, outer: DOMRect) {
  // A subpixel allowance handles browser rounding, not missing usable spacing.
  expect(inner.left).toBeGreaterThanOrEqual(outer.left - 1);
  expect(inner.right).toBeLessThanOrEqual(outer.right + 1);
  expect(inner.top).toBeGreaterThanOrEqual(outer.top - 1);
  expect(inner.bottom).toBeLessThanOrEqual(outer.bottom + 1);
}

function pickerGeometry() {
  const picker = slot("popover-content");
  const header = page
    .getByRole("button", { name: "上一级目录" })
    .element().parentElement!;
  const wrapper = slot("command-input-wrapper");
  const viewport = slot("scroll-area-viewport");
  const footer = confirm().element().parentElement!;
  return { picker, header, wrapper, viewport, footer };
}

function expectUsableGeometry() {
  const { picker, header, wrapper, viewport, footer } = pickerGeometry();
  const bounds = picker.getBoundingClientRect();
  for (const part of [header, wrapper, viewport, footer]) {
    within(part.getBoundingClientRect(), bounds);
  }
  const headerBounds = header.getBoundingClientRect();
  const inputBounds = wrapper.getBoundingClientRect();
  const listBounds = viewport.getBoundingClientRect();
  const footerBounds = footer.getBoundingClientRect();
  expect(inputBounds.top).toBeGreaterThanOrEqual(headerBounds.bottom);
  // Fractional layout can place an 8px gap at 7.99px.
  expect(
    Math.round(listBounds.top - inputBounds.bottom),
  ).toBeGreaterThanOrEqual(8);
  expect(
    Math.round(footerBounds.top - listBounds.bottom),
  ).toBeGreaterThanOrEqual(8);
  within(input().element().getBoundingClientRect(), inputBounds);
  within(confirm().element().getBoundingClientRect(), footerBounds);
  expect(bounds.left).toBeGreaterThanOrEqual(0);
  expect(bounds.right).toBeLessThanOrEqual(window.innerWidth);
  expect(bounds.top).toBeGreaterThanOrEqual(0);
  expect(bounds.bottom).toBeLessThanOrEqual(window.innerHeight);
}

beforeEach(async () => {
  unexpectedRequests = [];
  await page.viewport(1024, 900);
});

afterEach(async () => {
  try {
    await cleanup();
    await userEvent.cleanup();
  } finally {
    vi.unstubAllGlobals();
  }
  expect(unexpectedRequests).toEqual([]);
});

test("long directory lists have a visible scrollbar, usable spacing and independently scrolling content", async () => {
  serveDirectories();
  await openPicker("/many");
  await expect.element(option(basename(lastDirectory))).toBeInTheDocument();
  await expect
    .poll(() => slot("scroll-area-thumb").getBoundingClientRect().height)
    .toBeGreaterThan(0);
  expectUsableGeometry();

  const { picker, header, wrapper, viewport, footer } = pickerGeometry();
  const track = slot("scroll-area-scrollbar");
  const thumb = slot("scroll-area-thumb");
  await expect.element(page.elementLocator(track)).toBeVisible();
  await expect.element(page.elementLocator(thumb)).toBeVisible();
  expect(viewport.scrollHeight).toBeGreaterThan(viewport.clientHeight);
  expect(viewport.scrollTop).toBe(0);
  expect(getComputedStyle(viewport).overflowY).toMatch(/auto|scroll/);
  for (const control of [track, thumb]) {
    const style = getComputedStyle(control);
    expect(style.display).not.toBe("none");
    expect(style.visibility).toBe("visible");
    expect(Number(style.opacity)).toBeGreaterThan(0);
    expect(control.getBoundingClientRect().width).toBeGreaterThan(0);
  }
  expect(getComputedStyle(thumb).backgroundColor).not.toMatch(
    /^(transparent|rgba\(0, 0, 0, 0\))$/,
  );
  within(track.getBoundingClientRect(), picker.getBoundingClientRect());
  // Directory text has a reserved lane beside the visible scrollbar.
  expect(
    option(basename(lastDirectory)).element().getBoundingClientRect().right,
  ).toBeLessThanOrEqual(track.getBoundingClientRect().left + 1);

  const fixedBefore = [header, wrapper, footer].map((part) =>
    part.getBoundingClientRect(),
  );
  const pageScrollBefore = { x: window.scrollX, y: window.scrollY };
  await userEvent.wheel(viewport, { delta: { y: viewport.scrollHeight } });
  await expect.poll(() => viewport.scrollTop).toBeGreaterThan(0);
  await expect
    .poll(
      () =>
        option(basename(lastDirectory)).element().getBoundingClientRect()
          .bottom,
    )
    .toBeLessThanOrEqual(viewport.getBoundingClientRect().bottom + 1);
  within(
    option(basename(lastDirectory)).element().getBoundingClientRect(),
    viewport.getBoundingClientRect(),
  );
  [header, wrapper, footer].forEach((part, index) => {
    const after = part.getBoundingClientRect();
    expect(Math.abs(after.top - fixedBefore[index].top)).toBeLessThanOrEqual(1);
    expect(
      Math.abs(after.bottom - fixedBefore[index].bottom),
    ).toBeLessThanOrEqual(1);
  });
  expect(picker.scrollTop).toBe(0);
  expect(slot("command-list").scrollTop).toBe(0);
  expect({ x: window.scrollX, y: window.scrollY }).toEqual(pageScrollBefore);
  expectUsableGeometry();

  await option(basename(lastDirectory)).click();
  await expect.element(input()).toHaveValue(`${lastDirectory}/`);
  await expect.element(committed()).toHaveTextContent("/many");
  await expect.element(confirm()).toBeEnabled();
  await confirm().click();
  await expect.element(committed()).toHaveTextContent(lastDirectory);
  await expect.element(input()).not.toBeInTheDocument();
});

test("390px mobile picker keeps long names, header and choose action inside the viewport", async () => {
  serveDirectories();
  await page.viewport(390, 844);
  await openPicker("/many", 390);
  await expect.element(option(basename(lastDirectory))).toBeInTheDocument();
  expectUsableGeometry();
  expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(
    window.innerWidth,
  );
  const { viewport } = pickerGeometry();
  expect(viewport.scrollWidth).toBeLessThanOrEqual(viewport.clientWidth);
  await expect.element(confirm()).toBeVisible();
  const button = confirm().element().getBoundingClientRect();
  const hit = document.elementFromPoint(
    button.left + button.width / 2,
    button.top + button.height / 2,
  );
  expect(confirm().element().contains(hit)).toBe(true);
  await confirm().click();
  await expect.element(committed()).toHaveTextContent("/many");
});

test.each(["Enter", "Tab"])(
  "%s browses a child without committing until explicit confirmation",
  async (key) => {
    serveDirectories();
    await openPicker();
    await expect.element(option("projects")).toBeVisible();
    await expect
      .element(option("projects"))
      .toHaveAttribute("aria-selected", "true");
    await input().click();
    await userEvent.keyboard("{ArrowDown}");
    await expect
      .element(option("visible"))
      .toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{ArrowUp}");
    await expect
      .element(option("projects"))
      .toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard(`{${key}}`);
    await expect.element(input()).toHaveValue("/workspace/projects/");
    await expect.element(option("src")).toBeVisible();
    await expect.element(committed()).toHaveTextContent("/workspace");
    await expect.element(input()).toHaveFocus();
    await confirm().click();
    await expect.element(committed()).toHaveTextContent("/workspace/projects");
    await expect.element(input()).not.toBeInTheDocument();
    const trigger = page.getByRole("button", { name: "浏览目录" });
    await expect.element(trigger).toHaveFocus();
    await trigger.click();
    await expect.element(input()).toHaveValue("/workspace/projects/");
    await expect.element(input()).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    await expect.element(input()).not.toBeInTheDocument();
    await expect.element(trigger).toHaveFocus();
  },
);

test("parent button, Alt+ArrowUp, root and home navigation preserve the confirmed value", async () => {
  serveDirectories();
  await openPicker("/workspace/projects/src");
  await page.getByRole("button", { name: "上一级目录" }).click();
  await expect.element(input()).toHaveValue("/workspace/projects/");
  await expect.element(option("src")).toBeVisible();
  await input().click();
  await userEvent.keyboard("{Alt>}{ArrowUp}{/Alt}");
  await expect.element(input()).toHaveValue("/workspace/");
  await expect.element(option("projects")).toBeVisible();
  await page.getByRole("button", { name: "上一级目录" }).click();
  await expect.element(input()).toHaveValue("/");
  await expect.element(option("workspace")).toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "上一级目录" }))
    .toBeDisabled();
  await expect
    .element(committed())
    .toHaveTextContent("/workspace/projects/src");
  await confirm().click();
  await expect.poll(() => committed().element().textContent?.trim()).toBe("/");

  await page.getByRole("button", { name: "浏览目录" }).click();
  await page.getByRole("button", { name: "用户主目录" }).click();
  await expect.element(input()).toHaveValue("~/");
  await expect.element(option("home-project")).toBeVisible();
  await expect.poll(() => committed().element().textContent?.trim()).toBe("/");
  await confirm().click();
  await expect.poll(() => committed().element().textContent?.trim()).toBe("~");
});

test("hidden directories require the toggle or an explicit dot prefix", async () => {
  serveDirectories();
  await openPicker();
  await expect.element(option("projects")).toBeVisible();
  await expect.element(option(".private")).not.toBeInTheDocument();
  await page.getByRole("button", { name: "显示隐藏目录" }).click();
  await expect.element(option(".private")).toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "隐藏点目录" }))
    .toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "隐藏点目录" }).click();
  await expect.element(option(".private")).not.toBeInTheDocument();
  await input().fill("/workspace/.");
  await expect.element(option(".private")).toBeVisible();
  await expect.element(option("projects")).not.toBeInTheDocument();
  await expect.element(committed()).toHaveTextContent("/workspace");
});

test("directory errors disable confirmation and a successful new path recovers", async () => {
  serveDirectories();
  await openPicker();
  await input().fill("/denied/");
  await expect
    .element(page.getByRole("alert"))
    .toHaveTextContent("Permission denied by fixture");
  await expect.element(confirm()).toBeDisabled();
  await expect.element(committed()).toHaveTextContent("/workspace");
  await input().fill("/recovered/");
  await expect.element(option("available")).toBeVisible();
  await expect.element(page.getByRole("alert")).not.toBeInTheDocument();
  await expect.element(confirm()).toBeEnabled();
  await confirm().click();
  await expect.element(committed()).toHaveTextContent("/recovered");
});

test("an older delayed directory response cannot overwrite the latest path listing", async () => {
  let resolveSlow!: (response: Response) => void;
  const slow = new Promise<Response>((resolve) => {
    resolveSlow = resolve;
  });
  const requests = serveDirectories({ "/slow/": slow });
  try {
    await openPicker();
    await input().fill("/slow/");
    await expect.poll(() => requests.includes("/slow/")).toBe(true);
    await expect.element(confirm()).toBeDisabled();
    await input().fill("/latest/");
    await expect.element(option("current-result")).toBeVisible();
    await expect.element(confirm()).toBeEnabled();
    resolveSlow(Response.json(["/slow/obsolete-result"]));
    await slow;
    // Let the real fetch -> response.json -> schema -> reactive effect chain finish.
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => resolve()),
    );
    await tick();
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => resolve()),
    );
    await expect.element(input()).toHaveValue("/latest/");
    await expect.element(option("current-result")).toBeVisible();
    await expect.element(option("obsolete-result")).not.toBeInTheDocument();
    await expect.element(committed()).toHaveTextContent("/workspace");
    await confirm().click();
    await expect.element(committed()).toHaveTextContent("/latest");
  } finally {
    resolveSlow(Response.json([]));
  }
});
