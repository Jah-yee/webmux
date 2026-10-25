<script lang="ts">
  import { onDestroy } from "svelte";
  import IconExternalLink from "~icons/tabler/external-link";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import * as Card from "$lib/components/ui/card";
  import InlineNotice from "./InlineNotice.svelte";
  import TunnelStateBadge from "./TunnelStateBadge.svelte";
  import TunnelConfiguration from "./TunnelConfiguration.svelte";
  import { copyText } from "../clipboard";
  import { useTunnelSettings } from "../hooks/tunnel-settings.svelte";
  import type { TunnelSettingsUpdate } from "../schemas";

  const tunnel = useTunnelSettings();
  let confirmOpen = $state(false);
  let action = $state<"enable" | "disable" | "reset" | "save">();
  let pending = $state<TunnelSettingsUpdate>();
  let copied = $state(false);
  let copyFallback = $state("");
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  onDestroy(() => clearTimeout(copyTimer));
  const publicPage = $derived(
    !!tunnel.view &&
      (tunnel.view.publicUrl === location.origin ||
        tunnel.view.hostname === location.hostname),
  );
  const running = $derived(
    tunnel.view?.state === "starting" || tunnel.view?.state === "ready",
  );
  const titles = {
    enable: "启用公网访问？",
    disable: "关闭公网访问？",
    reset: "恢复配置文件设置？",
    save: "应用新的隧道配置？",
  };
  $effect(() => {
    if (!confirmOpen) {
      pending = undefined;
      action = undefined;
    }
  });

  function confirm(next: typeof action) {
    action = next;
    confirmOpen = true;
  }
  function save(draft: TunnelSettingsUpdate) {
    if (publicPage && running) {
      pending = draft;
      confirm("save");
    } else void tunnel.save(draft);
  }
  async function apply() {
    const selected = action;
    const draft = pending;
    confirmOpen = false;
    if (selected === "save" && draft) await tunnel.save(draft);
    else if (selected === "reset") await tunnel.reset();
    else if (selected) await tunnel.setEnabled(selected === "enable");
  }
  async function copyLink() {
    const url = tunnel.view?.publicUrl;
    if (!url) return;
    copied = await copyText(url);
    copyFallback = copied ? "" : url;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copied = false), 2000);
  }
</script>

