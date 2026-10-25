<script lang="ts">
  import type { Snippet } from "svelte";
  import IconArrowDown from "~icons/tabler/arrow-down";
  import { Button } from "$lib/components/ui/button";
  import SoftKeys from "./SoftKeys.svelte";
  import type { Modifiers } from "../input";
  import type { UploadProgress } from "../hooks/upload.svelte";
  import type { Connection } from "../ws";

  interface ViewportTerminal {
    readonly selection: string;
    readonly restoring: boolean;
    readonly atBottom: boolean;
    readonly connection: Connection;
    readonly modifiers: Modifiers;
    copySelection(): void;
    clearSelection(): void;
    scrollToBottom(): void;
  }
  interface ViewportUploads {
    readonly dragging: boolean;
    readonly progress: UploadProgress | null;
  }

  // The xterm host with its overlays, above the soft-key toolbar.
  let {
    container = $bindable(),
    terminal: t,
    visible,
    uploads,
    quickMenu,
    send,
    toggle,
    paste,
    keyboard,
  }: {
    container?: HTMLDivElement;
    terminal: ViewportTerminal;
    // Whether the terminal pane is on screen (mobile may show code instead).
    visible: boolean;
    uploads: ViewportUploads;
    quickMenu: Snippet<["top" | "bottom"]>;
    send: (input: string) => void;
    toggle: (key: keyof Modifiers) => void;
    paste: () => Promise<void>;
    keyboard: () => void;
  } = $props();
</script>

<div class="terminal-column">
  <div class="terminal-area">
    <div class="terminal-host" bind:this={container}></div>
    {#if uploads.dragging}
      <div
        class="pointer-events-none absolute inset-0 z-20 flex items-center justify-center border-2 border-dashed border-primary bg-background/90"
        role="status"
      >
        <p class="text-sm">
          {uploads.progress ? "请等待当前上传完成" : "松开以上传文件到临时目录"}
        </p>
      </div>
    {/if}
    {#if t.selection}
      <div
        class="absolute right-2 top-2 z-10 flex items-center gap-1 rounded-md border bg-popover p-1 shadow-md"
        role="toolbar"
        aria-label="终端文字选择"
      >
        <Button size="sm" onclick={() => t.copySelection()}>复制选中</Button>
        <Button variant="ghost" size="sm" onclick={() => t.clearSelection()}
          >取消选择</Button
        >
      </div>
    {/if}
    {#if t.restoring && t.connection !== "missing"}<div
        class="restore-overlay"
        role="status"
      >
        {t.connection === "connected" ? "正在恢复…" : "正在连接…"}
      </div>{/if}
    {#if !t.atBottom && !t.restoring && visible}<Button
        variant="secondary"
        size="sm"
        raised
        class="absolute right-3 bottom-3 z-10 sm:hidden"
        aria-label="回到最新"
        onmousedown={(event) => event.preventDefault()}
        onclick={() => t.scrollToBottom()}
        ><IconArrowDown aria-hidden="true" />回到最新</Button
      >{/if}
  </div>
  <SoftKeys
    {send}
    modifiers={t.modifiers}
    {toggle}
    paste={() => {
      void paste();
    }}
    {keyboard}
  >
    {#snippet trailing()}{@render quickMenu("top")}{/snippet}
  </SoftKeys>
</div>
