import { afterEach, expect, test, vi } from "vitest";
import { page } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import TunnelSettings from "../../src/components/TunnelSettings.svelte";
import type { TunnelInfo } from "../../src/schemas";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const off: TunnelInfo = {
  enabled: false,
  state: "off",
  named: false,
  hostname: "",
};

function renderCard(tunnel: TunnelInfo) {
  render(TunnelSettings, { loadTunnel: async () => tunnel });
}

async function statusText() {
  const region = page.getByRole("region", { name: "隧道状态" });
  await expect.element(region).toBeVisible();
  return region.element().textContent ?? "";
}

const cases: { name: string; tunnel: TunnelInfo; shows: string[] }[] = [
  {
    name: "not enabled",
    tunnel: off,
    shows: ["未启用", "当前未启用公网隧道"],
  },
  {
    name: "quick starting",
    tunnel: { ...off, enabled: true, state: "starting" },
    shows: ["启动中", "Quick Tunnel", "正在启动隧道"],
  },
  {
    name: "quick ready",
    tunnel: {
      ...off,
      enabled: true,
      state: "ready",
      publicUrl: "https://abc-def.trycloudflare.com",
    },
    shows: ["已就绪", "Quick Tunnel", "https://abc-def.trycloudflare.com"],
  },
  {
    name: "named ready",
    tunnel: {
      enabled: true,
      state: "ready",
      named: true,
      hostname: "term.example.com",
      publicUrl: "https://term.example.com",
    },
    shows: ["已就绪", "Named Tunnel", "公共主机名", "term.example.com"],
  },
  {
    name: "named not enabled",
    tunnel: { ...off, named: true, hostname: "term.example.com" },
    shows: ["未启用", "已配置 Named Tunnel", "term.example.com"],
  },
  {
    name: "named without hostname",
    tunnel: { ...off, named: true },
    shows: ["已配置 Named Tunnel", "缺少 hostname"],
  },
  {
    name: "failed",
    tunnel: { ...off, state: "failed" },
    shows: ["异常停止", "隧道已停止或连接断开"],
  },
];

for (const row of cases) {
  test(`tunnel status: ${row.name}`, async () => {
    renderCard(row.tunnel);
    const text = await statusText();
    for (const expected of row.shows) expect(text).toContain(expected);
  });
}

test("reports a status load failure", async () => {
  render(TunnelSettings, {
    loadTunnel: async () => {
      throw new Error("network down");
    },
  });
  const alert = page.getByRole("alert");
  await expect.element(alert).toBeVisible();
  expect(alert.element().textContent).toContain("无法读取隧道状态");
});

test("handles a server without tunnel status", async () => {
  render(TunnelSettings, { loadTunnel: async () => undefined });
  expect(await statusText()).toContain("服务端未提供隧道状态");
});

test("does not render an unexpected tunnel token", async () => {
  renderCard({ ...off, token: "cloudflare-secret" } as TunnelInfo);
  await statusText();
  expect(document.body.textContent).not.toContain("cloudflare-secret");
});

test("wraps long tunnel addresses in a narrow card", async () => {
  const hostname =
    "very-long-subdomain.very-long-domain.example.example.example.com";
  renderCard({
    enabled: true,
    state: "ready",
    named: true,
    hostname,
    publicUrl: `https://${hostname}`,
  });
  await statusText();
  const card = document.querySelector<HTMLElement>("#access");
  if (!card) throw new Error("tunnel card missing");
  card.style.width = "320px";
  expect(card.scrollWidth).toBeLessThanOrEqual(card.clientWidth);
});

test("copies the displayed tunnel template", async () => {
  const writeText = vi
    .spyOn(navigator.clipboard, "writeText")
    .mockResolvedValue();
  renderCard(off);
  await page.getByRole("button", { name: "复制配置模板" }).click();
  await expect.element(page.getByText("已复制")).toBeVisible();
  expect(writeText).toHaveBeenCalledExactlyOnceWith(
    page.getByLabelText("Named Tunnel 配置模板").element().textContent,
  );
});

test("reports a failed copy", async () => {
  vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
    new Error("denied"),
  );
  vi.spyOn(document, "execCommand").mockReturnValue(false);
  renderCard(off);
  await page.getByRole("button", { name: "复制配置模板" }).click();
  await expect.element(page.getByRole("alert")).toBeVisible();
  await expect.element(page.getByText("复制配置模板")).toBeVisible();
});
