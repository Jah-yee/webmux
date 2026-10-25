<script lang="ts">
  import IconInbox from "~icons/tabler/inbox";
  import IconTrash from "~icons/tabler/trash";
  import { tick } from "svelte";
  import { Button } from "$lib/components/ui/button";
  import { Textarea } from "$lib/components/ui/textarea";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import type { TerminalDraft, TerminalDrafts } from "../terminal-draft.svelte";

  let {
    draft,
    drafts,
    unavailable,
    saved,
    target,
    openScratch,
    send,
    resend,
    release,
    filePaste,
    controlNotice,
  }: {
    draft: TerminalDraft;
    drafts: TerminalDrafts;
    unavailable: string;
    // Where the draft is kept, e.g. "已保存在本机".
    saved: string;
    // The session the draft will be sent to.
    target: string;
    openScratch: () => void;
    send: () => void;
    // Repeats an unconfirmed submission under its original ID.
    resend: () => void;
    // Stops tracking a submission and makes its text editable again.
    release: () => void;
    filePaste: (event: ClipboardEvent) => boolean;
    controlNotice: () => void;
  } = $props();
  const submissionText = {
    saved: "已保存在本机，正在发送…",
    sending: "已发出，等待服务端确认…",
    received: "服务端已收到，正在写入终端…",
    unknown: "连接中断，结果未知；恢复连接后自动查询，也可按原提交重发",
    partial: "只有部分内容写入终端，提交键未写入；请检查终端",
  } as Record<string, string>;
  const locked = $derived(!!draft.submission);
  let textarea = $state<HTMLTextAreaElement | null>(null);
  let composing = $state(false);
  let clearOpen = $state(false);

  function remember() {
    if (textarea)
      drafts.select(draft, textarea.selectionStart, textarea.selectionEnd);
  }
  $effect(() => {
    const start = draft.selectionStart;
    const end = draft.selectionEnd;
    const text = draft.text;
    if (textarea && !composing) {
      // External insertions update the caret without opening the mobile keyboard.
      textarea.value = text;
      textarea.setSelectionRange(start ?? text.length, end ?? text.length);
    }
  });
  export function focus() {
    textarea?.focus({ preventScroll: true });
  }
  export function keyboard() {
    if (document.activeElement === textarea) textarea?.blur();
    else focus();
  }
  export function editKey(input: string) {
    if (composing || locked) return;
    const text = draft.text;
    let start = draft.selectionStart ?? text.length;
    let end = draft.selectionEnd ?? start;
    const previous = () =>
      start - (text.codePointAt(start - 2)! > 0xffff ? 2 : 1);
    const next = () => end + (text.codePointAt(end)! > 0xffff ? 2 : 1);
    if (input === "\x7f" || input === "\b") {
      if (start === end && start > 0) start = previous();
      drafts.select(draft, start, end);
      drafts.insert(draft, "");
    } else if (input === "\x1b[3~") {
      if (start === end && end < text.length) end = next();
      drafts.select(draft, start, end);
      drafts.insert(draft, "");
    } else if (
      ["\x1b[D", "\x1b[C", "\x1b[A", "\x1b[B", "\x1b[H", "\x1b[F"].includes(
        input,
      )
    ) {
      let position = start;
      const lineStart = start === 0 ? 0 : text.lastIndexOf("\n", start - 1) + 1;
      const lineEnd = text.indexOf("\n", end);
      if (input === "\x1b[D")
        position = start !== end ? start : Math.max(0, previous());
      if (input === "\x1b[C")
        position = start !== end ? end : Math.min(text.length, next());
      if (input === "\x1b[H") position = lineStart;
      if (input === "\x1b[F") position = lineEnd < 0 ? text.length : lineEnd;
      if (input === "\x1b[A") {
        const previousStart =
          lineStart < 2 ? 0 : text.lastIndexOf("\n", lineStart - 2) + 1;
        position =
          lineStart === 0
            ? 0
            : Math.min(lineStart - 1, previousStart + start - lineStart);
      }
      if (input === "\x1b[B") {
        const nextStart = lineEnd < 0 ? text.length : lineEnd + 1;
        const nextEnd = text.indexOf("\n", nextStart);
        position = Math.min(
          nextEnd < 0 ? text.length : nextEnd,
          nextStart + start - lineStart,
        );
      }
      drafts.select(draft, position, position);
    } else if (
      input === "\r" ||
      input === "\n" ||
      input === "\t" ||
      // oxlint-disable-next-line no-control-regex -- Reject raw terminal controls in composed text.
      !/[\x00-\x1f\x7f]/.test(input)
    ) {
      drafts.insert(draft, input === "\r" ? "\n" : input);
    } else controlNotice();
    void tick().then(focus);
  }
  export function terminalKey(event: KeyboardEvent) {
    focus();
    if (event.type !== "keydown" || event.isComposing) return;
    if (event.ctrlKey || event.metaKey || event.altKey) {
      controlNotice();
      return;
    }
    const keys: Record<string, string> = {
      Enter: "\n",
      Backspace: "\x7f",
      Delete: "\x1b[3~",
      Tab: "\t",
      ArrowLeft: "\x1b[D",
      ArrowRight: "\x1b[C",
      ArrowUp: "\x1b[A",
      ArrowDown: "\x1b[B",
      Home: "\x1b[H",
      End: "\x1b[F",
    };
    if (keys[event.key]) editKey(keys[event.key]);
    else if (Array.from(event.key).length === 1) editKey(event.key);
    else if (event.key === "Escape") controlNotice();
  }
