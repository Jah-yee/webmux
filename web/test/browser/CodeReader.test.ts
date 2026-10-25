import { afterEach, expect, test } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import Harness from "./CodeReaderHarness.svelte";
afterEach(async () => {
  await cleanup();
  await userEvent.cleanup();
});
test("read-only virtualized full file, real line selection, search and goto", async () => {
  const text = Array.from(
    { length: 1000 },
    (_, i) => `const line${i + 1} = ${i + 1};`,
  ).join("\n");
  render(Harness, { text });
  await expect
    .element(page.getByLabelText("只读代码"))
    .toMatchTextContent(new RegExp("const line1 = 1;"));
  expect(document.querySelectorAll(".cm-line").length).toBeLessThan(100);
  expect(
    document.querySelector(".cm-content")?.getAttribute("contenteditable"),
  ).toBe("false");
  const gutter = page.elementLocator(
    document.querySelector(".cm-lineNumbers")!,
  );
  await gutter.getByText("2", { exact: true }).click();
  await expect
    .element(page.getByLabelText("Selected reference"))
    .toMatchTextContent(new RegExp("2-2"));
  await gutter.getByText("4", { exact: true }).click({ modifiers: ["Shift"] });
  await expect
    .element(page.getByLabelText("Selected reference"))
    .toMatchTextContent(new RegExp("2-4"));
  await page.getByRole("button", { name: "跳转行" }).click();
  await page.getByRole("textbox", { name: "跳转到行" }).fill("900");
  await userEvent.keyboard("{Enter}");
  await expect
    .element(page.getByLabelText("只读代码"))
    .toMatchTextContent(new RegExp("const line900 = 900;"));
  await page.getByRole("button", { name: "文件内查找" }).click();
  await page.getByRole("textbox", { name: "查找" }).fill("line850");
  await page.getByRole("button", { name: "下一个匹配", exact: true }).click();
  await expect
    .element(page.getByLabelText("只读代码"))
    .toMatchTextContent(new RegExp("const line850 = 850;"));
});
test("renders HTML and control-looking content as code text", async () => {
  const text =
    '<img src="invalid" onerror="window.codeExecuted=true">\n<script>window.codeExecuted=true</script>\ncarriage\rreturn\x1b[31m text';
  render(Harness, { text, path: "file.html" });
  await expect
    .element(page.getByLabelText("只读代码"))
    .toMatchTextContent(new RegExp("window.codeExecuted"));
  expect(
    document.querySelector(
      '[data-code-reader] img[src="invalid"], [data-code-reader] [onerror], [data-code-reader] script',
    ),
  ).toBeNull();
  expect(
    (window as unknown as { codeExecuted?: boolean }).codeExecuted,
  ).toBeUndefined();
  expect(document.querySelectorAll(".cm-line")).toHaveLength(3);
});

test("keeps syntax highlighting across language and view extensions", async () => {
  render(Harness, {
    text: "const answer = 42; // highlighted",
    path: "test.ts",
  });
  const keyword = page
    .elementLocator(document.querySelector(".cm-content")!)
    .getByText("const", { exact: true });
  await expect.element(keyword).toBeVisible();
  await expect
    .poll(() => getComputedStyle(keyword.element()).color)
    .toBe("rgb(119, 0, 136)");
  const comment = page
    .elementLocator(document.querySelector(".cm-content")!)
    .getByText("// highlighted", { exact: true });
  await expect
    .poll(() => getComputedStyle(comment.element()).color)
    .toBe("rgb(153, 68, 0)");
});
