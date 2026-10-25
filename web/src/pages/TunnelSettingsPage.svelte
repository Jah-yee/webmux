<script lang="ts">
  import { onMount } from "svelte";
  import { Button } from "$lib/components/ui/button";
  import TunnelManager from "../components/TunnelManager.svelte";
  onMount(() => {
    document.title = "WebMux · 公网访问";
  });
</script>

<div class="safe-area-page min-h-dvh">
  <header class="app-header border-b bg-card">
    <div
      class="mx-auto flex min-h-(--app-header-height) max-w-6xl items-center justify-between gap-3 px-4 py-1 sm:px-8"
    >
      <a class="brand" href="/"><span class="brand-mark">WM</span> WebMux</a>
      <Button href="/settings" variant="outline">返回设置</Button>
    </div>
  </header>
  <main class="mx-auto max-w-3xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">
        公网访问
      </h1>
      <p class="mt-2 text-sm text-muted-foreground">
        配置 Cloudflare Tunnel，跨设备保存设置，无需重启终端服务。
      </p>
    </div>
    <TunnelManager />
  </main>
</div>
