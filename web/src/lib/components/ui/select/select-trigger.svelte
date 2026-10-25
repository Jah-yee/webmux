<script lang="ts">
  import { Select as SelectPrimitive } from "bits-ui";
  import IconSelector from "~icons/tabler/selector";
  import { cn, type WithoutChild } from "$lib/utils.js";

  let {
    ref = $bindable(null),
    class: className,
    children,
    ...restProps
  }: WithoutChild<SelectPrimitive.TriggerProps> = $props();
</script>

<SelectPrimitive.Trigger
  bind:ref
  data-slot="select-trigger"
  role="combobox"
  class={cn(
    "border-input dark:bg-input/30 dark:hover:bg-input/50 focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 flex min-h-(--control-default) w-full min-w-0 items-center justify-between gap-2 rounded-md border bg-card px-3 py-2 text-left text-base transition-colors outline-none select-none focus-visible:ring-2 aria-invalid:ring-1 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
    className,
  )}
  {...restProps}
>
  {@render children?.()}
  <IconSelector
    class="size-4 shrink-0 text-muted-foreground"
    aria-hidden="true"
  />
</SelectPrimitive.Trigger>
