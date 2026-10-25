<script lang="ts">
  import { Button } from "$lib/components/ui/button";
  import { Textarea } from "$lib/components/ui/textarea";
  import * as Dialog from "$lib/components/ui/dialog";

  // Manual paste when the clipboard cannot be read directly.
  let {
    open = $bindable(),
    text = $bindable(),
    filePaste,
    send,
    onCloseAutoFocus,
  }: {
    open: boolean;
    text: string;
    filePaste: (event: ClipboardEvent) => boolean;
    send: (text: string) => void;
    onCloseAutoFocus: (event: Event) => void;
  } = $props();
</script>

<Dialog.Root bind:open>
  <Dialog.Content class="sm:max-w-lg" {onCloseAutoFocus}>
    <Dialog.Header
      ><Dialog.Title>粘贴到终端</Dialog.Title><Dialog.Description
        >当前环境无法直接读取剪贴板，请在下方粘贴。多行文本遵循应用的 bracketed
        paste 模式。</Dialog.Description
      ></Dialog.Header
    >
    <Textarea
      aria-label="粘贴内容"
      onpaste={(event) => {
        if (filePaste(event)) open = false;
      }}
      bind:value={text}
      mono
      class="max-h-[50dvh] min-h-40 overflow-y-auto"
      autocapitalize="off"
      autocorrect="off"
      spellcheck={false}
    />
    <Dialog.Footer
      ><Button
        variant="outline"
        onclick={() => {
          open = false;
        }}>取消</Button
      ><Button
        onclick={() => {
          send(text);
          open = false;
        }}>发送</Button
      ></Dialog.Footer
    >
  </Dialog.Content>
</Dialog.Root>
