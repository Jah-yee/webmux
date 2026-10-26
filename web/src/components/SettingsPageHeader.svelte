<script lang="ts">
  import { Button } from "$lib/components/ui/button";

  let {
    href = "/settings",
    label = "返回设置",
  }: { href?: string; label?: string } = $props();
</script>

<header class="app-header border-b bg-card">
  <div
    class="mx-auto flex min-h-(--app-header-height) max-w-6xl items-center justify-between gap-3 px-4 py-1 sm:px-8"
  >
    <a class="brand" href="/"><span class="brand-mark">WM</span> WebMux</a>
    <Button {href} variant="outline">{label}</Button>
  </div>
</header>
