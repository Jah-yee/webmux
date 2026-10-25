<script lang="ts" module>
  import { type VariantProps, tv } from "tailwind-variants";

  export const emptyVariants = tv({
    base: "gap-4 rounded-none border-dashed p-6 flex w-full min-w-0 flex-1 flex-col items-center justify-center text-center text-balance",
    variants: {
      variant: {
        default: "",
        // A dashed outline marking an empty list inside a section.
        outline: "border",
        // A standalone empty page area on its own card surface.
        card: "border rounded-xl bg-card",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  });

  export type EmptyVariant = VariantProps<typeof emptyVariants>["variant"];
</script>

<script lang="ts">
  import { cn, type WithElementRef } from "$lib/utils.js";
  import type { HTMLAttributes } from "svelte/elements";

  let {
    ref = $bindable(null),
    class: className,
    variant = "default",
    children,
    ...restProps
  }: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
    variant?: EmptyVariant;
  } = $props();
</script>

<div
  bind:this={ref}
  data-slot="empty"
  class={cn(emptyVariants({ variant }), className)}
  {...restProps}
>
  {@render children?.()}
</div>
