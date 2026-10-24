<script lang="ts">
  import type { SessionInfo } from "../schemas";
  import { statusSummary } from "../session-status";
  import WorkspaceGroup from "./WorkspaceGroup.svelte";

  let {
    sessions,
    now,
    onclose,
  }: {
    // Visible sessions outside every registered repository.
    sessions: SessionInfo[];
    now: number;
    onclose: (session: SessionInfo) => void;
  } = $props();

  // Shell-reported directories are navigation hints; grouping stays on the
  // immutable launch directory so a `cd` never moves a session between groups.
  const paths = $derived([...new Set(sessions.map((s) => s.cwd))].sort());
  const reported = $derived(sessions.some((s) => s.reportedCwd));
  const summary = $derived(
    `普通目录或未注册的仓库 · ${statusSummary(sessions)}${reported ? "。按启动目录分组；Shell 报告目录仅作导航线索，不改变归属" : ""}`,
  );
</script>

<section class="min-w-0 space-y-8" aria-label="直接会话">
  <div>
    <h2 class="font-semibold">直接会话</h2>
    <p class="mt-1 text-xs text-muted-foreground">{summary}</p>
  </div>
  {#each paths as path (path)}<WorkspaceGroup
      title={path.split("/").pop() || "/"}
      {path}
      sessions={sessions.filter((s) => s.cwd === path)}
      {now}
      {onclose}
    />{/each}
</section>
