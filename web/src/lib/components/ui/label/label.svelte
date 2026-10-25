<script lang="ts" module>
  import { type VariantProps, tv } from "tailwind-variants";

  export const labelVariants = tv({
    base: "gap-2 font-medium group-data-[disabled=true]:opacity-50 peer-disabled:opacity-50 flex items-center select-none group-data-[disabled=true]:pointer-events-none peer-disabled:cursor-not-allowed",
    variants: {
      size: {
        default: "text-sm leading-snug",
        // Dense panels such as the code navigator.
        sm: "text-xs leading-snug",
      },
    },
    defaultVariants: {
      size: "default",
    },
  });

  export type LabelSize = VariantProps<typeof labelVariants>["size"];
</script>

<script lang="ts">
  import { Label as LabelPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  let {
    ref = $bindable(null),
    class: className,
    size = "default",
    ...restProps
  }: LabelPrimitive.RootProps & { size?: LabelSize } = $props();
</script>

<LabelPrimitive.Root
  bind:ref
  data-slot="label"
  class={cn(labelVariants({ size }), className)}
  {...restProps}
/>
