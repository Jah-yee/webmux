<script lang="ts">
  import { Select as SelectPrimitive } from "bits-ui";
  import { cn, type WithoutChild } from "$lib/utils.js";

  let {
    ref = $bindable(null),
    class: className,
    sideOffset = 4,
    align = "start",
    children,
    ...restProps
  }: WithoutChild<SelectPrimitive.ContentProps> = $props();
</script>

<SelectPrimitive.Portal>
  <SelectPrimitive.Content
    bind:ref
    {sideOffset}
    {align}
    preventScroll
    data-slot="select-content"
    class={cn(
      "relative z-50 max-h-(--bits-select-content-available-height) w-(--bits-select-anchor-width) max-w-[calc(100vw-2rem)] origin-(--bits-select-content-transform-origin) overflow-x-hidden overflow-y-auto rounded-lg bg-popover p-1 text-popover-foreground shadow-md ring-1 ring-foreground/10",
      className,
    )}
    {...restProps}
  >
    <SelectPrimitive.Viewport class="w-full min-w-0">
      {@render children?.()}
    </SelectPrimitive.Viewport>
  </SelectPrimitive.Content>
</SelectPrimitive.Portal>
