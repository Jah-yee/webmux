<script lang="ts">
  import IconChevronDown from "~icons/tabler/chevron-down";
  import { Button } from "$lib/components/ui/button";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import * as Collapsible from "$lib/components/ui/collapsible";
  import type { Repository, SessionInfo, Worktree } from "../schemas";
  import { homeWorktrees } from "../homeWorktrees.svelte";
  import { homeLayout, worktreeTitle } from "../homeWorktrees";
  import { statusSummary } from "../session-status";
  import WorkspaceGroup from "./WorkspaceGroup.svelte";

  let {
    repo,
    sessions,
    visibleIds,
    filtering,
    open,
    onopenchange,
    gitReady,
    registryReady,
    refreshing,
    now,
    onstart,
    oncurate,
    onunregister,
    ontogglepin,
    onremove,
    onclose,
  }: {
    repo: Repository;
    // All sessions associated with this repository.
    sessions: SessionInfo[];
    visibleIds?: ReadonlySet<string>;
    filtering: boolean;
    open: boolean;
    onopenchange: (open: boolean) => void;
    gitReady: boolean;
    registryReady: boolean;
    refreshing: boolean;
    now: number;
    // Without a worktree id the sheet creates a new worktree.
    onstart: (worktreeId?: string) => void;
    oncurate: () => void;
    onunregister: () => void;
    ontogglepin: (worktreeId: string, pinned: boolean) => void;
    onremove: (worktree: Worktree) => void;
    onclose: (session: SessionInfo) => void;
  } = $props();

  let menuOpen = $state(false);
  $effect(() => {
    if (filtering && !visible.length) menuOpen = false;
  });
  let contentReady = $state(false);
  $effect(() => {
    if (open) contentReady = true;
  });

  const visible = $derived(
    visibleIds
      ? sessions.filter((session) => visibleIds.has(session.id))
      : sessions,
  );
  const missing = $derived(
    sessions.filter(
      (s) => !repo.worktrees.some((w) => w.id === s.workspace?.worktreeId),
    ),
  );
  const renderLayout = $derived(
    homeLayout(homeWorktrees.value, repo, sessions),
  );
  const layout = $derived(
    visibleIds ? homeLayout(homeWorktrees.value, repo, visible) : renderLayout,
  );
</script>

<Collapsible.Root
  class="min-w-0 space-y-4"
  data-repository-id={repo.id}
  hidden={filtering && !visible.length}
  role="region"
  aria-label={`仓库 ${repo.name}`}
  {open}
  onOpenChange={onopenchange}
>
  <div class="flex flex-wrap items-center gap-2">
    <h2 class="w-full min-w-0 flex-none sm:w-auto sm:flex-1">
      <Collapsible.Trigger
        class="group flex min-w-0 max-w-full items-center gap-2 text-left font-semibold outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <IconChevronDown
          class="size-4 shrink-0 -rotate-90 transition-transform group-data-[state=open]:rotate-0"
        />
        <span class="min-w-0 break-all">{repo.name}</span>
      </Collapsible.Trigger>
    </h2>
    <Button
      variant="outline"
      size="sm"
      disabled={repo.availability !== "available" || !gitReady}
      onclick={() => onstart()}>新建 worktree</Button
    >
    <Button
      variant="outline"
      size="sm"
      disabled={!layout.unpinned.length}
      onclick={oncurate}>添加已有 worktree</Button
    >
    <DropdownMenu.Root bind:open={menuOpen}>
      <DropdownMenu.Trigger
        >{#snippet child({ props })}<Button
            {...props}
            variant="ghost"
            size="icon-sm"
            aria-label={`仓库菜单 ${repo.name}`}>⋯</Button
          >{/snippet}</DropdownMenu.Trigger
      >
      <DropdownMenu.Content align="end"
        ><DropdownMenu.Item disabled={!registryReady} onSelect={onunregister}
          >从列表移除仓库</DropdownMenu.Item
        ></DropdownMenu.Content
      >
    </DropdownMenu.Root>
    <p class="w-full text-xs text-muted-foreground">
      主页 {layout.shown.length} / 共 {repo.worktrees.length} 工作区 ·
      {statusSummary(visible)}
    </p>
    <p
      class="w-full font-mono text-xs break-all whitespace-pre-wrap text-muted-foreground"
    >
      {repo.anchorPath}
    </p>
    <p class="w-full text-xs text-muted-foreground">
      {refreshing
        ? "正在重新读取 Git 状态…"
        : `Git 读取时间：${new Date(repo.checkedAt).toLocaleString()}`}{repo.availability ===
      "unavailable"
        ? " · 不可用，状态未知"
        : ""}
    </p>
    {#if repo.error}<p
        role="alert"
        class="w-full text-sm break-all text-destructive"
      >
        {repo.error}
      </p>{/if}
  </div>
  {#if contentReady}<Collapsible.Content
      forceMount
      hidden={!open}
      class="space-y-8"
    >
      {#each renderLayout.shown as { tree, pinned } (tree.id)}
        <WorkspaceGroup
          title={worktreeTitle(tree)}
          path={tree.path}
          worktree={tree}
          sessions={sessions.filter((s) => s.workspace?.worktreeId === tree.id)}
          {visibleIds}
          hidden={!open ||
            (filtering &&
              !visible.some(
                (session) => session.workspace?.worktreeId === tree.id,
              ))}
          {now}
          {pinned}
          ontogglepin={() => ontogglepin(tree.id, !pinned)}
          {onclose}
          onstart={() => onstart(tree.id)}
          onremove={tree.kind === "linked" ? () => onremove(tree) : undefined}
        />
      {/each}
      {#if missing.length}<WorkspaceGroup
          title="已不可用工作区"
          sessions={missing}
          {visibleIds}
          hidden={!open ||
            Boolean(
              visibleIds &&
              !missing.some((session) => visibleIds.has(session.id)),
            )}
          {now}
          {onclose}
        />{/if}
      {#if !layout.shown.length && layout.unpinned.length}<p
          class="text-sm text-muted-foreground"
        >
          主页还没有 worktree。用「添加已有
          worktree」选择需要关注的工作区，或新建一个。
        </p>{/if}
      {#if !repo.worktrees.length && !missing.length}<p
          class="text-sm text-muted-foreground"
        >
          {repo.error
            ? "无法读取工作树，请检查路径与 Git 权限，或从列表移除。"
            : "暂无可用工作树。"}
        </p>{/if}
    </Collapsible.Content>{/if}
</Collapsible.Root>
