<script lang="ts">
  import IconArrowDown from "~icons/tabler/arrow-down";
  import IconArrowUp from "~icons/tabler/arrow-up";
  import IconArrowUpRight from "~icons/tabler/arrow-up-right";
  import IconDots from "~icons/tabler/dots";
  import IconInbox from "~icons/tabler/inbox";
  import IconPlus from "~icons/tabler/plus";
  import IconTrash from "~icons/tabler/trash";
  import IconX from "~icons/tabler/x";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import InlineNotice from "./InlineNotice.svelte";
  import { Button } from "$lib/components/ui/button";
  import { Label } from "$lib/components/ui/label";
  import { Textarea } from "$lib/components/ui/textarea";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import * as Dialog from "$lib/components/ui/dialog";
  import {
    scratchEntriesLimit,
    scratchEntryLimit,
    scratchStorageLimit,
  } from "../draftStorage";
  import type {
    ScratchAddResult,
    ScratchTakeMode,
    ScratchTakeResult,
    TerminalDraft,
    TerminalDrafts,
  } from "../terminal-draft.svelte";

  let {
    open = $bindable(false),
    draft,
    drafts,
    saved,
  }: {
    open: boolean;
    draft: TerminalDraft;
    drafts: TerminalDrafts;
    // Where the scratch list is kept, e.g. "已保存在本机".
    saved: string;
  } = $props();
  let closeButton = $state<HTMLButtonElement | null>(null);
  let newText = $state("");
  let notice = $state("");
  let noticeTone = $state<"danger" | "success">("danger");
  let conflictId = $state<string | null>(null);
  let clearOpen = $state(false);
  const locked = $derived(!!draft.submission);
  const full = $derived(draft.scratch.length >= scratchEntriesLimit);
  const stale = $derived(
    !!draft.scratchGeneration &&
      !!draft.generation &&
      draft.scratchGeneration !== draft.generation,
  );

  function failure(result: ScratchAddResult | ScratchTakeResult) {
    if (result === "full")
      return `最多保留 ${scratchEntriesLimit} 条暂存，请先删除一条。`;
    if (result === "too-long")
      return `单条暂存最多 ${scratchEntryLimit.toLocaleString()} 个字符。`;
    if (result === "over-total")
      return `每会话暂存合计最多 ${scratchStorageLimit.toLocaleString()} 个字符。`;
    if (result === "locked")
      return "提交结果确认前，当前草稿不能转入或取用暂存。";
    if (result === "empty") return "暂存正文不能为空。";
    return "暂存操作失败。";
  }
  function add() {
    const result = drafts.addScratch(draft, newText);
    if (result === "added") {
      newText = "";
      if (!draft.scratchPersisted) {
        notice = "暂存已添加，但未能保存到本机，刷新后可能丢失。";
        noticeTone = "danger";
      } else notice = "";
    } else {
      notice = failure(result);
      noticeTone = "danger";
    }
  }
  function stash() {
    const result = drafts.stash(draft);
    if (result === "added") {
      if (!draft.scratchPersisted) {
        notice = "当前草稿已存入暂存，但暂存未能保存到本机，刷新后可能丢失。";
        noticeTone = "danger";
      } else {
        notice = "当前草稿已存入暂存。";
        noticeTone = "success";
      }
    } else {
      notice = failure(result);
      noticeTone = "danger";
    }
  }
  function take(id: string) {
    const item = draft.scratch.find((candidate) => candidate.id === id);
    if (!item?.text) {
      notice = "这条暂存没有正文。";
      noticeTone = "danger";
      return;
    }
    if (draft.submission) {
      notice = failure("locked");
      noticeTone = "danger";
      return;
    }
    if (draft.text) {
      conflictId = id;
      return;
    }
    takeNow(id, "replace");
  }
  function takeNow(id: string, mode: ScratchTakeMode) {
    const result = drafts.takeScratch(draft, id, mode);
    conflictId = null;
    if (result === "taken") {
      if (!draft.scratchPersisted || !draft.persisted) {
        notice =
          "已取用到当前草稿，但部分内容未能保存到本机，刷新后可能丢失；请保留当前页面内容。";
        noticeTone = "danger";
      } else {
        notice = "";
        open = false;
      }
    } else {
      notice = failure(result);
      noticeTone = "danger";
    }
  }
  function edit(id: string, value: string, element: HTMLTextAreaElement) {
    if (drafts.editScratch(draft, id, value)) {
      if (!draft.scratchPersisted) {
        notice = "暂存未能保存到本机，刷新后可能丢失。";
        noticeTone = "danger";
      } else if (notice) notice = "";
      return;
    }
    element.value =
      draft.scratch.find((candidate) => candidate.id === id)?.text ?? "";
    notice = `单条暂存最多 ${scratchEntryLimit.toLocaleString()} 个字符，且每会话合计最多 ${scratchStorageLimit.toLocaleString()} 个字符。`;
    noticeTone = "danger";
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content
    class="flex h-[min(48rem,calc(100dvh-1rem))] max-h-[calc(100dvh-1rem)] max-w-[calc(100%-1rem)] flex-col gap-0 overflow-hidden p-0 sm:h-[min(48rem,calc(100dvh-4rem))] sm:max-w-2xl"
    showCloseButton={false}
    onOpenAutoFocus={(event) => {
      event.preventDefault();
      closeButton?.focus();
    }}
  >
    <div
      class="flex shrink-0 items-start gap-3 border-b px-4 py-4 sm:px-6 sm:py-5"
    >
      <span
        class="hidden size-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary sm:flex"
        aria-hidden="true"><IconInbox class="size-5" /></span
      >
      <Dialog.Header class="min-w-0 flex-1 gap-1 pr-0 text-left">
        <Dialog.Title>暂存</Dialog.Title>
        <Dialog.Description>先记下来，准备好后取用到草稿。</Dialog.Description>
      </Dialog.Header>
      <Dialog.Close>
        {#snippet child({ props })}
          <Button
            {...props}
            bind:ref={closeButton}
            variant="ghost"
            size="icon"
            class="size-11 -mr-2 -mt-1"
            title="关闭暂存"
            aria-label="关闭暂存"><IconX aria-hidden="true" /></Button
          >
        {/snippet}
      </Dialog.Close>
    </div>

    <div
      class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-4 sm:px-6"
      data-testid="scratch-scroll"
    >
      <div class="grid min-w-0 gap-3 [&>*]:min-w-0">
        {#if stale}
          <InlineNotice tone="warning"
            >这些暂存来自较早的会话实例。取用不会自动发送，请检查内容后再使用。</InlineNotice
          >
        {/if}
        {#if locked}
          <InlineNotice tone="info"
            >当前草稿有待确认的提交，转存和取用暂不可用；暂存仍可查看、编辑和删除。</InlineNotice
          >
        {/if}
        {#if draft.text}
          <div class="flex items-center gap-3 rounded-lg bg-muted/60 p-3">
            <div class="min-w-0 flex-1">
              <p class="text-xs font-medium text-muted-foreground">当前草稿</p>
              <p class="mt-1 truncate text-sm" title={draft.text}>
                {draft.text}
              </p>
            </div>
            <Button
              variant="outline"
              size="icon-touch"
              disabled={locked || full}
              title="将当前草稿存入暂存"
              aria-label="将当前草稿存入暂存"
              onclick={stash}
            >
              <IconInbox aria-hidden="true" />
            </Button>
          </div>
        {/if}
        <div class="flex min-h-11 items-center justify-between gap-2">
          <h3 class="flex items-center gap-2 text-sm font-medium">
            已暂存 <span
              class="rounded-md bg-muted px-2 py-0.5 text-xs tabular-nums text-muted-foreground"
              >{draft.scratch.length} / {scratchEntriesLimit}</span
            >
          </h3>
          <Button
            variant="quiet"
            size="icon-touch"
            disabled={!draft.scratch.length}
            title="清空暂存"
            aria-label="清空暂存"
            onclick={() => (clearOpen = true)}
            ><IconTrash aria-hidden="true" /></Button
          >
        </div>
        {#if draft.scratch.length}
          <div
            class="grid min-w-0 gap-3 [&>*]:min-w-0"
            role="list"
            aria-label="暂存列表"
          >
            {#each draft.scratch as item, index (item.id)}
              <div
                class="rounded-lg border bg-card p-3 transition-colors focus-within:border-ring/60"
                role="listitem"
              >
                <div class="mb-2 flex flex-wrap items-center gap-2">
                  <span
                    class="text-xs font-medium tabular-nums text-muted-foreground"
                    >暂存 {String(index + 1).padStart(2, "0")}</span
                  >
                  <span
                    class="ml-auto text-xs tabular-nums text-muted-foreground"
                    >{item.text.length.toLocaleString()} 字符</span
                  >
                  <DropdownMenu.Root>
                    <DropdownMenu.Trigger>
                      {#snippet child({ props })}
                        <Button
                          {...props}
                          variant="ghost"
                          size="icon-touch"
                          title="更多操作"
                          aria-label={`暂存第 ${index + 1} 条的更多操作`}
                          ><IconDots aria-hidden="true" /></Button
                        >
                      {/snippet}
                    </DropdownMenu.Trigger>
                    <DropdownMenu.Content align="end">
                      <DropdownMenu.Item
                        class="min-h-11 sm:min-h-9"
                        disabled={index === 0}
                        aria-label={`上移暂存第 ${index + 1} 条`}
                        onSelect={() => drafts.moveScratch(draft, item.id, -1)}
                        ><IconArrowUp
                          aria-hidden="true"
                        />上移</DropdownMenu.Item
                      >
                      <DropdownMenu.Item
                        class="min-h-11 sm:min-h-9"
                        disabled={index === draft.scratch.length - 1}
                        aria-label={`下移暂存第 ${index + 1} 条`}
                        onSelect={() => drafts.moveScratch(draft, item.id, 1)}
                        ><IconArrowDown
                          aria-hidden="true"
                        />下移</DropdownMenu.Item
                      >
                      <DropdownMenu.Separator />
                      <DropdownMenu.Item
                        class="min-h-11 sm:min-h-9"
                        variant="destructive"
                        aria-label={`删除暂存第 ${index + 1} 条`}
                        onSelect={() => drafts.removeScratch(draft, item.id)}
                        ><IconTrash aria-hidden="true" />删除</DropdownMenu.Item
                      >
                    </DropdownMenu.Content>
                  </DropdownMenu.Root>
                  <Button
                    variant="secondary"
                    size="icon-touch"
                    disabled={!item.text || locked}
                    title="取用到草稿"
                    aria-label={`取用暂存第 ${index + 1} 条`}
                    onclick={() => take(item.id)}
                    ><IconArrowUpRight
                      class="text-primary"
                      aria-hidden="true"
                    /></Button
                  >
                </div>
                <Textarea
                  value={item.text}
                  aria-label={`暂存第 ${index + 1} 条`}
                  rows={3}
                  maxlength={scratchEntryLimit}
                  variant="ghost"
                  mono
                  class="max-h-48 min-h-20 resize-y"
                  autocapitalize="off"
                  autocorrect="off"
                  spellcheck={false}
                  oninput={(event) =>
                    edit(
                      item.id,
                      event.currentTarget.value,
                      event.currentTarget,
                    )}
                />
                <div class="mt-2 flex items-center justify-between gap-3">
                  <time
                    class="text-xs text-muted-foreground"
                    datetime={new Date(item.updatedAt).toISOString()}
                    title={new Date(item.updatedAt).toLocaleString()}
                  >
                    更新于 {new Date(item.updatedAt).toLocaleTimeString([], {
                      hour: "2-digit",
                      minute: "2-digit",
                      hour12: false,
                    })}
                  </time>
                </div>
              </div>
            {/each}
          </div>
        {:else}
          <div
            class="grid justify-items-center gap-2 rounded-lg border border-dashed px-5 py-8 text-center"
          >
            <IconInbox
              class="mb-1 size-7 text-muted-foreground"
              aria-hidden="true"
            />
            <p class="text-sm font-medium">还没有暂存</p>
            <p class="max-w-64 text-sm text-muted-foreground">
              在下方记下后续任务，或存入当前草稿，稍后再逐条取用。
            </p>
          </div>
        {/if}
      </div>
    </div>

    <div
      class="grid max-h-[50dvh] shrink-0 gap-3 overflow-y-auto border-t bg-muted/30 px-4 py-4 sm:px-6"
    >
      {#if notice}
        <InlineNotice tone={noticeTone}>{notice}</InlineNotice>
      {/if}
      <div class="flex flex-wrap items-center justify-between gap-2">
        <Label for="scratch-new">新建暂存</Label>
        <span class="text-xs text-muted-foreground" role="status">{saved}</span>
      </div>
      <Textarea
        id="scratch-new"
        bind:value={newText}
        rows={2}
        maxlength={scratchEntryLimit}
        placeholder="记下稍后要做的事…"
        mono
        class="h-20 min-h-20 resize-none field-sizing-fixed"
        autocapitalize="off"
        autocorrect="off"
        spellcheck={false}
        aria-describedby="scratch-new-limits"
      />
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="min-w-0 text-xs text-muted-foreground">
          <p>取用到草稿后，仍需手动发送</p>
          <p id="scratch-new-limits" class="mt-1 tabular-nums">
            {newText.length.toLocaleString()} / {scratchEntryLimit.toLocaleString()}
            字符<span class="sr-only"
              >；最多 {scratchEntriesLimit} 条，每会话合计 {scratchStorageLimit.toLocaleString()}
              字符</span
            >
          </p>
        </div>
        <Button size="sm-touch" disabled={!newText || full} onclick={add}
          ><IconPlus aria-hidden="true" />添加暂存</Button
        >
      </div>
      {#if full}<p class="text-xs text-warning" role="status">
          已达 {scratchEntriesLimit} 条上限，取用或删除一条后可继续添加。
        </p>{/if}
    </div>
  </Dialog.Content>
</Dialog.Root>

<AlertDialog.Root
  open={conflictId !== null}
  onOpenChange={(value) => {
    if (!value) conflictId = null;
  }}
>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>当前草稿不为空</AlertDialog.Title>
      <AlertDialog.Description
        >可以将暂存追加到当前正文之后，或替换当前正文；替换前会把当前正文保存为一条新的暂存。</AlertDialog.Description
      >
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel>取消</AlertDialog.Cancel>
      <Button
        variant="outline"
        onclick={() => conflictId && takeNow(conflictId, "append")}
        >追加到当前草稿</Button
      >
      <Button onclick={() => conflictId && takeNow(conflictId, "replace")}
        >替换当前草稿</Button
      >
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root bind:open={clearOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>清空全部暂存？</AlertDialog.Title>
      <AlertDialog.Description
        >删除这个会话的全部暂存，不影响当前草稿、终端或已上传文件。此操作无法撤销。</AlertDialog.Description
      >
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel>取消</AlertDialog.Cancel>
      <AlertDialog.Action
        variant="destructive"
        onclick={() => {
          drafts.clearScratch(draft);
          clearOpen = false;
        }}>确认清空</AlertDialog.Action
      >
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
