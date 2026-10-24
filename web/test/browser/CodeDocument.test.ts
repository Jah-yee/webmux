import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import Harness from "./CodeDocumentHarness.svelte";
import { computeDiff } from "../../src/code-diff";

const shadow = () => document.querySelector("diffs-container")?.shadowRoot;
const code = () => shadow()?.querySelector("pre")?.textContent ?? "";
const gutter = (side: "deletion" | "addition", line: number) =>
  page.elementLocator(
    shadow()!.querySelector(
      `[data-line-type="change-${side}"][data-column-number="${line}"]`,
    )!,
  );
const send = () => page.getByRole("button", { name: /发送位置到终端/ }).click();
const inserted = () => page.getByLabelText("Inserted reference");

afterEach(async () => {
  await cleanup();
  await userEvent.cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem("webmux.codeDiffStyle");
  localStorage.removeItem("webmux.codeDiffOverflow");
  localStorage.removeItem("webmux.codeLineNumbers");
});

test("unchanged content cannot navigate to invisible lines and retains whole-file references", async () => {
  render(Harness, { unchanged: true });
  await expect.element(page.getByText(/文件内容没有变化；/)).toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "文件内查找" }))
    .toBeDisabled();
  const fullFile = page.getByRole("button", { name: "完整文件", exact: true });
  (fullFile.element() as HTMLElement).focus();
  await userEvent.keyboard("{Enter}");
  await expect
    .element(page.getByRole("menuitem", { name: /修改前的文件/ }))
    .toBeVisible();
  await expect
    .element(page.getByRole("menuitem", { name: /修改后的文件/ }))
    .toBeVisible();
  await userEvent.keyboard("{Escape}");
  await expect.element(fullFile).toHaveFocus();
  document.querySelector<HTMLElement>(".code-diff-reader")!.focus();
  await userEvent.keyboard("{Control>}f{/Control}");
  await userEvent.keyboard("{Control>}{Alt>}g{/Alt}{/Control}");
  await userEvent.keyboard("{F3}");
  expect(document.querySelector(".code-search, .code-goto-line")).toBeNull();
  await send();
  await expect
    .poll(() =>
      document
        .querySelector('[aria-label="Inserted reference"]')
        ?.textContent?.trim(),
    )
    .toBe("/repo/code.txt");
});

test("diff keeps line numbers visible without inheriting obsolete display preferences", async () => {
  localStorage.setItem("webmux.codeLineNumbers", "hide");
  localStorage.setItem("webmux.codeDiffStyle", "split");
  render(Harness);
  await expect.poll(code, { timeout: 10000 }).toContain("changed revision 1");
  expect(shadow()!.querySelector("pre")?.dataset.diffType).toBe("single");
  await expect.element(gutter("addition", 50)).toBeVisible();
  await expect
    .element(page.getByText("点击行号选择；Shift 点击选择范围"))
    .toBeVisible();
  await expect.element(page.getByLabelText("引用来源")).not.toBeInTheDocument();
  await expect
    .element(page.getByRole("button", { name: "引用整个文件" }))
    .not.toBeInTheDocument();
});

test("expired snapshots retain content and disable mouse and keyboard expansion", async () => {
  render(Harness);
  await expect.poll(code, { timeout: 10000 }).toContain("changed revision 1");
  const expand = page.getByRole("button", { name: /展开 30 行/ }).first();
  await expect.element(expand).toHaveAttribute("aria-disabled", "false");
  shadow()!.querySelector<HTMLElement>("[data-expand-button]")!.focus();
  await userEvent.keyboard("{Enter}");
  await expect.poll(code).toContain("ordinary line 20");
  await page.getByRole("button", { name: "Expire snapshot" }).click();
  await expect.element(expand).toHaveAttribute("aria-disabled", "true");
  const before = code();
  await page
    .elementLocator(shadow()!.querySelector("[data-unmodified-lines]")!)
    .click();
  expect(code()).toBe(before);
  await expect
    .element(page.getByRole("button", { name: /发送位置到终端/ }))
    .toBeDisabled();
});

test("syntax and inline changes are highlighted in a Worker without interpreting source HTML", async () => {
  render(Harness, { syntax: true });
  await expect
    .poll(code, { timeout: 10000 })
    .toContain("<img src=x onerror=alert(1)>");
  await expect
    .poll(() => shadow()?.querySelectorAll("[data-diff-span]").length ?? 0, {
      timeout: 10000,
    })
    .toBeGreaterThan(0);
  await expect
    .poll(() => shadow()!.querySelectorAll("[data-line] span[style]").length, {
      timeout: 10000,
    })
    .toBeGreaterThan(0);
  expect(shadow()!.querySelector("img, script, [onerror]")).toBeNull();
});

