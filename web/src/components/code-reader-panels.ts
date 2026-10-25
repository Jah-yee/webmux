import { mount, unmount } from "svelte";
import {
  EditorSelection,
  StateEffect,
  StateField,
  type Extension,
} from "@codemirror/state";
import {
  EditorView,
  keymap,
  runScopeHandlers,
  showPanel,
  type Command,
  type Panel,
} from "@codemirror/view";
import {
  SearchQuery,
  closeSearchPanel,
  findNext,
  findPrevious,
  getSearchQuery,
  gotoLine as defaultGotoLine,
  search,
  searchKeymap,
  selectMatches,
  setSearchQuery,
} from "@codemirror/search";
import type { SearchSpec } from "../code-search";
import CodeSearchPanel from "./CodeSearchPanel.svelte";
import CodeGotoLine from "./CodeGotoLine.svelte";

function spec(query: SearchQuery): SearchSpec {
  return {
    search: query.search,
    caseSensitive: query.caseSensitive,
    regexp: query.regexp,
    wholeWord: query.wholeWord,
  };
}

function focusInput(dom: HTMLElement) {
  const input = dom.querySelector("input");
  input?.focus();
  input?.select();
}

function searchPanel(view: EditorView): Panel {
  const dom = document.createElement("div");
  let query = getSearchQuery(view.state);
  const panel = mount(CodeSearchPanel, {
    target: dom,
    props: {
      query: spec(query),
      onquery: (next) => {
        const value = new SearchQuery(next);
        if (value.eq(query)) return;
        query = value;
        view.dispatch({ effects: setSearchQuery.of(value) });
      },
      onnext: () => findNext(view),
      onprevious: () => findPrevious(view),
      onall: () => selectMatches(view),
      onclose: () => closeSearchPanel(view),
    },
  });
  // Escape, Mod-g and F3 are registered for the "search-panel" scope.
  dom.addEventListener("keydown", (event) => {
    if (runScopeHandlers(view, event, "search-panel")) event.preventDefault();
  });
  return {
    dom,
    mount: () => focusInput(dom),
    update(update) {
      for (const transaction of update.transactions)
        for (const effect of transaction.effects)
          if (effect.is(setSearchQuery) && !effect.value.eq(query)) {
            query = effect.value;
            panel.setQuery(spec(query));
          }
    },
    destroy: () => void unmount(panel),
  };
}

/** Resolves CodeMirror's go-to-line syntax: N, +N, -N, N%, and N:column. */
function gotoLineSelection(
  view: EditorView,
  value: string,
): EditorSelection | undefined {
  const match = /^([+-])?(\d+)?(:\d+)?(%)?$/.exec(value.trim());
  if (!match) return;
  const { state } = view;
  const current = state.doc.lineAt(state.selection.main.head);
  const [, sign, digits, column, percent] = match;
  let line = digits ? Number(digits) : current.number;
  if (digits && percent) {
    let fraction = line / 100;
    if (sign)
      fraction =
        fraction * (sign === "-" ? -1 : 1) + current.number / state.doc.lines;
    line = Math.round(state.doc.lines * fraction);
  } else if (digits && sign) {
    line = line * (sign === "-" ? -1 : 1) + current.number;
  }
  const target = state.doc.line(Math.max(1, Math.min(state.doc.lines, line)));
  const offset = column ? Number(column.slice(1)) : 0;
  return EditorSelection.single(
    target.from + Math.max(0, Math.min(offset, target.length)),
  );
}

const toggleGoto = StateEffect.define<boolean>();
const gotoOpen = StateField.define<boolean>({
  create: () => false,
  update(open, transaction) {
    for (const effect of transaction.effects)
      if (effect.is(toggleGoto)) open = effect.value;
    return open;
  },
  provide: (field) =>
    showPanel.from(field, (open) => (open ? gotoPanel : null)),
});

function gotoPanel(view: EditorView): Panel {
  const dom = document.createElement("div");
  const close = () => {
    view.dispatch({ effects: toggleGoto.of(false) });
    view.focus();
  };
  const panel = mount(CodeGotoLine, {
    target: dom,
    props: {
      line: view.state.doc.lineAt(view.state.selection.main.head).number,
      onsubmit: (value) => {
        const selection = gotoLineSelection(view, value);
        if (!selection) return close();
        view.dispatch({
          selection,
          effects: [
            toggleGoto.of(false),
            EditorView.scrollIntoView(selection.main.from, { y: "center" }),
          ],
        });
        view.focus();
      },
      onclose: close,
    },
  });
  return {
    dom,
    mount: () => focusInput(dom),
    destroy: () => void unmount(panel),
  };
}

export const openGotoLine: Command = (view) => {
  if (view.state.field(gotoOpen, false))
    focusInput(view.dom.querySelector<HTMLElement>("[data-code-goto-line]")!);
  else view.dispatch({ effects: toggleGoto.of(true) });
  return true;
};

/** Search and go-to-line panels rendered with the app's shadcn components. */
export function codeReaderPanels(): Extension {
  return [
    search({ createPanel: searchPanel }),
    gotoOpen,
    keymap.of([
      ...searchKeymap.filter((binding) => binding.run !== defaultGotoLine),
      { key: "Mod-Alt-g", run: openGotoLine },
    ]),
  ];
}
