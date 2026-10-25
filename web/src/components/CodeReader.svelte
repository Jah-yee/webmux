<script lang="ts">
  import { onMount, type Snippet } from "svelte";
  import {
    Compartment,
    EditorState,
    StateEffect,
    StateField,
    Text,
    type Extension,
  } from "@codemirror/state";
  import {
    EditorView,
    lineNumbers,
    highlightActiveLine,
    highlightSpecialChars,
    Decoration,
  } from "@codemirror/view";
  import { openSearchPanel } from "@codemirror/search";
  import { codeReaderPanels, openGotoLine } from "./code-reader-panels";
  import { syntaxHighlighting } from "@codemirror/language";
  import { codeHighlightStyle } from "../code-highlight";
  import { Button } from "$lib/components/ui/button";
  import IconListNumbers from "~icons/tabler/list-numbers";
  import IconNumber from "~icons/tabler/number";
  import IconSearch from "~icons/tabler/search";
  import { store, stored } from "../api";
  let {
    text,
    path,
    onselect,
    lineAction,
    restoreLine = 1,
    draftEnabled = false,
    hint = $bindable(""),
  }: {
    text: string;
    path: string;
    onselect: (start: number, end: number) => void;
    lineAction?: Snippet<[row: number]>;
    restoreLine?: number;
    draftEnabled?: boolean;
    // Selection guidance, rendered by the document's action bar.
    hint?: string;
  } = $props();
  let host: HTMLDivElement;
  let view: EditorView | undefined;
  let anchor: number | undefined;
  let coarse = $state(false);
  let actionLine = $state<number>();
  let actionTop = $state<number>();
  const pickLine = StateEffect.define<number | undefined>();
  const gutterSlot = new Compartment();
  let lineGutter: Extension = [];
  let showGutter = $state(stored("codeLineNumbers", "show") !== "hide");
  function toggleGutter() {
    showGutter = !showGutter;
    store("codeLineNumbers", showGutter ? "show" : "hide");
    view?.dispatch({
      effects: gutterSlot.reconfigure(showGutter ? lineGutter : []),
    });
  }
  export function clearLineAction() {
    actionLine = undefined;
    actionTop = undefined;
    view?.dispatch({ effects: pickLine.of(undefined) });
  }
  function positionLineAction() {
    if (!view || actionLine === undefined || !coarse) return;
    view.requestMeasure({
      key: positionLineAction,
      read(editor) {
        if (actionLine === undefined || !coarse) return;
        const block = editor.lineBlockAt(
          editor.state.doc.line(actionLine).from,
        );
        const height = host.clientHeight;
        const center =
          editor.documentTop +
          block.top +
          block.height / 2 -
          host.getBoundingClientRect().top;
        if (center < 0 || center > height) return;
        return Math.max(22, Math.min(height - 22, center));
      },
      write(top) {
        actionTop = top;
      },
    });
  }
  export function line() {
    if (!view) return 1;
    const top = view.scrollDOM.getBoundingClientRect().top - view.documentTop;
    return view.state.doc.lineAt(view.lineBlockAtHeight(top).from).number;
  }
  function select(number: number, shift: boolean) {
    if (coarse) {
      anchor = undefined;
      actionTop = undefined;
      actionLine = number;
      if (view) {
        view.dispatch({
          selection: { anchor: view.state.doc.line(number).from },
          effects: pickLine.of(number),
        });
      }
      onselect(number, number);
      positionLineAction();
      return;
    }
    if (shift && anchor !== undefined) {
      onselect(Math.min(anchor, number), Math.max(anchor, number));
      anchor = undefined;
    } else {
      anchor = number;
      onselect(number, number);
    }
  }
  onMount(() => {
    let cancelled = false;
    const pointer = matchMedia("(pointer: coarse)");
    const updatePointer = () => {
      coarse = pointer.matches;
      if (!coarse) clearLineAction();
    };
    updatePointer();
    pointer.addEventListener("change", updatePointer);
    const selectionChanged = () => {
      const selection = document.getSelection();
      if (
        selection &&
        !selection.isCollapsed &&
        host.contains(selection.anchorNode)
      ) {
        // A transaction here would reset the browser's native selection.
        actionLine = undefined;
        actionTop = undefined;
      }
    };
    document.addEventListener("selectionchange", selectionChanged);
    lineGutter = lineNumbers({
      domEventHandlers: {
        click: (v, l, e) => {
          select(v.state.doc.lineAt(l.from).number, (e as MouseEvent).shiftKey);
          return true;
        },
      },
    });

    const extensions = [
      EditorState.readOnly.of(true),
      EditorView.editable.of(false),
      codeReaderPanels(),
      syntaxHighlighting(codeHighlightStyle),
      highlightActiveLine(),
      highlightSpecialChars(),
      EditorView.theme({
        "&": { height: "100%", fontSize: "13px" },
        ".cm-scroller": {
          overflow: "auto",
          fontFamily: "var(--font-mono, monospace)",
        },
        ".cm-content": { minWidth: "100%" },
        ".cm-gutters": {
          cursor: "pointer",
          backgroundColor: "var(--muted)",
          color: "var(--muted-foreground)",
          borderRight: "1px solid var(--border)",
        },
        ".cm-activeLineGutter": {
          backgroundColor: "var(--accent)",
          color: "var(--foreground)",
        },
        ".cm-panels": {
          backgroundColor: "var(--background)",
          color: "var(--foreground)",
        },
        ".cm-panels.cm-panels-bottom": { borderTop: "1px solid var(--border)" },
        ".cm-panels.cm-panels-top": { borderBottom: "1px solid var(--border)" },
        ".code-line-picked": {
          boxShadow: "inset 3px 0 var(--primary)",
          outline: "1px solid var(--primary)",
          outlineOffset: "-1px",
        },
      }),
      EditorView.domEventHandlers({
        click(event, editor) {
          if (!coarse || !lineAction) return false;
          const target = event.target;
          if (!(target instanceof Element) || !target.closest(".cm-line"))
            return false;
          if (document.getSelection()?.isCollapsed === false) return false;
          const pos = editor.posAtCoords({
            x: event.clientX,
            y: event.clientY,
          });
          if (pos === null) return false;
          const row = editor.state.doc.lineAt(pos).number;
          select(row, false);
          return false;
        },
        scroll() {
          positionLineAction();
          return false;
        },
      }),
      gutterSlot.of(showGutter ? lineGutter : []),
      StateField.define({
        create: () => Decoration.none,
        update(value, transaction) {
          for (const effect of transaction.effects) {
            if (effect.is(pickLine)) {
              return effect.value === undefined
                ? Decoration.none
                : Decoration.set([
                    Decoration.line({ class: "code-line-picked" }).range(
                      transaction.state.doc.line(effect.value).from,
                    ),
                  ]);
            }
          }
          return value;
        },
        provide: (field) => EditorView.decorations.from(field),
      }),
      EditorView.updateListener.of((u) => {
        if (u.geometryChanged || u.viewportChanged) positionLineAction();
        if (u.selectionSet) {
          const range = u.state.selection.main;
          if (!range.empty)
            onselect(
              u.state.doc.lineAt(range.from).number,
              u.state.doc.lineAt(Math.max(range.from, range.to - 1)).number,
            );
        }
      }),
    ];
    view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: Text.of(text.split(/\r?\n/)),
        extensions,
      }),
    });
    const target = Math.min(Math.max(restoreLine, 1), view.state.doc.lines);
    view.dispatch({
      effects: EditorView.scrollIntoView(view.state.doc.line(target).from, {
        y: "start",
        yMargin: 0,
      }),
    });
    void (async () => {
      const extension = path.split(".").at(-1)?.toLowerCase();
      let language;
      if (["js", "jsx", "ts", "tsx", "mjs", "svelte"].includes(extension ?? ""))
        language = (await import("@codemirror/lang-javascript")).javascript({
          typescript: extension?.startsWith("ts"),
          jsx: extension?.endsWith("x"),
        });
      else if (extension === "py")
        language = (await import("@codemirror/lang-python")).python();
      else if (extension === "json")
        language = (await import("@codemirror/lang-json")).json();
      else if (extension === "go")
        language = (await import("@codemirror/lang-go")).go();
      else if (extension === "rs")
        language = (await import("@codemirror/lang-rust")).rust();
      else if (extension === "html")
        language = (await import("@codemirror/lang-html")).html();
      else if (extension === "css")
        language = (await import("@codemirror/lang-css")).css();
      if (!cancelled && view && language) {
        if (!cancelled)
          view.dispatch({ effects: StateEffect.appendConfig.of(language) });
      }
    })().catch(() => {
      /* Plain text remains readable if language loading fails. */
    });
    return () => {
      cancelled = true;
      pointer.removeEventListener("change", updatePointer);
      document.removeEventListener("selectionchange", selectionChanged);
      view?.destroy();
      view = undefined;
    };
  });
  $effect(() => {
    hint = coarse
      ? `点击代码行，再点右侧 ＋ 发送位置到${draftEnabled ? "草稿" : "终端"}`
      : showGutter
        ? "点击行号；Shift 点击选择终点"
        : "显示行号后可点击行号选择";
    return () => {
      hint = "";
    };
  });
