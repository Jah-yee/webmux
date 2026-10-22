<script lang="ts">
  import { onMount } from "svelte";
  import { Badge } from "$lib/components/ui/badge";
  import { Button } from "$lib/components/ui/button";
  import { Label } from "$lib/components/ui/label";
  import { Switch } from "$lib/components/ui/switch";
  import * as Card from "$lib/components/ui/card";
  import InlineNotice from "./InlineNotice.svelte";
  import { APIError } from "../api";
  import { PushReasonSchema, type PushStatus } from "../schemas";
  import {
    advise,
    currentSubscription,
    describeTest,
    detectSupport,
    disable,
    enable,
    pushStatus,
    requestPermission,
    savedSubscriptionId,
    sendTest,
    sync,
    type PushAdvice,
    type PushSupport,
  } from "../push";

  // Both are injectable so component tests need no service worker or server.
  let {
    support = detectSupport(),
    loadStatus = pushStatus,
  }: {
    support?: PushSupport;
    loadStatus?: () => Promise<PushStatus>;
  } = $props();

  let status = $state<PushStatus>();
  let loadError = $state("");
  let subscribed = $state(false);
  let busy = $state(false);
  // The state a toggle in flight is heading for; the switch shows it until the
  // attempt settles and then shows the real subscription again.
  let target = $state(false);
  let notice = $state("");
  let error = $state("");
  // True while the browser's notification permission prompt is unanswered.
  let awaitingPermission = $state(false);
  // A server refusal at registration time overrides the status reason.
  let refused = $state<PushAdvice>();
  const advice = $derived<PushAdvice | undefined>(
    refused ?? (status ? advise(support, status) : undefined),
  );

  async function refresh() {
    try {
      status = await loadStatus();
      loadError = "";
    } catch (cause) {
      loadError = cause instanceof Error ? cause.message : String(cause);
    }
    subscribed =
      support.serviceWorker &&
      !!(await currentSubscription().catch(() => null)) &&
      !!savedSubscriptionId();
  }

  function failure(cause: unknown) {
    if (cause instanceof APIError && cause.status === 409) {
      const reason = PushReasonSchema.safeParse(cause.reason);
      if (reason.success) refused = reason.data;
    }
    error = cause instanceof Error ? cause.message : String(cause);
  }

  async function toggle(checked: boolean) {
    target = checked;
    busy = true;
    notice = error = "";
    try {
      if (checked) {
        if (!status?.publicKey) throw new Error("服务端未提供推送公钥");
        if (Notification.permission === "default") {
          awaitingPermission = true;
          try {
            await requestPermission();
          } finally {
            awaitingPermission = false;
          }
        }
        await enable(status.publicKey);
      } else {
        await disable();
      }
    } catch (cause) {
      failure(cause);
    } finally {
      await refresh();
      busy = false;
    }
  }

  async function test() {
    busy = true;
    notice = error = "";
    try {
      const result = describeTest(await sendTest());
      if (result.failed) error = result.message;
      else notice = result.message;
    } catch (cause) {
      failure(cause);
    } finally {
      await refresh();
      busy = false;
    }
  }

  onMount(() => {
    void (async () => {
      if (support.secure && support.serviceWorker) {
        const result = await sync().catch(() => "none" as const);
        if (result === "key-changed") error = "推送密钥已变更，请重新开启";
      }
      await refresh();
    })();
  });
</script>

