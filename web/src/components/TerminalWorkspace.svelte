<script lang="ts">
  import type { Snippet } from "svelte";
  import * as Resizable from "$lib/components/ui/resizable";

  // Resizable split of the terminal, its draft composer below it and the code
  // panel beside it (stacked instead on narrow screens).
  let {
    codeShown,
    desktop,
    draftOpen,
    terminalPane,
    draftPane,
    codePane,
  }: {
    codeShown: boolean;
    desktop: boolean;
    draftOpen: boolean;
    terminalPane: Snippet;
    draftPane: Snippet;
    codePane: Snippet;
  } = $props();
  let composerSize = $state(25);
  let terminalHeight = $state(0);
</script>

<div
  class="terminal-workspace"
  class:code-shown={codeShown}
  class:mobile-code={!desktop && codeShown}
>
  <Resizable.PaneGroup direction="horizontal">
    <Resizable.Pane defaultSize={50} minSize={25} class="terminal-pane">
      <div class="terminal-column" bind:clientHeight={terminalHeight}>
        <Resizable.PaneGroup direction="vertical">
          <Resizable.Pane
            defaultSize={100 - composerSize}
            minSize={20}
            order={0}
          >
            {@render terminalPane()}
          </Resizable.Pane>
          {#if draftOpen}
            <Resizable.Handle
              withHandle
              class="draft-divider"
              aria-label="调整草稿面板高度"
            />
            <Resizable.Pane
              order={1}
              defaultSize={composerSize}
              minSize={Math.min(
                80,
                (160 / Math.max(1, (terminalHeight || 500) - 8)) * 100,
              )}
              onResize={(size) => (composerSize = size)}
              class="draft-pane"
            >
              {@render draftPane()}
            </Resizable.Pane>
          {/if}
        </Resizable.PaneGroup>
      </div>
    </Resizable.Pane>
    <Resizable.Handle
      withHandle
      class="code-divider"
      aria-label="调整终端与代码宽度"
    />
    <Resizable.Pane defaultSize={50} minSize={25} class="code-pane">
      {@render codePane()}
    </Resizable.Pane>
  </Resizable.PaneGroup>
</div>
