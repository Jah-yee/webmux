<script lang="ts">
  import { onMount } from "svelte";
  import IconCopy from "~icons/tabler/copy";
  import IconRefresh from "~icons/tabler/refresh";
  import ConfirmDialog from "../components/ConfirmDialog.svelte";
  import { Button } from "$lib/components/ui/button";
  import * as Card from "$lib/components/ui/card";
  import InlineNotice from "../components/InlineNotice.svelte";
  import SettingsPageHeader from "../components/SettingsPageHeader.svelte";
  import TailscaleConfiguration from "../components/TailscaleConfiguration.svelte";
  import TunnelStateBadge from "../components/TunnelStateBadge.svelte";
  import { stored } from "../api";
  import { copyText } from "../clipboard";
  import { useTailscaleSettings } from "../hooks/tailscale-settings.svelte";
  import type { TailscaleSettingsUpdate, TailscaleStatus } from "../schemas";

  const tailscale = useTailscaleSettings();
  const ui = tailscale.state;
  const status = $derived(ui.view);
  const canReset = $derived(
    !ui.busy && !!status?.storageAvailable && status.source === "web",
  );
  let copyNotice = $state("");
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  let confirmOpen = $state(false);
  let action = $state<"funnel" | "save" | "reset">();
  let pending = $state<TailscaleSettingsUpdate>();
  $effect(() => {
    if (!confirmOpen) {
      pending = undefined;
      action = undefined;
    }
  });

  const providers = {
    linux: "Linux / macOS",
    "wsl-native": "WSL 内 Tailscale",
    windows: "Windows 主机",
    none: "未检测到",
  };
  const backend = {
    running: "运行中",
    stopped: "已停止",
    "needs-login": "需要登录",
    unavailable: "未安装或不可用",
    unknown: "无法确定",
  };
  const funnel = {
    available: "可用",
    unavailable: "当前节点未启用",
    unknown: "未知",
  };
  const modeText = {
    off: "未启用",
    serve: "Serve（仅 tailnet）",
    funnel: "Funnel（公网）",
  };
  const titles = {
    funnel: "开启公网 Funnel？",
    save: "替换当前访问的 Tailscale 入口？",
    reset: "恢复配置文件设置？",
  };
  const entryAuthority = (entry: TailscaleStatus["entries"][number]) =>
    entry.port === 443 ? entry.host : `${entry.host}:${entry.port}`;

  function note(message: string) {
    copyNotice = message;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copyNotice = ""), 2500);
  }
  async function copy(value: string) {
    const copied = await copyText(value);
    note(copied ? "已复制" : "复制失败，请检查浏览器剪贴板权限后重试");
  }
  async function copyURL() {
    if (status?.publicUrl) await copy(status.publicUrl);
  }
  async function copyTokenLink() {
    if (!status?.publicUrl) return;
    const token = stored("token");
    await copy(
      token
        ? status.publicUrl + "#token=" + encodeURIComponent(token)
        : status.publicUrl,
    );
  }
  function confirm(next: typeof action, draft?: TailscaleSettingsUpdate) {
    action = next;
    pending = draft;
    confirmOpen = true;
  }
  // Funnel needs an explicit risk confirmation. Changing the entry that
  // serves this very page would cut the connection, so confirm that too.
  function save(draft: TailscaleSettingsUpdate) {
    if (draft.mode === "funnel" && status?.mode !== "funnel")
      confirm("funnel", draft);
    else if (status?.publicUrl === location.origin) confirm("save", draft);
    else void tailscale.save(draft);
  }
  async function apply() {
    const selected = action;
    const draft = pending;
    confirmOpen = false;
    if (selected === "reset") await tailscale.reset();
    else if (draft)
      await tailscale.save({ ...draft, confirmFunnel: selected === "funnel" });
  }
  onMount(() => {
    document.title = "WebMux · Tailscale";
    return () => clearTimeout(copyTimer);
  });
</script>

