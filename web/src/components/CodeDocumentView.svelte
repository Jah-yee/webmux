<script lang="ts">
  import { untrack, onMount } from "svelte";
  import { api, APIError } from "../api";
  import {
    CodeFileSchema,
    CodeChangesSchema,
    CodeDiffSchema,
    type CodeFile,
    type CodeDiff,
  } from "../schemas";
  import { calculateDiff } from "../code-diff-client";
  import type { FileDiffMetadata, SelectedLineRange } from "@pierre/diffs";
  import CodeDiffReader from "./CodeDiffReader.svelte";
  import { codeReference, nodeId } from "../code-reference";
  import CodeReader from "./CodeReader.svelte";
  import { Button } from "$lib/components/ui/button";
  import { IconPlus } from "@tabler/icons-svelte";
  import type { TextInsertion } from "../terminal-draft.svelte";
  let {
    id,
    active = true,
    file,
    diff,
    status,
    paste,
    draftEnabled,
    refresh,
    openSide,
    beginOpen,
    contextId,
    comparison,
    onComparison,
    onState,
  }: {
    id: string;
    active?: boolean;
    file?: CodeFile;
    diff?: CodeDiff;
    status: string;
    paste: (text: string) => TextInsertion;
    draftEnabled: boolean;
    refresh: () => void;
    openSide: (file: CodeFile, intent: number) => void;
    beginOpen: () => number;
    contextId: string;
    comparison?: CodeDiff;
    onComparison: (diff: CodeDiff) => void;
    onState: (state: string) => void;
  } = $props();
  let mode = $state<"file" | "diff">("file");
  let shownDiff = $derived(diff ?? (mode === "diff" ? comparison : undefined));
  let mounted = false;
  const requests = new Set<AbortController>();
  onMount(() => {
    mounted = true;
    return () => {
      mounted = false;
      for (const c of requests) c.abort();
    };
  });
  // Renderer metadata crosses the highlighting Worker boundary; keep it plain.
  let fileDiff = $state.raw<FileDiffMetadata>();
  let modelSnapshot = $state("");
  let calculation = $state<"pending" | "ready" | "limited" | "error">(
    "pending",
  );
  let calculationReason = $state("");
  const diffReady = $derived(
    modelSnapshot === shownDiff?.snapshotId && calculation === "ready",
  );
  let side = $state<"old" | "new">("new");
  let start = $state<number>();
  let end = $state<number>();
  let invalidSelection = $state(false);
  let error = $state("");
  let busy = $state(false);
  let generation = $state(0);
  let reader = $state<CodeReader>();
  let diffReader = $state<CodeDiffReader>();
  let anchor = $state(1);
  let lastVersion = $state("");
  let doc = $derived(
    shownDiff
      ? side === "old"
        ? shownDiff.old
        : shownDiff.new
      : file?.document,
  );
  let preview = $derived.by(() => {
    if (
      invalidSelection ||
      !doc ||
      !doc.referenceable ||
      doc.source === "empty"
    )
      return "";
    try {
      return codeReference(doc, start, end);
    } catch {
      return "";
    }
  });
  $effect(() => {
    const version = file?.snapshotId ?? diff?.snapshotId ?? "";
    untrack(() => {
      if (version !== lastVersion) {
        anchor = reader?.line() ?? 1;
        lastVersion = version;
        mode = "file";
        start = undefined;
        end = undefined;
        invalidSelection = false;
        error = "";
        generation++;
      }
    });
  });
  $effect(() => {
    const current = shownDiff;
    untrack(() => {
      anchor = reader?.line() ?? 1;
      generation++;
    });
  });
  $effect(() => {
    const current = shownDiff;
    if (!active || current?.kind !== "text") return;
    if (modelSnapshot === current.snapshotId) return;
    const controller = new AbortController();
    untrack(() => {
      calculation = "pending";
      calculationReason = "";
      fileDiff = undefined;
    });
    void calculateDiff(
      current.snapshotId,
      current.old.text,
      current.new.text,
      controller.signal,
    )
      .then((result) => {
        if (
          controller.signal.aborted ||
          shownDiff?.snapshotId !== current.snapshotId
        )
          return;
        modelSnapshot = current.snapshotId;
        calculation = result.kind;
        if (result.kind === "ready") {
          fileDiff = {
            ...result.fileDiff,
            name: current.new.path || current.old.path,
            prevName:
              current.old.path !== current.new.path
                ? current.old.path
                : undefined,
            type:
              current.old.source === "empty"
                ? "new"
                : current.new.source === "empty"
                  ? "deleted"
                  : current.old.path !== current.new.path
                    ? result.fileDiff.hunks.length
                      ? "rename-changed"
                      : "rename-pure"
                    : result.fileDiff.type,
            cacheKey: current.snapshotId,
          };
          generation++;
        } else calculationReason = result.reason;
      })
      .catch((error: unknown) => {
        if (
          controller.signal.aborted ||
          shownDiff?.snapshotId !== current.snapshotId
        )
          return;
        modelSnapshot = current.snapshotId;
        calculation = "error";
        calculationReason =
          error instanceof Error
            ? error.message
            : "差异计算失败，请查看两侧完整文件";
      });
    return () => controller.abort();
  });
  async function compare() {
    if (!file) return;
    if (comparison) {
      mode = "diff";
      return;
    }
    const controller = new AbortController();
    requests.add(controller);
    busy = true;
    try {
      const params = new URLSearchParams({
        contextId,
        nodeId: file.document.nodeId ?? nodeId(file.document.path),
      });
      const list = await api(
        `/sessions/${id}/code/changes?${params}`,
        CodeChangesSchema,
        "GET",
        undefined,
        controller.signal,
      );
      const group = file.document.source !== "worktree" ? "staged" : "unstaged";
      const change = list.entries.find(
        (c) => c.group === group || c.group === "untracked",
      );
      if (!change) throw new Error("此文件没有可用的未提交差异，保留完整阅读");
      const value = await api(
        `/sessions/${id}/code/diff?${new URLSearchParams({ contextId, changeId: change.changeId })}`,
        CodeDiffSchema,
        "GET",
        undefined,
        controller.signal,
      );
      if (!mounted || controller.signal.aborted) return;
      const candidate = file.document.source === "head" ? value.old : value.new;
      if (candidate.contentVersion !== file.document.contentVersion) {
        onState("updated");
        throw new Error("比较来源已更新，请刷新完整文件后再切换 Diff");
      }
      onComparison(value);
      side = file.document.source === "head" ? "old" : "new";
      mode = "diff";
    } catch (e) {
      if (mounted && !controller.signal.aborted) error = (e as Error).message;
    } finally {
      requests.delete(controller);
      busy = false;
    }
  }
  function select(a: number, z: number) {
    error = "";
    invalidSelection = false;
    start = a;
    end = z;
  }
  function selectDiff(range: SelectedLineRange | null) {
    error = "";
    start = undefined;
    end = undefined;
    invalidSelection = false;
    if (!range) return;
    if (range.endSide && range.endSide !== range.side) {
      invalidSelection = true;
      error = "请选择同一来源的连续行";
      return;
    }
    side = range.side === "deletions" ? "old" : "new";
    start = Math.min(range.start, range.end);
    end = Math.max(range.start, range.end);
  }
  function referenceAt(row: number) {
    if (!doc) return;
    try {
      return { number: row, text: codeReference(doc, row) };
    } catch {
      return;
    }
  }
  function send(reference = preview) {
    if (!reference || status !== "unchanged") return;
    error =
      paste(reference) !== "failed" ? "" : "输入目标未就绪，位置未插入；请重试";
  }
  async function full(which: "old" | "new") {
    if (!shownDiff) return;
    const sid = shownDiff.snapshotId;
    const intent = beginOpen();
    const controller = new AbortController();
    requests.add(controller);
    try {
      const value = await api(
        `/sessions/${id}/code/snapshots/${sid}/file?side=${which}`,
        CodeFileSchema,
        "GET",
        undefined,
        controller.signal,
      );
      if (
        mounted &&
        !controller.signal.aborted &&
        shownDiff?.snapshotId === sid
      )
        openSide(value, intent);
    } catch (e) {
      if (mounted && !controller.signal.aborted) {
        if (e instanceof APIError && e.status === 410) onState("expired");
        error = (e as Error).message;
      }
    } finally {
      requests.delete(controller);
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col overflow-hidden">
  <div
    class="flex shrink-0 flex-wrap items-center gap-x-2 gap-y-1 border-b px-2 py-1 text-xs"
  >
    <p
      class="min-w-0 grow basis-40 truncate text-left text-muted-foreground [direction:rtl]"
      title={doc?.absolutePath}
    >
      <bdi>{doc?.absolutePath}</bdi>
    </p>
    {#if file}<div class="flex gap-2">
        <Button variant="outline" size="sm" onclick={() => (mode = "file")}
          >完整文件</Button
        ><Button
          variant="outline"
          size="sm"
          disabled={busy}
          onclick={() => void compare()}>查看 Diff</Button
        >
      </div>{/if}
    {#if shownDiff}
      {#if shownDiff.change.oldPath !== shownDiff.change.newPath}
        <p class="basis-full break-all">
          {shownDiff.change.oldPath} → {shownDiff.change.newPath}
        </p>
      {/if}
      <label class="flex items-center gap-1"
        >引用来源 <select
          aria-label="引用来源"
          bind:value={side}
          onchange={() => {
            start = undefined;
            end = undefined;
            invalidSelection = false;
            error = "";
            reader?.clearLineAction();
            diffReader?.clearLineAction();
          }}
          class="h-7 rounded border bg-background px-1"
          ><option value="new">新侧 ({shownDiff.new.source})</option><option
            value="old">旧侧 ({shownDiff.old.source})</option
          ></select
        ></label
      >
      <div class="flex gap-2">
        <Button variant="outline" size="sm" onclick={() => void full("old")}
          >旧侧完整文件</Button
        >
        <Button variant="outline" size="sm" onclick={() => void full("new")}
          >新侧完整文件</Button
        >
      </div>
    {/if}
    {#if status !== "unchanged"}<p role="status" class="basis-full">
        {status === "updated"
          ? "内容已更新"
          : status === "missing"
            ? "文件已不存在"
            : status === "expired"
              ? "快照已过期"
              : "检测失败，状态未知"}，保留旧内容。<Button
          variant="outline"
          size="sm"
          onclick={refresh}>刷新文档</Button
        >
      </p>{/if}
    {#if shownDiff?.kind === "metadata" || doc?.kind !== "text"}<p
        class="basis-full"
      >
        {shownDiff?.reason || doc?.reason}
      </p>{/if}
  </div>
  {#if shownDiff?.kind === "text" && !diffReady}
    <p class="p-2 text-sm" role="status">
      {calculation === "pending" ? "正在计算差异…" : calculationReason}
    </p>
    {#if calculation === "error" || calculation === "limited"}
      <Button variant="outline" size="sm" onclick={() => (modelSnapshot = "")}
        >重试差异计算</Button
      >
    {/if}
  {/if}
  {#if shownDiff?.kind === "text" && diffReady && fileDiff}
    {#key modelSnapshot}
      <CodeDiffReader
        bind:this={diffReader}
        {fileDiff}
        {side}
        expired={status === "expired"}
        {draftEnabled}
        onselect={selectDiff}
      >
        {#snippet lineAction(row)}
          {@const reference = referenceAt(row)}
          {#if reference}
            <Button
              size="icon"
              class="size-9 border border-border shadow-sm"
              aria-label={`发送第 ${row} 行位置到${draftEnabled ? "草稿" : "终端"}`}
              disabled={status !== "unchanged"}
              onclick={() => send(reference.text)}
              ><IconPlus class="size-5" aria-hidden="true" /></Button
            >
          {/if}
        {/snippet}
      </CodeDiffReader>
    {/key}
  {:else if doc?.kind === "text" && !shownDiff}
    {#key `${lastVersion}:${generation}`}<CodeReader
        bind:this={reader}
        text={doc.text}
        path={doc.path}
        restoreLine={anchor}
        onselect={select}
        {draftEnabled}
      >
        {#snippet lineAction(row)}
          {@const reference = referenceAt(row)}
          {#if reference}
            <Button
              size="icon"
              class="size-9 border border-border shadow-sm"
              aria-label={`发送第 ${reference.number} 行位置到${draftEnabled ? "草稿" : "终端"}`}
              title={`发送第 ${reference.number} 行位置到${draftEnabled ? "草稿" : "终端"}`}
              disabled={status !== "unchanged"}
              onclick={() => send(reference.text)}
              ><IconPlus class="size-5" aria-hidden="true" /></Button
            >
          {/if}
        {/snippet}
      </CodeReader>{/key}
  {/if}
  <div class="mt-auto shrink-0 space-y-1 border-t p-2">
    {#if error}<p class="text-sm text-destructive" role="alert">{error}</p>{/if}
    <div class="flex flex-wrap gap-2">
      <Button
        variant="outline"
        size="sm"
        onclick={() => {
          start = undefined;
          end = undefined;
          invalidSelection = false;
          error = "";
          reader?.clearLineAction();
          diffReader?.clearLineAction();
        }}>引用整个文件</Button
      >
      <Button
        size="sm"
        disabled={!preview || status !== "unchanged"}
        onclick={() => send()}
        >发送位置到{draftEnabled
          ? "草稿"
          : "终端"}{#if start !== undefined}（{start}{end !== undefined &&
          end !== start
            ? `-${end}`
            : ""} 行）{/if}</Button
      >
    </div>
  </div>
</div>
