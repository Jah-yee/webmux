<script lang="ts">
  import type { Snippet } from "svelte";
  import IconArrowLeft from "~icons/tabler/arrow-left";
  import IconArrowDown from "~icons/tabler/arrow-down";
  import IconMaximize from "~icons/tabler/maximize";
  import IconMinimize from "~icons/tabler/minimize";
  import IconUpload from "~icons/tabler/upload";
  import IconSearch from "~icons/tabler/search";
  import { navigate, sessionStatusText } from "../api";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import type { Fullscreen } from "../hooks/fullscreen.svelte";
  import type { SessionInfo } from "../schemas";
  import type { TerminalDraft } from "../terminal-draft.svelte";
  import type { Connection } from "../ws";

  interface HeaderTerminal {
    readonly info: SessionInfo | undefined;
    readonly connection: Connection;
    readonly exitCode: number | null;
    readonly restoring: boolean;
    readonly historyTruncated: boolean;
    readonly canFind: boolean;
    scrollToBottom(): void;
    clear(): void;
    resync(): void;
  }

  let {
    terminal: t,
    fullscreen,
    draft,
    draftSaved,
    scratchSaved,
    uploading,
    desktop,
    codeShown = $bindable(),
    forceSoftKeys = $bindable(),
    quickMenu,
    upload,
    toggleDraft,
    copy,
    paste,
    showScratch,
    showFontSize,
    close,
    showFind,
  }: {
    terminal: HeaderTerminal;
    fullscreen: Fullscreen;
    draft: TerminalDraft;
    draftSaved: string;
    scratchSaved: string;
    uploading: boolean;
    desktop: boolean;
    codeShown: boolean;
    forceSoftKeys: boolean;
    quickMenu: Snippet<["top" | "bottom"]>;
    upload: (files: File[]) => Promise<void>;
    toggleDraft: () => void;
    copy: () => Promise<void>;
    paste: () => Promise<void>;
    showScratch: () => void;
    showFontSize: () => void;
    close: () => void;
    showFind: () => void;
  } = $props();
  let fileInput = $state<HTMLInputElement | null>(null);
  let openingFind = false;
  const connectionText = $derived(
    t.connection === "connected"
      ? "已连接"
      : t.connection === "exited"
        ? "已退出"
        : t.connection === "missing"
          ? "不存在"
          : "重连中",
  );
  const uploadDisabled = $derived(
    uploading ||
      (!draft.enabled && t.connection !== "connected") ||
      t.connection === "missing" ||
      t.exitCode !== null,
  );
  function scrollToLatest() {
    t.scrollToBottom();
    if (!desktop) codeShown = false;
  }
</script>

<header
  class="app-header terminal-header flex h-(--app-header-height) shrink-0 items-center gap-2 border-b bg-card px-2"
