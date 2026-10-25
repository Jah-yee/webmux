<script lang="ts">
  import { NavigationMenu as NavigationMenuPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";
  import NavigationMenuViewport from "./navigation-menu-viewport.svelte";
  import { tv } from "tailwind-variants";

  const rootVariants = tv({
    base: "group/navigation-menu relative flex flex-1 items-center justify-center",
    variants: {
      layout: {
        default: "max-w-max",
        sections: "w-full min-w-0 justify-start [&>div]:w-full",
      },
    },
  });

  let {
    ref = $bindable(null),
    class: className,
    viewport = true,
    layout = "default",
    children,
    ...restProps
  }: NavigationMenuPrimitive.RootProps & {
    viewport?: boolean;
    layout?: "default" | "sections";
  } = $props();
</script>

<NavigationMenuPrimitive.Root
  bind:ref
  data-slot="navigation-menu"
  data-viewport={viewport}
  class={cn(rootVariants({ layout }), className)}
  {...restProps}
>
  {@render children?.()}
  {#if viewport}
    <NavigationMenuViewport />
  {/if}
</NavigationMenuPrimitive.Root>
