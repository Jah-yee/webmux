<script lang="ts">
  import { untrack } from "svelte";
  import TerminalInputComposer from "../../src/components/TerminalInputComposer.svelte";
  import ScratchPanel from "../../src/components/ScratchPanel.svelte";
  import type { TerminalDrafts } from "../../src/terminal-draft.svelte";

  let { drafts }: { drafts: TerminalDrafts } = $props();
  const draft = untrack(() => {
    const entry = drafts.get("aaaaaaaa");
    drafts.setEnabled(entry, true);
    return entry;
  });
  let scratchOpen = $state(false);
</script>

<div class="flex h-[32rem] min-h-0 flex-col border">
  <TerminalInputComposer
    {draft}
    {drafts}
    unavailable=""
    saved="已保存在本机"
    target="示例会话"
    openScratch={() => (scratchOpen = true)}
    send={() => {}}
    resend={() => {}}
    release={() => {}}
    filePaste={() => false}
    controlNotice={() => {}}
  />
</div>
<ScratchPanel bind:open={scratchOpen} {draft} {drafts} saved="已保存在本机" />