test("real Worker, local context expansion, side-aware references and snapshot replacement", async () => {
  const fetch = vi.spyOn(window, "fetch");
  render(Harness);
  await expect.poll(code, { timeout: 10000 }).toContain("changed revision 1");
  // No selection or layout change should be needed to make initial controls
  // accessible, including when the highlighter has nothing to add.
  await expect
    .element(page.getByRole("button", { name: "向下展开 30 行" }).first())
    .toHaveAttribute("tabindex", "0");
  await gutter("deletion", 50).click();
  await send();
  await expect
    .element(inserted())
    .toHaveTextContent("/repo/code.txt:50（索引版本）");
  await page
    .getByRole("button", { name: "向下展开 30 行", exact: true })
    .first()
    .click();
  await page
    .getByRole("button", { name: "向下展开 30 行", exact: true })
    .first()
    .click();
  const scroller = document.querySelector<HTMLElement>(".code-diff-reader")!;
  scroller.scrollTop = 0;
  await expect.poll(code, { timeout: 10000 }).toContain("ordinary line 1");
  await send();
  await expect
    .element(inserted())
    .toHaveTextContent("/repo/code.txt:50（索引版本）");
  expect(
    fetch.mock.calls.filter(([url]) => String(url).includes("/api/")),
  ).toEqual([]);
  await page.getByRole("button", { name: "Replace snapshot" }).click();
  await expect.poll(code, { timeout: 10000 }).toContain("changed revision 2");
  await send();
  await expect.element(inserted()).toHaveTextContent("/repo/code.txt");
});

test("responsive layout, wrap and line ranges preserve the actual selected source", async () => {
  render(Harness);
  await expect.poll(code, { timeout: 10000 }).toContain("changed revision 1");
  await page.getByRole("button", { name: "Toggle width" }).click();
  await expect
    .poll(() => shadow()?.querySelector("pre")?.dataset.diffType)
    .toBe("split");
  await gutter("addition", 50).click();
  await send();
  await expect.element(inserted()).toHaveTextContent("/repo/code.txt:50");
  await gutter("deletion", 50).click();
  await page.getByRole("button", { name: "Toggle width" }).click();
  await page.getByRole("button", { name: "自动换行", exact: true }).click();
  await expect
    .poll(() => shadow()?.querySelector("pre")?.dataset.overflow)
    .toBe("wrap");
  await send();
  await expect
    .element(inserted())
    .toHaveTextContent("/repo/code.txt:50（索引版本）");
  const oldRow = shadow()!.querySelector(
    '[data-line-type="change-deletion"][data-column-number="50"]',
  )!;
  await page.elementLocator(oldRow).click();
  // Same-side range through unchanged lines in the split view.
  await page.getByRole("button", { name: "Toggle width" }).click();
  await expect
    .poll(() => shadow()?.querySelector("pre")?.dataset.diffType)
    .toBe("split");
  const endSelector = '[data-deletions] [data-column-number="52"]';
  await expect
    .poll(() => Boolean(shadow()?.querySelector(endSelector)), {
      timeout: 10000,
    })
    .toBe(true);
  await page
    .elementLocator(shadow()!.querySelector(endSelector)!)
    .click({ modifiers: ["Shift"] });
  await send();
  await expect
    .element(inserted())
    .toHaveTextContent("/repo/code.txt:50-52（索引版本）");
  await page.getByRole("button", { name: "引用整个文件" }).click();
  await send();
  await expect.element(inserted()).toHaveTextContent("/repo/code.txt");
  await expect
    .element(page.getByRole("button", { name: "引用整个文件" }))
    .not.toBeInTheDocument();
});

