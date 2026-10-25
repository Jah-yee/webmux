<script lang="ts" module>
  import { type VariantProps, tv } from "tailwind-variants";

  // Matches Input: card surface, rounded control and a 2px focus ring.
  export const nativeSelectVariants = tv({
    base: "border-input placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground dark:bg-input/30 dark:hover:bg-input/50 focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive dark:aria-invalid:border-destructive/50 w-full min-w-0 appearance-none rounded-md border bg-card transition-colors select-none focus-visible:ring-2 aria-invalid:ring-1 outline-none disabled:pointer-events-none disabled:cursor-not-allowed",
    variants: {
      size: {
        default: "h-(--control-default) py-1 pr-8 pl-3 text-base md:text-sm",
        sm: "h-(--control-sm) py-0.5 pr-7 pl-2 text-xs",
      },
    },
    defaultVariants: {
      size: "default",
    },
  });

  export type NativeSelectSize = VariantProps<
    typeof nativeSelectVariants
  >["size"];
</script>

<script lang="ts">
  import IconSelector from "~icons/tabler/selector";
  import { cn, type WithElementRef } from "$lib/utils.js";
  import type { HTMLSelectAttributes } from "svelte/elements";

  type NativeSelectProps = Omit<
    WithElementRef<HTMLSelectAttributes, HTMLSelectElement>,
    "size"
  > & {
    size?: NativeSelectSize;
  };

  let {
    ref = $bindable(null),
    value = $bindable(),
    class: className,
    size = "default",
    children,
    ...restProps
  }: NativeSelectProps = $props();
</script>

<div
  class={cn(
    "group/native-select relative w-fit has-[select:disabled]:opacity-50",
    className,
  )}
  data-slot="native-select-wrapper"
  data-size={size}
>
  <select
    bind:value
    bind:this={ref}
    data-slot="native-select"
    data-size={size}
    class={nativeSelectVariants({ size })}
    {...restProps}
  >
    {@render children?.()}
  </select>
  <IconSelector
    class="pointer-events-none absolute top-1/2 right-2.5 size-4 -translate-y-1/2 text-muted-foreground select-none group-data-[size=sm]/native-select:right-2 group-data-[size=sm]/native-select:size-3.5"
    aria-hidden="true"
    data-slot="native-select-icon"
  />
</div>
