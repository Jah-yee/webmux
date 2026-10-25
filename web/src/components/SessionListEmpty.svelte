<script lang="ts">
  import IconSearch from "~icons/tabler/search";
  import IconTerminal2 from "~icons/tabler/terminal-2";
  import { Button } from "$lib/components/ui/button";
  import * as Empty from "$lib/components/ui/empty";

  let {
    filtered,
    canStart,
    onclear,
    onstart,
  }: {
    // Filters hide every session, as opposed to there being none at all.
    filtered: boolean;
    canStart: boolean;
    onclear: () => void;
    onstart: () => void;
  } = $props();
</script>

{#if filtered}
  <Empty.Root variant="card" class="min-h-72">
    <Empty.Header>
      <Empty.Media variant="icon"><IconSearch /></Empty.Media>
      <Empty.Title>没有匹配的会话</Empty.Title>
      <Empty.Description
        >试试其他名称、目录或分支，或清除筛选查看全部会话。</Empty.Description
      >
    </Empty.Header>
    <Empty.Content>
      <Button variant="outline" onclick={onclear}>清除筛选</Button>
    </Empty.Content>
  </Empty.Root>
{:else}
  <Empty.Root variant="card" class="min-h-72">
    <Empty.Header>
      <Empty.Media variant="icon"><IconTerminal2 /></Empty.Media>
      <Empty.Title>还没有会话</Empty.Title>
      <Empty.Description>
        直接启动 Shell 或 agent；也可添加已有仓库，按 worktree
        组织多个独立终端。
      </Empty.Description>
    </Empty.Header>
    <Empty.Content>
      <Button disabled={!canStart} onclick={() => onstart()}>＋ 新建会话</Button
      >
    </Empty.Content>
  </Empty.Root>
{/if}
