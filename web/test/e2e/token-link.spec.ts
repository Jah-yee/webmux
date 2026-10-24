import { test, expect } from "@playwright/test";
import { startE2EServer, type E2EServer } from "./server.mjs";

const token = "e2e a&b#c";
let server: E2EServer;

test.beforeAll(async () => {
  server = await startE2EServer({ token });
});

test.afterAll(async () => {
  await server?.close();
});

test("a #token= link signs in and leaves no token in the address or history", async ({
  page,
}) => {
  // Listen before navigating: the snapshot frame arrives with the page.
  const sessions = new Promise<string>((resolve) =>
    page.on("websocket", (socket) => {
      if (socket.url().endsWith("/ws/sessions"))
        socket.once("framereceived", () => resolve(socket.url()));
    }),
  );
  await page.goto(`${server.baseURL}/#token=${encodeURIComponent(token)}`);
  await expect(
    page
      .locator("header")
      .getByRole("button", { name: "＋ 新建会话", exact: true }),
  ).toBeVisible();
  expect(page.url()).toBe(`${server.baseURL}/`);
  expect(await page.evaluate(() => localStorage.getItem("webmux.token"))).toBe(
    token,
  );
  // The app's own socket authenticates through its subprotocol: no token in
  // the URL and no cookie, which every port of the host would share.
  expect(await sessions).not.toContain("token");
  expect(await page.evaluate(() => document.cookie)).not.toContain(
    "webmux_token",
  );
  expect(
    await page.evaluate(
      () =>
        new Promise<boolean>((resolve) => {
          const bare = new WebSocket(`ws://${location.host}/ws/sessions`);
          bare.onopen = () => {
            bare.close();
            resolve(true);
          };
          bare.onerror = () => resolve(false);
        }),
    ),
  ).toBe(false);
  await page.goBack();
  expect(page.url()).not.toContain("token");
});

test("an invalid #token= link shows the token form", async ({ page }) => {
  await page.goto(`${server.baseURL}/#token=wrong`);
  await expect(page.getByLabel("共享 Token", { exact: true })).toHaveValue(
    "wrong",
  );
  await expect(page.getByRole("alert")).toHaveText("authentication required");
  expect(page.url()).toBe(`${server.baseURL}/`);
});
