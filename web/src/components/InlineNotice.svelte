<script lang="ts">
  import type { Snippet } from "svelte";
  import IconAlertTriangle from "~icons/tabler/alert-triangle";
  import IconCircleCheck from "~icons/tabler/circle-check";
  import IconInfoCircle from "~icons/tabler/info-circle";
  import { cn } from "$lib/utils";

  let {
    tone = "info",
    children,
    class: className,
  }: {
    tone?: "info" | "success" | "warning" | "danger";
    children: Snippet;
    class?: string;
  } = $props();
  const colors = {
    info: "border-info/20 bg-info/5 text-info",
    success: "border-success/20 bg-success/5 text-success",
    warning: "border-warning/20 bg-warning/5 text-warning",
    danger: "border-destructive/20 bg-destructive/5 text-destructive",
  };
</script>

<div
  data-slot="inline-notice"
  data-tone={tone}
  role={tone === "danger" ? "alert" : "status"}
  class={cn(
    "flex min-w-0 items-start gap-2 rounded-lg border p-3 text-sm leading-relaxed",
    colors[tone],
    className,
  )}
>
  {#if tone === "danger" || tone === "warning"}
    <IconAlertTriangle class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
  {:else if tone === "success"}
    <IconCircleCheck class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
  {:else}
    <IconInfoCircle class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
  {/if}
  <div class="min-w-0 flex-1 wrap-anywhere">
    {@render children()}
  </div>
</div>
