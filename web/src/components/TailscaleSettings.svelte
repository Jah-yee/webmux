<script lang="ts">
  import { onMount } from "svelte";
  import { Button } from "$lib/components/ui/button";
  import * as Card from "$lib/components/ui/card";
  import TunnelStateBadge from "./TunnelStateBadge.svelte";
  import { api } from "../api";
  import { TailscaleStatusSchema, type TailscaleStatus } from "../schemas";

  let status = $state<TailscaleStatus>();
  let error = $state("");
  const modeText = {
    off: "未启用",
    serve: "Serve（仅 tailnet）",
    funnel: "Funnel（公网）",
  };
  onMount(() => {
    const controller = new AbortController();
    void api(
      "/tailscale",
      TailscaleStatusSchema,
      "GET",
      undefined,
      controller.signal,
    )
      .then((value) => {
        status = value;
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) error = (cause as Error).message;
      });
    return () => controller.abort();
  });
</script>

<Card.Root id="tailscale" class="scroll-mt-4">
  <Card.Header>
    <Card.Title>Tailscale（Serve / Funnel）</Card.Title>
    <Card.Description
      >使用 Tailscale 自动签发的 HTTPS 证书。Serve 仅 tailnet
      内设备可访问；Funnel 会公开到互联网。由配置文件管理，修改后需重启。</Card.Description
    >
  </Card.Header>
  <Card.Content class="space-y-4">
    {#if error}
      <p role="alert" class="text-sm text-destructive">{error}</p>
    {:else if status}
      <div class="flex flex-wrap items-center gap-2" role="status">
        <TunnelStateBadge state={status.state} />
        <span class="text-sm">{modeText[status.mode]}</span>
        {#if status.dnsName}
          <span class="text-sm text-muted-foreground">{status.dnsName}</span>
        {/if}
      </div>
      {#if status.publicUrl}
        <p class="text-sm wrap-anywhere">{status.publicUrl}</p>
      {:else if status.blockedReason || status.error}
        <p class="text-sm text-muted-foreground wrap-anywhere">
          {status.blockedReason || status.error}
        </p>
      {:else}
        <p class="text-sm text-muted-foreground">
          尚未启用；在配置文件的 [tailscale] 中设置 mode 后重启。
        </p>
      {/if}
    {:else}
      <p class="text-sm text-muted-foreground">正在读取 Tailscale 状态…</p>
    {/if}
    <Button href="/settings/tailscale" variant="outline">查看 Tailscale</Button>
  </Card.Content>
</Card.Root>
