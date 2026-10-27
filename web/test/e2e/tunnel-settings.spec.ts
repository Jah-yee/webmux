import {
  mkdtemp,
  mkdir,
  readFile,
  rename,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test as base, expect } from "@playwright/test";
import { startE2EServer, type E2EServer } from "./server.mjs";

const token = "tunnel-e2e-root-token-abcdefghijklmnopqrstuvwxyz-123456789";
const secret = "cloudflare-fixture-credential-not-for-real-network";
const test = base.extend<{ tunnelServer: E2EServer }>({
  tunnelServer: async ({ request }, use) => {
    const root = await mkdtemp(join(tmpdir(), "webmux-fake-tunnel-"));
    const cloudflared = join(root, "cloudflared");
    await writeFile(
      cloudflared,
      `#!/bin/sh\necho 'INF | https://acceptance.trycloudflare.com |' >&2\necho 'INF Registered tunnel connection connIndex=0' >&2\nexec sleep 600\n`,
      { mode: 0o700 },
    );
    const server = await startE2EServer({
      token,
      tunnel: { cloudflared, enabled: true },
    });
    try {
      expect((await request.get(server.apiURL + "/api/health")).status()).toBe(
        200,
      );
      await use(server);
    } finally {
      await server.close();
      await rm(root, { recursive: true, force: true });
    }
  },
});

test.use({ extraHTTPHeaders: { Authorization: `Bearer ${token}` } });

test("configure, switch and stop tunnels without ending the PTY session", async ({
  page,
  request,
  tunnelServer: server,
}) => {
  const created = await request.post(server.apiURL + "/api/sessions", {
    data: {
      name: "Tunnel acceptance",
      cwd: server.workDir,
      cmd: ["/bin/sh", "-c", "printf TUNNEL_SESSION_ALIVE; sleep 600"],
    },
  });
  expect(created.status()).toBe(201);
  const session = await created.json();
  await page.goto(server.baseURL + `/settings#token=${token}`);
  await page.getByRole("link", { name: "管理公网访问", exact: true }).click();
  await expect(page.getByText("已就绪", { exact: true })).toBeVisible();
  await page.getByRole("tab", { name: "Named Tunnel", exact: true }).click();
  await page
    .getByLabel("公共主机名", { exact: true })
    .fill("terminal.example.com");
  await page.getByLabel("Cloudflare 隧道凭据", { exact: true }).fill(secret);
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await expect(
    page.getByLabel("Cloudflare 隧道凭据", { exact: true }),
  ).toHaveValue("");
  await expect(
    page.getByText("https://terminal.example.com", { exact: true }),
  ).toBeVisible();
  const read = await request.get(server.apiURL + "/api/tunnel");
  expect(await read.text()).not.toContain(secret);
  const stateFile = join(server.stateDir, "tunnel-settings.json");
  expect((await stat(stateFile)).mode & 0o777).toBe(0o600);
  expect(JSON.parse(await readFile(stateFile, "utf8")).token).toBe(secret);
  await page.getByLabel("公共主机名", { exact: true }).fill("new.example.com");
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await expect(
    page.getByText("https://new.example.com", { exact: true }),
  ).toBeVisible();
  expect(JSON.parse(await readFile(stateFile, "utf8")).token).toBe(secret);
  await page.getByRole("tab", { name: "Quick Tunnel", exact: true }).click();
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await expect(
    page.getByText("https://acceptance.trycloudflare.com", { exact: true }),
  ).toBeVisible();
  expect(JSON.parse(await readFile(stateFile, "utf8")).token ?? "").toBe("");
  await page.getByRole("button", { name: "关闭公网访问", exact: true }).click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  expect(
    (await (await request.get(server.apiURL + "/api/tunnel")).json()).enabled,
  ).toBe(true);
  await page.getByRole("button", { name: "关闭公网访问", exact: true }).click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "确认", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "启用公网访问", exact: true }),
  ).toBeEnabled();
  expect(
    (
      await (
        await request.get(server.apiURL + `/api/sessions/${session.id}`)
      ).json()
    ).status,
  ).not.toBe("exited");
  expect(
    await (
      await request.get(server.apiURL + `/api/sessions/${session.id}/buffer`)
    ).text(),
  ).toContain("TUNNEL_SESSION_ALIVE");
  expect(server.logs()).not.toContain(secret);
});

test("saved shutdown survives restart and corrupt storage cannot restore TOML exposure", async ({
  request,
  tunnelServer: server,
}) => {
  expect(
    (
      await request.put(server.apiURL + "/api/tunnel/enabled", {
        data: { enabled: false },
      })
    ).status(),
  ).toBe(202);
  await server.restart();
  expect(
    await (await request.get(server.apiURL + "/api/tunnel")).json(),
  ).toMatchObject({ enabled: false, state: "off", source: "web" });
  expect(
    (
      await request.put(server.apiURL + "/api/tunnel/enabled", {
        data: { enabled: true },
      })
    ).status(),
  ).toBe(202);
  await server.restart();
  await expect
    .poll(
      async () =>
        (await (await request.get(server.apiURL + "/api/tunnel")).json()).state,
    )
    .toBe("ready");
  await request.put(server.apiURL + "/api/tunnel/enabled", {
    data: { enabled: false },
  });
  await writeFile(
    join(server.stateDir, "tunnel-settings.json"),
    "{invalid JSON",
  );
  await server.restart();
  const current = await (
    await request.get(server.apiURL + "/api/tunnel")
  ).json();
  expect(current).toMatchObject({ enabled: false, storageAvailable: false });
  expect(current.state).not.toBe("ready");
  expect((await request.get(server.apiURL + "/api/health")).status()).toBe(200);
  expect(
    await readFile(join(server.stateDir, "tunnel-settings.json"), "utf8"),
  ).toBe("{invalid JSON");
});

test("disk write failure still closes public access from the webpage", async ({
  page,
  request,
  tunnelServer: server,
}) => {
  await request.put(server.apiURL + "/api/tunnel/enabled", {
    data: { enabled: true },
  });
  await expect
    .poll(
      async () =>
        (await (await request.get(server.apiURL + "/api/tunnel")).json()).state,
    )
    .toBe("ready");
  const stateFile = join(server.stateDir, "tunnel-settings.json");
  await rename(stateFile, stateFile + ".saved");
  await mkdir(stateFile);
  await page.goto(server.baseURL + `/settings/tunnel#token=${token}`);
  await page.getByRole("button", { name: "关闭公网访问", exact: true }).click();
  const response = page.waitForResponse(
    (r) =>
      r.url().endsWith("/api/tunnel/enabled") && r.request().method() === "PUT",
  );
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "确认", exact: true })
    .click();
  const stopped = await response;
  expect(stopped.status()).toBe(503);
  expect(await stopped.json()).toMatchObject({ notPersisted: true });
  await expect(
    page.getByRole("button", { name: "启用公网访问", exact: true }),
  ).toBeVisible();
  expect(
    await (await request.get(server.apiURL + "/api/tunnel")).json(),
  ).toMatchObject({ enabled: false, state: "off" });
  expect(
    (
      await request.get(server.apiURL + "/api/health", {
        headers: { Host: "acceptance.trycloudflare.com" },
      })
    ).status(),
  ).toBe(403);
});
