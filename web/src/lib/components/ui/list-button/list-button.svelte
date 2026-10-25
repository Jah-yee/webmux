<script lang="ts" module>
  import { type VariantProps, tv } from "tailwind-variants";

  // Clickable regions inside lists, tab strips and cards. Unlike Button they
  // take no control sizing, so virtualized rows keep their fixed height; use
  // Button for standalone controls.
  export const listButtonVariants = tv({
    base: "",
    variants: {
      variant: {
        // Inherits the surrounding text, e.g. a breadcrumb segment.
        text: "",
        // A file or change in a navigation list.
        row: "gap-1.5 rounded px-2 py-1 text-left text-xs hover:bg-muted",
        // The body of a list item whose actions sit beside it.
        content:
          "gap-1 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50",
        // A selectable card; aria-pressed marks the chosen one.
        option:
          "gap-0.5 rounded-md border px-3 py-2 text-left outline-none hover:bg-accent focus-visible:ring-3 focus-visible:ring-ring/50 aria-pressed:border-primary aria-pressed:bg-accent",
        // A document tab and its close button.
        tab: "px-2 py-1.5 text-xs whitespace-nowrap aria-selected:font-medium aria-selected:text-foreground",
        "tab-close": "px-2",
      },
    },
    defaultVariants: {
      variant: "text",
    },
  });

  export type ListButtonVariant = VariantProps<
    typeof listButtonVariants
  >["variant"];
</script>

<script lang="ts">
  import { cn, type WithElementRef } from "$lib/utils.js";
  import type { HTMLButtonAttributes } from "svelte/elements";

  let {
    ref = $bindable(null),
    class: className,
    variant = "text",
    type = "button",
    children,
    ...restProps
  }: WithElementRef<HTMLButtonAttributes> & {
    variant?: ListButtonVariant;
  } = $props();
</script>

<button
  bind:this={ref}
  data-slot="list-button"
  class={cn(listButtonVariants({ variant }), className)}
  {type}
  {...restProps}
>
  {@render children?.()}
</button>
