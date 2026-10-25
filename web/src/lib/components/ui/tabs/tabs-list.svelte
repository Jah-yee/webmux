<script lang="ts" module>
  import { tv, type VariantProps } from "tailwind-variants";

  export const tabsListVariants = tv({
    base: "rounded-lg p-[3px] data-[variant=line]:rounded-none group/tabs-list inline-flex w-fit items-center justify-center text-muted-foreground group-data-vertical/tabs:h-fit group-data-vertical/tabs:flex-col",
    variants: {
      variant: {
        default: "bg-muted",
        line: "gap-1 bg-transparent",
      },
      size: {
        default: "group-data-horizontal/tabs:h-8",
        form: "group-data-horizontal/tabs:h-[calc(var(--control-default)+6px)]",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  });

  export type TabsListVariant = VariantProps<
    typeof tabsListVariants
  >["variant"];
</script>

<script lang="ts">
  import { Tabs as TabsPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  let {
    ref = $bindable(null),
    variant = "default",
    size = "default",
    class: className,
    ...restProps
  }: TabsPrimitive.ListProps & {
    variant?: TabsListVariant;
    size?: VariantProps<typeof tabsListVariants>["size"];
  } = $props();
</script>

<TabsPrimitive.List
  bind:ref
  data-slot="tabs-list"
  data-variant={variant}
  data-size={size}
  class={cn(tabsListVariants({ variant, size }), className)}
  {...restProps}
/>
