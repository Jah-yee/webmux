import { mkdtemp, readFile, rm } from "node:fs/promises";
import { join } from "node:path";
import {
  test as base,
  expect,
  type APIRequestContext,
  type Page,
} from "@playwright/test";
import { startE2EServer, type E2EServer } from "./server.mjs";

type Session = { id: string; status: string; exitCode: number | null };

async function sessions(request: APIRequestContext): Promise<Session[]> {
  const response = await request.get("/api/sessions");
  expect(response.ok(), "List isolated backend sessions").toBeTruthy();
  return response.json();
}

async function cleanSessions(request: APIRequestContext) {
  for (const session of await sessions(request)) {
    const closed = await request.delete(`/api/sessions/${session.id}`);
    expect(closed.status(), `Close ${session.id}`).toBe(204);
    await expect
      .poll(async () => {
        const response = await request.get(`/api/sessions/${session.id}`);
        return (await response.json()).status;
      })
      .toBe("exited");
    const removed = await request.delete(
      `/api/sessions/${session.id}?remove=1`,
    );
    expect(removed.status(), `Remove ${session.id}`).toBe(204);
  }
}

async function cleanRepositories(request: APIRequestContext) {
  const response = await request.get("/api/repositories");
  expect(response.ok()).toBeTruthy();
  for (const repository of await response.json()) {
    expect(
      (await request.delete(`/api/repositories/${repository.id}`)).status(),
    ).toBe(204);
  }
}

async function cleanLaunchConfigs(request: APIRequestContext) {
  const response = await request.get("/api/launch-configs");
  expect(response.ok()).toBeTruthy();
  for (const config of await response.json()) {
    expect(
      (await request.delete(`/api/launch-configs/${config.id}`)).status(),
    ).toBe(204);
  }
}

export const test = base.extend<
  { workDir: string; _isolation: void },
  { server: E2EServer }
>({
  server: [
    // oxlint-disable-next-line no-empty-pattern -- Playwright requires object destructuring for fixture dependencies.
    async ({}, use) => {
      const server = await startE2EServer();
      try {
        await use(server);
      } finally {
        await server.close();
      }
    },
    { scope: "worker", timeout: 240_000 },
  ],
  baseURL: async ({ server }, use) => {
    await use(server.baseURL);
  },
  workDir: async ({ server }, use) => {
    const directory = await mkdtemp(join(server.workDir, "case-"));
    try {
      await use(directory);
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  },
  _isolation: [
    async ({ request, page, server }, use, testInfo) => {
      await cleanSessions(request);
      await cleanRepositories(request);
      await cleanLaunchConfigs(request);
      const browserLogs: string[] = [];
      page.on("console", (message) =>
        browserLogs.push(`[${message.type()}] ${message.text()}`),
      );
      page.on("pageerror", (error) =>
        browserLogs.push(error.stack ?? error.message),
      );
      page.on("requestfailed", (request) =>
        browserLogs.push(
          `${request.method()} ${request.url()}: ${request.failure()?.errorText}`,
        ),
      );
      try {
        await use();
      } finally {
        try {
          if (testInfo.status !== testInfo.expectedStatus) {
            await testInfo.attach("server.log", {
              body: server.logs(),
              contentType: "text/plain",
            });
            await testInfo.attach("browser.log", {
              body: browserLogs.join("\n"),
              contentType: "text/plain",
            });
            const snapshot = await sessions(request);
            await testInfo.attach("sessions.json", {
              body: JSON.stringify(snapshot, null, 2),
              contentType: "application/json",
            });
            for (const session of snapshot) {
              const buffer = await request.get(
                `/api/sessions/${session.id}/buffer`,
              );
              await testInfo.attach(`terminal-${session.id}.txt`, {
                body: await buffer.body(),
                contentType: "text/plain",
              });
            }
          }
        } finally {
          await cleanSessions(request);
          await cleanRepositories(request);
          await cleanLaunchConfigs(request);
        }
      }
    },
    { auto: true },
  ],
});

export { expect };

export async function createShell(
  page: Page,
  name = "E2E shell",
  cwd?: string,
): Promise<string> {
  await page.goto("/");
  await page
    .locator("header")
    .getByRole("button", { name: "＋ 新建会话", exact: true })
    .click();
  await page.getByRole("button", { name: "Shell", exact: true }).click();
  if (cwd) await page.getByLabel("工作目录", { exact: true }).fill(cwd);
  await page.getByLabel("会话名称", { exact: false }).fill(name);
  await page.getByRole("button", { name: "创建并进入 →", exact: true }).click();
  await expect(page).toHaveURL(/\/s\/[a-z0-9]+$/);
  await expectConnected(page);
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await expect(page.locator(".xterm-helper-textarea")).toBeAttached();
  return new URL(page.url()).pathname.split("/").at(-1)!;
}

// A healthy connection shows no text; the status dot names it instead.
export async function expectConnected(page: Page) {
  const header = page.locator(".terminal-header");
  await expect(header.getByRole("img", { name: /· 已连接$/ })).toBeVisible();
  await expect(header.getByRole("status")).toHaveCount(0);
}

// Below 640px the header moves upload, fullscreen and scroll-to-latest into
// the terminal menu; this opens the menu there and returns the entry.
export async function headerAction(page: Page, name: string) {
  if ((page.viewportSize()?.width ?? 1280) >= 640)
    return page
      .locator(".terminal-header")
      .getByRole("button", { name, exact: true });
  await page.getByRole("button", { name: "终端菜单", exact: true }).click();
  return page.getByRole("menuitem", { name, exact: true });
}

export async function runCommand(page: Page, command: string) {
  const input = page.locator(".xterm-helper-textarea");
  await input.focus();
  await expect(page.locator(".restore-overlay")).toBeHidden();
  // type(), unlike fill() or insertText(), sends real keydown/keypress events.
  await page.keyboard.type(command);
  await page.keyboard.press("Enter");
}

export async function expectTerminalLine(page: Page, line: string) {
  // The E2E Chromium projects use xterm's built-in DOM renderer. Match a full
  // rendered line, not the echoed printf command or a mocked terminal buffer.
  await expect
    .poll(async () =>
      page
        .locator(".xterm-rows > div")
        .evaluateAll((rows) =>
          rows.map((row) =>
            (row.textContent ?? "").replaceAll("\u00a0", " ").trimEnd(),
          ),
        ),
    )
    .toContain(line);
}

export const quote = (value: string) =>
  "'" + value.replaceAll("'", "'\\''") + "'";

/** Turns the shell into a raw byte recorder so tests can assert exact PTY input. */
export async function recordInput(
  page: Page,
  directory: string,
  bracketed = true,
) {
  const path = join(directory, "received-input");
  await runCommand(
    page,
    `stty raw -echo; printf '\\033[?2004${bracketed ? "h" : "l"}RECORDER_READY\\r\\n'; cat > ${quote(path)}`,
  );
  await expectTerminalLine(page, "RECORDER_READY");
  await expect.poll(() => readFile(path, "utf8")).toBe("");
  return path;
}
