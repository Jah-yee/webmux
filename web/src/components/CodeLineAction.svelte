<script lang="ts">
  import { Button } from "$lib/components/ui/button";
  import IconPlus from "~icons/tabler/plus";

  // The per-line "send this location" button shown beside code rows.
  let {
    line,
    draftEnabled,
    disabled,
    tooltip = false,
    onclick,
  }: {
    line: number;
    draftEnabled: boolean;
    disabled: boolean;
    /** Also expose the label as a native hover tooltip. */
    tooltip?: boolean;
    onclick: () => void;
  } = $props();
  const label = $derived(
    `发送第 ${line} 行位置到${draftEnabled ? "草稿" : "终端"}`,
  );
</script>

<Button
  size="icon"
  raised
  aria-label={label}
  title={tooltip ? label : undefined}
  {disabled}
  {onclick}><IconPlus class="size-5" aria-hidden="true" /></Button
>
