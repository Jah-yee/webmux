import { afterEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import ScratchPanelHarness from "./ScratchPanelHarness.svelte";
import { createTerminalDrafts } from "../../src/terminal-draft.svelte";

afterEach(() => cleanup());

const openPanel = () => page.getByRole("button", { name: /打开暂存/ }).click();

test("adds a scratch note and takes it into the draft without sending", async () => {
  const drafts = createTerminalDrafts();
  const draft = drafts.get("aaaaaaaa");
  await render(ScratchPanelHarness, { drafts });
  await openPanel();
  await expect
    .element(page.getByRole("dialog", { name: "暂存" }))
    .toBeVisible();
  await expect.element(page.getByText(/还没有暂存/)).toBeVisible();
  await page.getByLabelText("新建暂存").fill("稍后检查测试结果");
  await page.getByRole("button", { name: "添加暂存" }).click();
  await expect
    .element(page.getByLabelText("暂存第 1 条"))
    .toHaveValue("稍后检查测试结果");
  await page.getByRole("button", { name: "取用暂存第 1 条" }).click();
  expect(draft.text).toBe("稍后检查测试结果");
  expect(draft.scratch).toHaveLength(0);
  await expect
    .element(page.getByRole("dialog", { name: "暂存" }))
    .not.toBeInTheDocument();
});

test("asks how to combine scratch with a non-empty draft", async () => {
  const drafts = createTerminalDrafts();
  const draft = drafts.get("aaaaaaaa");
  drafts.setEnabled(draft, true);
  drafts.edit(draft, "current");
  drafts.addScratch(draft, "next");
  await render(ScratchPanelHarness, { drafts });
  await openPanel();
  await page.getByRole("button", { name: "取用暂存第 1 条" }).click();
  await expect
    .element(page.getByRole("alertdialog", { name: "当前草稿不为空" }))
    .toBeVisible();
  await page.getByRole("button", { name: "追加到当前草稿" }).click();
  expect(draft.text).toBe("current\nnext");
  expect(draft.scratch).toHaveLength(0);
});

test("replacing keeps the displaced draft as a new scratch entry", async () => {
  const drafts = createTerminalDrafts();
  const draft = drafts.get("aaaaaaaa");
  drafts.setEnabled(draft, true);
  drafts.edit(draft, "current");
  drafts.addScratch(draft, "next");
  await render(ScratchPanelHarness, { drafts });
  await openPanel();
  await page.getByRole("button", { name: "取用暂存第 1 条" }).click();
  await page.getByRole("button", { name: "替换当前草稿" }).click();
  expect(draft.text).toBe("next");
  expect(draft.scratch.map((item) => item.text)).toEqual(["current"]);
});

test("cancelling a take conflict leaves both targets unchanged", async () => {
  const drafts = createTerminalDrafts();
  const draft = drafts.get("aaaaaaaa");
  drafts.setEnabled(draft, true);
  drafts.edit(draft, "current");
  drafts.addScratch(draft, "next");
  await render(ScratchPanelHarness, { drafts });
  await openPanel();
  await page.getByRole("button", { name: "取用暂存第 1 条" }).click();
  await page.getByRole("button", { name: "取消" }).click();
  await expect
    .element(page.getByRole("alertdialog", { name: "当前草稿不为空" }))
    .not.toBeInTheDocument();
  expect(draft.text).toBe("current");
  expect(draft.scratch.map((item) => item.text)).toEqual(["next"]);
});

test("stashes the current draft and deletes or clears notes", async () => {
  const drafts = createTerminalDrafts();
  const draft = drafts.get("aaaaaaaa");
  drafts.setEnabled(draft, true);
  drafts.edit(draft, "current");
  drafts.addScratch(draft, "first");
  drafts.addScratch(draft, "second");
  await render(ScratchPanelHarness, { drafts });
  await openPanel();
  await page.getByRole("button", { name: "将当前草稿存入暂存" }).click();
  expect(draft.text).toBe("");
  expect(draft.scratch.map((item) => item.text)).toEqual([
    "first",
    "second",
    "current",
  ]);
  await page.getByRole("button", { name: "暂存第 1 条的更多操作" }).click();
  await page.getByRole("menuitem", { name: "删除暂存第 1 条" }).click();
  expect(draft.scratch.map((item) => item.text)).toEqual(["second", "current"]);
  await page.getByRole("button", { name: "清空暂存" }).click();
  await page.getByRole("button", { name: "确认清空" }).click();
  expect(draft.scratch).toHaveLength(0);
  await expect.element(page.getByText(/还没有暂存/)).toBeVisible();
});

test("marks scratch from an earlier session run", async () => {
  const drafts = createTerminalDrafts();
  const draft = drafts.get("aaaaaaaa");
  drafts.bindGeneration(draft, "fedcba9876543210");
  drafts.addScratch(draft, "old note");
  draft.scratchGeneration = "0123456789abcdef";
  await render(ScratchPanelHarness, { drafts });
  await openPanel();
  await expect.element(page.getByText(/来自较早的会话实例/)).toBeVisible();
});

test("pending submissions disable stashing and taking while keeping notes editable", async () => {
  const drafts = createTerminalDrafts();
  const draft = drafts.get("aaaaaaaa");
  drafts.setEnabled(draft, true);
  drafts.edit(draft, "pending");
  drafts.addScratch(draft, "later");
  drafts.beginSubmission(draft, "s-00000001");
  await render(ScratchPanelHarness, { drafts });
  await openPanel();
  await expect.element(page.getByText(/当前草稿有待确认的提交/)).toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "将当前草稿存入暂存" }))
    .toBeDisabled();
  await expect
    .element(page.getByRole("button", { name: "取用暂存第 1 条" }))
    .toBeDisabled();
  await page.getByLabelText("暂存第 1 条").fill("later edited");
  expect(draft.scratch[0].text).toBe("later edited");
  expect(draft.text).toBe("pending");
});

test("reorders notes through the menu and disables moves at the boundaries", async () => {
  const drafts = createTerminalDrafts();
  const draft = drafts.get("aaaaaaaa");
  drafts.addScratch(draft, "first");
  drafts.addScratch(draft, "second");
  await render(ScratchPanelHarness, { drafts });
  await openPanel();
  await expect
    .element(page.getByRole("button", { name: "关闭暂存" }))
    .toHaveFocus();
  await page.getByRole("button", { name: "暂存第 1 条的更多操作" }).click();
  await expect
    .element(page.getByRole("menuitem", { name: "上移暂存第 1 条" }))
    .toHaveAttribute("aria-disabled", "true");
  await page.getByRole("menuitem", { name: "下移暂存第 1 条" }).click();
  expect(draft.scratch.map((item) => item.text)).toEqual(["second", "first"]);
  await page.getByRole("button", { name: "暂存第 2 条的更多操作" }).click();
  await expect
    .element(page.getByRole("menuitem", { name: "下移暂存第 2 条" }))
    .toHaveAttribute("aria-disabled", "true");
  await page.getByRole("menuitem", { name: "上移暂存第 2 条" }).click();
  expect(draft.scratch.map((item) => item.text)).toEqual(["first", "second"]);
});
