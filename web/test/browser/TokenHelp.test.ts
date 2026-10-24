import { afterEach, expect, test } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import TokenHelp from "../../src/components/TokenHelp.svelte";

afterEach(cleanup);

const trigger = () => page.getByRole("button", { name: "关于共享 Token" });
const help = () => page.getByRole("dialog", { name: "共享 Token 说明" });

test("help stays hidden until requested and can be dismissed with Escape", async () => {
  render(TokenHelp);
  await expect.element(help()).not.toBeInTheDocument();
  await userEvent.keyboard("{Tab}");
  await expect.element(trigger()).toHaveFocus();
  await userEvent.keyboard("{Enter}");
  await expect.element(help()).toBeVisible();
  await expect.element(trigger()).toHaveFocus();
  await userEvent.keyboard("{Escape}");
  await expect.element(help()).not.toBeInTheDocument();
  await expect.element(trigger()).toHaveFocus();
});

test("hovering the help content keeps it open for reading", async () => {
  render(TokenHelp);
  await userEvent.hover(trigger());
  await expect.element(help()).toBeVisible();
  await userEvent.hover(help());
  await expect
    .element(help())
    .toMatchTextContent(/~\/\.config\/webmux\/config\.toml/);
  await new Promise((resolve) => setTimeout(resolve, 200));
  await expect.element(help()).toBeVisible();
  await userEvent.keyboard("{Escape}");
  await expect.element(help()).not.toBeInTheDocument();
});

test("click can open the help without submitting a form", async () => {
  render(TokenHelp);
  await trigger().click();
  await expect.element(help()).toBeVisible();
  await expect.element(trigger()).toHaveAttribute("type", "button");
  await userEvent.keyboard("{Escape}");
  await expect.element(help()).not.toBeInTheDocument();
});
