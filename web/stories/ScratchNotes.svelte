<script lang="ts">
  import { untrack } from "svelte";
  import ScratchPanel from "../src/components/ScratchPanel.svelte";
  import TerminalInputComposer from "../src/components/TerminalInputComposer.svelte";
  import { createTerminalDrafts } from "../src/terminal-draft.svelte";

  let {
    count = 2,
    pending = false,
    saved = "已保存在本机",
  }: { count?: number; pending?: boolean; saved?: string } = $props();
  const drafts = createTerminalDrafts();
  const draft = drafts.get("scratch-ui-demo");
  untrack(() => {
    drafts.setEnabled(draft, true);
    drafts.edit(draft, "检查终端断线后的重连行为，补充必要的测试。");
    for (let i = 0; i < count; i++) {
      drafts.addScratch(
        draft,
        i % 2 === 0
          ? "整理测试结果\n记录失败用例、复现步骤和可能的原因。"
          : "更新使用说明\n补充草稿恢复与暂存的操作示例，确认手机端也能顺利完成。",
      );
      draft.scratch[i].updatedAt = 1792721400000;
    }
    if (pending) drafts.beginSubmission(draft, "s-00000001");
  });
  let open = $state(true);
</script>

<main class="mx-auto max-w-4xl p-4">
  <div class="flex h-72 flex-col rounded-lg border">
    <TerminalInputComposer
      {draft}
      {drafts}
      {saved}
      unavailable=""
      target="演示会话"
      openScratch={() => (open = true)}
      send={() => {}}
      resend={() => {}}
      release={() => {}}
      filePaste={() => false}
      controlNotice={() => {}}
    />
  </div>
  <ScratchPanel bind:open {draft} {drafts} {saved} />
</main>
