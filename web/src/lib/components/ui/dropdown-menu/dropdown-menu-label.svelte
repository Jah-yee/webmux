<script lang="ts" module>
  import { type VariantProps, tv } from "tailwind-variants";

  export const dropdownMenuLabelVariants = tv({
    base: "px-2 py-2 text-xs data-inset:pl-7",
    variants: {
      variant: {
        default: "text-muted-foreground",
        // Names the whole menu rather than a group inside it.
        title: "text-foreground font-medium",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  });

  export type DropdownMenuLabelVariant = VariantProps<
    typeof dropdownMenuLabelVariants
  >["variant"];
</script>

<script lang="ts">
  import { cn, type WithElementRef } from "$lib/utils.js";
  import type { HTMLAttributes } from "svelte/elements";

  let {
    ref = $bindable(null),
    class: className,
    inset,
    variant = "default",
    children,
    ...restProps
  }: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
    inset?: boolean;
    variant?: DropdownMenuLabelVariant;
  } = $props();
</script>

<div
  bind:this={ref}
  data-slot="dropdown-menu-label"
  data-inset={inset}
  class={cn(dropdownMenuLabelVariants({ variant }), className)}
  {...restProps}
>
  {@render children?.()}
</div>
