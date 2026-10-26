<script lang="ts">
  import { errorMessage } from "../error";
  import { onMount } from "svelte";
  import { Button } from "$lib/components/ui/button";
  import * as Card from "$lib/components/ui/card";
  import TunnelStateBadge from "./TunnelStateBadge.svelte";
  import { api } from "../api";
  import { ConfigSchema, type TunnelInfo } from "../schemas";

  let tunnel = $state<TunnelInfo>();
  let error = $state("");
  onMount(() => {
    const controller = new AbortController();
    void api("/config", ConfigSchema, "GET", undefined, controller.signal)
      .then((config) => {
        tunnel = config.tunnel;
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) error = errorMessage(cause);
      });
    return () => controller.abort();
  });
</script>

<Card.Root id="access" class="scroll-mt-4">
  <Card.Header>
    <Card.Title>公网访问（Cloudflare Tunnel）</Card.Title>
    <Card.Description
      >通过 HTTPS 入口从其他网络访问
      WebMux。配置和启停立即生效，无需重启；公网入口仍要求 WebMux Token。</Card.Description
    >
  </Card.Header>
  <Card.Content class="space-y-4">
    <section aria-label="隧道状态" class="min-w-0 space-y-2">
      {#if error}
        <p role="alert" class="text-sm text-destructive">{error}</p>
      {:else if tunnel}
        <div class="flex flex-wrap items-center gap-2" role="status">
          <TunnelStateBadge state={tunnel.state} />
          <span class="text-sm"
            >{tunnel.named ? "Named Tunnel" : "Quick Tunnel"}</span
          >
        </div>
        {#if tunnel.publicUrl || tunnel.hostname}
          <p class="text-sm wrap-anywhere">
            {tunnel.publicUrl || tunnel.hostname}
          </p>
        {/if}
      {:else}
        <p class="text-sm text-muted-foreground">正在读取隧道状态…</p>
      {/if}
    </section>
    <Button href="/settings/tunnel" variant="outline">管理公网访问</Button>
  </Card.Content>
</Card.Root>