<Card.Root id="push" class="scroll-mt-4">
  <Card.Header>
    <Card.Title>通知推送</Card.Title>
    <Card.Description
      >会话提问、等待授权、回合结束、出错或发出终端通知时提醒本设备；页面关闭或手机锁屏后也能收到，点击通知回到对应会话。只对当前设备生效。</Card.Description
    >
  </Card.Header>
  <Card.Content class="space-y-4">
    {#if loadError}
      <InlineNotice tone="danger">无法读取推送状态：{loadError}</InlineNotice>
    {:else if status}
      <div class="flex flex-wrap items-center gap-2 text-sm" role="status">
        {#if subscribed}
          <Badge>本设备已开启</Badge>
        {:else if advice === "ready"}
          <Badge variant="secondary">本设备未开启</Badge>
        {:else}
          <Badge variant="outline">不可用</Badge>
        {/if}
        <span class="text-muted-foreground"
          >服务端共有 {status.subscriptions} 台设备开启</span
        >
      </div>
    {/if}

    {#if advice === "ready"}
      <div class="flex items-center justify-between gap-4">
        <Label for="push-enabled">在本设备开启推送</Label>
        <Switch
          id="push-enabled"
          bind:checked={
            () => (busy ? target : subscribed), (value) => void toggle(value)
          }
          disabled={busy}
        />
      </div>
      {#if awaitingPermission}
        <InlineNotice>
          <div class="space-y-1">
            <p class="font-medium">等待浏览器通知授权</p>
            <p>
              请在地址栏右侧的弹窗中选择“允许”。如果只看到铃铛图标或“通知已屏蔽”提示，请点击该图标并选择“允许”。
            </p>
          </div>
        </InlineNotice>
      {/if}
    {:else if advice}
      <InlineNotice tone="warning">
        {#if advice === "insecure"}
          <p class="font-medium">推送需要 HTTPS 安全上下文</p>
          <p>当前地址既不是 HTTPS 也不是本机回环地址，浏览器不提供推送功能。</p>
          <ol class="mt-1 list-decimal pl-5">
            <li>
              用 Cloudflare Named Tunnel 的公网链接打开 WebMux：Windows
              托盘“复制公网链接”，或服务启动时打印的二维码；
            </li>
            <li>在新地址的 设置 → 通知推送 中开启。</li>
          </ol>
        {:else if advice === "ios-home-screen"}
          <p class="font-medium">iOS 需要从主屏幕打开</p>
          <p>需要 iOS / iPadOS 16.4 或更高版本。</p>
          <ol class="mt-1 list-decimal pl-5">
            <li>在 Safari 中点“分享 → 添加到主屏幕”；</li>
            <li>
              从主屏幕图标打开 WebMux，并重新输入 Token（主屏幕应用不共享 Safari
              的登录）；
            </li>
            <li>进入 设置 → 通知推送 开启。</li>
          </ol>
        {:else if advice === "unsupported"}
          <p class="font-medium">当前浏览器不支持推送</p>
          <p>
            浏览器缺少 Service Worker、Push API 或通知功能。请改用最新版的
            Chrome、Edge、Firefox 或 Safari。
          </p>
        {:else if advice === "quick_tunnel"}
          <p class="font-medium">Quick Tunnel 无法持续接收推送</p>
          <p>
            Quick Tunnel 每次启动都会更换地址，旧订阅点开即失效。请改用 Named
            Tunnel：
          </p>
          <ol class="mt-1 list-decimal pl-5">
            <li>
              在 Cloudflare 控制台创建 Tunnel，把公共主机名指向 WebMux
              本地地址；
            </li>
            <li>
              在 WebMux 配置的 <code>[tunnel]</code> 中写入
              <code>token</code>（<code>tunnel.token</code>）与
              <code>hostname</code>（<code>tunnel.hostname</code>）；
            </li>
            <li>重启 WebMux 并开启公网隧道；</li>
            <li>用新的公网链接打开 WebMux 并开启推送。</li>
          </ol>
        {:else if advice === "unsupported_origin"}
          <p class="font-medium">当前地址不支持推送</p>
          <p>
            推送只支持 Cloudflare Named Tunnel
            公网地址与本机回环地址（127.0.0.1、localhost）。请改用 Named Tunnel
            公网链接打开 WebMux 后再开启。
          </p>
        {:else if advice === "disabled"}
          <p class="font-medium">推送已在配置中停用</p>
          <p>
            服务端配置了 <code>[push] enabled = false</code>。如需推送，改为
            <code>true</code> 并重启 WebMux。
          </p>
        {:else if advice === "storage"}
          <p class="font-medium">服务端推送存储不可用</p>
          <p>无法读写状态目录中的 <code>push.json</code>，请查看服务日志。</p>
        {:else if advice === "denied"}
          <p class="font-medium">通知权限已被拒绝</p>
          <p>请在浏览器或系统设置中允许本站通知，然后刷新页面。</p>
        {/if}
      </InlineNotice>
      {#if subscribed}
        <Button variant="outline" disabled={busy} onclick={() => toggle(false)}
          >关闭本设备推送</Button
        >
      {/if}
    {/if}

    <!-- The test reaches every device, so this browser can trigger it for the
         phones even when it has not turned push on itself. -->
    {#if status && status.subscriptions > 0}
      <Button variant="outline" disabled={busy} onclick={test}
        >向全部 {status.subscriptions} 台设备发送测试通知</Button
      >
    {/if}

    {#if error}
      <InlineNotice tone="danger">{error}</InlineNotice>
    {:else if notice}
      <InlineNotice tone="success">{notice}</InlineNotice>
    {/if}

    <p class="text-xs text-muted-foreground">
      推送是尽力送达：锁屏、省电、勿扰或设备离线超过有效期时可能延迟或丢失；无
      Google Play 服务的 Android 设备不支持；桌面 Chrome、Edge 与 Firefox
      需要浏览器进程在运行。会话列表中的状态仍是唯一准确来源。
    </p>
  </Card.Content>
</Card.Root>
