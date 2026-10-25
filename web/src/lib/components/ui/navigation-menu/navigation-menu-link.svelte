<script lang="ts">
  import { NavigationMenu as NavigationMenuPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";
  import { tv } from "tailwind-variants";

  const linkVariants = tv({
    base: "flex items-center gap-2 outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring/50 [&_svg:not([class*='size-'])]:size-4",
    variants: {
      variant: {
        default:
          "data-active:focus:bg-muted data-active:hover:bg-muted data-active:bg-muted/50 hover:bg-muted focus:bg-muted rounded-none p-2 text-xs",
        section:
          "min-h-(--control-touch) rounded-md px-4 py-2 text-sm font-medium whitespace-nowrap text-muted-foreground hover:bg-muted hover:text-foreground data-active:bg-accent data-active:text-accent-foreground lg:min-h-(--control-default)",
      },
    },
  });

  let {
    ref = $bindable(null),
    class: className,
    variant = "default",
    ...restProps
  }: NavigationMenuPrimitive.LinkProps & {
    variant?: "default" | "section";
  } = $props();
</script>

<NavigationMenuPrimitive.Link
  bind:ref
  data-slot="navigation-menu-link"
  class={cn(linkVariants({ variant }), className)}
  {...restProps}
/>
