import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import TailscaleSettingsPage from "../../src/pages/TailscaleSettingsPage.svelte";
import type { TailscaleStatus } from "../../src/schemas";

const ready: TailscaleStatus = {
  provider: "linux",
  configuredProvider: "auto",
  backendState: "running",
  dnsName: "node.tailnet.ts.net",
  httpsReady: true,
  funnelAvailable: "unknown",
  mode: "serve",
  port: 443,
  source: "config",
  storageAvailable: true,
  canEnable: true,
  state: "ready",
  publicUrl: "https://node.tailnet.ts.net",
  entries: [],
};

type Call = { method: string; url: string; body?: unknown };

function serve(status: TailscaleStatus, fail = false) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ method, url: String(input), body });
      if (fail) return Response.json({ error: "检测失败" }, { status: 500 });
      if (method === "PUT")
        return Response.json({
          ...status,
          mode: body.mode,
          port: body.port,
          configuredProvider: body.provider,
          source: "web",
          state: "ready",
        });
      if (method === "DELETE")
        return Response.json({ ...status, mode: "off", source: "config" });
      return Response.json(status);
    }),
  );
  return calls;
}

const writes = (calls: Call[]) => calls.filter((c) => c.method !== "GET");

afterEach(async () => {
  await cleanup();
  await userEvent.cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

test("shows ready Serve state and copies the URL", async () => {
  serve(ready);
  const writeText = vi.fn<(text: string) => Promise<void>>();
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  render(TailscaleSettingsPage);
  await expect
    .element(page.getByText("https://node.tailnet.ts.net", { exact: true }))
    .toBeVisible();
  await expect
    .element(page.getByText("Serve（仅 tailnet）", { exact: true }))
    .toBeVisible();
  await page.getByRole("button", { name: "复制 Tailscale 地址" }).click();
  expect(writeText).toHaveBeenCalledWith("https://node.tailnet.ts.net");
});

test("warns about public Funnel", async () => {
  serve({
    ...ready,
    mode: "funnel",
    port: 8443,
    publicUrl: "https://node.tailnet.ts.net:8443",
  });
  render(TailscaleSettingsPage);
  await expect
    .element(page.getByText("公网 HTTPS", { exact: false }))
    .toBeVisible();
  await expect
    .element(page.getByText("Funnel（公网）", { exact: true }))
    .toBeVisible();
});

test("shows WSL NAT guidance", async () => {
  serve({
    ...ready,
    provider: "windows",
    configuredProvider: "windows",
    state: "failed",
    publicUrl: undefined,
    blockedReason: "WSL NAT 模式无法托管 Windows Tailscale",
    wsl: {
      networkingMode: "nat",
      windowsDetected: true,
      windowsRunning: true,
      windowsDnsName: "win.tailnet.ts.net",
    },
    entries: [{ host: "other.tailnet.ts.net", port: 443, public: true }],
  });
  render(TailscaleSettingsPage);
  await expect
    .element(
      page.getByText("WSL NAT 模式无法托管 Windows Tailscale", {
        exact: false,
      }),
    )
    .toBeVisible();
  await expect
    .element(
      page.getByText("托管 Windows Tailscale 需要 mirrored 网络", {
        exact: false,
      }),
    )
    .toBeVisible();
  await expect
    .element(page.getByText("other.tailnet.ts.net", { exact: true }))
    .toBeVisible();
  await expect
    .element(page.getByText("公网（Funnel）", { exact: true }))
    .toBeVisible();
});

test("refresh re-reads the status", async () => {
  const calls = serve(ready);
  render(TailscaleSettingsPage);
  await expect
    .element(page.getByText("https://node.tailnet.ts.net", { exact: true }))
    .toBeVisible();
  await page.getByRole("button", { name: "刷新检测" }).click();
  await vi.waitFor(() =>
    expect(calls.some((c) => c.url.endsWith("/tailscale?refresh=1"))).toBe(
      true,
    ),
  );
});

test("keeps the page usable when detection fails", async () => {
  serve(ready, true);
  render(TailscaleSettingsPage);
  await expect
    .element(page.getByText("检测失败", { exact: false }))
    .toBeVisible();
  await expect
    .element(page.getByRole("button", { name: "刷新检测" }))
    .toBeVisible();
});

test("saves a Serve port without confirmation", async () => {
  const calls = serve({ ...ready, publicUrl: undefined, state: "off" });
  render(TailscaleSettingsPage);
  const port = page.getByLabelText("Serve HTTPS 端口");
  await port.fill("8444");
  await page.getByRole("button", { name: "保存并应用" }).click();
  await vi.waitFor(() =>
    expect(writes(calls)).toEqual([
      {
        method: "PUT",
        url: expect.stringMatching(/\/api\/tailscale\/settings$/),
        body: { mode: "serve", port: 8444, provider: "auto" },
      },
    ]),
  );
  await expect
    .element(page.getByText("网页设置（覆盖配置文件）", { exact: false }))
    .toBeVisible();
});

test("rejects an invalid Serve port before sending", async () => {
  const calls = serve(ready);
  render(TailscaleSettingsPage);
  await page.getByLabelText("Serve HTTPS 端口").fill("70000");
  await page.getByRole("button", { name: "保存并应用" }).click();
  await expect
    .element(page.getByText("端口必须是 1–65535 的整数", { exact: false }))
    .toBeVisible();
  expect(writes(calls)).toEqual([]);
});

test("Funnel requires an explicit risk confirmation", async () => {
  const calls = serve({ ...ready, publicUrl: undefined, state: "off" });
  render(TailscaleSettingsPage);
  await page.getByRole("tab", { name: "Funnel" }).click();
  await page.getByRole("button", { name: "保存并应用" }).click();
  await expect
    .element(page.getByRole("alertdialog", { name: "开启公网 Funnel？" }))
    .toBeVisible();
  await page.getByRole("button", { name: "取消" }).click();
  expect(writes(calls)).toEqual([]);

  await page.getByRole("button", { name: "保存并应用" }).click();
  await page.getByRole("button", { name: "确认" }).click();
  await vi.waitFor(() =>
    expect(writes(calls).map((c) => c.body)).toEqual([
      { mode: "funnel", port: 443, provider: "auto", confirmFunnel: true },
    ]),
  );
});

test("explains why Serve cannot be enabled", async () => {
  serve({
    ...ready,
    mode: "off",
    state: "off",
    publicUrl: undefined,
    canEnable: false,
    enableBlockedReason: "requires a WebMux token of at least 43 characters",
  });
  render(TailscaleSettingsPage);
  await expect
    .element(page.getByText("at least 43 characters", { exact: false }))
    .toBeVisible();
});

test("reset is only offered for web settings and is confirmed", async () => {
  serve(ready);
  render(TailscaleSettingsPage);
  await expect
    .element(page.getByRole("button", { name: "恢复配置文件设置" }))
    .toBeDisabled();
  await cleanup();

  const calls = serve({ ...ready, source: "web" });
  render(TailscaleSettingsPage);
  await page.getByRole("button", { name: "恢复配置文件设置" }).click();
  await page.getByRole("button", { name: "确认" }).click();
  await vi.waitFor(() =>
    expect(writes(calls).map((c) => c.method)).toEqual(["DELETE"]),
  );
});
