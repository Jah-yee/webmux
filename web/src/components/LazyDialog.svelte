<script lang="ts" generics="T">
  import { untrack, type Snippet } from "svelte";
  import { Button } from "$lib/components/ui/button";
  import InlineNotice from "./InlineNotice.svelte";

  let {
    load,
    label,
    onclose,
    children,
  }: {
    load: () => Promise<T>;
    label: string;
    onclose: () => void;
    children: Snippet<[T]>;
  } = $props();
  const pending = untrack(() => load());
</script>

{#await pending}
  <InlineNotice class="fixed inset-x-4 bottom-4 z-50">
    <div class="flex items-center justify-between gap-2">
      <span>正在加载{label}…</span>
      <Button variant="outline" size="sm" onclick={onclose}>取消</Button>
    </div>
  </InlineNotice>
{:then page}
  {@render children(page)}
{:catch}
  <InlineNotice tone="danger" class="fixed inset-x-4 bottom-4 z-50">
    <div class="flex items-center justify-between gap-2">
      <span>{label}加载失败，请刷新页面后重试。</span>
      <Button variant="outline" size="sm" onclick={onclose}>关闭</Button>
    </div>
  </InlineNotice>
{/await}
