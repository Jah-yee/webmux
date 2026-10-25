<script lang="ts">
  import { onMount, tick } from "svelte";
  import type { CodeChange } from "../schemas";
  import { Button } from "$lib/components/ui/button";
  import { ListButton } from "$lib/components/ui/list-button";
  import IconAlertTriangle from "~icons/tabler/alert-triangle";
  import IconSquareLetterA from "~icons/tabler/square-letter-a";
  import IconSquareLetterC from "~icons/tabler/square-letter-c";
  import IconSquareLetterD from "~icons/tabler/square-letter-d";
  import IconSquareLetterM from "~icons/tabler/square-letter-m";
  import IconSquareLetterR from "~icons/tabler/square-letter-r";
  import IconSquareLetterT from "~icons/tabler/square-letter-t";
  import IconSquareLetterU from "~icons/tabler/square-letter-u";
  import IconSquareLetterX from "~icons/tabler/square-letter-x";
  let {
    entries,
    total,
    nextCursor,
    version,
    pending,
    enabled,
    onmore,
    onopen,
    group = "",
  }: {
    entries: CodeChange[];
    total: number;
    nextCursor: string;
    version: string;
    pending: boolean;
    enabled: boolean;
    onmore: () => void;
    onopen: (change: CodeChange) => void;
    group?: string;
  } = $props();
  const rowHeight = 32,
    overscan = 6;
  const groups = [
    ["unstaged", "未暂存"],
    ["staged", "已暂存"],
    ["untracked", "未跟踪"],
  ];
  // Git raw status letters (R/C carry a similarity score); untracked files are
  // reported as "A" by the backend but shown as "U" like most editors do.
  const statuses = {
    A: { icon: IconSquareLetterA, label: "新增", color: "text-success" },
    M: { icon: IconSquareLetterM, label: "修改", color: "text-warning" },
    D: { icon: IconSquareLetterD, label: "删除", color: "text-destructive" },
    R: { icon: IconSquareLetterR, label: "重命名", color: "text-info" },
    C: { icon: IconSquareLetterC, label: "复制", color: "text-info" },
    T: { icon: IconSquareLetterT, label: "类型变更", color: "text-info" },
    U: { icon: IconAlertTriangle, label: "冲突", color: "text-destructive" },
    untracked: {
      icon: IconSquareLetterU,
      label: "未跟踪",
      color: "text-success",
    },
    unknown: {
      icon: IconSquareLetterX,
      label: "未知",
      color: "text-muted-foreground",
    },
  };
  function statusOf(change: CodeChange) {
    if (change.group === "untracked") return statuses.untracked;
    return (
      statuses[change.status.charAt(0) as keyof typeof statuses] ??
      statuses.unknown
    );
  }
  const basename = (path: string) => path.slice(path.lastIndexOf("/") + 1);
  type Row = { key: string; label?: string; change?: CodeChange };
  const rows = $derived(
    groups
      .filter(([name]) => !group || name === group)
      .flatMap(([group, label]): Row[] => [
        { key: `group:${group}`, label },
        ...entries
          .filter((c) => c.group === group)
          .map((change) => ({ key: change.changeId, change })),
      ]),
  );
  let host: HTMLDivElement;
  let top = $state(0),
    height = $state(400);
  const start = $derived(Math.max(0, Math.floor(top / rowHeight) - overscan));
  const end = $derived(
    Math.min(rows.length, Math.ceil((top + height) / rowHeight) + overscan),
  );
  $effect(() => {
    void version;
    top = 0;
    if (host) host.scrollTop = 0;
  });
  onMount(() => {
    const observer = new ResizeObserver(() => {
      height = host.clientHeight;
    });
    observer.observe(host);
    height = host.clientHeight;
    return () => observer.disconnect();
  });
  function scroll() {
    top = host.scrollTop;
    if (
      enabled &&
      !pending &&
      nextCursor &&
      !document.hidden &&
      top + height >= rows.length * rowHeight - 4 * rowHeight
    )
      onmore();
  }
  async function keyboard(event: KeyboardEvent, index: number) {
    let target = index;
    if (event.key === "ArrowDown") target++;
    else if (event.key === "ArrowUp") target--;
    else if (event.key === "Home") target = 0;
    else if (event.key === "End") target = rows.length - 1;
    else return;
    event.preventDefault();
    const direction = event.key === "ArrowUp" || event.key === "End" ? -1 : 1;
    while (target >= 0 && target < rows.length && !rows[target].change)
      target += direction;
    if (target < 0 || target >= rows.length) {
      if (target >= rows.length && nextCursor && !pending && enabled) onmore();
      return;
    }
    if (target * rowHeight < top) host.scrollTop = target * rowHeight;
    else if ((target + 1) * rowHeight > top + height)
      host.scrollTop = (target + 1) * rowHeight - height;
    top = host.scrollTop;
    await tick();
    host
      .querySelector<HTMLButtonElement>(`[data-row-index="${target}"] button`)
      ?.focus({ preventScroll: true });
  }
</script>

<div
  class="min-h-0 flex-1 overflow-auto"
  bind:this={host}
  onscroll={scroll}
  role="list"
  aria-label="修改列表"
>
  <div
    class="relative h-(--list-height)"
    style:--list-height={`${rows.length * rowHeight}px`}
    style:--row-height={`${rowHeight}px`}
    role="presentation"
  >
    {#each rows.slice(start, end) as row, offset (row.key)}
      <div
        class="absolute top-(--row-top) right-0 left-0 h-(--row-height)"
        style:--row-top={`${(start + offset) * rowHeight}px`}
        data-row-index={start + offset}
        role="listitem"
      >
        {#if row.change}
          {@const change = row.change}
          {@const status = statusOf(change)}
          {@const path =
            change.oldPath === change.newPath
              ? change.newPath
              : `${change.oldPath} → ${change.newPath}`}
          <ListButton
            variant="row"
            class="flex h-full w-full min-w-0 items-center"
            title={`${status.label} ${path}`}
            aria-label={`${status.label} ${path}`}
            onclick={() => onopen(change)}
            onkeydown={(event) => void keyboard(event, start + offset)}
            ><status.icon
              class={`size-4 shrink-0 ${status.color}`}
              aria-hidden="true"
            /><span class="truncate">{basename(change.newPath)}</span
            ></ListButton
          >
        {:else}<h3 class="px-2 py-1 text-xs font-medium">{row.label}</h3>{/if}
      </div>
    {/each}
  </div>
</div>
<div class="shrink-0 border-t p-2 text-xs">
  <p>已加载 {entries.length} / {total} 项</p>
  {#if nextCursor}<Button
      variant="outline"
      size="sm"
      disabled={pending || !enabled}
      onclick={onmore}>{pending ? "加载中…" : "加载更多修改"}</Button
    >
  {:else if !pending && total === 0}<p>没有匹配的修改</p>{/if}
</div>
