<script lang="ts">
  import { Label } from "$lib/components/ui/label";
  import { Slider } from "$lib/components/ui/slider";
  import * as Dialog from "$lib/components/ui/dialog";

  let {
    open = $bindable(),
    fontSize,
    setFontSize,
    onCloseAutoFocus,
  }: {
    open: boolean;
    fontSize: number;
    setFontSize: (value: number) => void;
    onCloseAutoFocus: (event: Event) => void;
  } = $props();
</script>

<Dialog.Root bind:open>
  <Dialog.Content {onCloseAutoFocus}>
    <Dialog.Header
      ><Dialog.Title>字体大小</Dialog.Title><Dialog.Description
        >设置保存在此设备上，也可以用双指捏合调整。</Dialog.Description
      ></Dialog.Header
    >
    <Label for="terminal-font">{fontSize}px</Label>
    <Slider
      id="terminal-font"
      type="single"
      min={12}
      max={20}
      step={1}
      value={fontSize}
      onValueChange={setFontSize}
      aria-label="字体大小"
    />
  </Dialog.Content>
</Dialog.Root>
