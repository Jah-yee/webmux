<script lang="ts">
  import {
    settings,
    changeSettings,
    reloadSettings,
    saveSettings,
  } from "../settings.svelte";
  import { defaultCodeReader } from "../settings-intent";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import { Switch } from "$lib/components/ui/switch";
  import * as Card from "$lib/components/ui/card";
  import InlineNotice from "./InlineNotice.svelte";
  const code = $derived(settings.values.codeReader ?? defaultCodeReader);
  let fontDirty = $state(false);
  let fontSize = $state<number | undefined>(14);
  $effect(() => {
    if (!fontDirty)
      fontSize =
        settings.values.fontSize ??
        (matchMedia("(pointer: coarse)").matches ? 13 : 14);
  });
  const validFont = $derived(
    fontSize !== undefined &&
      Number.isInteger(fontSize) &&
      fontSize >= 8 &&
      fontSize <= 20,
  );
</script>

<Card.Root id="appearance" class="scroll-mt-4">
  <Card.Header>
    <Card.Title>外观与阅读</Card.Title>
    <Card.Description
      >保存到当前服务端，在已连接的设备间即时同步。恢复默认后使用设备的系统主题与默认字号。</Card.Description
    >
  </Card.Header>
  <Card.Content class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <p role="status" class="text-sm text-muted-foreground">
        {settings.saving
          ? "正在保存…"
          : !settings.ready
            ? "设置不可用"
            : settings.connected
              ? "已连接 · 设置已同步"
              : "正在重连 · 显示最后确认的设置"}
      </p>
      <Button variant="outline" size="sm" onclick={() => void reloadSettings()}
        >重新读取</Button
      >
    </div>
    {#if settings.error}<InlineNotice tone="danger"
        >{settings.error}</InlineNotice
      >{/if}
    <fieldset disabled={!settings.ready} class="space-y-6">
      <div class="space-y-2">
        <p class="text-sm font-medium">主题</p>
        <div class="flex flex-wrap gap-2" role="group" aria-label="主题">
          {#each [{ value: null, label: "跟随系统" }, { value: "light", label: "浅色" }, { value: "dark", label: "深色" }] as option (option.value)}
            <Button
              variant={settings.values.theme === option.value
                ? "secondary"
                : "outline"}
              size="sm"
              aria-pressed={settings.values.theme === option.value}
              onclick={() =>
                changeSettings({
                  theme: option.value as "light" | "dark" | null,
                })}>{option.label}</Button
            >
          {/each}
        </div>
      </div>
      <div class="space-y-2">
        <Label for="settings-font">终端字号</Label>
        <Input
          id="settings-font"
          type="number"
          min={8}
          max={20}
          step={1}
          bind:value={fontSize}
          oninput={() => {
            fontDirty = true;
          }}
          aria-invalid={!validFont}
          onchange={async () => {
            if (validFont) {
              try {
                await saveSettings({ fontSize: fontSize! });
                fontDirty = false;
              } catch {
                /* retain input */
              }
            }
          }}
        />
        {#if !validFont}<p role="alert" class="text-sm text-destructive">
            请输入 8–20 之间的整数。
          </p>{/if}
        <Button
          variant="ghost"
          size="sm"
          onclick={() => {
            fontDirty = false;
            changeSettings({ fontSize: null });
          }}>恢复默认字号</Button
        >
      </div>
      <div class="flex items-center justify-between gap-3">
        <Label for="settings-lines">代码行号</Label>
        <Switch
          id="settings-lines"
          checked={code.lineNumbers === "show"}
          onCheckedChange={(value) =>
            changeSettings({
              codeReader: { ...code, lineNumbers: value ? "show" : "hide" },
            })}
        />
      </div>
      <div class="flex items-center justify-between gap-3">
        <Label for="settings-wrap">Diff 自动换行</Label>
        <Switch
          id="settings-wrap"
          checked={code.diffOverflow === "wrap"}
          onCheckedChange={(value) =>
            changeSettings({
              codeReader: { ...code, diffOverflow: value ? "wrap" : "scroll" },
            })}
        />
      </div>
      <Button
        variant="ghost"
        size="sm"
        onclick={() => changeSettings({ codeReader: null })}
        >恢复默认阅读设置</Button
      >
      <div class="flex items-center justify-between gap-3">
        <Label for="settings-softkeys">始终显示底部工具栏</Label>
        <Switch
          id="settings-softkeys"
          checked={settings.values.forceSoftKeys ?? false}
          onCheckedChange={(value) => changeSettings({ forceSoftKeys: value })}
        />
      </div>
      <Button
        variant="ghost"
        size="sm"
        onclick={() => changeSettings({ forceSoftKeys: null })}
        >恢复默认工具栏</Button
      >
    </fieldset>
  </Card.Content>
</Card.Root>
