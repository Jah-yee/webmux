<script lang="ts">
  import InlineNotice from "../components/InlineNotice.svelte";
  import { onMount } from "svelte";
  import WorkspaceToolbar from "../components/WorkspaceToolbar.svelte";
  import { api } from "../api";
  import {
    LaunchConfigsSchema,
    PresetsSchema,
    ConfigSchema,
    SessionEventSchema,
    RepositoriesSchema,
    type Preset,
    type SessionInfo,
    type Repository,
    type Worktree,
  } from "../schemas";
  import type { LaunchConfigsState } from "../launchConfigs";
  import { connect, type Connection } from "../ws";
  import { Button } from "$lib/components/ui/button";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import NewSessionSheet from "../components/NewSessionSheet.svelte";
  import CloseSessionDialog from "../components/CloseSessionDialog.svelte";
  import AddRepositoryDialog from "../components/AddRepositoryDialog.svelte";
  import RemoveWorktreeDialog from "../components/RemoveWorktreeDialog.svelte";
  import AddHomeWorktreesDialog from "../components/AddHomeWorktreesDialog.svelte";
  import UnregisterRepositoryDialog from "../components/UnregisterRepositoryDialog.svelte";
  import WorkspaceHeader from "../components/WorkspaceHeader.svelte";
  import SessionFilters from "../components/SessionFilters.svelte";
  import RepositoryGroup from "../components/RepositoryGroup.svelte";
  import DirectSessions from "../components/DirectSessions.svelte";
  import SessionListEmpty from "../components/SessionListEmpty.svelte";
  import { homeWorktrees } from "../homeWorktrees.svelte";
  import { homeLayout, pruneHomeWorktrees, setPinned } from "../homeWorktrees";
  import { matchesSessionQuery } from "../session-location";
  import { matchesStatusFilter, type StatusFilter } from "../session-status";
  import type { TerminalDrafts } from "../terminal-draft.svelte";

  let { drafts }: { drafts: TerminalDrafts } = $props();

  let sessions = $state<SessionInfo[]>([]);
  let repositories = $state<Repository[]>([]);
  let presets = $state<Preset[]>([]);
  // Loaded independently of presets: a failure only disables that group.
  let launchConfigs = $state<LaunchConfigsState>({ status: "loading" });
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
  let closing = $state<SessionInfo | null>(null);
  let error = $state("");
  let now = $state(Date.now());
  let expanded = $state<Record<string, boolean>>({});
  let query = $state("");
  let setupError = $state("");
  let setupBusy = $state(false);
  let statusFilter = $state<StatusFilter>("all");
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
      if (!matchesStatusFilter(session.status, statusFilter)) return false;
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
    // Pick up configs changed on other devices since the page loaded.
    if (launchConfigs.status !== "ready") launchConfigs = { status: "loading" };
    void loadLaunchConfigs();
  }
  async function loadLaunchConfigs() {
    try {
      const configs = await api("/launch-configs", LaunchConfigsSchema);
      if (!disposed) launchConfigs = { status: "ready", configs };
    } catch (err) {
      if (!disposed)
        launchConfigs = {
          status: "unavailable",
          error: (err as Error).message,
        };
    }
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
  onMount(() => {
    document.title = "WebMux · 工作区";
    // Optional Git discovery must not hold existing terminals behind the
    // startup skeleton. Abort late reads so they cannot regroup visible cards.
    const startup = new AbortController();
    const startupTimeout = setTimeout(() => startup.abort(), 1000);
    startupRefresh = refresh(startup.signal);
    void loadLaunchConfigs();
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
      ws.close();
      clearInterval(clock);
      document.removeEventListener("visibilitychange", visible);
      window.removeEventListener("pageshow", visible);
    };
  });
</script>

<div class="workspace-page safe-area-page min-h-dvh">
  <WorkspaceHeader
    {connection}
    {hostname}
    {setupSettled}
    canStart={presets.length > 0}
    onstart={() => start()}
  />
  {#if connection === "reconnecting"}<div
      role="status"
      class="notice-warning fixed inset-x-0 top-12 z-40"
    >
      连接已断开，重连中…
    </div>{/if}
  <main class="mx-auto max-w-6xl px-4 py-5 sm:px-8 sm:py-6">
    <WorkspaceToolbar
      {loading}
      {filtering}
      visibleCount={visible.length}
      sessionCount={list.length}
      repositoryCount={repositories.length}
      canAddRepository={gitReady && registryReady}
      {refreshing}
      refreshDisabled={refreshing || initializing}
      onadd={() => {
        adding = true;
      }}
      onrefresh={() => void refresh()}
    >
      <SessionFilters
        sessions={list}
        bind:query
        bind:statusFilter
        filtered={filtering && visible.length > 0}
        shortcutDisabled={Boolean(
          creating ||
          adding ||
          removing ||
          curating ||
          unregisterOpen ||
          closing,
        )}
        onclear={clearFilters}
      />
    </WorkspaceToolbar>
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
          <RepositoryGroup
            {repo}
            sessions={visible.filter(
              (s) => s.workspace?.repositoryId === repo.id,
            )}
            {filtering}
            open={filtering
              ? (filterExpanded[repo.id] ?? true)
              : (expanded[repo.id] ?? false)}
            onopenchange={(open) => {
              if (filtering) filterExpanded[repo.id] = open;
              else expanded[repo.id] = open;
            }}
            {gitReady}
            {registryReady}
            {refreshing}
            {now}
            onstart={(worktreeId) =>
              worktreeId === undefined
                ? start("new", repo.id)
                : start("existing", repo.id, worktreeId)}
            oncurate={() => {
              curating = repo;
            }}
            onunregister={() => {
              unregistering = repo;
              unregisterOpen = true;
            }}
            ontogglepin={(worktreeId, pinned) =>
              pin(repo.id, [worktreeId], pinned)}
            onremove={(tree) => {
              removing = tree;
            }}
            onclose={(s) => {
              closing = s;
            }}
          />
        {/each}
        {#if direct.length}<DirectSessions
            sessions={direct}
            {now}
            onclose={(s) => {
              closing = s;
            }}
          />{/if}
        {#if (filtering && !visible.length) || (!repositories.length && !list.length)}<SessionListEmpty
            filtered={filtering && !visible.length}
            canStart={presets.length > 0}
            onclear={clearFilters}
            onstart={() => start()}
          />{/if}
      </div>
    {/if}
  </main>
</div>
{#if creating}<NewSessionSheet
    {presets}
    {launchConfigs}
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
<UnregisterRepositoryDialog
  repository={unregistering}
  bind:open={unregisterOpen}
  onchange={refresh}
/>
