import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import TunnelManager from "../../src/components/TunnelManager.svelte";
import type { TunnelSettings } from "../../src/schemas";

const off: TunnelSettings = {
  enabled: false,
  mode: "quick",
  hostname: "",
  tokenConfigured: false,
  source: "config",
  state: "off",
  canEnable: true,
  storageAvailable: true,
};

function serve(initial = off, failDisable = false) {
  let state = { ...initial };
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const path = new URL(String(input), location.href).pathname;
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body)) as Record<string, unknown>;
        writes.push({ path, body });
        if (path.endsWith("/enabled")) {
          state = {
            ...state,
            enabled: !!body.enabled,
            state: body.enabled ? "ready" : "off",
          };
          if (!body.enabled && failDisable)
            return Response.json(
              {
                error: "当前隧道已关闭，但关闭状态未能保存",
                notPersisted: true,
              },
              { status: 503 },
            );
        } else {
          state = {
            ...state,
            mode: body.mode as "quick" | "named",
            hostname: String(body.hostname),
            source: "web",
            tokenConfigured: body.mode === "named",
          };
        }
      } else if (init?.method === "DELETE") {
        writes.push({ path, body: {} });
        state = { ...off };
      }
      return Response.json(state);
    }),
  );
  return writes;
}

afterEach(async () => {
  await cleanup();
  await userEvent.cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const save = () =>
  page.getByRole("button", { name: "保存并应用", exact: true });
const dialog = () => page.getByRole("alertdialog");

test("validates named fields and never fills a saved credential back into the form", async () => {
  const writes = serve();
  render(TunnelManager);
  await page.getByRole("tab", { name: "Named Tunnel", exact: true }).click();
  await save().click();
  expect(writes).toEqual([]);
  await page
    .getByLabelText("公共主机名", { exact: true })
    .fill("https://term.example.com/path");
  await page
    .getByLabelText("Cloudflare 隧道凭据", { exact: true })
    .fill("private-cloudflare-token");
  await save().click();
  expect(writes).toEqual([]);
  await page
    .getByLabelText("公共主机名", { exact: true })
    .fill("term.example.com");
  await save().click();
  await expect
    .element(page.getByLabelText("Cloudflare 隧道凭据", { exact: true }))
    .toHaveValue("");
  expect(writes[0]?.body).toEqual({
    mode: "named",
    hostname: "term.example.com",
    token: "private-cloudflare-token",
  });
  await page
    .getByLabelText("公共主机名", { exact: true })
    .fill("new.example.com");
  await save().click();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1]?.body).toEqual({
    mode: "named",
    hostname: "new.example.com",
  });
});

test("enable and reset require confirmation and cancel performs no mutation", async () => {
  const writes = serve({ ...off, source: "web" });
  render(TunnelManager);
  await page.getByRole("button", { name: "启用公网访问", exact: true }).click();
  await dialog().getByRole("button", { name: "取消", exact: true }).click();
  expect(writes).toEqual([]);
  await page.getByRole("button", { name: "启用公网访问", exact: true }).click();
  await dialog().getByRole("button", { name: "确认", exact: true }).click();
  await expect
    .element(page.getByRole("button", { name: "关闭公网访问", exact: true }))
    .toBeEnabled();
  expect(writes[0]?.body).toEqual({ enabled: true });
  await page
    .getByRole("button", { name: "恢复配置文件设置", exact: true })
    .click();
  await dialog().getByRole("button", { name: "取消", exact: true }).click();
  expect(writes).toHaveLength(1);
  await page
    .getByRole("button", { name: "恢复配置文件设置", exact: true })
    .click();
  await dialog().getByRole("button", { name: "确认", exact: true }).click();
  await expect
    .element(page.getByRole("button", { name: "启用公网访问", exact: true }))
    .toBeEnabled();
  await expect
    .element(
      page.getByRole("button", { name: "恢复配置文件设置", exact: true }),
    )
    .toBeDisabled();
});

test("storage failures disable configuration but preserve emergency shutdown", async () => {
  const writes = serve(
    {
      ...off,
      enabled: true,
      state: "ready",
      storageAvailable: false,
      storageError: "read-only filesystem",
      canEnable: false,
    },
    true,
  );
  render(TunnelManager);
  await expect.element(save()).toBeDisabled();
  await page.getByRole("button", { name: "关闭公网访问", exact: true }).click();
  await dialog().getByRole("button", { name: "确认", exact: true }).click();
  await expect
    .element(page.getByRole("button", { name: "启用公网访问", exact: true }))
    .toBeDisabled();
  expect(writes).toEqual([
    { path: "/api/tunnel/enabled", body: { enabled: false } },
  ]);
  await expect
    .element(page.getByRole("alert").filter({ hasText: "关闭状态未能保存" }))
    .toBeVisible();
});

test("public-origin configuration changes warn and cancellation keeps credentials local", async () => {
  const writes = serve({
    ...off,
    enabled: true,
    state: "ready",
    mode: "named",
    hostname: location.hostname,
    tokenConfigured: true,
    publicUrl: location.origin,
  });
  render(TunnelManager);
  await page
    .getByLabelText("公共主机名", { exact: true })
    .fill("new.example.com");
  await save().click();
  await expect.element(dialog()).toMatchTextContent(/当前公网页面会断开/);
  await dialog().getByRole("button", { name: "取消", exact: true }).click();
  expect(writes).toEqual([]);
});
