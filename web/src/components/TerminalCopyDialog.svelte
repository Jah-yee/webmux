<script lang="ts">
  import { Button } from "$lib/components/ui/button";
  import { Textarea } from "$lib/components/ui/textarea";
  import * as Dialog from "$lib/components/ui/dialog";

  // Manual copy when the clipboard cannot be written directly.
  let {
    open = $bindable(),
    text,
    onCloseAutoFocus,
  }: {
    open: boolean;
    text: string;
    onCloseAutoFocus: (event: Event) => void;
  } = $props();
</script>

<Dialog.Root bind:open>
  <Dialog.Content class="sm:max-w-lg" {onCloseAutoFocus}>
    <Dialog.Header
      ><Dialog.Title>复制终端文字</Dialog.Title><Dialog.Description
        >当前环境不支持剪贴板访问，请长按或全选复制。</Dialog.Description
      ></Dialog.Header
    >
    <Textarea
      aria-label="复制内容"
      readonly
      value={text}
      mono
      class="max-h-[50dvh] min-h-40 overflow-y-auto"
    />
    <Dialog.Footer
      ><Button
        onclick={() => {
          open = false;
        }}>关闭</Button
      ></Dialog.Footer
    >
  </Dialog.Content>
</Dialog.Root>
