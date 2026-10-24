<script lang="ts">
  import { untrack } from "svelte";
  import {
    useTerminal,
    type TerminalController,
  } from "../../src/hooks/terminal.svelte";
  import type { TerminalUserInput } from "../../src/hooks/terminal-input";

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

<div
  bind:this={container}
  class="terminal-host"
  data-testid="terminal-host"
  style="position: fixed; left: 0; top: 0; width: 400px; height: 750px"
></div>
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
</div>
