<script lang="ts">
  import type { Snippet } from "svelte";
  import IconFolderPlus from "~icons/tabler/folder-plus";
  import IconRefresh from "~icons/tabler/refresh";
  import { Button } from "$lib/components/ui/button";

  let {
    loading,
    filtering,
    visibleCount,
    sessionCount,
    repositoryCount,
    canAddRepository,
    refreshing,
    refreshDisabled,
    onadd,
    onrefresh,
    children,
  }: {
    loading: boolean;
    filtering: boolean;
    visibleCount: number;
    sessionCount: number;
    repositoryCount: number;
    canAddRepository: boolean;
    refreshing: boolean;
    refreshDisabled: boolean;
    onadd: () => void;
    onrefresh: () => void;
    // Search and status filters below the title row.
    children: Snippet;
  } = $props();
</script>

<div class="mb-5 space-y-3 border-b pb-4">
  <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
    <div class="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
      <h1 class="text-lg font-semibold tracking-tight">工作区</h1>
      <p role="status" class="text-xs text-muted-foreground">
        {#if loading}正在加载…
        {:else if filtering}显示 {visibleCount} / {sessionCount} 个会话
        {:else}{repositoryCount} 个仓库 · {sessionCount} 个会话{/if}
      </p>
    </div>
    <div
      class="ml-auto flex max-w-full flex-wrap items-center justify-end gap-1"
    >
      <Button
        variant="ghost"
        size="sm"
        class="min-h-9 sm:min-h-8"
        disabled={!canAddRepository}
        onclick={() => onadd()}
        ><IconFolderPlus aria-hidden="true" />添加仓库</Button
      >
      <Button
        variant="ghost"
        size="icon-sm"
        class="min-h-9 min-w-9 sm:min-h-8 sm:min-w-8"
        aria-label="刷新 Git 状态"
        title={refreshing ? "正在刷新 Git 状态…" : "刷新 Git 状态"}
        aria-busy={refreshing}
        disabled={refreshDisabled}
        onclick={() => onrefresh()}
        ><IconRefresh
          class={refreshing ? "animate-spin motion-reduce:animate-none" : ""}
          aria-hidden="true"
        /></Button
      >
    </div>
  </div>
  {@render children()}
</div>
