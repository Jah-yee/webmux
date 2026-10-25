<script lang="ts">
  import { untrack } from "svelte";
  import IconEye from "~icons/tabler/eye";
  import IconEyeOff from "~icons/tabler/eye-off";
  import IconPlus from "~icons/tabler/plus";
  import IconTrash from "~icons/tabler/trash";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import { Textarea } from "$lib/components/ui/textarea";
  import * as Dialog from "$lib/components/ui/dialog";
  import InlineNotice from "./InlineNotice.svelte";
  import { api, APIError, joinArgs, parseArgs } from "../api";
  import {
    LaunchConfigSchema,
    PresetsSchema,
    type LaunchConfig,
    type Preset,
  } from "../schemas";
  import {
    LAUNCH_ENV_MAX,
    validateLaunchDraft,
    type EnvRow,
  } from "../launchConfigs";

  let {
    open = $bindable(false),
    configId,
    onsaved,
  }: {
    open: boolean;
    /** Config being edited; undefined creates a new one. */
    configId: string | undefined;
    onsaved: (config: LaunchConfig) => void;
  } = $props();

  let name = $state("");
  let command = $state("");
  let rows = $state<EnvRow[]>([]);
  let revealed = $state<Record<number, boolean>>({});
  let nextKey = 0;
  let loading = $state(false);
  let loadError = $state("");
  let error = $state("");
  let busy = $state(false);
  let presets = $state<Preset[]>([]);
  let presetsLoading = $state(false);
  let presetsError = $state("");
  let presetName = $state("");
  let presetArgs = $state("");
  let fillError = $state("");
  const selectedPreset = $derived(presets.find((p) => p.name === presetName));
  const draft = $derived(validateLaunchDraft(name, command, rows));

  function row(name = "", value = ""): EnvRow {
    return { key: nextKey++, name, value };
  }

  // Each opening starts from the server copy, so cancel/Esc discards edits.
  // Editing never saves from a summary: the full config (with env values) must
  // load first, otherwise a whole-replacement PUT would drop the variables.
  $effect.pre(() => {
    if (!open) return;
    const controller = new AbortController();
    untrack(() => {
      name = "";
      command = "";
      rows = [];
      revealed = {};
      error = "";
      loadError = "";
      fillError = "";
      presetName = "";
      presetArgs = "";
      busy = false;
      loading = !!configId;
      if (configId)
        api(
          `/launch-configs/${configId}`,
          LaunchConfigSchema,
          "GET",
          undefined,
          controller.signal,
        )
          .then((config) => {
            name = config.name;
            command = joinArgs(config.cmd);
            rows = Object.entries(config.env).map(([key, value]) =>
              row(key, value),
            );
          })
          .catch((err: unknown) => {
            if (!controller.signal.aborted)
              loadError = `无法读取启动配置：${(err as Error).message}`;
          })
          .finally(() => {
            if (!controller.signal.aborted) loading = false;
          });
      presetsLoading = true;
      presetsError = "";
      api("/presets", PresetsSchema, "GET", undefined, controller.signal)
        .then((list) => {
          presets = list;
        })
        .catch((err: unknown) => {
          if (!controller.signal.aborted)
            presetsError = `无法读取内置预设：${(err as Error).message}`;
        })
        .finally(() => {
          if (!controller.signal.aborted) presetsLoading = false;
        });
    });
    return () => controller.abort();
  });

  function fillFromPreset() {
    if (!selectedPreset) return;
    fillError = "";
    try {
      const extra = selectedPreset.allowArgs ? parseArgs(presetArgs) : [];
      command = joinArgs([...selectedPreset.cmd, ...extra]);
      if (!name.trim()) name = selectedPreset.label;
    } catch (err) {
      fillError = (err as Error).message;
    }
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!draft.ok || busy || loading || loadError) return;
    busy = true;
    error = "";
    try {
      const saved = await api(
        configId ? `/launch-configs/${configId}` : "/launch-configs",
        LaunchConfigSchema,
        configId ? "PUT" : "POST",
        draft.value,
      );
      onsaved(saved);
      open = false;
    } catch (err) {
      error =
        err instanceof APIError && err.status === 409
          ? `已有同名启动配置“${draft.value.name}”，请换一个名称。`
          : (err as Error).message;
    } finally {
      busy = false;
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content class="max-h-[90dvh] overflow-y-auto sm:max-w-xl">
    <form class="grid min-w-0 gap-4" onsubmit={submit}>
      <Dialog.Header>
        <Dialog.Title>{configId ? "编辑启动配置" : "新建启动配置"}</Dialog.Title
        >
        <Dialog.Description
          >保存在服务端，所有设备的新建会话页都能选用。</Dialog.Description
        >
      </Dialog.Header>
      {#if loading}
        <p role="status" class="text-sm text-muted-foreground">
          正在读取启动配置…
        </p>
      {:else if loadError}
        <InlineNotice tone="danger">{loadError}</InlineNotice>
      {:else}
        <fieldset disabled={busy} class="grid min-w-0 gap-4">
          <div class="grid gap-2">
            <Label for="launch-config-name">名称</Label>
            <Input
              id="launch-config-name"
              bind:value={name}
              placeholder="例如：Claude 恢复"
              autocomplete="off"
              aria-invalid={!!name && !!draft.errors.name}
              aria-describedby={name && draft.errors.name
                ? "launch-config-name-error"
                : undefined}
            />
            {#if name && draft.errors.name}<p
                id="launch-config-name-error"
                class="text-xs text-destructive"
              >
                {draft.errors.name}
              </p>{/if}
          </div>
          <div class="grid min-w-0 gap-2 rounded-lg border p-3">
            <p id="launch-config-presets" class="text-sm font-medium">
              从内置预设填入
            </p>
            {#if presetsError}
              <p class="text-xs text-muted-foreground">{presetsError}</p>
            {:else if presetsLoading}
              <p class="text-xs text-muted-foreground">正在读取内置预设…</p>
            {:else if !presets.length}
              <p class="text-xs text-muted-foreground">没有内置预设。</p>
            {:else}
              <div
                class="flex flex-wrap gap-2"
                role="group"
                aria-labelledby="launch-config-presets"
              >
                {#each presets as preset (preset.name)}
                  <Button
                    type="button"
                    size="sm"
                    variant={presetName === preset.name ? "default" : "outline"}
                    aria-pressed={presetName === preset.name}
                    onclick={() => {
                      presetName = preset.name;
                      presetArgs = "";
                      fillError = "";
                    }}>{preset.label}</Button
                  >
                {/each}
              </div>
              {#if selectedPreset?.allowArgs}
                <Input
                  aria-label="预设追加参数"
                  bind:value={presetArgs}
                  placeholder="追加参数，例如 --resume"
                  autocapitalize="off"
                  autocorrect="off"
                  spellcheck={false}
                />
              {/if}
              <div class="flex flex-wrap items-center gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  disabled={!selectedPreset}
                  onclick={fillFromPreset}>填入命令</Button
                >
                {#if fillError}<span class="text-xs text-destructive"
                    >{fillError}</span
                  >{/if}
              </div>
            {/if}
          </div>
          <div class="grid gap-2">
            <Label for="launch-config-command">启动命令</Label>
            <Textarea
              id="launch-config-command"
              bind:value={command}
              rows={3}
              placeholder="claude --resume 或 bash -lc 'npm run dev'"
              mono
              class="max-h-[30dvh]"
              autocapitalize="off"
              autocorrect="off"
              spellcheck={false}
              aria-invalid={!!command.trim() && !!draft.errors.command}
              aria-describedby="launch-config-command-help"
            />
            {#if command.trim() && draft.errors.command}<p
                class="text-xs text-destructive"
              >
                {draft.errors.command}
              </p>{/if}
            <p
              id="launch-config-command-help"
              class="text-xs text-muted-foreground"
            >
              支持引号和转义；直接执行程序，不展开 shell
              变量或管道。保存的是此刻命令的副本，之后修改或删除内置预设不会影响它。
            </p>
          </div>
          <fieldset class="grid min-w-0 gap-2">
            <legend class="mb-2 text-sm font-medium">
              环境变量 <span class="font-normal text-muted-foreground"
                >可选</span
              >
            </legend>
            {#each rows as envRow, index (envRow.key)}
              {@const rowErrors = draft.errors.rows[envRow.key]}
              <div class="grid min-w-0 gap-1">
                <div class="flex min-w-0 flex-wrap items-center gap-2">
                  <Input
                    mono
                    class="min-w-0 flex-1 basis-32"
                    aria-label={`变量名 ${index + 1}`}
                    bind:value={envRow.name}
                    placeholder="NAME"
                    autocomplete="off"
                    autocapitalize="off"
                    autocorrect="off"
                    spellcheck={false}
                    aria-invalid={!!rowErrors?.name}
                  />
                  <div class="flex min-w-0 flex-2 basis-48 items-center gap-1">
                    <Input
                      mono
                      class="min-w-0 flex-1"
                      type={revealed[envRow.key] ? "text" : "password"}
                      aria-label={`变量值 ${index + 1}`}
                      bind:value={envRow.value}
                      placeholder="值"
                      autocomplete="off"
                      autocapitalize="off"
                      autocorrect="off"
                      spellcheck={false}
                      aria-invalid={!!rowErrors?.value}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={`显示变量值 ${index + 1}`}
                      aria-pressed={!!revealed[envRow.key]}
                      title={revealed[envRow.key] ? "隐藏变量值" : "显示变量值"}
                      onclick={() => {
                        revealed[envRow.key] = !revealed[envRow.key];
                      }}
                      >{#if revealed[envRow.key]}<IconEyeOff
                          aria-hidden="true"
                        />{:else}<IconEye aria-hidden="true" />{/if}</Button
                    >
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={`删除变量 ${index + 1}`}
                      title="删除变量"
                      onclick={() => {
                        rows = rows.filter((item) => item.key !== envRow.key);
                      }}><IconTrash aria-hidden="true" /></Button
                    >
                  </div>
                </div>
                {#if rowErrors}<p class="text-xs text-destructive">
                    {[rowErrors.name, rowErrors.value]
                      .filter(Boolean)
                      .join("；")}
                  </p>{/if}
              </div>
            {/each}
            {#if draft.errors.env}<p class="text-xs text-destructive">
                {draft.errors.env}
              </p>{/if}
            <div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={rows.length >= LAUNCH_ENV_MAX}
                onclick={() => {
                  rows = [...rows, row()];
                }}><IconPlus aria-hidden="true" />添加环境变量</Button
              >
            </div>
            <p class="text-xs text-muted-foreground">
              变量值保存在服务端状态文件中，默认遮挡；列表与新建会话页只显示变量个数，不显示值。
            </p>
          </fieldset>
          {#if error}<InlineNotice tone="danger">{error}</InlineNotice>{/if}
        </fieldset>
      {/if}
      <Dialog.Footer>
        <Button
          type="button"
          variant="outline"
          onclick={() => {
            open = false;
          }}>取消</Button
        >
        {#if !loadError}<Button
            type="submit"
            disabled={loading || busy || !draft.ok}
            >{busy ? "正在保存…" : "保存"}</Button
          >{/if}
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
