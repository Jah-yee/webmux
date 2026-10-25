<script lang="ts">
  import { onMount } from "svelte";
  import { api, sessionStatusText } from "../api";
  import {
    NoContentSchema,
    RemovalPreviewSchema,
    type RemovalPreview,
    type Worktree,
  } from "../schemas";
  import { Button } from "$lib/components/ui/button";
  import { Textarea } from "$lib/components/ui/textarea";
  import { Checkbox } from "$lib/components/ui/checkbox";
  import { Label } from "$lib/components/ui/label";
  import ResponsiveDialog from "./ResponsiveDialog.svelte";
  let {
    worktree,
    onclose,
    onchange,
  }: { worktree: Worktree; onclose: () => void; onchange: () => void } =
    $props();
  let preview = $state<RemovalPreview | null>(null);
  let busy = $state(false);
  let error = $state("");
  let force = $state(false);
  let confirmPath = $state("");
  let closeSessions = $state(false);
  const route = $derived(
    `/repositories/${worktree.repositoryId}/worktrees/${worktree.id}`,
  );
  async function load() {
    busy = true;
    error = "";
    preview = null;
    force = false;
    confirmPath = "";
    closeSessions = false;
    try {
      preview = await api(
        route + "/removal-preview",
        RemovalPreviewSchema,
        "POST",
        {},
      );
    } catch (err) {
      error = (err as Error).message;
    } finally {
      busy = false;
    }
  }
  onMount(() => {
    void load();
  });
  async function remove() {
    if (!preview) return;
    busy = true;
    error = "";
    try {
      await api(route + "/remove", NoContentSchema, "POST", {
        confirmationId: preview.confirmationId,
        mode: force ? "force" : "safe",
        ...(force ? { confirmPath, closeSessions } : {}),
      });
      onclose();
    } catch (err) {
      error = `${(err as Error).message}。操作未完整完成；部分会话可能已关闭，请重新读取预览后确认。`;
      preview = null;
      force = false;
    } finally {
      busy = false;
      onchange();
    }
  }
</script>

<ResponsiveDialog
  title={force ? "强制删除 worktree" : "删除 worktree"}
  description={force
    ? "危险操作：停止已确认的 WebMux 会话，并永久丢弃删除时目录中的全部内容。"
    : "先检查文件与会话。删除 worktree 不删除分支，也不移除已退出的会话记录。"}
  {onclose}
  {busy}
>
  <form
    class="space-y-4"
    onsubmit={(event) => {
      event.preventDefault();
      void remove();
    }}
  >
    <div class="rounded-lg border bg-muted/40 p-3 text-sm">
      <p class="font-medium">
        {worktree.branch ||
          (worktree.detached
            ? `detached · ${worktree.head?.slice(0, 12)}`
            : "无分支")}
      </p>
      <p class="mt-1 font-mono break-all whitespace-pre-wrap">
        {worktree.path}
      </p>
      {#if worktree.detached}<p class="mt-2 text-destructive">
          detached 提交可能没有分支保护，请先在终端保留需要的提交。
        </p>{:else}<p class="mt-2 text-muted-foreground">
          分支保留；目录内未提交、未跟踪及忽略文件不会保留。
        </p>{/if}
    </div>
    {#if busy}<p role="status" class="text-sm">正在处理，请勿重复提交…</p>{/if}
    {#if preview}
      {#if preview.blockers.length}
        <div class="rounded-lg border border-destructive/30 p-3 text-sm">
          <p class="font-medium">安全删除被阻止</p>
          <ul class="mt-2 list-disc space-y-1 pl-5">
            {#each preview.blockers as blocker}<li class="break-all">
                {blocker}
              </li>{/each}
          </ul>
        </div>
      {:else}<p class="text-sm">
          当前工作树可安全删除：没有受保护的文件或活动会话。
        </p>{/if}
      {#if preview.sessions.length}
        <div class="space-y-2 text-sm">
          <p class="font-medium">关联会话（按启动目录识别）</p>
          <ul class="space-y-1">
            {#each preview.sessions as session (session.id)}<li
                class="break-all"
              >
                {session.title} · {sessionStatusText(session)} · {session.cwd}
              </li>{/each}
          </ul>
        </div>
      {/if}
      <p class="text-xs text-muted-foreground">
        不追踪终端内 cd
        后的位置，也无法停止外部进程。外部写入和确认后产生的文件也可能丢失。确认
        5 分钟内有效；目标或会话变化后必须重新预览。
      </p>
      {#if force}
        <div class="space-y-2">
          <Label for="confirm-worktree-path">输入完整路径以确认删除</Label>
          <Textarea
            id="confirm-worktree-path"
            bind:value={confirmPath}
            rows={2}
            mono
            class="break-all"
            autocomplete="off"
            autocapitalize="off"
            autocorrect="off"
            spellcheck={false}
            disabled={busy}
          />
        </div>
        <Label class="items-start">
          <Checkbox
            bind:checked={closeSessions}
            disabled={busy}
            class="mt-0.5"
          />
          <span
            >我同意关闭上面列出的活动会话，并永久删除该目录的全部内容（包括忽略文件）。</span
          >
        </Label>
      {/if}
    {/if}
    {#if error}<p role="alert" class="break-all text-sm text-destructive">
        {error}
      </p>{/if}
    <div class="flex flex-wrap justify-end gap-2">
      <Button type="button" variant="outline" disabled={busy} onclick={onclose}
        >取消</Button
      >
      {#if !preview}<Button type="button" disabled={busy} onclick={load}
          >重新读取预览</Button
        >
      {:else if force}
        <Button
          type="button"
          variant="outline"
          disabled={busy}
          onclick={() => {
            force = false;
            confirmPath = "";
            closeSessions = false;
          }}>返回安全预览</Button
        >
        <Button
          type="submit"
          variant="destructive"
          disabled={busy ||
            confirmPath !== preview.worktree.path ||
            !closeSessions}>关闭会话并强制删除</Button
        >
      {:else if !preview.blockers.length}
        <Button type="submit" disabled={busy}>确认安全删除</Button>
      {:else if preview.forceAllowed}
        <Button
          type="button"
          variant="destructive"
          disabled={busy}
          onclick={() => {
            force = true;
          }}>进入强制删除确认</Button
        >
      {/if}
    </div>
  </form>
</ResponsiveDialog>
