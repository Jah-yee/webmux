<script lang="ts">
  import { untrack } from "svelte";
  import IconEye from "~icons/tabler/eye";
  import IconEyeOff from "~icons/tabler/eye-off";
  import IconPlus from "~icons/tabler/plus";
  import IconTrash from "~icons/tabler/trash";
  import { Button } from "$lib/components/ui/button";
  import { Checkbox } from "$lib/components/ui/checkbox";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import { Textarea } from "$lib/components/ui/textarea";
  import * as Dialog from "$lib/components/ui/dialog";
  import * as Select from "$lib/components/ui/select";
  import InlineNotice from "./InlineNotice.svelte";
  import LaunchConfigIcon from "./LaunchConfigIcon.svelte";
  import { api, APIError } from "../api";
  import { LaunchConfigSchema, type LaunchConfig } from "../schemas";
  import {
    LAUNCH_ENV_MAX,
    LAUNCH_ICON_OPTIONS,
    launchConfigIcon,
    launchCommandDraft,
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
  let icon = $state("auto");
  let command = $state("");
  let loadBashrc = $state(false);
  let rows = $state<EnvRow[]>([]);
  let revealed = $state<Record<number, boolean>>({});
  let nextKey = 0;
  let loading = $state(false);
  let loadError = $state("");
  let error = $state("");
  let busy = $state(false);
  const draft = $derived(validateLaunchDraft(name, command, rows, loadBashrc));
  const iconOptions = [
    { value: "auto", label: "自动匹配" },
    ...LAUNCH_ICON_OPTIONS,
  ];
  const autoIcon = $derived(
    draft.ok ? launchConfigIcon(draft.value) : "terminal",
  );

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
      icon = "auto";
      command = "";
      loadBashrc = false;
      rows = [];
      revealed = {};
      error = "";
      loadError = "";
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
            icon = config.icon || "auto";
            const restored = launchCommandDraft(config.cmd);
            command = restored.command;
            loadBashrc = restored.loadBashrc;
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
    });
    return () => controller.abort();
  });

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
        { ...draft.value, ...(icon === "auto" ? {} : { icon }) },
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
          <div class="grid gap-2">
            <Label for="launch-config-icon">图标</Label>
            <Select.Root type="single" bind:value={icon} items={iconOptions}>
              <Select.Trigger id="launch-config-icon" aria-label="图标">
                <span class="flex items-center gap-2">
                  <LaunchConfigIcon icon={icon === "auto" ? autoIcon : icon} />
                  {iconOptions.find((option) => option.value === icon)?.label ??
                    "终端"}
                </span>
              </Select.Trigger>
              <Select.Content>
                {#each iconOptions as option (option.value)}
                  <Select.Item value={option.value} label={option.label}>
                    <LaunchConfigIcon
                      icon={option.value === "auto" ? autoIcon : option.value}
                    />
                    {option.label}
                  </Select.Item>
                {/each}
              </Select.Content>
            </Select.Root>
            <p class="text-xs text-muted-foreground">
              自动匹配启动程序，也可指定图标。品牌图标来自 LobeHub。
            </p>
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
              支持引号和转义；直接执行程序，不展开 shell 变量或管道。需要 shell
              语法时，请显式使用 bash -lc '命令'。
            </p>
            <div class="flex items-center gap-2">
              <Checkbox
                id="launch-config-bashrc"
                bind:checked={loadBashrc}
                aria-describedby="launch-config-bashrc-help"
              />
              <Label for="launch-config-bashrc">加载 ~/.bashrc</Label>
            </div>
            <p
              id="launch-config-bashrc-help"
              class="text-xs text-muted-foreground"
            >
              使用交互式 Bash 读取 PATH
              和环境变量；只需填写普通命令，参数仍原样传递，不展开变量或管道。
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
