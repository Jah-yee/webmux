<script lang="ts">
  import InlineNotice from "../components/InlineNotice.svelte";
  import { onMount } from "svelte";
  import {
    IconSun,
    IconMoon,
    IconChevronDown,
    IconSettings,
    IconFolderPlus,
    IconRefresh,
    IconTerminal2,
    IconSearch,
  } from "@tabler/icons-svelte";
  import { toggleTheme } from "../theme";
  import { api } from "../api";
  import {
    PresetsSchema,
    ConfigSchema,
    SessionEventSchema,
    RepositoriesSchema,
    NoContentSchema,
    type Preset,
    type SessionInfo,
    type Repository,
    type Worktree,
  } from "../schemas";
  import { connect, type Connection } from "../ws";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import * as Collapsible from "$lib/components/ui/collapsible";
  import * as Empty from "$lib/components/ui/empty";
  import NewSessionSheet from "../components/NewSessionSheet.svelte";
  import CloseSessionDialog from "../components/CloseSessionDialog.svelte";
  import WorkspaceGroup from "../components/WorkspaceGroup.svelte";
  import AddRepositoryDialog from "../components/AddRepositoryDialog.svelte";
  import RemoveWorktreeDialog from "../components/RemoveWorktreeDialog.svelte";
  import AddHomeWorktreesDialog from "../components/AddHomeWorktreesDialog.svelte";
  import { homeWorktrees } from "../homeWorktrees.svelte";
  import {
    homeLayout,
    pruneHomeWorktrees,
    setPinned,
    worktreeTitle,
  } from "../homeWorktrees";
  import { matchesSessionQuery } from "../session-location";
  import type { TerminalDrafts } from "../terminal-draft.svelte";

  let { drafts }: { drafts: TerminalDrafts } = $props();

  let sessions = $state<SessionInfo[]>([]);
  let repositories = $state<Repository[]>([]);
  let presets = $state<Preset[]>([]);
  let cwd = $state("~");
  let hostname = $state("");
  let setupSettled = $state(false);
  let loading = $state(true);
  let initializing = $state(true);
  let refreshing = $state(false);
  let repositoryError = $state("");
  let capabilityError = $state("");
  let gitReady = $state(false);
  let registryReady = $state(false);
  let connection = $state<Connection>("connecting");
  const statusLabel = $derived(
    connection === "connected"
      ? hostname
        ? `${hostname} 在线`
        : "在线"
      : "正在连接",
  );
  let creating = $state<{
    mode: "direct" | "existing" | "new";
    repositoryId: string;
    worktreeId: string;
  } | null>(null);
  let adding = $state(false);
  let removing = $state<Worktree | null>(null);
  let curating = $state<Repository | null>(null);
  let unregistering = $state<Repository | null>(null);
  let unregisterOpen = $state(false);
  let unregisterBusy = $state(false);
  let unregisterError = $state("");
  let closing = $state<SessionInfo | null>(null);
  let error = $state("");
  let now = $state(Date.now());
  let dark = $state(document.documentElement.classList.contains("dark"));
  let expanded = $state<Record<string, boolean>>({});
  let query = $state("");
  let searchInput = $state<HTMLInputElement | null>(null);
  let setupError = $state("");
  let setupBusy = $state(false);
  let statusFilter = $state<"all" | "active" | "waiting" | "exited">("all");
  let filterExpanded = $state<Record<string, boolean>>({});
  let startupRefresh: Promise<void> | undefined;
  let refreshGeneration = 0;
  let disposed = false;
  const list = $derived(
    [...sessions].sort(
      // Background output must not move the card under the user's pointer.
      (a, b) =>
        Date.parse(b.createdAt) - Date.parse(a.createdAt) ||
        a.id.localeCompare(b.id),
    ),
  );
  const search = $derived(query.trim().toLocaleLowerCase());
  const filtering = $derived(Boolean(search) || statusFilter !== "all");
  const visible = $derived(
    list.filter((session) => {
      const statusMatches =
        statusFilter === "all" ||
        (statusFilter === "active"
          ? session.status === "running" || session.status === "idle"
          : session.status === statusFilter);
      if (!statusMatches) return false;
      if (!search) return true;
      const repository = repositories.find(
        (repo) => repo.id === session.workspace?.repositoryId,
      );
      const tree = repository?.worktrees.find(
        (worktree) => worktree.id === session.workspace?.worktreeId,
      );
      return matchesSessionQuery(
        session,
        search,
        repository?.name,
        tree?.branch,
      );
    }),
  );
  const visibleRepositories = $derived(
    repositories.filter(
      (repo) =>
        !filtering ||
        visible.some((session) => session.workspace?.repositoryId === repo.id),
    ),
  );
  const filters = $derived([
    { id: "all", label: "全部", count: list.length },
    {
      id: "active",
      label: "运行中",
      count: list.filter((s) => s.status === "running" || s.status === "idle")
        .length,
    },
    {
      id: "waiting",
      label: "等待中",
      count: list.filter((s) => s.status === "waiting").length,
    },
    {
      id: "exited",
      label: "已退出",
      count: list.filter((s) => s.status === "exited").length,
    },
  ] as const);
  $effect(() => {
    // A new query opens matching repositories, while still allowing the user
    // to collapse them without changing the unfiltered workspace layout.
    void query;
    void statusFilter;
    filterExpanded = {};
  });
  function clearFilters() {
    query = "";
    statusFilter = "all";
  }
  const direct = $derived(
    visible.filter(
      (s) =>
        !s.workspace ||
        !repositories.some((r) => r.id === s.workspace?.repositoryId),
    ),
  );
  // Shell-reported directories are navigation hints; grouping stays on the
  // immutable launch directory so a `cd` never moves a session between groups.
  const directPaths = $derived([...new Set(direct.map((s) => s.cwd))].sort());
  const directReported = $derived(direct.some((s) => s.reportedCwd));
  const directSummary = $derived(
    `普通目录或未注册的仓库 · ${counts(direct)}${directReported ? "。按启动目录分组；Shell 报告目录仅作导航线索，不改变归属" : ""}`,
  );
  $effect(() => {
    if (!loading)
      for (const repo of repositories) {
        if (!(repo.id in expanded))
          expanded[repo.id] = sessions.some(
            (s) => s.workspace?.repositoryId === repo.id,
          );
      }
  });
  function start(
    mode: "direct" | "existing" | "new" = "direct",
    repositoryId = "",
    worktreeId = "",
  ) {
    creating = { mode, repositoryId, worktreeId };
  }
  function counts(items: SessionInfo[]) {
    return `运行 ${items.filter((s) => s.status === "running" || s.status === "idle").length} · 等待 ${items.filter((s) => s.status === "waiting").length} · 退出 ${items.filter((s) => s.status === "exited").length}`;
  }
  function pin(repositoryId: string, ids: string[], pinned: boolean) {
    const repo = repositories.find((r) => r.id === repositoryId);
    if (repo)
      homeWorktrees.save(setPinned(homeWorktrees.value, repo, ids, pinned));
  }
  async function refresh(signal?: AbortSignal) {
    // Coalesce lifecycle refreshes with the bounded startup request instead
    // of superseding it with another, potentially slower Git discovery.
    if (initializing && startupRefresh) return startupRefresh;
    const generation = ++refreshGeneration;
    refreshing = true;
    repositoryError = "";
    try {
      const result = await api(
        "/repositories",
        RepositoriesSchema,
        "GET",
        undefined,
        signal,
      );
      if (!disposed && generation === refreshGeneration) {
        repositories = result;
        homeWorktrees.save(pruneHomeWorktrees(homeWorktrees.value, result));
      }
    } catch (err) {
      if (!disposed && generation === refreshGeneration)
        repositoryError = signal?.aborted
          ? "初次读取超时，请重试刷新"
          : (err as Error).message;
    } finally {
      if (!disposed && generation === refreshGeneration) refreshing = false;
    }
  }
  async function unregister() {
    if (!unregistering) return;
    unregisterBusy = true;
    unregisterError = "";
    try {
      await api(`/repositories/${unregistering.id}`, NoContentSchema, "DELETE");
      unregisterOpen = false;
      await refresh();
    } catch (err) {
      unregisterError = (err as Error).message;
    } finally {
      unregisterBusy = false;
    }
  }
  async function loadSetup(signal?: AbortSignal) {
    if (setupBusy) return;
    setupBusy = true;
    const results = await Promise.allSettled([
      api("/presets", PresetsSchema, "GET", undefined, signal).then((p) => {
        if (!disposed) presets = p;
      }),
      api("/config", ConfigSchema, "GET", undefined, signal).then((c) => {
        if (disposed) return;
        cwd = c.defaultCwd;
        hostname = c.hostname;
        gitReady = c.workspace.gitAvailable;
        registryReady = c.workspace.registryAvailable;
        capabilityError = [c.workspace.gitError, c.workspace.registryError]
          .filter(Boolean)
          .join(" · ");
      }),
    ]);
    if (!disposed) {
      setupBusy = false;
      setupSettled = true;
      setupError = results.some((result) => result.status === "rejected")
        ? "部分启动配置加载失败，相关创建操作暂不可用。"
        : "";
    }
  }
  function searchShortcut(event: KeyboardEvent) {
    if (
      event.defaultPrevented ||
      event.isComposing ||
      event.metaKey ||
      event.ctrlKey ||
      event.altKey ||
      creating ||
      adding ||
      removing ||
      curating ||
      unregisterOpen ||
      closing
    )
      return;
    const target = event.target;
    if (
      event.key !== "/" ||
      (target instanceof HTMLElement &&
        (target.isContentEditable ||
          target.closest('input, textarea, select, [role="dialog"]')))
    )
      return;
    event.preventDefault();
    searchInput?.focus();
  }
  onMount(() => {
    document.title = "WebMux · 工作区";
    const theme = new MutationObserver(() => {
      dark = document.documentElement.classList.contains("dark");
    });
    theme.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    // Optional Git discovery must not hold existing terminals behind the
    // startup skeleton. Abort late reads so they cannot regroup visible cards.
    const startup = new AbortController();
    const startupTimeout = setTimeout(() => startup.abort(), 1000);
    startupRefresh = refresh(startup.signal);
    void Promise.allSettled([loadSetup(startup.signal), startupRefresh]).then(
      () => {
        clearTimeout(startupTimeout);
        startupRefresh = undefined;
        if (!disposed) initializing = false;
      },
    );
    const ws = connect(
      "/ws/sessions",
      (data) => {
        if (typeof data !== "string")
          throw new Error("会话列表帧必须是 JSON 文本");
        const event = SessionEventSchema.parse(JSON.parse(data));
        if (event.type === "snapshot") {
          sessions = event.sessions;
          drafts.reconcile(event.sessions.map((session) => session.id));
          loading = false;
        } else if (event.type === "created" || event.type === "updated")
          sessions = [
            ...sessions.filter((s) => s.id !== event.session.id),
            event.session,
          ];
        else if (event.type === "removed") {
          drafts.remove(event.id);
          sessions = sessions.filter((s) => s.id !== event.id);
        }
        now = Date.now();
      },
      (state) => {
        const reconnected =
          state === "connected" && connection === "reconnecting";
        connection = state;
        if (reconnected) void refresh();
      },
      (message) => {
        error = message;
      },
    );
    const visible = () => {
      if (!document.hidden) void refresh();
    };
    document.addEventListener("visibilitychange", visible);
    window.addEventListener("pageshow", visible);
    const clock = setInterval(() => {
      now = Date.now();
    }, 30000);
    return () => {
      disposed = true;
      ++refreshGeneration;
      clearTimeout(startupTimeout);
      startup.abort();
      theme.disconnect();
      ws.close();
      clearInterval(clock);
      document.removeEventListener("visibilitychange", visible);
      window.removeEventListener("pageshow", visible);
    };
  });
