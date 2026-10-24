<script lang="ts">
  import { onMount, tick, untrack, type Snippet } from "svelte";
  import {
    CodeView,
    type CodeViewOptions,
    type FileDiffMetadata,
    type SelectedLineRange,
    type SelectionSide,
  } from "@pierre/diffs";
  import { SearchQuery } from "@codemirror/search";
  import { Text } from "@codemirror/state";
  import { z } from "zod";
  import { store, stored } from "../api";
  import { acquireDiffHighlighter } from "../code-diff-highlighter";
  import { Button } from "$lib/components/ui/button";
  import CodeSearchPanel, { type SearchSpec } from "./CodeSearchPanel.svelte";
  import CodeGotoLine from "./CodeGotoLine.svelte";
  import IconArrowsHorizontal from "~icons/tabler/arrows-horizontal";
  import IconSearch from "~icons/tabler/search";
  import IconTextWrap from "~icons/tabler/text-wrap";

  let {
    fileDiff,
    side,
    expired,
    draftEnabled,
    onselect,
    lineAction,
  }: {
    fileDiff: FileDiffMetadata;
    side: "old" | "new";
    expired: boolean;
    draftEnabled: boolean;
    onselect: (range: SelectedLineRange | null) => void;
    lineAction?: Snippet<[line: number]>;
  } = $props();

  const OverflowSchema = z.enum(["scroll", "wrap"]);
  let overflow = $state(
    OverflowSchema.catch("scroll").parse(stored("codeDiffOverflow", "scroll")),
  );
  let width = $state(0);
  let dark = $state(false);
  let coarse = $state(false);
  let selected = $state<SelectedLineRange | null>(null);
  let host: HTMLDivElement;
  let root: HTMLDivElement;
  let viewer: CodeView | undefined;
  let mounted = $state(false);
  let panel = $state<"search" | "goto">();
  let search = $state<SearchSpec>({
    search: "",
    caseSensitive: false,
    regexp: false,
    wholeWord: false,
  });
  let matchIndex = $state(-1);
  const id = "diff";
  const diffStyle = $derived(width >= 760 ? "split" : "unified");
  const hasChanges = $derived(fileDiff.hunks.length > 0);
  const totals = $derived(
    fileDiff.hunks.reduce(
      (sum, hunk) => ({
        additions: sum.additions + hunk.additionLines,
        deletions: sum.deletions + hunk.deletionLines,
      }),
      { additions: 0, deletions: 0 },
    ),
  );
  const sourceSide = $derived<SelectionSide>(
    side === "old" ? "deletions" : "additions",
  );
  const sourceLines = $derived(
    side === "old" ? fileDiff.deletionLines : fileDiff.additionLines,
  );
  const matches = $derived.by(() => {
    if (!hasChanges || !search.search) return [];
    const query = new SearchQuery(search);
    if (!query.valid) return [];
    const doc = Text.of(sourceLines.join("").split("\n"));
    const cursor = query.getCursor(doc);
    const result: number[] = [];
    for (
      let match = cursor.next();
      !match.done && result.length < 5000;
      match = cursor.next()
    ) {
      result.push(doc.lineAt(match.value.from).number);
    }
    return result;
  });

  export function clearLineAction() {
    selected = null;
    viewer?.clearSelectedLines({ notify: false });
  }
  function choose(range: SelectedLineRange | null) {
    selected = range;
    onselect(range);
  }
  function scrollToLine(line: number) {
    if (!hasChanges || !sourceLines.length) return;
    const number = Math.max(1, Math.min(sourceLines.length, line));
    const startOf = (hunk: FileDiffMetadata["hunks"][number]) =>
      side === "old" ? hunk.deletionStart : hunk.additionStart;
    let region = fileDiff.hunks.findIndex((hunk) => number < startOf(hunk));
    if (region < 0) region = fileDiff.hunks.length;
    const previous = fileDiff.hunks[region - 1];
    const previousEnd = previous
      ? startOf(previous) +
        (side === "old" ? previous.deletionCount : previous.additionCount) -
        1
      : 0;
    if (number > previousEnd) {
      if (expired) return;
      const item = viewer?.getRenderedItems()[0];
      if (item?.type === "diff")
        item.instance.expandHunk(region, "both", Number.POSITIVE_INFINITY);
    }
    const range = { start: number, end: number, side: sourceSide };
    viewer?.scrollTo({
      type: "line",
      id,
      lineNumber: number,
      side: sourceSide,
      align: "center",
    });
    viewer?.setSelectedLines({ id, range }, { notify: false });
    choose(range);
  }
  async function openPanel(value: "search" | "goto", toggle = true) {
    if (!hasChanges) return;
    panel = toggle && panel === value ? undefined : value;
    await tick();
    root
      .querySelector<HTMLInputElement>(
        ".code-search input, .code-goto-line input",
      )
      ?.focus();
  }
  function nextMatch(direction: number) {
    if (!matches.length) return;
    const current = matchIndex < 0 && direction < 0 ? 0 : matchIndex;
    matchIndex = (current + direction + matches.length) % matches.length;
    scrollToLine(matches[matchIndex]);
  }
  function goto(value: string) {
    const match = /^([+-])?(\d+)?(:\d+)?(%)?$/.exec(value.trim());
    if (match) {
      const current = selected?.start ?? 1;
      const [, sign, digits, , percent] = match;
      let target = digits ? Number(digits) : current;
      if (digits && percent) {
        target =
          Math.round((sourceLines.length * target) / 100) *
            (sign === "-" ? -1 : 1) +
          (sign ? current : 0);
      } else if (digits && sign)
        target = current + target * (sign === "-" ? -1 : 1);
      scrollToLine(target);
    }
    panel = undefined;
    host.focus();
  }
  function options(): CodeViewOptions<undefined> {
    return {
      diffStyle,
      overflow,
      theme: { light: "pierre-light", dark: "pierre-dark" },
      themeType: dark ? "dark" : "light",
      diffIndicators: "bars",
      lineDiffType: "word-alt",
      maxLineDiffLength: 4096,
      tokenizeMaxLineLength: 4096,
      disableFileHeader: true,
      enableLineSelection: true,
      lineHoverHighlight: "number",
      expansionLineCount: 30,
      hunkSeparators: "line-info",
      itemMetrics: { lineHeight: 22 },
      layout: { paddingTop: 0, paddingBottom: 0, gap: 0 },
      onSelectedLinesChange: (selection) => choose(selection?.range ?? null),
      onLineNumberClick(props) {
        if (
          props.type !== "diff-line" ||
          props.event.shiftKey ||
          (selected && selected.start !== selected.end)
        )
          return;
        // 1.3.6 toggles a selected row by number alone, even when clicking the
        // opposite side. Explicitly retain the clicked side for references.
        const range = {
          start: props.lineNumber,
          end: props.lineNumber,
          side: props.annotationSide,
        };
        viewer?.setSelectedLines({ id, range }, { notify: false });
        choose(range);
      },
      onLineClick(props) {
        if (!coarse || props.type !== "diff-line") return;
        const nativeSelection = document.getSelection();
        if (nativeSelection && !nativeSelection.isCollapsed) return;
        const range = {
          start: props.lineNumber,
          end: props.lineNumber,
          side: props.annotationSide,
        };
        viewer?.setSelectedLines({ id, range }, { notify: false });
        choose(range);
      },
      // CodeView publishes getRenderedItems() after the item render callback.
      // Defer until then so plain-text files get named, focusable controls on
      // the first render, even when highlighting needs no second render.
      onPostRender: () => queueMicrotask(enhanceControls),
      // Only fixed application CSS enters the shadow root. Source text is
      // rendered by the library's escaped token renderer.
      unsafeCSS: `
        [data-expand-button][aria-disabled="true"] { opacity: .45; cursor: not-allowed; }
        [data-expand-button]:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; }
        [data-line], [data-column-number] { font-variant-ligatures: none; }
      `,
    };
  }
  // The library's expand controls are role=button divs. Supply names and
  // keyboard handling here, and enforce the frozen-snapshot policy.
  function enhanceControls() {
    for (const item of viewer?.getRenderedItems() ?? []) {
      for (const button of item.element.shadowRoot?.querySelectorAll<HTMLElement>(
        "[data-expand-button]",
      ) ?? []) {
        const all =
          button.hasAttribute("data-expand-all-button") ||
          button.hasAttribute("data-expand-both");
        button.setAttribute(
          "aria-label",
          all
            ? "展开全部上下文"
            : button.hasAttribute("data-expand-up")
              ? "向上展开 30 行"
              : "向下展开 30 行",
        );
        button.setAttribute("aria-disabled", String(expired));
        button.tabIndex = expired ? -1 : 0;
        if (button.hasAttribute("data-expand-all-button"))
          button.textContent = "展开全部";
      }
      for (const label of item.element.shadowRoot?.querySelectorAll<HTMLElement>(
        "[data-unmodified-lines]",
      ) ?? []) {
        const count = label.textContent?.match(/\d+/)?.[0];
        if (count) label.textContent = `${count} 行未修改`;
      }
    }
  }
  function expandTarget(event: Event) {
    return event
      .composedPath()
      .find(
        (node): node is HTMLElement =>
          node instanceof HTMLElement &&
          (node.hasAttribute("data-expand-button") ||
            node.hasAttribute("data-unmodified-lines")),
      );
  }
  onMount(() => {
    const pointer = matchMedia("(pointer: coarse)");
    const updatePointer = () => {
      coarse = pointer.matches;
    };
    updatePointer();
    pointer.addEventListener("change", updatePointer);
    const updateTheme = () => {
      dark = document.documentElement.classList.contains("dark");
    };
    updateTheme();
    const themeObserver = new MutationObserver(updateTheme);
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    width = host.clientWidth;
    const resize = new ResizeObserver(() => {
      width = host.clientWidth;
    });
    resize.observe(host);
    const highlighter = acquireDiffHighlighter();
    viewer = new CodeView(options(), highlighter.pool);
    viewer.setup(host);
    viewer.setItems([{ id, type: "diff", fileDiff }]);
    mounted = true;
    const guardExpansion = (event: Event) => {
      if (expired && expandTarget(event)) {
        event.preventDefault();
        event.stopImmediatePropagation();
      }
    };
    const keydown = (event: KeyboardEvent) => {
      const button = expandTarget(event);
      if (button && (event.key === "Enter" || event.key === " ")) {
        event.preventDefault();
        if (!expired) {
          const separator = button.closest<HTMLElement>("[data-expand-index]");
          const index = Number(separator?.dataset.expandIndex);
          const item = viewer?.getRenderedItems()[0];
          if (item?.type === "diff" && Number.isInteger(index)) {
            const all =
              button.hasAttribute("data-expand-all-button") ||
              button.hasAttribute("data-expand-both");
            item.instance.expandHunk(
              index,
              all
                ? "both"
                : button.hasAttribute("data-expand-up")
                  ? "up"
                  : "down",
              all ? Number.POSITIVE_INFINITY : 30,
            );
          }
        }
      } else if (
        (event.metaKey || event.ctrlKey) &&
        event.key.toLowerCase() === "f"
      ) {
        event.preventDefault();
        void openPanel("search", false);
      } else if (
        (event.metaKey || event.ctrlKey) &&
        event.altKey &&
        event.key.toLowerCase() === "g"
      ) {
        event.preventDefault();
        void openPanel("goto", false);
      } else if (
        event.key === "F3" ||
        ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "g")
      ) {
        event.preventDefault();
        nextMatch(event.shiftKey ? -1 : 1);
      } else if (event.key === "Escape") {
        panel = undefined;
        host.focus();
      }
    };
    const selectionChanged = () => {
      if (!coarse) return;
      const nativeSelection = document.getSelection();
      if (nativeSelection && !nativeSelection.isCollapsed) selected = null;
    };
    host.addEventListener("pointerdown", guardExpansion, true);
    host.addEventListener("click", guardExpansion, true);
    root.addEventListener("keydown", keydown);
    document.addEventListener("selectionchange", selectionChanged);
    return () => {
      mounted = false;
      resize.disconnect();
      themeObserver.disconnect();
      pointer.removeEventListener("change", updatePointer);
      host.removeEventListener("pointerdown", guardExpansion, true);
      host.removeEventListener("click", guardExpansion, true);
      root.removeEventListener("keydown", keydown);
      document.removeEventListener("selectionchange", selectionChanged);
      viewer?.cleanUp();
      viewer = undefined;
      highlighter.release();
    };
  });
  $effect(() => {
    const current = options();
    if (mounted) {
      untrack(() => {
        const selection = viewer?.getSelectedLines();
        viewer?.setOptions(current);
        viewer?.render();
        if (selection) {
          viewer?.setSelectedLines(selection, { notify: false });
          choose(selection.range);
        }
      });
    }
  });
  $effect(() => {
    void expired;
    if (mounted) enhanceControls();
  });
  $effect(() => {
    void search;
    void side;
    matchIndex = -1;
  });
