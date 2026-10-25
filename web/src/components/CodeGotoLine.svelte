<script lang="ts">
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import IconX from "~icons/tabler/x";

  let {
    line,
    onsubmit,
    onclose,
  }: {
    line: number;
    onsubmit: (value: string) => void;
    onclose: () => void;
  } = $props();
  // svelte-ignore state_referenced_locally
  let value = $state(String(line));
</script>

<form
  data-code-goto-line
  class="flex items-center gap-1 p-1"
  onsubmit={(e) => {
    e.preventDefault();
    onsubmit(value);
  }}
>
  <Input
    bind:value
    aria-label="跳转到行"
    placeholder="行号，可用 +N、-N、N%、行:列"
    inputmode="numeric"
    class="h-7 flex-1"
    onkeydown={(e) => {
      if (e.key !== "Escape") return;
      e.preventDefault();
      onclose();
    }}
  />
  <Button type="submit" size="sm">跳转</Button>
  <Button
    type="button"
    variant="ghost"
    size="icon-sm"
    aria-label="关闭跳转行"
    title="关闭跳转行（Esc）"
    onclick={onclose}><IconX aria-hidden="true" /></Button
  >
</form>
