<script lang="ts">
  import { NavigationMenu as NavigationMenuPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";
  import { tv } from "tailwind-variants";

  const listVariants = tv({
    base: "group flex flex-1 list-none",
    variants: {
      layout: {
        default: "items-center justify-center gap-0",
        sections: "w-max min-w-full gap-1 lg:w-full lg:flex-col",
      },
    },
  });

  let {
    ref = $bindable(null),
    class: className,
    layout = "default",
    ...restProps
  }: NavigationMenuPrimitive.ListProps & {
    layout?: "default" | "sections";
  } = $props();
</script>

<NavigationMenuPrimitive.List
  bind:ref
  data-slot="navigation-menu-list"
  class={cn(listVariants({ layout }), className)}
  {...restProps}
/>
