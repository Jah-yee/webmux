<script lang="ts">
  import WorkspaceGroup from "../src/components/WorkspaceGroup.svelte";
  import {
    session as fixture,
    worktree as worktreeFixture,
    now,
  } from "./fixtures";
  import type { SessionInfo } from "../src/schemas";

  let {
    multiple = false,
    withWorktree = false,
  }: { multiple?: boolean; withWorktree?: boolean } = $props();
  let notice = $state("");
  const path = "/Users/myot/.codex/worktrees/ui-autoresearch/webmux";
  const worktree = {
    ...worktreeFixture,
    path,
    branch: "codex/flatten-workspace-session-layout-for-mobile",
    available: true,
    locked: false,
    lockReason: "",
    prunable: false,
    dirty: false,
    hasUntracked: false,
    hasIgnored: false,
    error: "",
  };
  const shell: SessionInfo = {
    ...fixture,
    title: "myot@myotdeMacBook-Pro:~/.codex/worktrees/ui-autoresearch",
    cwd: path,
    clients: 0,
    preview: [
      "AGENTS.md        README.md        autoresearch    cmd       go.mod    openspec",
      "CONTRIBUTING.md  SECURITY.md      baseline        deploy    go.sum    web",
      "Makefile         acceptance.local bin             docs      internal",
      "➜  webmux git:(v0.1.8) ls",
      "AGENTS.md        README.md        autoresearch    cmd       go.mod    openspec",
      "CONTRIBUTING.md  SECURITY.md      baseline        deploy    go.sum    web",
      "Makefile         acceptance.local bin             docs      internal",
      "➜  webmux git:(v0.1.8) cat acceptance.local",
      "cat: acceptance.local: Is a directory",
      "➜  webmux git:(v0.1.8) cd ..",
      "",
    ].join("\n"),
  };
  const agent: SessionInfo = {
    ...shell,
    id: "agentdemo",
    title: "Codex · 完善移动端终端交互",
    status: "running",
    clients: 1,
    reportedCwd: {
      path: "/srv/builds/webmux-agent",
      host: "build.example",
      remote: true,
      since: "2026-01-01T11:59:00Z",
    },
    preview: [
      "› 检查终端在小屏幕上的输入和滚动行为",
      "",
      "  ✓ 读取 TerminalInputComposer.svelte",
      "  ✓ 检查键盘弹出时的视口变化",
      "  ✓ 保留断线后的输入草稿",
      "",
      "  Running browser tests…",
      "  12 passed",
      "",
      "  正在检查触控选择和快捷键。",
      "",
    ].join("\n"),
  };
</script>

<main class="mx-auto max-w-5xl px-4 py-8 sm:px-8 sm:py-12">
  <WorkspaceGroup
    title={withWorktree ? worktree.branch : "webmux"}
    {path}
    worktree={withWorktree ? worktree : undefined}
    sessions={multiple ? [shell, agent] : [shell]}
    {now}
    pinned={withWorktree ? true : undefined}
    onstart={withWorktree
      ? () => (notice = "此预览使用示例数据，未启动真实会话。")
      : undefined}
    ontogglepin={withWorktree
      ? () => (notice = "此预览使用示例数据，未修改主页工作区。")
      : undefined}
    onclose={() => (notice = "此预览使用示例数据，未关闭真实会话。")}
  />
  {#if notice}<p role="status" class="mt-4 text-sm text-muted-foreground">
      {notice}
    </p>{/if}
</main>
