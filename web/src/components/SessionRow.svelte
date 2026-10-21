<script lang="ts">
  import { navigate } from "../api";
  import SessionStatus from "./SessionStatus.svelte";
  import type { SessionInfo } from "../schemas";
  import { previewSettings } from "../previewSettings.svelte";
  import { selectPreview } from "../preview";
  import { IconArrowUpRight, IconDots } from "@tabler/icons-svelte";
  import { Button } from "$lib/components/ui/button";
  import * as Item from "$lib/components/ui/item";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";

  let {
    session,
    now,
    onclose,
  }: {
    session: SessionInfo;
    now: number;
    onclose: (session: SessionInfo) => void;
  } = $props();
  const seconds = $derived(
    Math.max(0, Math.floor((now - Date.parse(session.lastActiveAt)) / 1000)),
  );
  const relative = $derived(
    seconds < 60
      ? "刚刚"
      : seconds < 3600
        ? `${Math.floor(seconds / 60)} 分钟前`
        : `${Math.floor(seconds / 3600)} 小时前`,
  );
  const preview = $derived(selectPreview(session.preview, previewSettings));
</script>

<Item.Root
  role="listitem"
  class="session-row relative min-h-14 min-w-0 border-0 p-0"
  data-session-id={session.id}
  data-status={session.status}
>
  <a
    class="session-link group block w-full min-w-0 rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
    aria-label={session.title}
    href={`/s/${session.id}`}
    onclick={(event) => {
      if (
        event.button === 0 &&
        !event.ctrlKey &&
        !event.metaKey &&
        !event.shiftKey &&
        !event.altKey
      ) {
        event.preventDefault();
        navigate(`/s/${session.id}`);
      }
    }}
  >
    <div class="min-w-0 px-4 pt-4 pr-14">
      <Item.Title
        class="min-w-0 max-w-full text-sm font-medium group-hover:underline"
        title={session.title}
      >
        <span class="truncate">{session.title}</span>
        <IconArrowUpRight
          class="size-4 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
      </Item.Title>
      <div
        class="mt-2 grid auto-rows-[minmax(2.5rem,auto)] grid-cols-[repeat(auto-fit,minmax(min(100%,8rem),1fr))] items-start gap-x-4 gap-y-1 text-xs text-muted-foreground"
      >
        <SessionStatus {session} />
        <span
          >{session.clients
            ? `${session.clients} 个设备已连接`
            : "后台会话"}</span
        >
        <span>{relative}</span>
      </div>
    </div>
    <pre
      data-slot="terminal-preview"
      aria-label="终端输出预览"
      style:height={`calc(${previewSettings.lines}lh + 1rem)`}
      class:preview-empty={!preview}
      class="mt-4 min-w-0 overflow-hidden px-4 pb-4 font-mono text-xs leading-5 whitespace-pre text-foreground/85">{preview ||
        (session.preview
          ? "末尾行已隐藏，暂无可显示内容"
          : "等待终端输出…")}</pre>
  </a>
  <Item.Actions class="absolute top-1.5 right-1.5">
    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        {#snippet child({ props })}
          <Button
            {...props}
            variant="ghost"
            size="icon-sm"
            class="size-11"
            aria-label={`会话菜单 ${session.title}`}
            ><IconDots class="size-4" aria-hidden="true" /></Button
          >
        {/snippet}
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end">
        <DropdownMenu.Item
          variant="destructive"
          onSelect={() => onclose(session)}
        >
          {session.status === "exited" ? "移除会话" : "关闭会话"}
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  </Item.Actions>
</Item.Root>

<style>
  :global(.session-row) {
    background: var(--card);
    box-shadow: inset 0 0 0 1px var(--border);
  }

  :global(.session-row:hover) {
    background: color-mix(in oklab, var(--muted) 45%, var(--card));
  }

  .preview-empty {
    color: var(--muted-foreground);
    white-space: normal;
    overflow-wrap: anywhere;
  }
</style>
