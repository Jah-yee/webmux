<script lang="ts">
  import { SvelteSet } from "svelte/reactivity";
  import type { Worktree } from "../schemas";
  import { worktreeTitle } from "../homeWorktrees";
  import { Button } from "$lib/components/ui/button";
  import { Badge } from "$lib/components/ui/badge";
  import * as Command from "$lib/components/ui/command";
  import { ScrollArea } from "$lib/components/ui/scroll-area";
  import ResponsiveDialog from "./ResponsiveDialog.svelte";
  let {
    repositoryName,
    worktrees,
    active,
    onadd,
    onclose,
  }: {
    repositoryName: string;
    worktrees: Worktree[];
    active: ReadonlySet<string>;
    onadd: (ids: string[]) => void;
    onclose: () => void;
  } = $props();
  const selected = new SvelteSet<string>();
</script>

<ResponsiveDialog
  title="添加已有 worktree 到主页"
  description={`${repositoryName}：只把正在关注的 worktree 放到主页。移出主页不会删除 worktree；有会话的 worktree 始终显示。`}
  {onclose}
>
  <form
    class="space-y-4"
    onsubmit={(event) => {
      event.preventDefault();
      onadd([...selected]);
      onclose();
    }}
  >
    <Command.Root
      label="可添加的 worktree"
      variant="outline"
      class="h-auto min-w-0"
      filter={(value, search, keywords) =>
        [value, ...(keywords ?? [])].some((text) =>
          text.toLowerCase().includes(search.trim().toLowerCase()),
        )
          ? 1
          : 0}
      disableInitialScroll
    >
      <Command.Input
        placeholder="按分支或路径搜索…"
        autocomplete="off"
        autocapitalize="off"
        autocorrect="off"
        spellcheck={false}
      />
      <ScrollArea
        type="always"
        class="h-72 min-w-0 overflow-hidden"
        scrollbarYClasses="bg-muted/50 [&_[data-slot=scroll-area-thumb]]:bg-muted-foreground"
      >
        <Command.List
          aria-label="可添加的 worktree 列表"
          aria-multiselectable="true"
          class="max-h-none overflow-visible pr-3"
        >
          <Command.Empty>没有匹配的 worktree</Command.Empty>
          {#each worktrees as tree (tree.id)}
            <Command.Item
              value={tree.id}
              keywords={[worktreeTitle(tree), tree.path]}
              data-checked={selected.has(tree.id)}
              aria-label={worktreeTitle(tree)}
              onSelect={() => {
                if (selected.has(tree.id)) selected.delete(tree.id);
                else selected.add(tree.id);
              }}
            >
              <span class="min-w-0 flex-1 space-y-1">
                <span class="flex flex-wrap items-center gap-1.5">
                  <span class="break-all font-medium"
                    >{worktreeTitle(tree)}</span
                  >
                  {#if tree.kind === "main"}<Badge variant="secondary"
                      >主工作区</Badge
                    >{/if}
                  {#if active.has(tree.id)}<Badge variant="outline"
                      >有会话</Badge
                    >{/if}
                  {#if !tree.available}<Badge variant="destructive"
                      >不可用</Badge
                    >{:else if tree.dirty}<Badge variant="outline">有修改</Badge
                    >{/if}
                </span>
                <span
                  class="block font-mono break-all whitespace-pre-wrap text-muted-foreground"
                  >{tree.path}</span
                >
              </span>
            </Command.Item>
          {/each}
        </Command.List>
      </ScrollArea>
    </Command.Root>
    <p role="status" class="text-xs text-muted-foreground">
      {selected.size
        ? `已选择 ${selected.size} 个`
        : "点击或用 ↑↓ 和 Enter 选择，可多选。"}
    </p>
    <div class="flex justify-end gap-2">
      <Button type="button" variant="outline" onclick={onclose}>取消</Button>
      <Button type="submit" disabled={!selected.size}
        >{selected.size
          ? `添加 ${selected.size} 个到主页`
          : "添加到主页"}</Button
      >
    </div>
  </form>
</ResponsiveDialog>
