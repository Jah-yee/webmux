<script lang="ts">
  import { tick, untrack } from "svelte";
  import {
    useTerminal,
    type TerminalController,
  } from "../../src/hooks/terminal.svelte";
  import type { TerminalUserInput } from "../../src/hooks/terminal-input";
  import TerminalFindBar from "../../src/components/TerminalFindBar.svelte";
  import { terminalFindShortcut } from "../../src/hooks/terminal-find-shortcut";
  import { hasTouchInput } from "../../src/touchInput";

  type Props = {
    // Draft-mode routing, as Terminal.svelte passes it to the hook.
    userInput?: TerminalUserInput;
    // Receives explicit clipboard paste requests; without it they fail.
    onPaste?: () => void;
    // Exposes the hook's public API to tests that drive it directly.
    expose?: (terminal: TerminalController) => void;
  };
  const props: Props = $props();
  const { userInput, onPaste, expose } = untrack(() => ({ ...props }));

  let container: HTMLDivElement;
  let copied = $state("");
  let copies = $state(0);
  let findOpen = $state(false);
  let findPanel = $state<TerminalFindBar>();
  let returnFocus: HTMLElement | null = null;
  function openFind() {
    if (!findOpen) returnFocus = document.activeElement as HTMLElement | null;
    findOpen = true;
    void tick().then(() => findPanel?.focus());
  }
  function closeFind() {
    terminal.clearFind();
    findOpen = false;
    if (document.activeElement instanceof HTMLElement)
      document.activeElement.blur();
    if (!hasTouchInput()) void tick().then(() => returnFocus?.focus());
  }
  const terminal = useTerminal(
    () => container,
    "terminal-render-fixture",
    {
      paste: () => {
        if (!onPaste)
          throw new Error(
            "Unexpected clipboard paste in terminal rendering fixture",
          );
        onPaste();
      },
      copySelection: (text) => {
        copied = text;
        copies++;
      },
    },
    () => true,
    userInput,
  );
  expose?.(terminal);
</script>

<div use:terminalFindShortcut={openFind}>
  <div
    bind:this={container}
    class="terminal-host"
    data-testid="terminal-host"
    style="position: fixed; left: 0; top: 0; width: 400px; height: 750px"
  ></div>
  <div style="position: fixed; left: 0; top: 0; width: 400px">
    <TerminalFindBar
      bind:this={findPanel}
      open={findOpen}
      {terminal}
      onclose={closeFind}
    />
  </div>
  {#if userInput}<textarea aria-label="Fixture draft"></textarea>{/if}
</div>
<textarea
  aria-label="Fixture code pane"
  style="position: fixed; left: 450px; top: 0"></textarea>
<div style="position: fixed; left: 0; top: 760px">
  <output aria-label="Terminal connection">{terminal.connection}</output>
  <output aria-label="Terminal restoration"
    >{terminal.restoring ? "restoring" : "ready"}</output
  >
  <output aria-label="Terminal error">{terminal.error}</output>
  <button onclick={() => terminal.focus()}>Focus terminal input</button>
  <button onclick={() => terminal.resync()}>Resync terminal</button>
  <output aria-label="Terminal selection">{terminal.selection}</output>
  <output aria-label="Copied terminal text">{copied}</output>
  <output aria-label="Copy requests">{copies}</output>
  <button onclick={() => terminal.copySelection()}>Copy selection</button>
  <button onclick={() => terminal.clearSelection()}>Clear selection</button>
  <output aria-label="Terminal position"
    >{terminal.atBottom ? "latest" : "history"}</output
  >
  <button onclick={() => terminal.scrollToBottom()}>Scroll to latest</button>
  <button onclick={openFind}>Find terminal output</button>
</div>
