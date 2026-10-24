<script lang="ts">
  import CodeDocumentView from "../src/components/CodeDocumentView.svelte";
  import type { CodeDiff } from "../src/schemas";

  let inserted = $state("");
  const oldText = `import { createSession } from "./session";

export interface SessionOptions {
  cwd: string;
  command: string[];
  timeout: number;
}

export async function startSession(options: SessionOptions) {
  const timeout = options.timeout || 5000;
  const session = await createSession({
    cwd: options.cwd,
    command: options.command.join(" "),
    timeout,
  });

  console.log("Session started", session.id);
  return session;
}
`;
  const newText = oldText
    .replace("timeout: number;", "timeout?: number;\n  signal?: AbortSignal;")
    .replace("options.timeout || 5000", "options.timeout ?? 30_000")
    .replace('options.command.join(" ")', "options.command")
    .replace("    timeout,", "    timeout,\n    signal: options.signal,")
    .replace('  console.log("Session started", session.id);\n', "");
  const document = {
    nodeId: "c2Vzc2lvbi50cw",
    path: "src/session.ts",
    absolutePath: "/workspace/webmux/src/session.ts",
    source: "index" as const,
    oid: "",
    contentVersion: "before",
    kind: "text" as const,
    text: oldText,
    lineCount: oldText.split("\n").length - 1,
    referenceable: true,
    reason: "",
    mediaType: "",
    size: new TextEncoder().encode(oldText).length,
    downloadable: true,
  };
  const diff: CodeDiff = {
    snapshotId: "diff-story",
    checkedAt: "2026-10-21T00:00:00Z",
    kind: "text",
    reason: "",
    change: {
      nodeId: document.nodeId,
      changeId: "session-change",
      group: "unstaged",
      oldPath: document.path,
      newPath: document.path,
      status: "M",
      oldOid: "",
      newOid: "",
      oldMode: "100644",
      newMode: "100644",
    },
    old: document,
    new: {
      ...document,
      source: "worktree",
      text: newText,
      contentVersion: "after",
      lineCount: newText.split("\n").length - 1,
      size: new TextEncoder().encode(newText).length,
    },
  };
</script>

<main class="flex h-dvh min-h-0 flex-col bg-background text-foreground">
  <div class="flex items-center gap-2 border-b px-3 py-2 text-sm">
    <span class="font-medium">src/session.ts</span>
    <span class="text-xs text-muted-foreground">未暂存修改 · 示例数据</span>
  </div>
  <div class="min-h-0 flex-1">
    <CodeDocumentView
      id="demo"
      {diff}
      status="unchanged"
      contextId="demo"
      draftEnabled={true}
      paste={(text) => {
        inserted = text;
        return "draft";
      }}
      refresh={() => {}}
      beginOpen={() => 1}
      openSide={() => {}}
      onComparison={() => {}}
      onState={() => {}}
    />
  </div>
  {#if inserted}
    <p class="border-t p-2 text-xs" role="status">草稿：{inserted}</p>
  {/if}
</main>
