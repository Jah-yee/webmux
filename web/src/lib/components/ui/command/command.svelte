<script lang="ts" module>
  import { type VariantProps, tv } from "tailwind-variants";

  export const commandVariants = tv({
    base: "bg-popover text-popover-foreground rounded-none flex size-full flex-col overflow-hidden",
    variants: {
      variant: {
        default: "",
        // Embedded in a form or dialog rather than in its own popover.
        outline: "border",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  });

  export type CommandVariant = VariantProps<typeof commandVariants>["variant"];
</script>

<script lang="ts">
  import { Command as CommandPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  export type CommandRootApi = CommandPrimitive.Root;

  let {
    api = $bindable(null),
    ref = $bindable(null),
    value = $bindable(""),
    class: className,
    variant = "default",
    ...restProps
  }: CommandPrimitive.RootProps & {
    api?: CommandRootApi | null;
    variant?: CommandVariant;
  } = $props();
</script>

<CommandPrimitive.Root
  bind:this={api}
  bind:value
  bind:ref
  data-slot="command"
  class={cn(commandVariants({ variant }), className)}
  {...restProps}
/>
