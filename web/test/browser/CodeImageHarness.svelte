<script lang="ts">
  import CodeDocumentView from "../../src/components/CodeDocumentView.svelte";
  import type { CodeDiff, CodeFile } from "../../src/schemas";
  let { file, diff }: { file?: CodeFile; diff?: CodeDiff } = $props();
  let status = $state("unchanged");
  let inserted = $state("");
</script>

<div data-testid="frame" style="height:650px;width:650px">
  <CodeDocumentView
    id="session"
    {file}
    {diff}
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