</script>

<div class="flex min-h-0 flex-1 flex-col overflow-hidden">
  <div class="flex shrink-0 items-center gap-1 border-b px-1 py-0.5">
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label="文件内查找"
      title="文件内查找"
      onclick={() => view && openSearchPanel(view)}
      ><IconSearch aria-hidden="true" /></Button
    >
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label="跳转行"
      title="跳转行"
      onclick={() => view && openGotoLine(view)}
      ><IconListNumbers aria-hidden="true" /></Button
    >
    <Button
      variant={showGutter ? "secondary" : "ghost"}
      size="icon-sm"
      aria-label="显示行号"
      title={showGutter ? "隐藏行号" : "显示行号"}
      aria-pressed={showGutter}
      onclick={toggleGutter}><IconNumber aria-hidden="true" /></Button
    >
  </div>
  <div class="relative min-h-0 flex-1 overflow-hidden">
    <div
      bind:this={host}
      data-code-reader
      class="h-full overflow-hidden"
      aria-label="只读代码"
    ></div>
    {#if coarse && actionLine !== undefined && actionTop !== undefined && lineAction}
      <div
        class="absolute top-(--action-top) right-1 z-10 -translate-y-1/2"
        style:--action-top={`${actionTop}px`}
      >
        {@render lineAction(actionLine)}
      </div>
    {/if}
  </div>
</div>
