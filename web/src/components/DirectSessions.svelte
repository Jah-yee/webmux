<script lang="ts">
  import type { SessionInfo } from "../schemas";
  import { statusSummary } from "../session-status";
  import WorkspaceGroup from "./WorkspaceGroup.svelte";

  let {
    sessions,
    visibleIds,
    now,
    onclose,
  }: {
    // All sessions outside every registered repository.
    sessions: SessionInfo[];
    visibleIds?: ReadonlySet<string>;
    now: number;
    onclose: (session: SessionInfo) => void;
  } = $props();

  const visible = $derived(
    visibleIds
      ? sessions.filter((session) => visibleIds.has(session.id))
      : sessions,
  );

  // Shell-reported directories are navigation hints; grouping stays on the
  // immutable launch directory so a `cd` never moves a session between groups.
  const paths = $derived([...new Set(sessions.map((s) => s.cwd))].sort());
  const reported = $derived(visible.some((s) => s.reportedCwd));
  const summary = $derived(
    `普通目录或未注册的仓库 · ${statusSummary(visible)}${reported ? "。按启动目录分组；Shell 报告目录仅作导航线索，不改变归属" : ""}`,
  );
</script>

<section
  class="min-w-0 space-y-8"
  aria-label="直接会话"
  hidden={!visible.length}
>
  <div>
    <h2 class="font-semibold">直接会话</h2>
    <p class="mt-1 text-xs text-muted-foreground">{summary}</p>
  </div>
  {#each paths as path (path)}<WorkspaceGroup
      title={path.split("/").pop() || "/"}
      {path}
      sessions={sessions.filter((s) => s.cwd === path)}
      {visibleIds}
      hidden={Boolean(
        visibleIds && !visible.some((session) => session.cwd === path),
      )}
      {now}
      {onclose}
    />{/each}
</section>