</script>

<section class="terminal-composer" aria-label="终端输入草稿">
  {#if draft.restoredAt}
    <p class="shrink-0 truncate text-xs text-muted-foreground" role="status">
      已恢复本机草稿（{new Date(draft.restoredAt).toLocaleString()}）· 目标：{target}
      · 点击发送才会提交
    </p>
  {/if}
  <Textarea
    bind:ref={textarea}
    value={draft.text}
    readonly={locked}
    aria-label="草稿内容"
    mono
    class="min-h-0 flex-1 resize-none overflow-y-auto field-sizing-fixed"
    autocapitalize="off"
    autocorrect="off"
    spellcheck={false}
    oninput={(event) =>
      drafts.edit(
        draft,
        event.currentTarget.value,
        event.currentTarget.selectionStart,
        event.currentTarget.selectionEnd,
      )}
    onselect={remember}
    onkeyup={remember}
    onclick={remember}
    onblur={remember}
    oncompositionstart={() => (composing = true)}
    oncompositionend={(event) => {
      composing = false;
      drafts.edit(
        draft,
        event.currentTarget.value,
        event.currentTarget.selectionStart,
        event.currentTarget.selectionEnd,
      );
    }}
    onpaste={(event) => {
      filePaste(event);
    }}
    onkeydown={(event) => {
      if (event.isComposing) return;
      if ((event.ctrlKey || event.metaKey) && event.key === "Enter")
        event.preventDefault();
    }}
  />
  <div class="flex shrink-0 flex-wrap items-center gap-2">
    <span
      class="min-w-0 flex-[1_1_100%] wrap-anywhere sm:flex-[1_1_12rem] text-xs text-muted-foreground"
      role="status"
      aria-label="草稿提交状态"
      >{draft.submission
        ? submissionText[draft.submission.state]
        : unavailable || `草稿模式 · ${saved}`}</span
    >
    <div
      class="ml-auto flex max-w-full flex-wrap items-center justify-end gap-2"
    >
      {#if draft.submission?.state === "unknown"}
        <Button
          variant="outline"
          size="sm"
          disabled={!!unavailable}
          title="服务端若已收到原提交，不会重复写入"
          onclick={resend}>按原提交重发</Button
        >
      {/if}
      {#if draft.submission?.state === "unknown" || draft.submission?.state === "partial"}
        <Button
          variant="outline"
          size="sm"
          title="停止跟踪这次提交，把正文作为普通草稿继续编辑"
          onclick={release}>转为草稿</Button
        >
      {/if}
      <Button
        variant="ghost"
        size="sm"
        title="打开暂存"
        disabled={composing}
        aria-label={draft.scratch.length
          ? `打开暂存，共 ${draft.scratch.length} 条`
          : "打开暂存"}
        onclick={openScratch}
        ><IconInbox aria-hidden="true" /><span class="sr-only">暂存</span
        >{#if draft.scratch.length}<span
            class="rounded-sm bg-muted px-1.5 text-xs tabular-nums"
            aria-hidden="true">{draft.scratch.length}</span
          >{/if}</Button
      >
      <Button
        variant="ghost"
        size="icon-sm"
        title="清空草稿"
        aria-label="清空草稿"
        disabled={!draft.text || composing}
        onclick={() => (clearOpen = true)}
        ><IconTrash aria-hidden="true" /></Button
      >
      {#if !draft.submission}
        <Button
          size="sm"
          disabled={!draft.text || !!unavailable || composing}
          onclick={() => {
            if (!composing) send();
          }}>发送</Button
        >
      {/if}
    </div>
  </div>
</section>

<AlertDialog.Root bind:open={clearOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>清空草稿？</AlertDialog.Title>
      <AlertDialog.Description
        >清空当前草稿及其本机保存，不影响终端或已上传文件。此操作无法撤销。</AlertDialog.Description
      >
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel>取消</AlertDialog.Cancel>
      <AlertDialog.Action
        variant="destructive"
        onclick={() => {
          drafts.clear(draft);
          clearOpen = false;
        }}>确认清空</AlertDialog.Action
      >
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