</script>

<svelte:window onkeydown={searchShortcut} />

<div class="workspace-page safe-area-page min-h-dvh">
  <header
    class="app-header workspace-header flex min-h-(--app-header-height) items-center border-b bg-card"
  >
    <div
      class="mx-auto flex w-full max-w-6xl flex-wrap items-center justify-between gap-2 px-4 py-0.5 sm:px-8"
    >
      <a class="brand" href="/"><span class="brand-mark">WM</span> WebMux</a>
      <div
        class="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-1 sm:gap-2"
      >
        <!-- Reserved width keeps the actions still. The dot is pinned at the
             end beside the actions, and the text appears only in its final
             form (connected, hostname known), so nothing already painted
             moves; long hostnames truncate. -->
        <span
          class="hidden h-4 w-52 min-w-0 items-center justify-end gap-2 text-xs whitespace-nowrap text-muted-foreground sm:flex"
          title={statusLabel}
          >{#if connection === "connected" && setupSettled}<span
              class="min-w-0 truncate">{statusLabel}</span
            >{:else}<span class="sr-only">{statusLabel}</span>{/if}<span
            class={`status-dot ${connection === "connected" ? "status-running" : "status-waiting"}`}
          ></span></span
        >
        <Button size="sm" disabled={!presets.length} onclick={() => start()}
          >＋ 新建会话</Button
        >
        <Button
          href="/settings"
          variant="ghost"
          size="icon-sm"
          aria-label="设置"
          title="设置"><IconSettings aria-hidden="true" /></Button
        >
        <Button
          variant="ghost"
          size="icon-sm"
          class="shrink-0"
          aria-label={dark ? "切换为白色主题" : "切换为黑色主题"}
          title={dark ? "切换为白色主题" : "切换为黑色主题"}
          onclick={toggleTheme}
          >{#if dark}<IconSun class="size-4" />{:else}<IconMoon
              class="size-4"
            />{/if}</Button
        >
      </div>
    </div>
  </header>
  {#if connection === "reconnecting"}<div
      role="status"
      class="notice-warning fixed inset-x-0 top-12 z-40"
    >
      连接已断开，重连中…
    </div>{/if}
  <main class="mx-auto max-w-6xl px-4 py-5 sm:px-8 sm:py-6">
    <div class="workspace-toolbar mb-5 space-y-3 border-b pb-4">
      <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <div class="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
          <h1 class="text-lg font-semibold tracking-tight">工作区</h1>
          <p role="status" class="text-xs text-muted-foreground">
            {#if loading}正在加载…
            {:else if filtering}显示 {visible.length} / {list.length} 个会话
            {:else}{repositories.length} 个仓库 · {list.length} 个会话{/if}
          </p>
        </div>
        <div
          class="ml-auto flex max-w-full flex-wrap items-center justify-end gap-1"
        >
          <Button
            variant="ghost"
            size="sm"
            class="min-h-9 sm:min-h-8"
            disabled={!gitReady || !registryReady}
            onclick={() => {
              adding = true;
            }}><IconFolderPlus aria-hidden="true" />添加仓库</Button
          >
          <Button
            variant="ghost"
            size="icon-sm"
            class="min-h-9 min-w-9 sm:min-h-8 sm:min-w-8"
            aria-label="刷新 Git 状态"
            title={refreshing ? "正在刷新 Git 状态…" : "刷新 Git 状态"}
            aria-busy={refreshing}
            disabled={refreshing || initializing}
            onclick={() => void refresh()}
            ><IconRefresh
              class={refreshing
                ? "animate-spin motion-reduce:animate-none"
                : ""}
              aria-hidden="true"
            /></Button
          >
        </div>
      </div>
      <div class="flex flex-col gap-2 md:flex-row md:items-center md:gap-4">
        <div class="relative min-w-0 flex-1">
          <IconSearch
            class="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <Input
            type="search"
            aria-label="搜索会话"
            aria-keyshortcuts="/"
            title="按 / 搜索，Esc 清除筛选"
            placeholder="搜索会话、仓库、目录或分支"
            bind:ref={searchInput}
            bind:value={query}
            onkeydown={(event) => {
              if (event.key === "Escape" && !event.isComposing) {
                event.preventDefault();
                clearFilters();
              }
            }}
            class="h-9 bg-card pl-9 pr-9 text-base md:text-sm"
          />
          <kbd
            class="pointer-events-none absolute top-1/2 right-3 hidden -translate-y-1/2 rounded border px-1 text-xs text-muted-foreground md:block"
            aria-hidden="true">/</kbd
          >
        </div>
        <div
          class="session-filters flex flex-wrap gap-x-1"
          role="group"
          aria-label="按会话状态筛选"
        >
          {#each filters as filter (filter.id)}
            <Button
              variant="ghost"
              size="sm"
              class="min-h-9 gap-1.5 px-2"
              aria-pressed={statusFilter === filter.id}
              onclick={() => (statusFilter = filter.id)}
            >
              {filter.label}
              <span
                class="inline-block w-[3ch] font-mono text-xs text-right tabular-nums opacity-60"
                >{filter.count}</span
              >
            </Button>
          {/each}
        </div>
      </div>
      {#if filtering && visible.length}
        <div class="flex items-center gap-2 text-xs text-muted-foreground">
          <span>已筛选会话</span>
          <Button
            variant="link"
            size="sm"
            class="min-h-9 sm:min-h-8"
            onclick={clearFilters}>清除筛选</Button
          >
        </div>
      {/if}
    </div>
    {#if setupError}<InlineNotice tone="warning" class="mb-4">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span>{setupError}</span>
          <Button
            variant="ghost"
            size="sm"
            class="min-h-9 sm:min-h-8"
            disabled={setupBusy}
            onclick={() => void loadSetup()}
          >
            {setupBusy ? "正在重试…" : "重试"}
          </Button>
        </div>
      </InlineNotice>{/if}
    {#if error}<InlineNotice tone="danger" class="mb-4">
        {error}
      </InlineNotice>{/if}
    {#if capabilityError}<InlineNotice tone="warning" class="mb-4">
        仓库管理部分不可用：{capabilityError}。仍可直接启动会话。
      </InlineNotice>{/if}
    {#if repositoryError}<InlineNotice tone="danger" class="mb-4">
        Git 状态刷新失败：{repositoryError}。保留上次快照与全部会话。
      </InlineNotice>{/if}
    {#if loading || initializing}
      <div class="space-y-4" aria-label="正在加载会话">
        {#each [0, 1, 2] as item (item)}<Skeleton class="h-40 w-full" />{/each}
      </div>
    {:else}
      <div class="space-y-8">
        {#each visibleRepositories as repo (repo.id)}
          {@const repoSessions = visible.filter(
            (s) => s.workspace?.repositoryId === repo.id,
          )}
          {@const missing = repoSessions.filter(
            (s) =>
              !repo.worktrees.some((w) => w.id === s.workspace?.worktreeId),
          )}
          {@const layout = homeLayout(homeWorktrees.value, repo, repoSessions)}
          <Collapsible.Root
            class="min-w-0 space-y-4"
            data-repository-id={repo.id}
            role="region"
            aria-label={`仓库 ${repo.name}`}
            open={filtering
              ? (filterExpanded[repo.id] ?? true)
              : (expanded[repo.id] ?? false)}
            onOpenChange={(open) => {
              if (filtering) filterExpanded[repo.id] = open;
              else expanded[repo.id] = open;
            }}
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
                onclick={() => start("new", repo.id)}>新建 worktree</Button
              >
              <Button
                variant="outline"
                size="sm"
                disabled={!layout.unpinned.length}
                onclick={() => {
                  curating = repo;
                }}>添加已有 worktree</Button
              >
              <DropdownMenu.Root>
                <DropdownMenu.Trigger
                  >{#snippet child({ props })}<Button
                      {...props}
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`仓库菜单 ${repo.name}`}>⋯</Button
                    >{/snippet}</DropdownMenu.Trigger
                >
                <DropdownMenu.Content align="end"
                  ><DropdownMenu.Item
                    disabled={!registryReady}
                    onSelect={() => {
                      unregistering = repo;
                      unregisterError = "";
                      unregisterOpen = true;
                    }}>从列表移除仓库</DropdownMenu.Item
                  ></DropdownMenu.Content
                >
              </DropdownMenu.Root>
              <p class="w-full text-xs text-muted-foreground">
                主页 {layout.shown.length} / 共 {repo.worktrees.length} 工作区 ·
                {counts(repoSessions)}
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
            <Collapsible.Content class="space-y-8">
              {#each layout.shown.filter(({ tree }) => !filtering || repoSessions.some((session) => session.workspace?.worktreeId === tree.id)) as { tree, pinned } (tree.id)}
                <WorkspaceGroup
                  title={worktreeTitle(tree)}
                  path={tree.path}
                  worktree={tree}
                  sessions={repoSessions.filter(
                    (s) => s.workspace?.worktreeId === tree.id,
                  )}
                  {now}
                  {pinned}
                  ontogglepin={() => pin(repo.id, [tree.id], !pinned)}
                  onclose={(s) => {
                    closing = s;
                  }}
                  onstart={() => start("existing", repo.id, tree.id)}
                  onremove={tree.kind === "linked"
                    ? () => {
                        removing = tree;
                      }
                    : undefined}
                />
              {/each}
              {#if missing.length}<WorkspaceGroup
                  title="已不可用工作区"
                  sessions={missing}
                  {now}
                  onclose={(s) => {
                    closing = s;
                  }}
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
            </Collapsible.Content>
          </Collapsible.Root>
        {/each}
        {#if direct.length}
          <section class="min-w-0 space-y-8" aria-label="直接会话">
            <div>
              <h2 class="font-semibold">直接会话</h2>
              <p class="mt-1 text-xs text-muted-foreground">{directSummary}</p>
            </div>
            {#each directPaths as path (path)}<WorkspaceGroup
                title={path.split("/").pop() || "/"}
                {path}
                sessions={direct.filter((s) => s.cwd === path)}
                {now}
                onclose={(s) => {
                  closing = s;
                }}
              />{/each}
          </section>
        {/if}
        {#if filtering && !visible.length}
          <Empty.Root class="workspace-empty">
            <Empty.Header>
              <Empty.Media variant="icon"><IconSearch /></Empty.Media>
              <Empty.Title>没有匹配的会话</Empty.Title>
              <Empty.Description
                >试试其他名称、目录或分支，或清除筛选查看全部会话。</Empty.Description
              >
            </Empty.Header>
            <Empty.Content>
              <Button variant="outline" onclick={clearFilters}>清除筛选</Button>
            </Empty.Content>
          </Empty.Root>
        {:else if !repositories.length && !list.length}
          <Empty.Root class="workspace-empty">
            <Empty.Header>
              <Empty.Media variant="icon"><IconTerminal2 /></Empty.Media>
              <Empty.Title>还没有会话</Empty.Title>
              <Empty.Description>
                直接启动 Shell 或 agent；也可添加已有仓库，按 worktree
                组织多个独立终端。
              </Empty.Description>
            </Empty.Header>
            <Empty.Content>
              <Button disabled={!presets.length} onclick={() => start()}
                >＋ 新建会话</Button
              >
            </Empty.Content>
          </Empty.Root>
        {/if}
      </div>
    {/if}
  </main>
</div>
{#if creating}<NewSessionSheet
    {presets}
    {repositories}
    defaultCwd={cwd}
    initialMode={creating.mode}
    initialRepositoryId={creating.repositoryId}
    initialWorktreeId={creating.worktreeId}
    onchange={refresh}
    onworktreecreated={(tree) => pin(tree.repositoryId, [tree.id], true)}
    onclose={() => {
      creating = null;
    }}
  />{/if}
{#if closing}<CloseSessionDialog
    session={closing}
    onremoved={() => {
      if (closing) drafts.remove(closing.id);
    }}
    onclose={() => {
      closing = null;
    }}
  />{/if}
{#if adding}<AddRepositoryDialog
    defaultCwd={cwd}
    onchange={refresh}
    onclose={() => {
      adding = false;
    }}
  />{/if}
{#if removing}<RemoveWorktreeDialog
    worktree={removing}
    onchange={refresh}
    onclose={() => {
      removing = null;
    }}
  />{/if}
{#if curating}<AddHomeWorktreesDialog
    repositoryName={curating.name}
    worktrees={homeLayout(homeWorktrees.value, curating, sessions).unpinned}
    active={new Set(sessions.map((s) => s.workspace?.worktreeId ?? ""))}
    onadd={(ids) => {
      if (curating) pin(curating.id, ids, true);
    }}
    onclose={() => {
      curating = null;
    }}
  />{/if}
<AlertDialog.Root bind:open={unregisterOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header
      ><AlertDialog.Title>从列表移除仓库？</AlertDialog.Title
      ><AlertDialog.Description class="break-all whitespace-pre-wrap"
        >{unregistering?.anchorPath}<br
        />只移除持久登记，不删除任何目录或分支，不关闭会话。原会话仍可在「直接会话」中访问。</AlertDialog.Description
      ></AlertDialog.Header
    >
    {#if unregisterError}<p
        role="alert"
        class="text-sm break-all text-destructive"
      >
        {unregisterError}
      </p>{/if}
    <AlertDialog.Footer
      ><AlertDialog.Cancel disabled={unregisterBusy}>取消</AlertDialog.Cancel
      ><Button disabled={unregisterBusy} onclick={unregister}
        >{unregisterBusy ? "正在移除…" : "确认移除登记"}</Button
      ></AlertDialog.Footer
    >
  </AlertDialog.Content>
</AlertDialog.Root>
