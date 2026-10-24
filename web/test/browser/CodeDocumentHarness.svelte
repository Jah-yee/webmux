<script lang="ts">
  import CodeDocumentView from "../../src/components/CodeDocumentView.svelte";
  import { CodeDiffSchema } from "../../src/schemas";
  let {
    deleted = false,
    syntax = false,
    unchanged = false,
  }: { deleted?: boolean; syntax?: boolean; unchanged?: boolean } = $props();
  let revision = $state(1);
  let active = $state(true);
  let status = $state("unchanged");
  let inserted = $state("");
  let width = $state(650);
  const original = $derived(
    syntax
      ? 'const message = "<img src=x onerror=alert(1)>";\n'
      : Array.from(
          { length: deleted ? 20000 : 120 },
          (_, i) =>
            `ordinary line ${i + 1}${deleted ? " " + "x".repeat(60) : ""}`,
        ).join("\n") + "\n",
  );
  const diff = $derived(
    CodeDiffSchema.parse({
      snapshotId: `snapshot-${revision}`,
      checkedAt: "2026-10-14T00:00:00Z",
      kind: "text",
      reason: "",
      change: {
        nodeId: "Y29kZS50eHQ",
        changeId: "change",
        group: "unstaged",
        oldPath: "code.txt",
        newPath: "code.txt",
        status: deleted ? "D" : "M",
        oldOid: "",
        newOid: "",
        oldMode: "100644",
        newMode: deleted ? "000000" : "100644",
      },
      old: {
        nodeId: "Y29kZS50eHQ",
        path: syntax ? "code.ts" : "code.txt",
        absolutePath: "/repo/code.txt",
        source: "index",
        oid: "",
        contentVersion: "old",
        kind: "text",
        text: original,
        lineCount: deleted ? 20000 : 120,
        referenceable: true,
        reason: "",
        mediaType: "",
        size: original.length,
        downloadable: true,
      },
      new: {
        nodeId: "Y29kZS50eHQ",
        path: syntax ? "code.ts" : "code.txt",
        absolutePath: "/repo/code.txt",
        source: deleted ? "empty" : "worktree",
        oid: "",
        contentVersion: `new-${revision}`,
        kind: "text",
        text: deleted
          ? ""
          : unchanged
            ? original
            : syntax
              ? original.replace("message", "content")
              : original.replace(
                  "ordinary line 50\n",
                  `changed revision ${revision}\n`,
                ),
        lineCount: deleted ? 0 : 120,
        referenceable: !deleted,
        reason: "",
        mediaType: "",
        size: deleted ? 0 : original.length,
        downloadable: !deleted,
      },
    }),
  );
</script>

<button onclick={() => revision++}>Replace snapshot</button>
<button onclick={() => (active = !active)}>Toggle active</button>
<button onclick={() => (status = "expired")}>Expire snapshot</button>
<button onclick={() => (width = width === 650 ? 900 : 650)}>Toggle width</button
>
<div style:height="650px" style:width={`${width}px`}>
  <CodeDocumentView
    id="session"
    {diff}
    {active}
    {status}
    contextId="context"
    draftEnabled={false}
    paste={(text) => {
      inserted = text;
      return "terminal";
    }}
    refresh={() => {}}
    beginOpen={() => 1}
    openSide={() => {}}
    onComparison={() => {}}
    onState={(value) => (status = value)}
  />
</div>
<p aria-label="Inserted reference">{inserted}</p>
