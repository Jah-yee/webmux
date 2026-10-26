import { randomUUID } from "node:crypto";
import { test, expect, expectConnected, openSessionLink } from "./fixtures";

test("session links open the terminal in a new tab and keep the list", async ({
  page,
  request,
  workDir,
}) => {
  const errors: string[] = [];
  page.context().on("page", (popup) => {
    popup.on("pageerror", (error) => errors.push(error.message));
  });
  const name = `New tab ${randomUUID().slice(0, 8)}`;
  const created = await request.post("/api/sessions", {
    data: {
      cwd: workDir,
      cmd: ["/bin/bash", "--noprofile", "--norc", "-i"],
      name,
    },
  });
  expect(created.status()).toBe(201);
  const { id } = await created.json();

  await page.goto("/");
  const timeOrigin = await page.evaluate(() => performance.timeOrigin);
  const terminal = await openSessionLink(page, name);

  expect(new URL(terminal.url()).pathname).toBe(`/s/${id}`);
  await expectConnected(terminal);
  await expect(terminal.locator(".xterm-helper-textarea")).toBeAttached();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("link", { name, exact: true })).toBeVisible();
  expect(await page.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
  expect(errors).toEqual([]);
});