<div class="safe-area-page min-h-dvh">
  <SettingsPageHeader />
  <main class="mx-auto max-w-3xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">
        Tailscale
      </h1>
      <p class="mt-2 text-sm text-muted-foreground">
        Serve 仅 tailnet 内设备可访问；Funnel
        会公开到互联网。网页设置保存后立即生效并覆盖配置文件的
        [tailscale]，无需重启。
      </p>
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <Button
        variant="outline"
        disabled={ui.loading || ui.busy}
        onclick={() => tailscale.load(true)}
        ><IconRefresh aria-hidden="true" />刷新检测</Button
      >
      {#if ui.loading}<span role="status" class="text-sm text-muted-foreground"
          >正在读取…</span
        >{/if}
      {#if ui.notice}<span role="status" class="text-sm text-muted-foreground"
          >{ui.notice}</span
        >{/if}
    </div>
    {#if ui.error}
      <InlineNotice tone="danger"
        >{ui.error}。Tailscale 操作失败不影响终端与其他设置。</InlineNotice
      >
    {/if}

    {#if status}
      {#if status.mode === "funnel"}
        <InlineNotice tone="warning">
          当前为 Funnel：<strong>公网 HTTPS</strong>，任何拿到链接与 Token
          的人都能访问并执行命令。仅在明确需要时保持开启。
        </InlineNotice>
      {/if}
      {#if status.blockedReason || status.error}
        <InlineNotice tone={status.state === "failed" ? "danger" : "warning"}>
          {status.blockedReason || status.error}
        </InlineNotice>
      {/if}

      <Card.Root>
        <Card.Header>
          <Card.Title>检测</Card.Title>
          <Card.Description
            >Tailscale 状态只在此处展示；WebMux 不会自动修改 tailnet
            设置、认证或已存在的其他入口。</Card.Description
          >
        </Card.Header>
        <Card.Content class="space-y-4">
          <div class="flex flex-wrap items-center gap-2" role="status">
            <TunnelStateBadge state={status.state} />
            <span class="text-sm">{providers[status.provider]}</span>
            {#if status.dnsName}<span
                class="font-mono text-xs text-muted-foreground"
                >{status.dnsName}</span
              >{/if}
          </div>
          <dl class="grid gap-3 text-sm sm:grid-cols-2">
            <div>
              <dt class="text-muted-foreground">后端状态</dt>
              <dd>{backend[status.backendState]}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">HTTPS 证书</dt>
              <dd>{status.httpsReady ? "已就绪" : "未就绪"}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">Funnel 节点属性</dt>
              <dd>{funnel[status.funnelAvailable]}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">配置 provider</dt>
              <dd>{status.configuredProvider}</dd>
            </div>
          </dl>
          {#if !status.httpsReady}
            <p class="text-sm text-muted-foreground">
              在 Tailscale 管理台为节点启用 HTTPS
              后点击“刷新检测”再保存；证书未就绪时不会执行 Serve/Funnel。
            </p>
          {/if}
        </Card.Content>
      </Card.Root>

      <Card.Root>
        <Card.Header>
          <Card.Title>托管入口</Card.Title>
          <Card.Description
            >WebMux
            只操作自己记录的精确端口，切换或关闭时先移除这个目标再应用新设置；不会调用
            reset。</Card.Description
          >
        </Card.Header>
        <Card.Content class="space-y-4">
          <dl class="grid gap-3 text-sm sm:grid-cols-2">
            <div>
              <dt class="text-muted-foreground">模式</dt>
              <dd>{modeText[status.mode]}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">端口</dt>
              <dd>{status.port === 0 ? "自动" : status.port}</dd>
            </div>
          </dl>
          {#if status.publicUrl}
            <div class="space-y-2">
              <p class="font-mono text-sm wrap-anywhere">{status.publicUrl}</p>
              <div class="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  onclick={() => copyURL()}
                  aria-label="复制 Tailscale 地址"
                  ><IconCopy aria-hidden="true" />复制地址</Button
                >
                <Button
                  variant="outline"
                  onclick={() => copyTokenLink()}
                  aria-label="复制带 Token 的 Tailscale 链接"
                  ><IconCopy aria-hidden="true" />复制带 Token 链接</Button
                >
              </div>
              {#if copyNotice}<span
                  role="status"
                  class="text-xs text-muted-foreground">{copyNotice}</span
                >{/if}
            </div>
          {:else}
            <p class="text-sm text-muted-foreground">
              当前没有已就绪的地址。{status.mode === "off"
                ? "选择 Serve 或 Funnel 并保存即可创建入口。"
                : "修复上方原因后重新保存即可再次应用。"}
            </p>
          {/if}
          <p class="text-sm text-muted-foreground">
            来源：{status.source === "web"
              ? "网页设置（覆盖配置文件）"
              : "配置文件"}。网页保存后持久化，重启后继续生效。
          </p>
          {#if !status.storageAvailable}
            <InlineNotice tone="danger"
              >网页设置不可用：{status.storageError}</InlineNotice
            >
          {:else if !status.canEnable}
            <InlineNotice tone="warning"
              >无法开启 Serve/Funnel：{status.enableBlockedReason}</InlineNotice
            >
          {/if}
          {#key ui.editVersion}
            <TailscaleConfiguration
              current={status}
              disabled={ui.busy || !status.storageAvailable}
              onsave={save}
            />
          {/key}
          <Button
            variant="outline"
            disabled={!canReset}
            onclick={() => confirm("reset")}>恢复配置文件设置</Button
          >
        </Card.Content>
      </Card.Root>

      {#if status.wsl}
        <Card.Root>
          <Card.Header>
            <Card.Title>WSL 环境</Card.Title>
            <Card.Description
              >WSL 中通常只在 Windows 主机运行 Tailscale。Windows provider
              必须显式选择，并要求 mirrored 网络。</Card.Description
            >
          </Card.Header>
          <Card.Content class="space-y-3">
            <p>
              网络模式：<strong>{status.wsl.networkingMode}</strong>；Windows
              Tailscale：{status.wsl.windowsDetected
                ? status.wsl.windowsRunning
                  ? "检测到且运行中"
                  : "检测到但未运行"
                : "未检测到"}
              {#if status.wsl.windowsDnsName}
                （{status.wsl.windowsDnsName}）
              {/if}
            </p>
            {#if status.configuredProvider === "windows" && status.mode !== "off" && status.wsl.networkingMode !== "mirrored"}
              <InlineNotice tone="warning">
                托管 Windows Tailscale 需要 mirrored 网络。可选方案：把
                %USERPROFILE%\.wslconfig 的 [wsl2] 设为 networkingMode=mirrored
                后执行 wsl --shutdown（会结束所有 WSL 会话）；或在 WSL 内安装
                Tailscale 并把 provider 设为 linux；或继续使用 Windows 托盘的
                Tailscale 转发并手工配置 Serve。WebMux 不会自动修改 .wslconfig
                或防火墙。
              </InlineNotice>
            {/if}
            {#if status.configuredProvider !== "windows" && status.wsl.windowsDetected}
              <p class="text-muted-foreground">
                要托管 Windows 节点，请在上方把 Provider 选为“Windows
                主机”；自动只检测，不会修改 Windows 全局配置。
              </p>
            {/if}
            {#if status.configuredProvider === "windows"}
              <p class="text-muted-foreground">
                权限不足时，请在 Windows 管理员终端执行一次 tailscale set
                --operator=&lt;Windows 用户&gt;；WebMux 不会使用 sudo
                或自动提权。
              </p>
            {/if}
          </Card.Content>
        </Card.Root>
      {/if}

      <Card.Root>
        <Card.Header>
          <Card.Title>现有 Serve / Funnel 入口</Card.Title>
          <Card.Description
            >只显示 host、port 与是否公开；不显示其他应用的目标路径。WebMux
            不会覆盖或删除这些入口。</Card.Description
          >
        </Card.Header>
        <Card.Content>
          {#if status.entries.length === 0}
            <p class="text-sm text-muted-foreground">没有现有入口。</p>
          {:else}
            <ul class="space-y-2 text-sm" aria-label="现有 Tailscale 入口">
              {#each status.entries as entry (entryAuthority(entry))}
                <li
                  class="flex flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2"
                >
                  <span class="font-mono wrap-anywhere"
                    >{entryAuthority(entry)}</span
                  >
                  <span class="text-muted-foreground"
                    >{entry.public
                      ? "公网（Funnel）"
                      : "仅 tailnet（Serve）"}</span
                  >
                </li>
              {/each}
            </ul>
          {/if}
        </Card.Content>
      </Card.Root>
    {/if}
  </main>
</div>

<ConfirmDialog
  bind:open={confirmOpen}
  title={action ? titles[action] : "确认操作"}
  onconfirm={apply}
>
  {#if action === "funnel"}
    Funnel 会把 WebMux 公开到互联网：任何获得地址和 WebMux Token
    的人都能以服务用户身份执行命令。设置会保存，服务重启后仍保持公开。
  {:else if action === "reset"}
    删除网页设置并关闭当前 Tailscale 入口；下次启动按 config.toml 的 [tailscale]
    决定是否创建入口。
  {:else}
    你正在通过这个 Tailscale 地址访问
    WebMux。应用新设置会先移除该入口，当前页面可能断开，需改用新地址或本地地址访问。
  {/if}
</ConfirmDialog>
