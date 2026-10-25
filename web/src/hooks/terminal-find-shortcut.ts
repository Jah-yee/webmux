import { isTerminalFindShortcut } from "../terminal-search";

// Attach only to the terminal column, including its draft but not code panes.
export function terminalFindShortcut(node: HTMLElement, open: () => void) {
  const keydown = (event: KeyboardEvent) => {
    if (!isTerminalFindShortcut(event, navigator.platform)) return;
    event.preventDefault();
    event.stopPropagation();
    open();
  };
  node.addEventListener("keydown", keydown, true);
  return { destroy: () => node.removeEventListener("keydown", keydown, true) };
}
