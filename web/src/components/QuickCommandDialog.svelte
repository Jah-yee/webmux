<script lang="ts">
  import InlineNotice from "./InlineNotice.svelte";
  import { untrack } from "svelte";
  import IconBolt from "~icons/tabler/bolt";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import { Switch } from "$lib/components/ui/switch";
  import { Textarea } from "$lib/components/ui/textarea";
  import * as Dialog from "$lib/components/ui/dialog";
  import { QuickCommandSchema, type QuickCommand } from "../schemas";
  import { newQuickCommandId, normalizeCommand } from "../quickCommands";

  let {
    open = $bindable(false),
    initial,
    save,
  }: {
    open: boolean;
    /** Command being edited; undefined creates a new one. */
    initial: QuickCommand | undefined;
    save: (cmd: QuickCommand) => string;
  } = $props();
  let name = $state("");
  let command = $state("");
  let enter = $state(false);
  let error = $state("");
  let commandInput = $state<HTMLTextAreaElement | null>(null);
  const draft = $derived(
    QuickCommandSchema.safeParse({
      id: initial?.id ?? "pending",
      name: name.trim(),
      command: normalizeCommand(command),
      enter,
    }),
  );

  // Each opening starts from the saved value, so cancel/Esc discards edits.
  $effect.pre(() => {
    if (!open) return;
    untrack(() => {
      name = initial?.name ?? "";
      command = initial?.command ?? "";
      enter = initial?.enter ?? false;
      error = "";
    });
  });

  function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!draft.success) return;
    error = save({ ...draft.data, id: initial?.id ?? newQuickCommandId() });
    if (!error) open = false;
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content
    class="sm:max-w-lg"
    onOpenAutoFocus={(event) => {
      if (initial) return;
      event.preventDefault();
      commandInput?.focus();
    }}
  >
    <form class="grid gap-4" onsubmit={submit}>
      <Dialog.Header>
        <Dialog.Title>{initial ? "编辑快捷指令" : "添加快捷指令"}</Dialog.Title>
        <Dialog.Description
          >在终端点击 <IconBolt
            class="inline size-3.5 align-[-2px]"
            aria-label="闪电"
          /> 按钮即可发送这条指令。</Dialog.Description
        >
      </Dialog.Header>
      <div class="grid gap-2">
        <Label for="quick-command-name">名称（可选）</Label>
        <Input
          id="quick-command-name"
          bind:value={name}
          maxlength={40}
          placeholder="例如：查看状态"
          autocomplete="off"
        />
      </div>
      <div class="grid gap-2">
        <Label for="quick-command-body">指令</Label>
        <Textarea
          id="quick-command-body"
          bind:ref={commandInput}
          bind:value={command}
          rows={4}
          maxlength={4096}
          placeholder="例如：git status"
          mono
          class="max-h-[40dvh]"
          autocapitalize="off"
          autocorrect="off"
          spellcheck={false}
        />
      </div>
      <div class="flex items-start gap-3">
        <Switch
          id="quick-command-enter"
          bind:checked={enter}
          aria-describedby="quick-command-enter-help"
        />
        <div class="grid gap-1">
          <Label for="quick-command-enter">发送后自动回车</Label>
          <p
            id="quick-command-enter-help"
            class="text-xs text-muted-foreground"
          >
            开启后点击即执行；关闭时只填入，由你确认后再回车。
          </p>
        </div>
      </div>
      {#if error}
        <InlineNotice tone="danger">{error}</InlineNotice>
      {/if}
      <Dialog.Footer>
        <Button
          type="button"
          variant="outline"
          onclick={() => {
            open = false;
          }}>取消</Button
        >
        <Button type="submit" disabled={!draft.success}>保存</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
