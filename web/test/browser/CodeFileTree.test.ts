import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import CodeFileTree from "../../src/components/CodeFileTree.svelte";
import { nodeId } from "../../src/code-reference";
import type { CodeListing } from "../../src/schemas";

function listing(paths: string[], nextCursor = ""): CodeListing {
  return {
    children: paths.map((entry) => {
      const directory = entry.endsWith("/");
      const path = entry.replace(/\/$/, "");
      return {
        nodeId: nodeId(path),
        path,
        name: path.split("/").at(-1)!,
        kind: directory ? "directory" : "file",
        gitStatus: "",
        ignored: false,
        referenceable: !directory,
        mediaType: "",
      };
    }),
    nextCursor,
    complete: true,
    limited: false,
    contentVersion: "v1",
    checkedAt: "2026-10-25T00:00:00Z",
  };
}
function setup(
  fetchDirectory: (
    path: string,
    cursor: string,
    signal: AbortSignal,
  ) => Promise<CodeListing>,
  directory = "",
) {
  const openFile = vi.fn();
  const view = render(CodeFileTree, {
    listing: listing(["docs/", "web/", "README.md"]),
    rootPath: "/workspace/webmux",
    pending: false,
    activePath: "docs/architecture.md",
    directory,
    refresh: 0,
    collapse: 0,
    fetchDirectory,
    openFile,
    loadMore: vi.fn(),
  });
  return { openFile, view };
}
afterEach(async () => {
  await cleanup();
  await userEvent.cleanup();
});

test("expands independent branches, preserves siblings, paginates and opens opaque file IDs", async () => {
  const fetchDirectory = vi.fn(async (path: string, cursor: string) =>
    path === "docs"
      ? listing(
          [cursor ? "docs/usage.md" : "docs/architecture.md"],
          cursor ? "" : "next",
        )
      : listing(["web/App.svelte"]),
  );
  const { openFile: open } = setup(fetchDirectory);
  await page.getByRole("button", { name: "docs", exact: true }).click();
  await page.getByRole("button", { name: "web", exact: true }).click();
  await expect
    .element(page.getByRole("button", { name: "architecture.md" }))
    .toHaveAttribute("aria-current", "page");
  await expect
    .element(page.getByRole("button", { name: "README.md" }))
    .toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "App.svelte" }))
    .toBeVisible();
  await page.getByRole("button", { name: "下一页" }).click();
  await expect
    .element(page.getByRole("button", { name: "usage.md" }))
    .toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "architecture.md" }))
    .toBeVisible();
  expect(fetchDirectory.mock.calls.map((call) => call.slice(0, 2))).toEqual([
    ["docs", ""],
    ["web", ""],
    ["docs", "next"],
  ]);
  await page.getByRole("button", { name: "architecture.md" }).click();
  expect(open).toHaveBeenCalledWith(
    "docs/architecture.md",
    nodeId("docs/architecture.md"),
  );
  await page.getByRole("button", { name: "docs", exact: true }).click();
  await expect
    .element(page.getByRole("button", { name: "architecture.md" }))
    .not.toBeInTheDocument();
  await page.getByRole("button", { name: "docs", exact: true }).click();
  expect(fetchDirectory).toHaveBeenCalledTimes(3);
});

test("arrow navigation expands folders, enters children, and returns to the parent", async () => {
  setup(async () => listing(["docs/architecture.md"]));
  const folder = page.getByRole("button", { name: "docs", exact: true });
  folder.element().focus();
  await userEvent.keyboard("{ArrowRight}");
  const file = page.getByRole("button", { name: "architecture.md" });
  await expect.element(file).toBeVisible();
  await userEvent.keyboard("{ArrowRight}");
  await expect.element(file).toHaveFocus();
  await userEvent.keyboard("{ArrowLeft}");
  await expect.element(folder).toHaveFocus();
  await userEvent.keyboard("{ArrowLeft}");
  await expect.element(folder).toHaveAttribute("aria-expanded", "false");
  await userEvent.keyboard("{End}");
  await expect
    .element(page.getByRole("button", { name: "README.md" }))
    .toHaveFocus();
});

