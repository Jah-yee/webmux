<script lang="ts">
  import { untrack, onMount, type Snippet } from "svelte";
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
  import { Label } from "$lib/components/ui/label";
  import * as NativeSelect from "$lib/components/ui/native-select";
  import CodeLineAction from "./CodeLineAction.svelte";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import IconChevronDown from "~icons/tabler/chevron-down";
  import CodeImageView from "./CodeImageView.svelte";
  import CodeDownloadButton from "./CodeDownloadButton.svelte";
  import {
    basename,
    codeRawPath,
    formatBytes,
    imageFormat,
    isImageType,
  } from "../code-image";
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
    menu = $bindable(),
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
    menu?: Snippet;
  } = $props();
  let mode = $state<"file" | "diff">("file");
  let shownDiff = $derived(diff ?? (mode === "diff" ? comparison : undefined));
  let mounted = false;
  const requests = new Set<AbortController>();
  onMount(() => {
    mounted = true;
    menu = documentMenu;
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
  // Selection guidance from the active reader, shown beside the actions.
  let hint = $state("");
  let doc = $derived(
    shownDiff
      ? side === "old"
        ? shownDiff.old
        : shownDiff.new
      : file?.document,
  );
  const defaultSide = $derived<"old" | "new">(
    shownDiff?.new.source === "empty" || file?.document.source === "head"
      ? "old"
      : "new",
  );
  const diffSides = ["new", "old"] as const;
  function sourceLabel(source: string) {
    return source === "worktree"
      ? "工作区"
      : source === "index"
        ? "索引版本"
        : source === "head"
          ? "HEAD 版本"
          : source === "empty"
            ? "不存在"
            : "";
  }
  const snapshotId = $derived(shownDiff?.snapshotId ?? file?.snapshotId ?? "");
  // Diff snapshots need the side; file snapshots ignore it.
  const rawSide = $derived(shownDiff ? side : undefined);
  // SVG is a text document: preview by default, source on request or failure.
  let svgView = $state<"preview" | "source">("preview");
  let svgNotice = $state("");
  const svgFile = $derived(
    !shownDiff && doc?.kind === "text" && doc.mediaType === "image/svg+xml",
  );
  const imagePreview = $derived(
    !!doc &&
      doc.source !== "empty" &&
      (shownDiff
        ? shownDiff.kind === "image" &&
          (doc.kind === "image" ||
            (doc.kind === "text" && doc.mediaType === "image/svg+xml"))
        : doc.kind === "image" || (svgFile && svgView === "preview")),
  );
  // Image documents, over-limit images and either side of an image Diff.
  const imageDocument = $derived(
    !!doc &&
      (shownDiff ? shownDiff.kind === "image" : isImageType(doc.mediaType)),
  );
  const imageChange = $derived(
    shownDiff?.kind !== "image"
      ? ""
      : shownDiff.old.source === "empty"
        ? "图片已新增"
        : shownDiff.new.source === "empty"
          ? "图片已删除"
          : "图片已修改",
  );
  const renamed = $derived(
    !!shownDiff && shownDiff.change.oldPath !== shownDiff.change.newPath,
  );
  const unreadable = $derived(
    shownDiff?.kind === "metadata" ||
      (doc?.kind !== "text" && doc?.kind !== "image"),
  );
  // Notices stay visible on narrow screens, where the rest of the document
  // bar moves into the code menu.
  const notices = $derived(
    !!imageChange || status !== "unchanged" || unreadable || !!svgNotice,
  );
  const sideSize = (value: CodeDiff["old"]) =>
    value.source === "empty" ? "不存在" : formatBytes(value.size);
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
        svgNotice = "";
        generation++;
      }
    });
  });
  $effect(() => {
    const current = shownDiff;
    untrack(() => {
      anchor = reader?.line() ?? 1;
      generation++;
      start = undefined;
      end = undefined;
      invalidSelection = false;
      if (current?.kind !== "image") side = defaultSide;
      // A deleted or added image has nothing to preview on one side.
      if (
        current?.kind === "image" &&
        (side === "new" ? current.new : current.old).source === "empty"
      )
        side = side === "new" ? "old" : "new";
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
    if (!range) {
      side = defaultSide;
      return;
    }
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
  function clearSelection() {
    start = undefined;
    end = undefined;
    invalidSelection = false;
    error = "";
    reader?.clearLineAction();
    diffReader?.clearLineAction();
  }
  function showSvg(view: "preview" | "source") {
    if (view === svgView) return;
    clearSelection();
    svgNotice = "";
    svgView = view;
  }
  function previewFailed(message: string) {
    if (!svgFile || svgView !== "preview") return;
    clearSelection();
    svgView = "source";
    svgNotice = `SVG 预览不可用，已切换到源码：${message}`;
  }
  function downloadFailed(message: string, code: number) {
    error = message;
    if (code === 410) onState("expired");
    else if (code === 409) onState("updated");
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

<!-- Narrow screens move the document bar into the code panel's menu. -->
{#snippet documentMenu()}
  <DropdownMenu.Label variant="title" class="break-all">
    {doc?.absolutePath}
    {#if renamed}
      <span class="mt-1 block text-muted-foreground"
        >{shownDiff?.change.oldPath} → {shownDiff?.change.newPath}</span
      >
    {/if}
  </DropdownMenu.Label>
  {#if file}
    <DropdownMenu.Item
      disabled={busy}
      onSelect={() => (shownDiff ? (mode = "file") : void compare())}
      >{shownDiff ? "返回文件" : "查看 Diff"}</DropdownMenu.Item
    >
  {/if}
  {#if svgFile}
    <DropdownMenu.Separator />
    <DropdownMenu.RadioGroup
      value={svgView}
      onValueChange={(value) => showSvg(value as "preview" | "source")}
    >
      <DropdownMenu.GroupHeading>SVG 显示方式</DropdownMenu.GroupHeading>
      <!-- The view changes underneath; close like the other entries. -->
      <DropdownMenu.RadioItem value="preview" closeOnSelect
        >预览</DropdownMenu.RadioItem
      >
      <DropdownMenu.RadioItem value="source" closeOnSelect
        >源码</DropdownMenu.RadioItem
      >
    </DropdownMenu.RadioGroup>
  {/if}
  {#if shownDiff}
    {#if imageChange}
      <DropdownMenu.Separator />
      <DropdownMenu.RadioGroup
        value={side}
        onValueChange={(value) => {
          side = value as "old" | "new";
          clearSelection();
        }}
      >
        <DropdownMenu.GroupHeading>预览来源</DropdownMenu.GroupHeading>
        {#each diffSides as which}
          <DropdownMenu.RadioItem
            value={which}
            closeOnSelect
            disabled={shownDiff[which].source === "empty"}
            >{which === "old" ? "修改前" : "修改后"}<span
              class="ml-auto text-xs text-muted-foreground"
              >{sourceLabel(shownDiff[which].source)}</span
            ></DropdownMenu.RadioItem
          >
        {/each}
      </DropdownMenu.RadioGroup>
    {/if}
    <DropdownMenu.Separator />
    <DropdownMenu.Group>
      <DropdownMenu.GroupHeading>完整文件</DropdownMenu.GroupHeading>
      {@render fullItems()}
    </DropdownMenu.Group>
  {/if}
  <DropdownMenu.Separator />
  <DropdownMenu.Item onSelect={refresh}>刷新文档</DropdownMenu.Item>
{/snippet}

{#snippet fullItems()}
  {#if shownDiff}
    {#each diffSides as which}
      {#if shownDiff[which].source !== "empty"}
        <DropdownMenu.Item onSelect={() => void full(which)}>
          {which === "old" ? "修改前的文件" : "修改后的文件"}
          <span class="ml-auto text-xs text-muted-foreground"
            >{sourceLabel(shownDiff[which].source)}</span
          >
        </DropdownMenu.Item>
      {/if}
    {/each}
  {/if}
{/snippet}

<div class="flex h-full min-h-0 flex-col overflow-hidden">
  <div
    class={[
      "shrink-0 space-y-1 border-b px-2 py-1 text-xs",
      !notices && "max-sm:hidden",
    ]}
  >
    <div class="flex items-center gap-2 max-sm:hidden">
      <p
        class="min-w-0 flex-1 truncate text-left text-muted-foreground [direction:rtl]"
        title={doc?.absolutePath}
      >
        <bdi>{doc?.absolutePath}</bdi>
      </p>
      {#if file}<Button
          variant="ghost"
          size="sm"
          disabled={busy}
          onclick={() => (shownDiff ? (mode = "file") : void compare())}
          >{shownDiff ? "返回文件" : "查看 Diff"}</Button
        >{/if}
      {#if svgFile}<div
          class="flex shrink-0 gap-1"
          role="group"
          aria-label="SVG 显示方式"
        >
          <Button
            variant={svgView === "preview" ? "secondary" : "outline"}
            size="sm"
            aria-pressed={svgView === "preview"}
            onclick={() => showSvg("preview")}>预览</Button
          ><Button
            variant={svgView === "source" ? "secondary" : "outline"}
            size="sm"
            aria-pressed={svgView === "source"}
            onclick={() => showSvg("source")}>源码</Button
          >
        </div>{/if}
      {#if shownDiff}
        {#if imageChange}<Label size="sm" class="shrink-0"
            >预览来源
            <NativeSelect.Root
              size="sm"
              aria-label="预览来源"
              bind:value={side}
              onchange={clearSelection}
              ><NativeSelect.Option value="new"
                >修改后（{sourceLabel(
                  shownDiff.new.source,
                )}）</NativeSelect.Option
              ><NativeSelect.Option value="old"
                >修改前（{sourceLabel(
                  shownDiff.old.source,
                )}）</NativeSelect.Option
              ></NativeSelect.Root
            ></Label
          >{/if}
        <DropdownMenu.Root>
          <DropdownMenu.Trigger>
            {#snippet child({ props })}
              <Button {...props} variant="ghost" size="sm">
                完整文件<IconChevronDown aria-hidden="true" />
              </Button>
            {/snippet}
          </DropdownMenu.Trigger>
          <DropdownMenu.Content align="end">
            {@render fullItems()}
          </DropdownMenu.Content>
        </DropdownMenu.Root>
      {/if}
    </div>
    {#if renamed}<p class="break-all max-sm:hidden">
        {shownDiff?.change.oldPath} → {shownDiff?.change.newPath}
      </p>{/if}
    {#if imageChange && shownDiff}<p>
        <span class="font-medium">{imageChange}</span> ·
        <span aria-label="大小变化"
          >{sideSize(shownDiff.old)} → {sideSize(shownDiff.new)}</span
        >
      </p>{/if}
    {#if status !== "unchanged"}<p role="status">
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
    {#if unreadable}<p>
        {shownDiff?.reason || doc?.reason}
      </p>{/if}
    {#if svgNotice}<p role="status">{svgNotice}</p>{/if}
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
        bind:hint
        onselect={selectDiff}
      >
        {#snippet lineAction(row)}
          {@const reference = referenceAt(row)}
          {#if reference}<CodeLineAction
              line={reference.number}
              {draftEnabled}
              disabled={status !== "unchanged"}
              onclick={() => send(reference.text)}
            />{/if}
        {/snippet}
      </CodeDiffReader>
    {/key}
  {:else if imagePreview && doc}
    {#key `${snapshotId}:${rawSide ?? ""}`}<CodeImageView
        {id}
        {snapshotId}
        side={rawSide}
        document={doc}
        onerror={previewFailed}
      />{/key}
  {:else if doc?.kind === "text" && !shownDiff}
    {#key `${lastVersion}:${generation}`}<CodeReader
        bind:this={reader}
        text={doc.text}
        path={doc.path}
        restoreLine={anchor}
        onselect={select}
        {draftEnabled}
        bind:hint
      >
        {#snippet lineAction(row)}
          {@const reference = referenceAt(row)}
          {#if reference}<CodeLineAction
              line={reference.number}
              {draftEnabled}
              tooltip
              disabled={status !== "unchanged"}
              onclick={() => send(reference.text)}
            />{/if}
        {/snippet}
      </CodeReader>{/key}
  {:else if imageDocument && doc}
    <div class="space-y-1 p-2 text-sm">
      {#if doc.source === "empty"}<p role="status">
          {side === "old" ? "旧侧" : "新侧"}不存在
        </p>
      {:else}<p aria-label="图片信息">
          {imageFormat(doc.mediaType)} · {formatBytes(doc.size)}
        </p>
        {#if doc.downloadable}<p class="text-muted-foreground">
            不加载预览，可下载原始文件查看。
          </p>{/if}
      {/if}
    </div>
  {/if}
  <div class="mt-auto shrink-0 space-y-1 border-t p-2">
    {#if error}<p class="text-sm text-destructive" role="alert">{error}</p>{/if}
    <div class="flex flex-wrap items-center gap-2">
      <!-- Zero basis: the text truncates instead of wrapping the buttons. -->
      <p class="min-w-0 flex-1 basis-0 truncate text-xs text-muted-foreground">
        {#if shownDiff}{side === "old" ? "修改前" : "修改后"} · {sourceLabel(
            doc?.source ?? "",
          )}{/if}{#if hint}<span class="max-sm:hidden"
            >{shownDiff ? " · " : ""}<span>{hint}</span></span
          >{/if}
      </p>
      {#if start !== undefined || invalidSelection}<Button
          variant="outline"
          size="sm"
          onclick={() => {
            side = defaultSide;
            clearSelection();
          }}>引用整个文件</Button
        >{/if}
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
      {#if imageDocument && doc && doc.source !== "empty"}<CodeDownloadButton
          path={codeRawPath(id, snapshotId, rawSide)}
          filename={basename(doc.path)}
          disabled={!doc.downloadable}
          onerror={downloadFailed}
        />{#if !doc.downloadable}<p
            class="self-center text-xs text-muted-foreground"
          >
            超过 100 MiB 下载上限，不可下载
          </p>{/if}{/if}
    </div>
  </div>
</div>
