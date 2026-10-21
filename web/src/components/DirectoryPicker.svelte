<script lang="ts">
  import { tick } from "svelte";
  import { IconFolder, IconArrowUp, IconHome } from "@tabler/icons-svelte";
  import { api } from "../api";
  import { DirectoriesSchema } from "../schemas";
  import { Button } from "$lib/components/ui/button";
  import * as Popover from "$lib/components/ui/popover";
  import * as Command from "$lib/components/ui/command";
  import { ScrollArea } from "$lib/components/ui/scroll-area";

  let { value = $bindable("") }: { value: string } = $props();
  let open = $state(false);
  // Prepare the floating content only when browsing is first requested, then
  // retain it through closes so the primitive can restore focus and animate.
  let browsed = $state(false);
  let query = $state("");
  let selected = $state("");
  let input = $state<HTMLInputElement | null>(null);
  let directories = $state<string[]>([]);
  let loading = $state(false);
  let error = $state("");
  let showHidden = $state(false);
  const current = $derived(
    query === "~" ? "~/" : query.slice(0, query.lastIndexOf("/") + 1) || "./",
  );
  const visible = $derived(
    directories.filter(
      (dir) =>
        showHidden ||
        query.split("/").pop()?.startsWith(".") ||
        !dir.split("/").pop()?.startsWith("."),
    ),
  );

  function startBrowse(nextOpen: boolean) {
    if (nextOpen) {
      browsed = true;
      query = (value || "~").replace(/\/+$/, "") + "/";
      selected = "";
    }
  }

  async function enter(dir: string) {
    query = dir.replace(/\/+$/, "") + "/";
    selected = "";
    await tick();
    input?.focus();
  }

  function parent() {
    const path = current.replace(/\/+$/, "");
    void enter(path.slice(0, path.lastIndexOf("/")) || "/");
  }

  function choose() {
    value = current === "/" ? "/" : current.replace(/\/+$/, "");
    open = false;
  }

  $effect(() => {
    if (!open) return;
    const prefix = query;
    let active = true;
    directories = [];
    selected = "";
    loading = true;
    error = "";
    const timer = setTimeout(() => {
      void api(
        "/fs/dirs?prefix=" + encodeURIComponent(prefix),
        DirectoriesSchema,
      )
        .then((result) => {
          if (active) directories = result;
        })
        .catch((err) => {
          if (active) error = (err as Error).message;
        })
        .finally(() => {
          if (active) loading = false;
        });
    }, 150);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  });
</script>

<Popover.Root bind:open onOpenChange={startBrowse}>
  <Popover.Trigger>
    {#snippet child({ props })}
      <Button {...props} variant="outline" aria-label="浏览目录">浏览</Button>
    {/snippet}
  </Popover.Trigger>
  {#if browsed}<Popover.Content
      class="directory-picker w-[min(90vw,520px)] gap-0 overflow-hidden p-0"
      align="end"
      onOpenAutoFocus={(event) => {
        event.preventDefault();
        input?.focus();
      }}
    >
      <Command.Root class="contents" shouldFilter={false} bind:value={selected}>
        <div class="flex shrink-0 items-center gap-1 border-b px-2 py-2">
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label="上一级目录"
            title="上一级目录（Alt+↑）"
            disabled={current === "/"}
            onclick={parent}><IconArrowUp class="size-4" /></Button
          >
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label="用户主目录"
            onclick={() => enter("~")}><IconHome class="size-4" /></Button
          >
          <span
            class="min-w-0 flex-1 truncate font-mono text-xs"
            title={current}>{current}</span
          >
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-pressed={showHidden}
            onclick={() => (showHidden = !showHidden)}
            >{showHidden ? "隐藏点目录" : "显示隐藏目录"}</Button
          >
        </div>
        <Command.Input
          bind:value={query}
          bind:ref={input}
          placeholder="输入路径，或筛选目录…"
          aria-label="目录路径"
          autocapitalize="off"
          autocorrect="off"
          spellcheck={false}
          onkeydown={(event) => {
            if (event.altKey && event.key === "ArrowUp") {
              event.preventDefault();
              event.stopPropagation();
              parent();
            } else if (event.key === "Tab" && !event.shiftKey && selected) {
              event.preventDefault();
              void enter(selected);
            }
          }}
        />
        <ScrollArea
          type="always"
          class="directory-scroll mx-2 mb-2 min-h-0 flex-1 overflow-hidden"
          scrollbarYClasses="bg-muted/50 [&_[data-slot=scroll-area-thumb]]:bg-muted-foreground"
        >
          <Command.List class="max-h-none overflow-visible pr-3">
            <Command.Empty>
              {loading
                ? "正在读取目录…"
                : error
                  ? "无法读取目录"
                  : "没有匹配的子目录"}
            </Command.Empty>
            {#each visible as dir (dir)}
              <Command.Item value={dir} onSelect={() => enter(dir)}>
                <IconFolder class="size-4 text-muted-foreground" />
                <span class="min-w-0 flex-1 truncate" title={dir}
                  >{dir.split("/").pop() || "/"}</span
                >
                <span class="text-muted-foreground" aria-hidden="true">›</span>
              </Command.Item>
            {/each}
          </Command.List>
        </ScrollArea>
      </Command.Root>
      {#if error}<p role="alert" class="px-3 py-2 text-xs text-destructive">
          {error}
        </p>{/if}
      <div
        class="flex shrink-0 items-center justify-between gap-2 border-t p-2"
      >
        <span class="text-xs text-muted-foreground"
          >↑↓ 选择 · Enter / Tab 进入</span
        >
        <Button
          type="button"
          size="sm"
          disabled={loading || !!error}
          onclick={choose}>选择当前目录</Button
        >
      </div>
    </Popover.Content>{/if}
</Popover.Root>

<style>
  :global(.directory-picker) {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: auto auto minmax(0, 1fr) auto;
    max-height: min(
      calc(100dvh - 16px),
      var(--bits-popover-content-available-height, calc(100dvh - 16px))
    );
  }

  :global(.directory-picker [data-slot="command-input-wrapper"]) {
    margin: 0.5rem;
    padding: 0;
    flex-shrink: 0;
  }

  :global(.directory-scroll [data-slot="scroll-area-viewport"]) {
    max-height: min(45dvh, 320px);
  }
</style>
