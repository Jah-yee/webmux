<script lang="ts">
  import { onDestroy } from "svelte";
  import { navigate } from "../api";
  import SessionStatus from "./SessionStatus.svelte";
  import type { SessionInfo } from "../schemas";
  import { previewSettings } from "../previewSettings.svelte";
  import { selectPreview } from "../preview";
  import { copyText } from "../clipboard";
  import { locationHints } from "../session-location";
  import IconArrowUpRight from "~icons/tabler/arrow-up-right";
  import IconDots from "~icons/tabler/dots";
  import { Badge } from "$lib/components/ui/badge";
  import { Button } from "$lib/components/ui/button";
  import * as Item from "$lib/components/ui/item";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";

  let {
    session,
    now,
    groupPath,
    onclose,
  }: {
    session: SessionInfo;
    now: number;
    groupPath?: string;
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
  const hints = $derived(locationHints(session, groupPath));
  const copyTarget = $derived(
    session.reportedCwd
      ? { label: "复制 Shell 报告目录", value: session.reportedCwd.path }
      : { label: "复制启动目录", value: session.cwd },
  );
  const reportedSince = $derived(
    session.reportedCwd
      ? new Date(session.reportedCwd.since).toLocaleString()
      : "",
  );
  let copyNotice = $state("");
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  onDestroy(() => clearTimeout(copyTimer));
  async function copyDirectory() {
    const copied = await copyText(copyTarget.value);
    copyNotice = copied ? "已复制" : "复制失败，请手动选择路径";
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copyNotice = ""), 2000);
  }
</script>

<Item.Root
  role="listitem"
  variant="card"
  size="flush"
  class="relative min-h-14 min-w-0"
  data-session-id={session.id}
  data-status={session.status}
>
  <a
    data-session-link
    class="group block w-full min-w-0 rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
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
      <Item.Title class="min-w-0 max-w-full" title={session.title}>
        <span class="truncate group-hover:underline">{session.title}</span>
        <IconArrowUpRight
          class="size-4 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
      </Item.Title>
      <!-- The status slot fits an exit label, so status changes never move the
           trailing text; only the trailing text varies in width. Text shares
           the badge's border and padding to align baselines. -->
      <div
        class="mt-2 flex flex-wrap items-start gap-x-3 gap-y-1 text-xs text-muted-foreground"
      >
        <div class="max-w-full min-w-[min(8.5rem,100%)]">
          <SessionStatus {session} />
        </div>
        <span class="border border-transparent py-0.5"
          >{session.clients ? `${session.clients} 个设备已连接` : "后台会话"} · {relative}</span
        >
        {#if copyNotice}<span role="status" class="text-success"
            >{copyNotice}</span
          >{/if}
      </div>
      {#if hints.launch || hints.reported}
        <div class="mt-2 space-y-1 text-xs text-muted-foreground">
          {#if hints.launch}
            <p class="flex min-w-0 items-baseline gap-1.5">
              <span class="shrink-0">启动目录</span>
              <span class="min-w-0 font-mono break-all" title={hints.launch}
                >{hints.launch}</span
              >
            </p>
          {/if}
          {#if hints.reported}
            <p
              class="flex min-w-0 flex-wrap items-baseline gap-x-1.5 gap-y-1"
              title={`Shell 通过 OSC 7 于 ${reportedSince} 报告；仅作导航线索，不改变会话归属`}
            >
              <span class="shrink-0">Shell 报告目录</span>
              <span class="min-w-0 font-mono break-all"
                >{hints.reported.path}</span
              >
              {#if hints.reported.remote}<Badge variant="outline"
                  >远端 {hints.reported.host}</Badge
                >{/if}
            </p>
          {/if}
        </div>
      {/if}
    </div>
    <pre
      data-slot="terminal-preview"
      aria-label="终端输出预览"
      style:--preview-lines={previewSettings.lines}
      class:preview-empty={!preview}
      class="h-preview mt-4 min-w-0 overflow-hidden px-4 pb-4 font-mono text-xs leading-5 whitespace-pre text-foreground/85">{preview ||
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
            class="size-9 sm:size-11"
            aria-label={`会话菜单 ${session.title}`}
            ><IconDots class="size-4" aria-hidden="true" /></Button
          >
        {/snippet}
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end">
        <DropdownMenu.Item onSelect={() => void copyDirectory()}
          >{copyTarget.label}</DropdownMenu.Item
        >
        <DropdownMenu.Separator />
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
  .preview-empty {
    color: var(--muted-foreground);
    white-space: normal;
    overflow-wrap: anywhere;
  }
</style>
