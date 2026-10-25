import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import type { Page, WebSocket } from "@playwright/test";
import {
  test,
  expect,
  createShell,
  expectTerminalLine,
  runCommand,
} from "./fixtures";

const banner = (page: Page) =>
  page.getByRole("status", { name: "终端控制", exact: true });

async function setHidden(page: Page, hidden: boolean) {
  await page.evaluate((value) => {
    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => value,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  }, hidden);
}

test("a second tab observes the controller's grid until it takes control", async ({
  page,
  request,
  workDir,
}) => {
  const id = await createShell(page, "Control", workDir);
  const size = async () => {
    const info = await (await request.get(`/api/sessions/${id}`)).json();
    return `${info.cols}x${info.rows}`;
  };
  await expect(banner(page)).toBeHidden();
  await expect.poll(size).not.toBe("80x24");
  const controllerSize = await size();

  // A new tab has its own client identity and starts as an observer.
  const other = await page.context().newPage();
  await other.setViewportSize({ width: 320, height: 480 });
  await other.goto(`/s/${id}`);
  await expect(banner(other)).toContainText("观察中");
  await other.setViewportSize({ width: 300, height: 420 });
  await other.waitForTimeout(500);
  expect(await size()).toBe(controllerSize);

  await other.getByRole("button", { name: "接管控制", exact: true }).click();
  await expect(banner(other)).toBeHidden();
  await expect(banner(page)).toContainText("观察中");
  await expect.poll(size).not.toBe(controllerSize);

  // Typing in the first tab takes control back and restores its grid.
  await runCommand(page, "echo back-in-control");
  await expectTerminalLine(page, "back-in-control");
  await expect(banner(page)).toBeHidden();
  await expect(banner(other)).toContainText("观察中");
  await expect.poll(size).toBe(controllerSize);
  await other.close();
});

test("returning to the foreground appends missed output without a full replay", async ({
  page,
  request,
  workDir,
}) => {
  const response = await request.post("/api/sessions", {
    data: {
      cwd: workDir,
      name: "Resume",
      cmd: [
        "/bin/bash",
        "-c",
        "printf 'before-marker\\n'; while [ ! -e go ]; do sleep 0.05; done; printf 'after-marker\\n'; exec sleep 600",
      ],
    },
  });
  expect(response.status()).toBe(201);
  const { id } = await response.json();
  const sockets: WebSocket[] = [];
  const frames = new Map<WebSocket, Buffer[]>();
  page.on("websocket", (socket) => {
    if (!socket.url().includes("/ws/s/")) return;
    sockets.push(socket);
    frames.set(socket, []);
    socket.on("framereceived", ({ payload }) => {
      if (typeof payload !== "string") frames.get(socket)!.push(payload);
    });
  });
  await page.goto(`/s/${id}`);
  await expectTerminalLine(page, "before-marker");
  await expect(page.locator(".restore-overlay")).toBeHidden();

  await setHidden(page, true);
  await writeFile(join(workDir, "go"), "");
  await expect
    .poll(async () => (await request.get(`/api/sessions/${id}/buffer`)).text())
    .toContain("after-marker");
  await page.waitForTimeout(200);
  await setHidden(page, false);

  await expectTerminalLine(page, "after-marker");
  await expectTerminalLine(page, "before-marker");
  await expect.poll(() => sockets.length).toBeGreaterThan(1);
  const resumed = sockets.at(-1)!;
  expect(resumed.url()).toMatch(/[?&]offset=\d+/);
  expect(resumed.url()).toMatch(/[?&]generation=[0-9a-f]{16}/);
  // The grid comes first, then the position of the resumed stream.
  await expect.poll(() => frames.get(resumed)!.length).toBeGreaterThan(1);
  const [control, position] = frames.get(resumed)!;
  expect(control[0]).toBe(6);
  expect(position[0]).toBe(4);
  expect(JSON.parse(position.subarray(1).toString())).toMatchObject({
    mode: "append",
  });
});
