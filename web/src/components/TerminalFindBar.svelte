<script lang="ts">
  import { tick } from "svelte";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import IconArrowUp from "~icons/tabler/arrow-up";
  import IconArrowDown from "~icons/tabler/arrow-down";
  import IconCopy from "~icons/tabler/copy";
  import IconX from "~icons/tabler/x";
  import type { TerminalController } from "../hooks/terminal.svelte";
  import {
    terminalFindError,
    terminalFindResultText,
    terminalFindScope,
    terminalFindReplayText,
    type TerminalFindOptions,
  } from "../terminal-search";

  let {
    open,
    terminal: t,
    onclose,
  }: {
    open: boolean;
    terminal: Pick<
      TerminalController,
      | "canFind"
      | "findResults"
      | "findReplayed"
      | "bufferType"
      | "scrollback"
      | "historyTruncated"
      | "restoring"
      | "selection"
      | "findNext"
      | "findPrevious"
      | "clearFind"
      | "copySelection"
    >;
    onclose: () => void;
  } = $props();
  let input = $state<HTMLInputElement | null>(null);
  let query = $state("");
  let options = $state<TerminalFindOptions>({
    caseSensitive: false,
    wholeWord: false,
    regex: false,
  });
  let composing = $state(false);
  const error = $derived(terminalFindError(query, options.regex));
  const disabled = $derived(
    !t.canFind || t.restoring || !query || !!error || composing,
  );
  const results = $derived(terminalFindResultText(t.findResults));
  $effect(() => {
    if (open)
      void tick().then(() => {
        if (open) input?.focus();
      });
  });
  export function focus() {
    input?.focus();
  }
  function search(previous = false, incremental = false) {
    if (composing) return;
    if (!query) {
      t.clearFind();
      return;
    }
    if (disabled) return;
    if (previous) t.findPrevious(query, options);
    else t.findNext(query, options, incremental);
  }
  function toggle(key: keyof TerminalFindOptions) {
    options[key] = !options[key];
    search(false, true);
  }
  function findKeys(node: HTMLFormElement) {
    const keydown = (event: KeyboardEvent) => {
      if (event.isComposing || composing || event.keyCode === 229) return;
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        onclose();
      } else if (event.key === "Enter" && event.target === input) {
        event.preventDefault();
        search(event.shiftKey);
      }
    };
    node.addEventListener("keydown", keydown);
    return { destroy: () => node.removeEventListener("keydown", keydown) };
  }
</script>

{#if open}
  <form
    role="search"
    aria-label="查找终端输出"
    class="absolute inset-x-2 top-2 z-20 rounded-md border bg-popover p-2 text-popover-foreground shadow-md"
    onsubmit={(event) => event.preventDefault()}
    use:findKeys
  >
    <div class="flex flex-wrap items-center gap-1">
      <Input
        bind:ref={input}
        value={query}
        aria-label="查找终端输出"
        aria-invalid={!!error}
        placeholder="查找终端输出"
        class="min-w-24 flex-1"
        autocapitalize="off"
        autocorrect="off"
        spellcheck={false}
        oninput={(event) => {
          query = event.currentTarget.value;
          search(false, true);
        }}
        oncompositionstart={() => (composing = true)}
        oncompositionend={(event) => {
          composing = false;
          query = event.currentTarget.value;
          search(false, true);
        }}
      />
      <div class="flex flex-wrap items-center gap-1">
        {#each [{ key: "caseSensitive", label: "区分大小写", text: "Aa" }, { key: "wholeWord", label: "全词匹配", text: "W" }, { key: "regex", label: "正则表达式", text: ".*" }] as const as option (option.key)}
          <Button
            variant={options[option.key] ? "secondary" : "ghost"}
            size="icon-sm"
            aria-label={option.label}
            title={option.label}
            aria-pressed={options[option.key]}
            onclick={() => toggle(option.key)}
            ><span class="font-mono text-xs">{option.text}</span></Button
          >
        {/each}
        {#each [{ previous: true, label: "上一个匹配", shortcut: "Shift+Enter", icon: IconArrowUp }, { previous: false, label: "下一个匹配", shortcut: "Enter", icon: IconArrowDown }] as direction (direction.label)}
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={direction.label}
            title={`${direction.label}（${direction.shortcut}）`}
            {disabled}
            onclick={() => search(direction.previous)}
            ><direction.icon aria-hidden="true" /></Button
          >
        {/each}
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="复制选中"
          title="复制选中"
          disabled={!t.selection}
          onclick={() => t.copySelection()}
          ><IconCopy aria-hidden="true" /></Button
        >
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="关闭查找"
          title="关闭查找（Esc）"
          onclick={onclose}><IconX aria-hidden="true" /></Button
        >
      </div>
    </div>
    <div
      role="status"
      aria-live="polite"
      aria-label="查找结果"
      class="mt-1 text-xs"
      class:text-destructive={!!error}
    >
      {error || results}
    </div>
    <p class="mt-1 text-xs text-muted-foreground">
      {terminalFindScope(t.scrollback, t.bufferType, t.historyTruncated)}
    </p>
    {#if query && terminalFindReplayText(t.restoring, t.findReplayed)}<p
        class="mt-1 text-xs text-muted-foreground"
      >
        {terminalFindReplayText(t.restoring, t.findReplayed)}
      </p>{/if}
  </form>
{/if}
