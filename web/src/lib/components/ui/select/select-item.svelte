<script lang="ts">
  import { Select as SelectPrimitive } from "bits-ui";
  import IconCheck from "~icons/tabler/check";
  import { cn, type WithoutChild } from "$lib/utils.js";

  let {
    ref = $bindable(null),
    class: className,
    children: childrenProp,
    disabled = false,
    ...restProps
  }: WithoutChild<SelectPrimitive.ItemProps> = $props();
</script>

<SelectPrimitive.Item
  bind:ref
  data-slot="select-item"
  {disabled}
  aria-disabled={disabled || undefined}
  class={cn(
    "relative flex min-h-(--control-default) w-full min-w-0 cursor-default items-center gap-2 rounded-sm py-2 pr-8 pl-2 text-sm outline-none select-none data-highlighted:bg-accent data-highlighted:text-accent-foreground data-disabled:pointer-events-none data-disabled:opacity-50",
    className,
  )}
  {...restProps}
>
  {#snippet children({ selected, highlighted })}
    {@render childrenProp?.({ selected, highlighted })}
    {#if selected}
      <IconCheck
        class="pointer-events-none absolute right-2 size-4"
        aria-hidden="true"
      />
    {/if}
  {/snippet}
</SelectPrimitive.Item>
