<script lang="ts">
  import { untrack } from "svelte";
  import {
    useFullscreen,
    type Fullscreen,
  } from "../../src/hooks/fullscreen.svelte";

  let {
    errors,
    expose,
  }: {
    errors: string[];
    expose: (fullscreen: Fullscreen) => void;
  } = $props();
  const fullscreen = useFullscreen((message) => errors.push(message));
  untrack(() => expose(fullscreen));
</script>

<output aria-label="Fullscreen state"
  >{fullscreen.active ? "fullscreen" : "windowed"}
  {fullscreen.busy ? "busy" : "idle"}</output
>
