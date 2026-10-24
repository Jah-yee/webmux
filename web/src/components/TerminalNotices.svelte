<script lang="ts">
  import { navigate } from "../api";
  import { Button } from "$lib/components/ui/button";
  import type { UploadProgress } from "../hooks/upload.svelte";
  import type { ControlState } from "../schemas";
  import type { TerminalDraft } from "../terminal-draft.svelte";
  import type { Connection } from "../ws";

  interface NoticeTerminal {
    readonly control: ControlState | undefined;
    readonly controlling: boolean;
    readonly connection: Connection;
    readonly exitCode: number | null;
    readonly error: string;
    claim(): void;
  }
  interface NoticeUploads {
    readonly progress: UploadProgress | null;
    cancel(): void;
  }

  // Status strips between the header and the terminal, in display order.
  let {
    terminal: t,
    uploads,
    draft,
    draftSaved,
    error,
    notice = $bindable(),
    showScratch,
    dismissScratch,
    remove,
  }: {
    terminal: NoticeTerminal;
    uploads: NoticeUploads;
    draft: TerminalDraft;
    draftSaved: string;
    error: string;
    notice: string;
    showScratch: () => void;
    dismissScratch: () => void;
    remove: () => void;
  } = $props();
</script>

{#if !draft.enabled && !draft.scratchUpdatedWhileHidden && (draft.text || draft.updatedWhileHidden || draft.scratch.length)}
  <p class="sr-only" role="status">
    {#if draft.updatedWhileHidden}隐藏草稿已更新{:else if draft.text}有保留的隐藏草稿{/if}
    {#if draft.scratch.length}
      · 共 {draft.scratch.length} 条暂存{/if}
    · 开启草稿模式查看；{draftSaved}
  </p>
{/if}
{#if !draft.enabled && draft.scratchUpdatedWhileHidden}
  <div
    class="flex shrink-0 flex-wrap items-center gap-2 border-b bg-muted px-4 py-1.5 text-sm"
    role="status"
  >
    <span class="min-w-0 flex-1"
      >{#if draft.updatedWhileHidden}隐藏草稿已更新 ·
      {/if}暂存已更新 · 共 {draft.scratch.length} 条，不会自动发送</span
    >
    <Button variant="outline" size="sm" onclick={showScratch}>查看暂存</Button>
    <Button variant="ghost" size="sm" onclick={dismissScratch}>知道了</Button>
  </div>
{/if}
{#if uploads.progress}
  <div
    class="shrink-0 space-y-1 border-b bg-muted px-3 py-2"
    aria-label="文件上传状态"
  >
    <div class="flex items-center gap-3 text-xs" role="status">
      <span class="min-w-0 flex-1 truncate"
        >{uploads.progress.waiting
          ? "文件已上传，等待终端恢复："
          : "正在上传 "}{uploads.progress.name}{uploads.progress.total > 1
          ? `（${uploads.progress.index}/${uploads.progress.total}）`
          : ""}</span
      >
      <span>{uploads.progress.percent}%</span>
      <Button variant="ghost" size="sm" onclick={() => uploads.cancel()}
        >取消</Button
      >
    </div>
    <progress
      aria-label="文件上传进度"
      max={100}
      value={uploads.progress.percent}
      class="block h-1 w-full accent-primary"
    ></progress>
  </div>
{/if}
{#if notice}<div
    class="flex shrink-0 items-center gap-2 border-b bg-muted px-4 py-1.5 text-sm"
    role="status"
  >
    <span class="min-w-0 flex-1">{notice}</span>
    <Button variant="ghost" size="sm" onclick={() => (notice = "")}
      >知道了</Button
    >
  </div>{/if}
{#if t.control && !t.controlling && t.connection === "connected" && t.exitCode === null}
  <div
    class="flex shrink-0 items-center gap-2 border-b bg-muted px-3 py-1 text-xs"
    role="status"
    aria-label="终端控制"
  >
    <span class="min-w-0 flex-1 truncate"
      >{t.control.controller
        ? `观察中 · ${t.control.controller.label || "另一设备"}控制输入与尺寸`
        : "观察中 · 当前没有控制端"}（{t.control.cols}×{t.control.rows}）</span
    >
    <Button variant="outline" size="sm" onclick={() => t.claim()}
      >接管控制</Button
    >
  </div>
{/if}
{#if error || t.error}<p
    role="alert"
    class="shrink-0 bg-destructive/10 px-4 py-2 text-sm text-destructive"
  >
    {error || t.error}
  </p>{/if}
{#if t.connection === "missing"}<div class="notice-warning">
    会话不存在或已被移除。<Button
      variant="outline"
      size="sm"
      onclick={() => navigate("/")}>返回列表</Button
    >
  </div>{/if}
{#if t.exitCode !== null}
  <div
    class="flex shrink-0 flex-wrap items-center gap-3 border-b bg-muted px-4 py-2 text-sm"
    role="status"
  >
    <span>进程已退出（code {t.exitCode}）</span>
    <Button variant="outline" size="sm" onclick={() => navigate("/")}
      >返回列表</Button
    >
    <Button variant="outline" size="sm" onclick={remove}>移除会话</Button>
  </div>
{/if}
