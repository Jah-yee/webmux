<script lang="ts">
  import SessionRow from "../src/components/SessionRow.svelte";
  import WorkspaceGroup from "../src/components/WorkspaceGroup.svelte";
  import QuickCommandDialog from "../src/components/QuickCommandDialog.svelte";
  import CloseSessionDialog from "../src/components/CloseSessionDialog.svelte";
  import TerminalInputComposer from "../src/components/TerminalInputComposer.svelte";
  import ScratchPanel from "../src/components/ScratchPanel.svelte";
  import CodeChangesList from "../src/components/CodeChangesList.svelte";
  import ResponsiveDialog from "../src/components/ResponsiveDialog.svelte";
  import { Button } from "../src/lib/components/ui/button";
  import { createTerminalDrafts } from "../src/terminal-draft.svelte";
  import {
    changes,
    longError,
    longPath,
    longToken,
    now,
    session,
    worktree,
  } from "./fixtures";

  let {
    scenario,
    saveError = "",
  }: {
    scenario:
      | "session"
      | "workspace"
      | "command"
      | "close"
      | "composer"
      | "changes"
      | "workspace-dialog";
    saveError?: string;
  } = $props();
  let open = $state(true);
  let scratchOpen = $state(false);
  let notice = $state("");
  const drafts = createTerminalDrafts();
  const draft = drafts.get("storybook-draft");
  drafts.setEnabled(draft, true);
  drafts.edit(
    draft,
    Array.from({ length: 30 }, (_, i) => `第 ${i + 1} 行 ${longPath}`).join(
      "\n",
    ),
  );
  drafts.addScratch(draft, "稍后检查测试结果并把失败原因整理成摘要。");
  drafts.addScratch(draft, `补充 ${longPath} 的变更说明，并确认没有遗漏。`);
</script>

<main
  class="mx-auto w-full max-w-5xl p-4"
  data-testid="overflow-story"
  data-scenario={scenario}
>
  {#if scenario === "session"}
    <div role="list" aria-label="会话列表">
      <SessionRow
        {session}
        {now}
        onclose={() => (notice = "关闭操作仅作演示")}
      />
    </div>
  {:else if scenario === "workspace"}
    <WorkspaceGroup
      title={worktree.branch!}
      path={longPath}
      {worktree}
      sessions={[
        session,
        { ...session, id: "abcdefgi", status: "exited", exitCode: 127 },
      ]}
      {now}
      onclose={() => (notice = "关闭操作仅作演示")}
      onstart={() => (notice = "启动操作仅作演示")}
      onremove={() => (notice = "删除操作仅作演示")}
    />
  {:else if scenario === "command"}
    <Button onclick={() => (open = true)}>打开快捷指令</Button>
    <QuickCommandDialog
      bind:open
      initial={{
        id: "storybook-command",
        name: "多语言快捷指令".repeat(4),
        command: `printf '%s\\n' '${longPath}'\n`.repeat(3),
        enter: false,
      }}
      save={() => saveError}
    />
  {:else if scenario === "close"}
    <Button onclick={() => (open = true)}>打开关闭确认</Button>
    {#if open}<CloseSessionDialog
        {session}
        onclose={() => (open = false)}
      />{/if}
  {:else if scenario === "composer"}
    <div class="flex h-[min(28rem,75dvh)] min-h-0 flex-col border">
      <TerminalInputComposer
        {draft}
        {drafts}
        unavailable={`连接暂不可用，请保留草稿：${longToken}`}
        saved="已保存在本机"
        target={session.title}
        openScratch={() => (scratchOpen = true)}
        send={() => (notice = "发送操作仅作演示")}
        resend={() => (notice = "重发操作仅作演示")}
        release={() => (notice = "转为草稿仅作演示")}
        filePaste={() => false}
        controlNotice={() => (notice = "控制键仅作演示")}
      />
    </div>
    <ScratchPanel
      bind:open={scratchOpen}
      {draft}
      {drafts}
      saved="已保存在本机"
    />
  {:else if scenario === "changes"}
    <section
      class="flex h-[min(32rem,80dvh)] min-h-0 min-w-0 flex-col border"
      aria-label="代码修改"
    >
      <CodeChangesList
        entries={changes}
        total={changes.length}
        nextCursor=""
        version="storybook"
        pending={false}
        enabled={true}
        onmore={() => {}}
        onopen={(change) => (notice = change.newPath)}
      />
    </section>
  {:else if scenario === "workspace-dialog"}
    <Button onclick={() => (open = true)}>打开工作区</Button>
    {#if open}
      <ResponsiveDialog
        title={`工作区 ${longToken}`}
        description={longError}
        onclose={() => (open = false)}
      >
        <div class="space-y-4">
          <label class="flex items-start gap-2">
            <input type="checkbox" class="mt-1 size-4 shrink-0" />
            <span>已检查工作区路径</span>
          </label>
          {#each Array.from({ length: 8 }, (_, i) => i) as i}
            <p class="break-all">文件 {i + 1}：{longPath}</p>
          {/each}
          <Button onclick={() => (open = false)}>关闭预览</Button>
        </div>
      </ResponsiveDialog>
    {/if}
  {/if}
  {#if notice}<p class="mt-2 break-all" role="status">{notice}</p>{/if}
</main>
