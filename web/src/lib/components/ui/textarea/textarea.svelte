<script lang="ts" module>
  import { type VariantProps, tv } from "tailwind-variants";

  export const textareaVariants = tv({
    base: "border-input dark:bg-input/30 focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive dark:aria-invalid:border-destructive/50 disabled:bg-input/50 dark:disabled:bg-input/80 rounded-md border bg-card px-3 py-2 text-base transition-colors focus-visible:ring-2 aria-invalid:ring-1 md:text-sm flex field-sizing-content min-h-16 w-full outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50",
    variants: {
      variant: {
        default: "",
        // Edits text in place inside a list item or card.
        ghost:
          "border-transparent bg-transparent px-1 py-1 leading-relaxed shadow-none dark:bg-transparent",
      },
      // Commands, paths and terminal text.
      mono: {
        true: "font-mono",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  });

  export type TextareaVariant = VariantProps<
    typeof textareaVariants
  >["variant"];
</script>

<script lang="ts">
  import { cn, type WithElementRef, type WithoutChildren } from "$lib/utils.js";
  import type { HTMLTextareaAttributes } from "svelte/elements";

  let {
    ref = $bindable(null),
    value = $bindable(),
    class: className,
    variant = "default",
    mono = false,
    "data-slot": dataSlot = "textarea",
    ...restProps
  }: WithoutChildren<WithElementRef<HTMLTextareaAttributes>> & {
    variant?: TextareaVariant;
    mono?: boolean;
  } = $props();
</script>

<textarea
  bind:this={ref}
  data-slot={dataSlot}
  class={cn(textareaVariants({ variant, mono }), className)}
  bind:value
  {...restProps}></textarea>