>
  <Button
    variant="ghost"
    size="icon-sm"
    aria-label="返回会话列表"
    title="返回会话列表"
    onclick={() => navigate("/")}
  >
    <IconArrowLeft class="size-4" aria-hidden="true" />
  </Button>
  <span class="min-w-0 flex-1 truncate text-sm font-medium"
    >{t.info?.title || "终端"}</span
  >
  <!-- Text appears before the dot so reconnecting only narrows the title
       instead of shifting the dot and the actions. -->
  {#if !t.info || t.connection !== "connected"}<span
      class="text-xs whitespace-nowrap text-muted-foreground"
      role="status">{connectionText}</span
    >{/if}
  {#if t.info}<span
      class={`status-dot status-${t.info.status}`}
      class:status-failed={t.info.status === "exited" &&
        t.info.exitCode !== null &&
        t.info.exitCode !== 0}
      role="img"
      aria-label={`${sessionStatusText(t.info)} · ${connectionText}`}
      title={`${sessionStatusText(t.info)} · ${connectionText}`}
    ></span>{/if}
  {#if fullscreen.offered}<Button
      variant="ghost"
      size="icon-sm"
      class="max-sm:hidden"
      aria-label={fullscreen.active ? "退出全屏" : "全屏"}
      title={fullscreen.active ? "退出全屏" : "全屏"}
      aria-pressed={fullscreen.active}
      aria-busy={fullscreen.busy}
      disabled={fullscreen.busy}
      onclick={() => {
        void fullscreen.toggle();
      }}
    >
      {#if fullscreen.active}<IconMinimize class="size-4" aria-hidden="true" />
      {:else}<IconMaximize class="size-4" aria-hidden="true" />{/if}
    </Button>{/if}
  <Button
    variant="ghost"
    size="icon-sm"
    class="max-sm:hidden"
    aria-label="上传文件"
    title="上传文件"
    disabled={uploadDisabled}
    onclick={() => fileInput?.click()}
    ><IconUpload class="size-4" aria-hidden="true" /></Button
  >
  <Input
    bind:ref={fileInput}
    type="file"
    multiple
    class="hidden"
    aria-label="选择上传文件"
    onchange={(event) => {
      const input = event.currentTarget;
      const files = Array.from(input.files ?? []);
      input.value = "";
      void upload(files);
    }}
  />
  <Button
    variant="ghost"
    size="icon-sm"
    class="max-sm:hidden"
    aria-label="滚动到最新位置"
    title="滚动到最新位置"
    disabled={t.restoring}
    onclick={scrollToLatest}
    ><IconArrowDown class="size-4" aria-hidden="true" /></Button
  >
  <div class="quick-commands-header flex">{@render quickMenu("bottom")}</div>
  <Button
    variant={draft.enabled ? "default" : "outline"}
    size="sm"
    aria-label={draft.scratch.length
      ? `草稿模式，${draft.scratch.length} 条暂存`
      : "草稿模式"}
    aria-pressed={draft.enabled}
    disabled={t.connection === "missing"}
    title={draft.enabled
      ? "关闭只隐藏草稿，不发送或清空"
      : draft.scratchUpdatedWhileHidden
        ? `暂存已更新，开启查看；${scratchSaved}`
        : draft.scratch.length
          ? `开启查看草稿与 ${draft.scratch.length} 条暂存；${scratchSaved}`
          : draft.updatedWhileHidden
            ? `隐藏草稿已更新，开启查看；${draftSaved}`
            : draft.text
              ? `开启查看保留的草稿；${draftSaved}`
              : "开启草稿模式"}
    onclick={toggleDraft}
    >草稿{#if !draft.enabled && draft.scratch.length}
      ·{draft.scratch
        .length}{:else if !draft.enabled && (draft.text || draft.updatedWhileHidden)}
      ·{/if}</Button
  >
  <Button
    variant="outline"
    size="sm"
    aria-pressed={codeShown}
    onclick={() => (codeShown = !codeShown)}
    >{codeShown ? (desktop ? "折叠代码" : "终端") : "代码"}</Button
  >
  <DropdownMenu.Root>
    <DropdownMenu.Trigger>
      {#snippet child({ props })}<Button
          {...props}
          variant="ghost"
          size="icon-sm"
          aria-label="终端菜单"
          title="终端菜单">⋯</Button
        >{/snippet}
    </DropdownMenu.Trigger>
    <DropdownMenu.Content
      align="end"
      onCloseAutoFocus={(event) => {
        if (openingFind) {
          event.preventDefault();
          openingFind = false;
        }
      }}
    >
      <!-- Narrow headers keep only frequent actions; these mirror the
           buttons hidden below 640px. -->
      {#if fullscreen.offered}<DropdownMenu.Item
          class="sm:hidden"
          disabled={fullscreen.busy}
          onSelect={() => {
            void fullscreen.toggle();
          }}>{fullscreen.active ? "退出全屏" : "全屏"}</DropdownMenu.Item
        >{/if}
      <DropdownMenu.Item
        class="sm:hidden"
        disabled={uploadDisabled}
        onSelect={() => fileInput?.click()}>上传文件</DropdownMenu.Item
      >
      <DropdownMenu.Item
        class="sm:hidden"
        disabled={t.restoring}
        onSelect={scrollToLatest}>滚动到最新位置</DropdownMenu.Item
      >
      <DropdownMenu.Separator class="sm:hidden" />
      <DropdownMenu.Item
        disabled={!t.canFind}
        onSelect={() => {
          openingFind = true;
          showFind();
        }}><IconSearch aria-hidden="true" />查找</DropdownMenu.Item
      >
      <DropdownMenu.Item
        onSelect={() => {
          void copy();
        }}
        >{t.historyTruncated
          ? "复制全部缓冲（更早输出已被覆盖）"
          : "复制全部缓冲"}</DropdownMenu.Item
      >
      <DropdownMenu.Item
        onSelect={() => {
          void paste();
        }}>粘贴</DropdownMenu.Item
      >
      <DropdownMenu.Item onSelect={showScratch}
        >暂存{draft.scratch.length
          ? ` (${draft.scratch.length})`
          : ""}</DropdownMenu.Item
      >
      <DropdownMenu.Item onSelect={() => t.clear()}>清屏</DropdownMenu.Item>
      <DropdownMenu.Item onSelect={() => t.resync()}>重新同步</DropdownMenu.Item
      >
      <DropdownMenu.Item onSelect={showFontSize}>字体大小</DropdownMenu.Item>
      <DropdownMenu.CheckboxItem bind:checked={forceSoftKeys}>
        强制显示底部工具栏
      </DropdownMenu.CheckboxItem>
      <DropdownMenu.Separator />
      <DropdownMenu.Item
        variant="destructive"
        disabled={!t.info}
        onSelect={close}
        >{t.exitCode !== null ? "移除会话" : "关闭会话"}</DropdownMenu.Item
      >
    </DropdownMenu.Content>
  </DropdownMenu.Root>
</header>