test("large deletions stay virtualized, with source-line goto and search", async () => {
  render(Harness, { deleted: true });
  await expect.poll(code, { timeout: 10000 }).toContain("ordinary line 1");
  expect(shadow()!.querySelectorAll("[data-line]").length).toBeLessThan(300);
  await send();
  await expect
    .element(inserted())
    .toHaveTextContent("/repo/code.txt（索引版本）");
  await page.getByRole("button", { name: "完整文件", exact: true }).click();
  await expect
    .element(page.getByRole("menuitem", { name: /修改后的文件/ }))
    .not.toBeInTheDocument();
  await userEvent.keyboard("{Escape}");
  document.querySelector<HTMLElement>(".code-diff-reader")!.focus();
  await userEvent.keyboard("{Control>}{Alt>}g{/Alt}{/Control}");
  await page.getByRole("textbox", { name: "跳转到行" }).fill("19800");
  await userEvent.keyboard("{Enter}");
  await expect.poll(code, { timeout: 10000 }).toContain("ordinary line 19800");
  await send();
  await expect
    .element(inserted())
    .toHaveTextContent("/repo/code.txt:19800（索引版本）");
  expect(shadow()!.querySelectorAll("[data-line]").length).toBeLessThan(300);
  await page.getByRole("button", { name: "文件内查找" }).click();
  await page
    .getByRole("textbox", { name: "查找", exact: true })
    .fill("ordinary line 12345 ");
  await page.getByRole("button", { name: "下一个匹配", exact: true }).click();
  await expect.poll(code, { timeout: 10000 }).toContain("ordinary line 12345");
});

test("search can reveal collapsed context and initially searches backwards from the end", async () => {
  render(Harness);
  await expect.poll(code, { timeout: 10000 }).toContain("changed revision 1");
  await page.getByRole("button", { name: "文件内查找" }).click();
  await page
    .getByRole("textbox", { name: "查找", exact: true })
    .fill("ordinary line 1");
  await page.getByRole("button", { name: "上一个匹配", exact: true }).click();
  await expect.poll(code).toContain("ordinary line 120");
  await send();
  await expect.element(inserted()).toHaveTextContent("/repo/code.txt:120");
});

test("replacing or hiding a document cancels its Worker and ignores late results", async () => {
  const NativeWorker = Worker;
  class WorkerStub {
    static instances: WorkerStub[] = [];
    data?: { snapshotId: string };
    onmessage?: (event: { data: unknown }) => void;
    terminate = vi.fn();
    constructor(url: URL | string, options?: WorkerOptions) {
      if (!String(url).includes("code-diff.worker"))
        return new NativeWorker(url, options) as unknown as WorkerStub;
      WorkerStub.instances.push(this);
    }
    postMessage(data: { snapshotId: string }) {
      this.data = data;
    }
    reply(text: string) {
      this.onmessage?.({
        data: {
          snapshotId: this.data!.snapshotId,
          result: computeDiff("", text + "\n"),
        },
      });
    }
  }
  vi.stubGlobal("Worker", WorkerStub);
  render(Harness);
  await expect.poll(() => WorkerStub.instances.length).toBe(1);
  const obsolete = WorkerStub.instances[0];
  await page.getByRole("button", { name: "Replace snapshot" }).click();
  await expect.poll(() => WorkerStub.instances.length).toBe(2);
  expect(obsolete.terminate).toHaveBeenCalledTimes(1);
  WorkerStub.instances[1].reply("new snapshot result");
  await expect.poll(code, { timeout: 10000 }).toContain("new snapshot result");
  obsolete.reply("obsolete snapshot result");
  expect(code()).not.toContain("obsolete snapshot result");
  await page.getByRole("button", { name: "Replace snapshot" }).click();
  await expect.poll(() => WorkerStub.instances.length).toBe(3);
  await page.getByRole("button", { name: "Toggle active" }).click();
  expect(WorkerStub.instances[2].terminate).toHaveBeenCalledTimes(1);
  await page.getByRole("button", { name: "Toggle active" }).click();
  await expect.poll(() => WorkerStub.instances.length).toBe(4);
  WorkerStub.instances[3].reply("resumed snapshot result");
  await expect
    .poll(code, { timeout: 10000 })
    .toContain("resumed snapshot result");
  await page.getByRole("button", { name: "Replace snapshot" }).click();
  await expect.poll(() => WorkerStub.instances.length).toBe(5);
  const limited = WorkerStub.instances[4];
  limited.onmessage?.({
    data: {
      snapshotId: limited.data!.snapshotId,
      result: { kind: "limited", reason: "计算受限" },
    },
  });
  await expect.element(page.getByRole("status")).toHaveTextContent("计算受限");
  await expect
    .element(page.getByRole("button", { name: "完整文件", exact: true }))
    .toBeEnabled();
  await page.getByRole("button", { name: "重试差异计算" }).click();
  await expect.poll(() => WorkerStub.instances.length).toBe(6);
  WorkerStub.instances[5].reply("retried result");
  await expect.poll(code, { timeout: 10000 }).toContain("retried result");
});
