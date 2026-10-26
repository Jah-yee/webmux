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
  state: "ready",
  publicUrl: "https://node.tailnet.ts.net",
  entries: [],
};

function serve(status: TailscaleStatus, fail = false) {
  let calls = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async () => {
      calls++;
      if (fail) return Response.json({ error: "检测失败" }, { status: 500 });
      return Response.json(status);
    }),
  );
  return () => calls;
}

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

test("shows WSL NAT guidance and no fake toggle", async () => {
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
  await vi.waitFor(() => expect(calls()).toBeGreaterThan(1));
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
