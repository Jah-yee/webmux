<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import IconCheck from "~icons/tabler/check";
  import IconCopy from "~icons/tabler/copy";
  import IconExternalLink from "~icons/tabler/external-link";
  import { Badge, type BadgeVariant } from "$lib/components/ui/badge";
  import { Button } from "$lib/components/ui/button";
  import * as Card from "$lib/components/ui/card";
  import * as Tabs from "$lib/components/ui/tabs";
  import InlineNotice from "./InlineNotice.svelte";
  import { api } from "../api";
  import { copyText } from "../clipboard";
  import { ConfigSchema, type TunnelInfo } from "../schemas";

  const docsUrl =
    "https://github.com/mzdluo123/webmux/blob/main/docs/cloudflare-tunnel.md";
  const template = `[tunnel]
enabled = true
token = "Cloudflare 隧道 Token"
hostname = "term.example.com"`;

  async function fetchTunnel(
    signal?: AbortSignal,
  ): Promise<TunnelInfo | undefined> {
    return (await api("/config", ConfigSchema, "GET", undefined, signal))
      .tunnel;
  }

  let {
    loadTunnel = fetchTunnel,
  }: {
    loadTunnel?: (signal?: AbortSignal) => Promise<TunnelInfo | undefined>;
  } = $props();

  let tunnel = $state<TunnelInfo>();
  let statusError = $state("");
  let mode = $state("named");
  let copied = $state<"idle" | "copied" | "failed">("idle");
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  onDestroy(() => clearTimeout(copyTimer));

  const stateLabel = $derived(
    tunnel?.state === "ready"
      ? "已就绪"
      : tunnel?.state === "starting"
        ? "启动中"
        : tunnel?.state === "failed"
          ? "异常停止"
          : "未启用",
  );
  const stateVariant = $derived<BadgeVariant>(
    tunnel?.state === "ready"
      ? "success"
      : tunnel?.state === "starting"
        ? "warning"
        : tunnel?.state === "failed"
          ? "destructive"
          : "outline",
  );
  const modeLabel = $derived(tunnel?.named ? "Named Tunnel" : "Quick Tunnel");

  async function load(signal: AbortSignal) {
    try {
      const info = await loadTunnel(signal);
      if (!info) {
        statusError = "当前服务端未提供隧道状态；请刷新页面或升级 WebMux。";
        return;
      }
      tunnel = info;
      statusError = "";
    } catch (cause) {
      if (signal.aborted) return;
      const detail = cause instanceof Error ? cause.message : String(cause);
      statusError = `无法读取隧道状态：${detail}`;
    }
  }
  onMount(() => {
    const controller = new AbortController();
    load(controller.signal);
    return () => controller.abort();
  });

  async function copyTemplate() {
    copied = (await copyText(template)) ? "copied" : "failed";
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copied = "idle"), 2000);
  }
</script>