</script>

<div
  bind:this={root}
  class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden"
>
  <div
    class="flex min-w-0 shrink-0 flex-wrap items-center gap-1 border-b px-2 py-1"
  >
    <span
      class="mr-auto flex gap-2 font-mono text-xs tabular-nums"
      role="img"
      aria-label={`新增 ${totals.additions} 行，删除 ${totals.deletions} 行`}
      ><span class="text-success">+{totals.additions}</span><span
        class="text-destructive">−{totals.deletions}</span
      ></span
    >
    <Button
      variant={panel === "search" ? "secondary" : "ghost"}
      size="icon-sm"
      aria-label="文件内查找"
      aria-expanded={panel === "search"}
      aria-keyshortcuts="Control+f Meta+f"
      title="文件内查找（Ctrl / ⌘ F）"
      disabled={!hasChanges}
      onclick={() => void openPanel("search")}
      ><IconSearch aria-hidden="true" /></Button
    >
    <Button
      variant={overflow === "wrap" ? "secondary" : "ghost"}
      size="icon-sm"
      aria-label="自动换行"
      title="自动换行"
      aria-pressed={overflow === "wrap"}
      onclick={() => {
        overflow = overflow === "wrap" ? "scroll" : "wrap";
        store("codeDiffOverflow", overflow);
      }}><IconTextWrap aria-hidden="true" /></Button
    >
  </div>
  {#if panel === "search"}
    <div class="shrink-0 border-b">
      <CodeSearchPanel
        query={search}
        onquery={(value) => {
          search = value;
        }}
        onnext={() => nextMatch(1)}
        onprevious={() => nextMatch(-1)}
        onclose={() => {
          panel = undefined;
          host.focus();
        }}
      />
      <p class="px-2 pb-1 text-xs text-muted-foreground" role="status">
        {side === "old" ? "修改前" : "修改后"} · {matches.length
          ? `${matchIndex + 1} / ${matches.length}${matches.length === 5000 ? "+" : ""}`
          : "无匹配"}
      </p>
    </div>
  {:else if panel === "goto"}
    <div class="shrink-0 border-b">
      <CodeGotoLine
        line={selected?.start ?? 1}
        onsubmit={goto}
        onclose={() => {
          panel = undefined;
          host.focus();
        }}
      />
    </div>
  {/if}
  {#if diffStyle === "split"}
    <div
      class="grid shrink-0 grid-cols-2 border-b bg-muted/40 font-mono text-[11px] text-muted-foreground"
    >
      <span class="truncate border-r px-3 py-1">修改前</span><span
        class="truncate px-3 py-1">修改后</span
      >
    </div>
  {/if}
  <div class="relative min-h-0 min-w-0 flex-1">
    <div
      bind:this={host}
      class="code-diff-reader h-full overflow-x-hidden overflow-y-auto overscroll-contain outline-none [contain:strict] [overflow-anchor:none]"
      tabindex="-1"
      aria-label="只读代码"
    ></div>
    {#if !hasChanges}
      <p
        class="absolute inset-x-0 top-8 text-center text-sm text-muted-foreground"
        role="status"
      >
        文件内容没有变化；可通过“完整文件”查看内容
      </p>
    {/if}
    {#if coarse && selected && selected.start === selected.end && (!selected.endSide || selected.side === selected.endSide) && lineAction}
      <div class="absolute right-3 bottom-3 z-10">
        {@render lineAction(selected.start)}
      </div>
    {/if}
  </div>
  <div
    class="flex shrink-0 items-center gap-1.5 border-t px-2 py-1 text-[11px] text-muted-foreground"
  >
    <IconArrowsHorizontal class="size-3.5 shrink-0" aria-hidden="true" />
    <span
      >{!hasChanges
        ? "当前可引用整个文件"
        : coarse
          ? `点击代码行，点 ＋ 发送到${draftEnabled ? "草稿" : "终端"}`
          : "点击行号选择；Shift 点击选择范围"}</span
    >
  </div>
</div>

<style>
  .code-diff-reader {
    --diffs-font-family: var(--font-mono, monospace);
    --diffs-font-size: 13px;
    --diffs-line-height: 22px;
  }
</style>
