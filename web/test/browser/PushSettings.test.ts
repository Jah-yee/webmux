import { afterEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import PushSettings from "../../src/components/PushSettings.svelte";
import type { PushSupport } from "../../src/push";
import type { PushStatus } from "../../src/schemas";

afterEach(() => cleanup());

const capable: PushSupport = {
  secure: true,
  serviceWorker: true,
  pushManager: true,
  notification: true,
  ios: false,
  standalone: false,
  permission: "default",
};
const available: PushStatus = {
  enabled: true,
  available: true,
  publicKey: "BP4z",
  subscriptions: 2,
};

const cases: {
  name: string;
  support?: Partial<PushSupport>;
  status?: Partial<PushStatus>;
  shows: string[];
  toggle: boolean;
}[] = [
  {
    name: "ready",
    shows: ["本设备未开启", "服务端共有 2 台设备开启"],
    toggle: true,
  },
  {
    name: "quick tunnel",
    status: { available: false, reason: "quick_tunnel", publicKey: undefined },
    shows: ["Quick Tunnel 无法持续接收推送", "tunnel.token", "tunnel.hostname"],
    toggle: false,
  },
  {
    name: "unsupported origin",
    status: { available: false, reason: "unsupported_origin" },
    shows: ["当前地址不支持推送", "Named Tunnel"],
    toggle: false,
  },
  {
    name: "insecure context",
    support: { secure: false, serviceWorker: false, pushManager: false },
    shows: ["推送需要 HTTPS 安全上下文", "复制公网链接"],
    toggle: false,
  },
  {
    name: "iOS Safari tab",
    support: { ios: true, pushManager: false },
    shows: ["iOS 需要从主屏幕打开", "添加到主屏幕", "重新输入 Token"],
    toggle: false,
  },
  {
    name: "permission denied",
    support: { permission: "denied" },
    shows: ["通知权限已被拒绝", "刷新页面"],
    toggle: false,
  },
  {
    name: "disabled in config",
    status: { enabled: false, available: false, reason: "disabled" },
    shows: ["推送已在配置中停用", "[push] enabled = false"],
    toggle: false,
  },
];

for (const row of cases) {
  test(`push card: ${row.name}`, async () => {
    render(PushSettings, {
      support: { ...capable, ...row.support },
      loadStatus: async () => ({ ...available, ...row.status }),
    });
    for (const text of row.shows)
      await expect
        .element(page.getByText(text, { exact: false }).first())
        .toBeVisible();
    await expect
      .element(
        page.getByText("会话列表中的状态仍是唯一准确来源", { exact: false }),
      )
      .toBeVisible();
    expect(page.getByRole("switch").elements()).toHaveLength(
      row.toggle ? 1 : 0,
    );
  });
}