<Card.Root id="access" class="scroll-mt-4">
  <Card.Header>
    <Card.Title>公网访问（Cloudflare Tunnel）</Card.Title>
    <Card.Description
      >默认只在本机或可信 VPN 内访问。需要从任意网络打开 WebMux 时，可经
      Cloudflare Tunnel 获得 HTTPS 入口；公网入口仍要求 WebMux Token。</Card.Description
    >
  </Card.Header>
  <Card.Content class="space-y-4">
    <section aria-label="隧道状态" class="min-w-0 space-y-2">
      {#if statusError}
        <p role="alert" class="text-xs text-destructive">{statusError}</p>
      {:else if tunnel}
        <div class="flex flex-wrap items-center gap-2" role="status">
          <Badge variant={stateVariant}>{stateLabel}</Badge>
          {#if tunnel.state !== "off"}
            <span class="text-sm">{modeLabel}</span>
          {:else if tunnel.named}
            <span class="text-sm">已配置 Named Tunnel</span>
          {/if}
        </div>
        {#if tunnel.state === "ready"}
          {#if tunnel.publicUrl}
            <p class="min-w-0 text-sm wrap-anywhere">
              公网地址：<code>{tunnel.publicUrl}</code>
            </p>
          {:else}
            <p class="text-sm text-muted-foreground">
              隧道已就绪，但服务端未报告公网地址。
            </p>
          {/if}
        {:else if tunnel.state === "starting"}
          <p class="text-sm text-muted-foreground">
            正在启动隧道；就绪后此处显示公网地址。
          </p>
        {:else if tunnel.state === "failed"}
          <p class="text-sm text-muted-foreground">
            隧道已停止或连接断开，本机与 VPN
            访问不受影响；请在托盘重试或查看服务日志。
          </p>
        {:else}
          <p class="text-sm text-muted-foreground">
            当前未启用公网隧道；可在托盘临时开启，或设置
            <code>[tunnel].enabled = true</code> 后重启。
          </p>
        {/if}
        {#if tunnel.named}
          {#if tunnel.hostname}
            <p class="min-w-0 text-sm wrap-anywhere">
              公共主机名：<code>{tunnel.hostname}</code>
            </p>
          {:else}
            <p class="text-sm text-warning">
              Named Tunnel 缺少 <code>hostname</code
              >，无法发布固定公网地址；请在配置中补充后重启。
            </p>
          {/if}
        {/if}
      {:else}
        <p class="text-sm text-muted-foreground">正在读取隧道状态…</p>
      {/if}
    </section>
    <Tabs.Root bind:value={mode}>
      <Tabs.List class="w-full">
        <Tabs.Trigger value="quick">Quick Tunnel</Tabs.Trigger>
        <Tabs.Trigger value="named">Named Tunnel（推荐）</Tabs.Trigger>
      </Tabs.List>
      <Tabs.Content value="quick" class="space-y-2 pt-3">
        <p>
          适合临时访问，不需要 Cloudflare 账号。运行
          <code>webmux --tunnel</code>，或在配置文件的
          <code>[tunnel]</code> 段中设置
          <code>enabled = true</code>。每次启动地址都会变化，不支持手机推送。
        </p>
        <p>
          若配置了根 <code>token</code>，隧道模式要求至少 43
          个字符；留空则每次启动自动生成。
        </p>
      </Tabs.Content>
      <Tabs.Content value="named" class="space-y-3 pt-3">
        <p>
          需要 Cloudflare 账号，以及已接入 Cloudflare
          的域名。固定入口也用于手机推送。
        </p>
        <ol class="list-decimal space-y-1 pl-5">
          <li>
            Cloudflare 控制台 → <strong
              >Networking → Tunnels → Create a tunnel → Cloudflared</strong
            >，创建隧道（旧界面为 Zero Trust → Networks → Tunnels）。
          </li>
          <li>
            打开隧道 <strong>Overview → Add a replica</strong>，复制安装命令里的
            <code>eyJ...</code> token；不要运行这条安装命令，WebMux 会自己启动 cloudflared。
          </li>
          <li>
            <strong>Routes → Add route → Published application</strong
            >：填写子域名和域名，Path 留空；Service URL 填
            <code>http://127.0.0.1:7681</code>（与 <code>listen</code>
            一致）。旧界面称 Public Hostname。
          </li>
          <li>把下方模板加入 WebMux 配置文件，替换 token 与 hostname。</li>
          <li>
            重启 WebMux，打开 <code>https://&lt;hostname&gt;</code
            >；首次登录使用 WebMux Token。
          </li>
        </ol>
        <div class="space-y-2">
          <pre
            aria-label="Named Tunnel 配置模板"
            class="min-w-0 overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs leading-5 whitespace-pre text-foreground dark:bg-background">{template}</pre>
          <div class="flex flex-wrap items-center gap-2">
            <Button variant="outline" onclick={copyTemplate}>
              {#if copied === "copied"}
                <IconCheck aria-hidden="true" />已复制
              {:else}
                <IconCopy aria-hidden="true" />复制配置模板
              {/if}
            </Button>
            <Button
              variant="outline"
              href={docsUrl}
              target="_blank"
              rel="noreferrer"
              >查看完整步骤与故障排查<IconExternalLink
                aria-hidden="true"
              /></Button
            >
          </div>
          {#if copied === "failed"}
            <p role="alert" class="text-xs text-destructive">
              复制失败，请手动选择上方模板。
            </p>
          {/if}
        </div>
        <InlineNotice tone="warning">
          <p>
            根 <code>token</code> 是 WebMux 登录 Token（隧道模式至少 43
            个字符）；<code>[tunnel].token</code> 是 Cloudflare 隧道凭据，两者不同。
          </p>
          <p>
            <code>hostname</code>
            只写纯主机名，不要带 https://、路径、端口或通配符。修改配置后必须重启
            WebMux；重启会结束所有终端会话。
          </p>
        </InlineNotice>
      </Tabs.Content>
    </Tabs.Root>
  </Card.Content>
</Card.Root>
