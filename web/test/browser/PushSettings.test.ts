import { afterEach, expect, test, vi } from "vitest";
import { page } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import PushSettings from "../../src/components/PushSettings.svelte";
import type { PushSupport } from "../../src/push";
import type { PushStatus } from "../../src/schemas";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

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
    shows: [
      "Quick Tunnel 无法持续接收推送",
      "旧订阅点开即失效",
      "查看公网访问配置",
    ],
    toggle: false,
  },
  {
    name: "unsupported origin",
    status: { available: false, reason: "unsupported_origin" },
    shows: ["当前地址不支持推送", "查看公网访问配置"],
    toggle: false,
  },
  {
    name: "insecure context",
    support: { secure: false, serviceWorker: false, pushManager: false },
    shows: ["推送需要 HTTPS 安全上下文", "查看公网访问配置"],
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
      .element(page.getByText("推送是尽力送达", { exact: true }))
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

test("any browser can send the test to every device once one is subscribed", async () => {
  // A desktop that cannot or has not turned push on still drives the phones.
  const button = page.getByRole("button", { name: /发送测试通知/ });
  render(PushSettings, {
    support: capable,
    loadStatus: async () => ({
      ...available,
      available: false,
      reason: "unsupported_origin",
    }),
  });
  await expect
    .element(page.getByText("向全部 2 台设备发送测试通知"))
    .toBeVisible();
  cleanup();

  render(PushSettings, {
    support: capable,
    loadStatus: async () => ({ ...available, subscriptions: 0 }),
  });
  await expect.element(page.getByText("本设备未开启")).toBeVisible();
  expect(button.elements()).toHaveLength(0);
});

test("a pending permission prompt explains where to allow notifications", async () => {
  // Chrome's quiet permission UI leaves the prompt pending until the user
  // clicks the address-bar bell; the card must not sit silently disabled.
  const prompt = Promise.withResolvers<NotificationPermission>();
  vi.stubGlobal("Notification", {
    permission: "default",
    requestPermission: () => prompt.promise,
  });
  render(PushSettings, {
    support: capable,
    loadStatus: async () => available,
  });
  const toggle = page.getByRole("switch");
  await toggle.click();
  const hint = page.getByText("等待浏览器通知授权");
  await expect.element(hint).toBeVisible();
  await expect
    .element(page.getByText("通知已屏蔽", { exact: false }))
    .toBeVisible();
  await expect.element(toggle).toBeDisabled();

  prompt.resolve("denied");
  await expect
    .element(page.getByText("未获得通知权限", { exact: false }))
    .toBeVisible();
  await expect.element(hint).not.toBeInTheDocument();
  await expect.element(toggle).not.toBeChecked();
  await expect.element(toggle).toBeEnabled();
});
