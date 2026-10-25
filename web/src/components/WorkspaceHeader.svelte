<script lang="ts">
  import { onMount } from "svelte";
  import IconMoon from "~icons/tabler/moon";
  import IconSettings from "~icons/tabler/settings";
  import IconSun from "~icons/tabler/sun";
  import { Button } from "$lib/components/ui/button";
  import { toggleTheme } from "../theme";
  import type { Connection } from "../ws";

  let {
    connection,
    hostname,
    setupSettled,
    canStart,
    onstart,
  }: {
    connection: Connection;
    hostname: string;
    setupSettled: boolean;
    canStart: boolean;
    onstart: () => void;
  } = $props();

  let dark = $state(document.documentElement.classList.contains("dark"));
  const statusLabel = $derived(
    connection === "connected"
      ? hostname
        ? `${hostname} 在线`
        : "在线"
      : "正在连接",
  );

  onMount(() => {
    const theme = new MutationObserver(() => {
      dark = document.documentElement.classList.contains("dark");
    });
    theme.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    return () => theme.disconnect();
  });
</script>

<header
  class="app-header flex min-h-(--app-header-height) items-center border-b bg-card"
>
  <div
    class="mx-auto flex w-full max-w-6xl flex-wrap items-center justify-between gap-2 px-4 py-0.5 sm:px-8"
  >
    <a class="brand" href="/"><span class="brand-mark">WM</span> WebMux</a>
    <div
      class="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-1 sm:gap-2"
    >
      <!-- Reserved width keeps the actions still. The dot is pinned at the
           end beside the actions, and the text appears only in its final
           form (connected, hostname known), so nothing already painted
           moves; long hostnames truncate. -->
      <span
        class="hidden h-4 w-52 min-w-0 items-center justify-end gap-2 text-xs whitespace-nowrap text-muted-foreground sm:flex"
        title={statusLabel}
        >{#if connection === "connected" && setupSettled}<span
            class="min-w-0 truncate">{statusLabel}</span
          >{:else}<span class="sr-only">{statusLabel}</span>{/if}<span
          class={`status-dot ${connection === "connected" ? "status-running" : "status-waiting"}`}
        ></span></span
      >
      <Button size="sm" disabled={!canStart} onclick={() => onstart()}
        >＋ 新建会话</Button
      >
      <Button
        href="/settings"
        variant="ghost"
        size="icon-sm"
        aria-label="设置"
        title="设置"><IconSettings aria-hidden="true" /></Button
      >
      <Button
        variant="ghost"
        size="icon-sm"
        class="shrink-0"
        aria-label={dark ? "切换为白色主题" : "切换为黑色主题"}
        title={dark ? "切换为白色主题" : "切换为黑色主题"}
        onclick={toggleTheme}
        >{#if dark}<IconSun class="size-4" />{:else}<IconMoon
            class="size-4"
          />{/if}</Button
      >
    </div>
  </div>
</header>