<div class="space-y-6">
  {#if tunnel.loading}
    <p role="status">正在读取公网访问设置…</p>
  {/if}
  {#if tunnel.error}<InlineNotice tone="danger">{tunnel.error}</InlineNotice
    >{/if}
  {#if tunnel.notice}<p role="status" class="text-sm text-muted-foreground">
      {tunnel.notice}
    </p>{/if}
  {#if tunnel.view}
    {@const current = tunnel.view}
    <Card.Root>
      <Card.Header>
        <Card.Title>连接状态</Card.Title>
        <Card.Description
          >隧道进程独立运行，启停不会结束终端会话。本机与可信 VPN 访问不受影响。</Card.Description
        >
      </Card.Header>
      <Card.Content class="space-y-4">
        <div class="flex flex-wrap items-center gap-2" role="status">
          <TunnelStateBadge state={current.state} />
          <span class="text-sm"
            >{current.mode === "named" ? "Named Tunnel" : "Quick Tunnel"}</span
          >
          <span class="text-sm text-muted-foreground"
            >期望开关：{current.enabled ? "开启" : "关闭"}</span
          >
        </div>
        <p class="text-sm text-muted-foreground">
          来源：{current.source === "web"
            ? "网页设置（覆盖配置文件）"
            : "配置文件"}。命令行 --tunnel
          只覆盖本次进程；网页操作会更新持久化设置。
        </p>
        {#if current.hostname}<p class="text-sm wrap-anywhere">
            公共主机名：<code>{current.hostname}</code>
          </p>{/if}
        {#if current.publicUrl}
          <p class="text-sm wrap-anywhere">
            公网地址：<code>{current.publicUrl}</code>
          </p>
          <Button variant="outline" onclick={copyLink}
            >{copied ? "已复制" : "复制访问链接"}</Button
          >
          {#if copyFallback}
            <div class="grid gap-2">
              <Label for="tunnel-copy">复制失败，请手动复制链接</Label>
              <Input
                id="tunnel-copy"
                readonly
                value={copyFallback}
                onfocus={(event) => event.currentTarget.select()}
              />
            </div>
          {/if}
        {/if}
        {#if current.error}<InlineNotice tone="danger"
            >{current.error}</InlineNotice
          >{/if}
        {#if !current.storageAvailable}
          <InlineNotice tone="warning"
            >设置存储不可用：{current.storageError}。保存和启用已禁用，仍可关闭当前隧道；关闭状态可能无法保存。</InlineNotice
          >
        {/if}
        {#if !current.canEnable && current.enableBlockedReason}
          <InlineNotice tone="warning"
            >{current.enableBlockedReason}</InlineNotice
          >
        {/if}
        <div class="flex flex-wrap gap-2">
          {#if current.enabled || running}
            {#if current.state === "failed"}
              <Button
                disabled={tunnel.busy ||
                  !current.canEnable ||
                  !current.storageAvailable}
                onclick={() => confirm("enable")}>重试开启</Button
              >
            {/if}
            <Button
              variant="destructive"
              disabled={tunnel.busy}
              onclick={() => confirm("disable")}>关闭公网访问</Button
            >
          {:else}
            <Button
              disabled={tunnel.busy ||
                !current.canEnable ||
                !current.storageAvailable}
              onclick={() => confirm("enable")}>启用公网访问</Button
            >
          {/if}
          {#if tunnel.shutdownUnpersisted}
            <Button
              variant="outline"
              disabled={tunnel.busy}
              onclick={() => void tunnel.setEnabled(false)}
              >重新保存关闭状态</Button
            >
          {/if}
          <Button
            variant="outline"
            disabled={tunnel.busy}
            onclick={() => void tunnel.refresh()}>刷新状态</Button
          >
        </div>
      </Card.Content>
    </Card.Root>
    <Card.Root>
      <Card.Header>
        <Card.Title>隧道配置</Card.Title>
        <Card.Description
          >保存后立即应用到正在运行的隧道。凭据不会从服务端读回，也不会保存在浏览器中。</Card.Description
        >
      </Card.Header>
      <Card.Content class="space-y-4">
        {#key tunnel.editVersion}
          <TunnelConfiguration
            {current}
            disabled={tunnel.busy || !current.storageAvailable}
            onsave={save}
          />
        {/key}
        <p class="text-sm text-muted-foreground">
          更换主机名后，已有推送通知仍指向旧地址。请通过新地址打开 WebMux
          并重新订阅推送。
        </p>
        <p class="text-sm text-muted-foreground">
          WebMux 登录 Token 至少需要 43
          个字符，仍通过配置文件或配置命令管理；修改登录 Token 后需重启服务。
        </p>
        <Button
          variant="outline"
          href="https://github.com/mzdluo123/webmux/blob/main/docs/cloudflare-tunnel.md"
          target="_blank"
          rel="noreferrer"
          >打开配置指南<IconExternalLink aria-hidden="true" /></Button
        >
      </Card.Content>
    </Card.Root>
    <Card.Root>
      <Card.Header>
        <Card.Title>恢复配置文件设置</Card.Title>
        <Card.Description
          >删除网页覆盖并停止当前隧道；下次启动由 TOML 决定是否自动开启。不修改
          config.toml。</Card.Description
        >
      </Card.Header>
      <Card.Content>
        <Button
          variant="outline"
          disabled={tunnel.busy ||
            !current.storageAvailable ||
            current.source !== "web"}
          onclick={() => confirm("reset")}>恢复配置文件设置</Button
        >
      </Card.Content>
    </Card.Root>
  {:else if !tunnel.loading}
    <Button variant="outline" onclick={() => void tunnel.refresh()}
      >重新加载</Button
    >
  {/if}
  <InlineNotice tone="warning"
    >公网访问不是进程沙箱。任何获得访问地址和 WebMux Token
    的人都能以服务用户身份执行命令。修改隧道设置等同于扩大 Shell
    暴露面；仅向可信设备提供入口。</InlineNotice
  >
</div>

<AlertDialog.Root bind:open={confirmOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title
        >{action ? titles[action] : "确认操作"}</AlertDialog.Title
      >
      <AlertDialog.Description>
        {#if action === "enable"}
          任何获得地址和 WebMux Token
          的人都能以服务用户身份执行命令。开启状态会保存，服务重启后将自动恢复公网访问。
        {:else if action === "reset"}
          删除网页设置并停止当前隧道；下次启动按 config.toml
          的开关决定是否公开。已保存的网页隧道凭据会被删除。
        {:else if action === "save"}
          将停止旧 cloudflared 并按新配置启动，公网连接会短暂中断。
        {:else}
          撤销公网授权并停止 cloudflared，本机与 VPN
          访问仍可用，终端会话继续运行。
        {/if}
        {#if publicPage && action !== "enable"}应用后当前公网页面会断开，请确保仍可通过本机、VPN
          或新地址访问。{/if}
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel>取消</AlertDialog.Cancel>
      <AlertDialog.Action onclick={apply}>确认</AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