test("a branch failure can be retried without hiding sibling folders", async () => {
  const fetchDirectory = vi
    .fn()
    .mockRejectedValueOnce(new Error("directory unavailable"))
    .mockResolvedValue(listing(["docs/architecture.md"]));
  setup(fetchDirectory);
  await page.getByRole("button", { name: "docs", exact: true }).click();
  await expect
    .element(page.getByRole("alert"))
    .toHaveTextContent("directory unavailable");
  await expect
    .element(page.getByRole("button", { name: "web", exact: true }))
    .toBeVisible();
  await page.getByRole("button", { name: "重试加载目录" }).click();
  await expect
    .element(page.getByRole("button", { name: "architecture.md" }))
    .toBeVisible();
});

test("locating a directory expands every ancestor and cancels in-flight reads on teardown", async () => {
  const signals: AbortSignal[] = [];
  setup(async (path, _cursor, signal) => {
    signals.push(signal);
    return path === "docs" ? listing(["docs/images/"]) : new Promise(() => {});
  }, "docs/images");
  await expect
    .element(page.getByRole("button", { name: "images", exact: true }))
    .toHaveAttribute("aria-expanded", "true");
  expect(signals).toHaveLength(2);
  await cleanup();
  expect(signals.at(-1)?.aborted).toBe(true);
});

test("refresh replaces expanded branch data and collapse hides all descendants", async () => {
  const fetchDirectory = vi
    .fn()
    .mockResolvedValueOnce(listing(["docs/old.md"]))
    .mockResolvedValue(listing(["docs/new.md"]));
  const { view: rendering } = setup(fetchDirectory);
  const view = await rendering;
  await page.getByRole("button", { name: "docs", exact: true }).click();
  await expect
    .element(page.getByRole("button", { name: "old.md" }))
    .toBeVisible();
  await view.rerender({ refresh: 1 });
  await expect
    .element(page.getByRole("button", { name: "new.md" }))
    .toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "old.md" }))
    .not.toBeInTheDocument();
  await expect
    .element(page.getByRole("button", { name: "docs", exact: true }))
    .toHaveAttribute("aria-expanded", "true");
  await view.rerender({ collapse: 1 });
  await expect
    .element(page.getByRole("button", { name: "new.md" }))
    .not.toBeInTheDocument();
  await expect
    .element(page.getByRole("button", { name: "README.md" }))
    .toBeVisible();
});

test("locating a directory loads missing ancestor pages and locating root reopens it", async () => {
  const view = await render(CodeFileTree, {
    listing: listing(["README.md"], "root-next"),
    rootPath: "/workspace/webmux",
    pending: false,
    activePath: "",
    directory: "",
    refresh: 0,
    collapse: 0,
    fetchDirectory: async (path: string, cursor: string) =>
      path === "docs"
        ? listing(
            cursor ? ["docs/images/"] : ["docs/first.md"],
            cursor ? "" : "docs-next",
          )
        : listing(["docs/images/screenshot.png"]),
    openFile: () => {},
    loadMore: async () => {
      await view.rerender({ listing: listing(["README.md", "docs/"]) });
    },
  });
  await view.rerender({ directory: "docs/images" });
  await expect
    .element(page.getByRole("button", { name: "screenshot.png", exact: true }))
    .toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "README.md", exact: true }))
    .toBeVisible();
  const root = page.getByRole("button", { name: "webmux", exact: true });
  await root.click();
  await expect
    .element(page.getByRole("button", { name: "README.md", exact: true }))
    .not.toBeInTheDocument();
  await view.rerender({ directory: "", refresh: 1 });
  await expect
    .element(page.getByRole("button", { name: "README.md", exact: true }))
    .toBeVisible();
});
