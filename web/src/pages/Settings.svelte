<script lang="ts">
  import { onMount } from "svelte";
  import IconArrowDown from "~icons/tabler/arrow-down";
  import IconArrowUp from "~icons/tabler/arrow-up";
  import IconBolt from "~icons/tabler/bolt";
  import IconPlus from "~icons/tabler/plus";
  import IconRocket from "~icons/tabler/rocket";
  import IconTrash from "~icons/tabler/trash";
  import { Badge } from "$lib/components/ui/badge";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import { ListButton } from "$lib/components/ui/list-button";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import * as Card from "$lib/components/ui/card";
  import * as Empty from "$lib/components/ui/empty";
  import * as Item from "$lib/components/ui/item";
  import { api, joinArgs, navigate } from "../api";
  import {
    LaunchConfigsSchema,
    NoContentSchema,
    PreviewSettingsSchema,
    type LaunchConfigSummary,
    type QuickCommand,
  } from "../schemas";
  import { LAUNCH_CONFIGS_LIMIT } from "../launchConfigs";
  import {
    defaultPreviewSettings,
    previewSettings,
    updatePreviewSettings,
  } from "../previewSettings.svelte";
  import { selectPreview } from "../preview";
  import {
    QUICK_COMMANDS_LIMIT,
    commandLabel,
    commandPreview,
  } from "../quickCommands";
  import { quickCommands, saveQuickCommands } from "../quickCommands.svelte";
  import QuickCommandDialog from "../components/QuickCommandDialog.svelte";
  import LaunchConfigDialog from "../components/LaunchConfigDialog.svelte";
  import InlineNotice from "../components/InlineNotice.svelte";
  import TunnelSettings from "../components/TunnelSettings.svelte";
  import PushSettings from "../components/PushSettings.svelte";
  import { Switch } from "$lib/components/ui/switch";
  import { browserDraftStorage } from "../draftStorage";

  // Only a session ID is accepted as the return target, never an arbitrary path.
  const returnSession = new URLSearchParams(location.search).get("session");
  const sessionId =
    returnSession && /^[a-z2-7]{8}$/.test(returnSession) ? returnSession : "";
  let dialogOpen = $state(false);
  let editing = $state<QuickCommand>();
  let deleteOpen = $state(false);
  let deleting = $state<QuickCommand>();
  const full = $derived(quickCommands.list.length >= QUICK_COMMANDS_LIMIT);

  function edit(cmd?: QuickCommand) {
    editing = cmd;
    dialogOpen = true;
  }
  function upsert(cmd: QuickCommand) {
    const list = quickCommands.list;
    const exists = list.some((item) => item.id === cmd.id);
    if (!exists && list.length >= QUICK_COMMANDS_LIMIT)
      return `最多保存 ${QUICK_COMMANDS_LIMIT} 条快捷指令，请先删除一条后再保存。`;
    saveQuickCommands(
      exists
        ? list.map((item) => (item.id === cmd.id ? cmd : item))
        : [...list, cmd],
    );
    return "";
  }
  function move(index: number, offset: -1 | 1) {
    const list = [...quickCommands.list];
    [list[index], list[index + offset]] = [list[index + offset], list[index]];
    saveQuickCommands(list);
  }
  // Launch configs live on the server; a load failure (for example 503 when
  // the state file is corrupt or locked) only disables this card.
  let launchConfigs = $state<LaunchConfigSummary[]>([]);
  let launchState = $state<"loading" | "ready" | "unavailable">("loading");
  let launchError = $state("");
  let launchDialogOpen = $state(false);
  let launchEditing = $state<string>();
  let launchDeleting = $state<LaunchConfigSummary>();
  let launchDeleteOpen = $state(false);
  let launchDeleteBusy = $state(false);
  let launchDeleteError = $state("");
  const launchFull = $derived(launchConfigs.length >= LAUNCH_CONFIGS_LIMIT);
  async function loadLaunchConfigs() {
    try {
      launchConfigs = await api("/launch-configs", LaunchConfigsSchema);
      launchError = "";
      launchState = "ready";
    } catch (err) {
      launchError = (err as Error).message;
      launchState = "unavailable";
    }
  }
  function editLaunchConfig(id?: string) {
    launchEditing = id;
    launchDialogOpen = true;
  }
  async function deleteLaunchConfig() {
    if (!launchDeleting) return;
    launchDeleteBusy = true;
    launchDeleteError = "";
    try {
      await api(
        `/launch-configs/${launchDeleting.id}`,
        NoContentSchema,
        "DELETE",
      );
      launchDeleteOpen = false;
      await loadLaunchConfigs();
    } catch (err) {
      launchDeleteError = (err as Error).message;
    } finally {
      launchDeleteBusy = false;
    }
  }
  const draftStorage = browserDraftStorage();
  let keepDrafts = $state(draftStorage.enabled);
  let savedDrafts = $state(draftStorage.count());
  let savedScratch = $state(draftStorage.scratchCount());
  let clearDraftsOpen = $state(false);
  let lines = $state<number | undefined>(previewSettings.lines);
  let skip = $state<number | undefined>(previewSettings.skip);
  const validation = $derived(PreviewSettingsSchema.safeParse({ lines, skip }));
  const example = [
    ...Array.from({ length: 12 }, (_, index) => `输出第 ${index + 1} 行`),
    "模型 · 工作目录 · 状态栏",
    "❯ 输入提示符",
  ].join("\n");
  const preview = $derived(selectPreview(example, previewSettings));

  $effect(() => {
    if (validation.success) updatePreviewSettings(validation.data);
  });
  onMount(() => {
    document.title = "WebMux · 设置";
    const target = location.hash.slice(1);
    if (target) document.getElementById(target)?.scrollIntoView();
    void loadLaunchConfigs();
  });
</script>

{#snippet deleteButton(label: string, onclick: () => void)}
  <Button
    variant="ghost"
    size="icon-sm"
    aria-label={`删除 ${label}`}
    title="删除"
    {onclick}><IconTrash aria-hidden="true" /></Button
  >
{/snippet}

<div class="safe-area-page min-h-dvh">
  <header class="app-header border-b bg-card">
    <div
      class="mx-auto flex min-h-(--app-header-height) max-w-6xl items-center justify-between gap-3 px-4 py-1 sm:px-8"
    >
      <a class="brand" href="/"><span class="brand-mark">WM</span> WebMux</a>
      {#if sessionId}
        <Button variant="outline" onclick={() => navigate(`/s/${sessionId}`)}
          >返回终端</Button
        >
      {:else}
        <Button href="/" variant="outline">返回工作区</Button>
      {/if}
    </div>
  </header>
  <main class="mx-auto max-w-3xl space-y-6 px-4 py-8 sm:px-8 sm:py-10">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">设置</h1>
      <p class="mt-2 text-sm text-muted-foreground">
        快捷指令、草稿和预览只保存在当前浏览器；启动配置保存在服务端，在所有设备间共享。公网访问与通知推送涉及服务端能力，本页提供说明或当前设备操作；修改服务端配置需要编辑配置文件并重启。
      </p>
    </div>
    <Card.Root id="quick-commands" class="scroll-mt-4">
      <Card.Header>
        <Card.Title>快捷指令</Card.Title>
        <Card.Description
          >点击终端底部工具栏或顶栏的 <IconBolt
            class="inline size-3.5 align-[-2px]"
            aria-label="闪电"
          /> 按钮即可发送；仅保存在当前浏览器。</Card.Description
        >
      </Card.Header>
      <Card.Content class="space-y-4">
        {#if quickCommands.list.length === 0}
          <Empty.Root variant="outline">
            <Empty.Header>
              <Empty.Media variant="icon"><IconBolt /></Empty.Media>
              <Empty.Title>还没有快捷指令</Empty.Title>
              <Empty.Description
                >把常用命令或提示词存起来，在终端一键发送。</Empty.Description
              >
            </Empty.Header>
            <Empty.Content>
              <Button onclick={() => edit()}
                ><IconPlus aria-hidden="true" />添加快捷指令</Button
              >
            </Empty.Content>
          </Empty.Root>
        {:else}
          <Item.Group class="gap-2" aria-label="快捷指令列表">
            {#each quickCommands.list as cmd, index (cmd.id)}
              {@const label = commandLabel(cmd)}
              <Item.Root variant="outline" size="sm">
                <ListButton
                  variant="content"
                  class="flex min-w-0 flex-1 flex-col"
                  aria-label={`编辑 ${label}`}
                  onclick={() => edit(cmd)}
                >
                  <span class="flex min-w-0 items-center gap-2">
                    <span class="truncate text-xs font-medium">{label}</span>
                    {#if cmd.enter}<Badge variant="secondary">回车</Badge>{/if}
                  </span>
                  {#if cmd.name}<span
                      class="truncate font-mono text-xs text-muted-foreground"
                      >{commandPreview(cmd)}</span
                    >{/if}
                </ListButton>
                <Item.Actions class="gap-1">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`上移 ${label}`}
                    title="上移"
                    disabled={index === 0}
                    onclick={() => move(index, -1)}
                    ><IconArrowUp aria-hidden="true" /></Button
                  >
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`下移 ${label}`}
                    title="下移"
                    disabled={index === quickCommands.list.length - 1}
                    onclick={() => move(index, 1)}
                    ><IconArrowDown aria-hidden="true" /></Button
                  >
                  {@render deleteButton(label, () => {
                    deleting = cmd;
                    deleteOpen = true;
                  })}
                </Item.Actions>
              </Item.Root>
            {/each}
          </Item.Group>
          <div class="flex flex-wrap items-center gap-3">
            <Button variant="outline" disabled={full} onclick={() => edit()}
              ><IconPlus aria-hidden="true" />添加快捷指令</Button
            >
            {#if full}<span class="text-xs text-muted-foreground"
                >最多 {QUICK_COMMANDS_LIMIT} 条</span
              >{/if}
          </div>
        {/if}
      </Card.Content>
    </Card.Root>
    <Card.Root
      role="region"
      id="launch-configs"
      class="scroll-mt-4"
      aria-labelledby="launch-configs-title"
    >
      <Card.Header>
        <Card.Title id="launch-configs-title">启动配置</Card.Title>
        <Card.Description
          >保存“名称 + 完整命令 +
          环境变量”，在新建会话时选用；保存在服务端，所有设备共享。内置预设来自服务端配置文件的
          <code class="font-mono">[[presets]]</code
          >，需编辑配置文件并重启服务才会变化，不在这里管理。启动配置保存的是创建时的命令副本，之后修改或删除内置预设不会影响它。</Card.Description
        >
      </Card.Header>
      <Card.Content class="space-y-4">
        {#if launchState === "loading"}
          <!-- Same shape as the empty state, the usual first result, so the
               cards below do not shift when the list arrives. -->
          <Empty.Root variant="outline" aria-busy="true">
            <Empty.Header>
              <Empty.Media variant="icon"><IconRocket /></Empty.Media>
              <Empty.Title role="status">正在读取启动配置…</Empty.Title>
              <Empty.Description
                >把常用命令、参数和环境变量存起来，在新建会话时一键选用。</Empty.Description
              >
            </Empty.Header>
            <Empty.Content>
              <Button disabled
                ><IconPlus aria-hidden="true" />新建启动配置</Button
              >
            </Empty.Content>
          </Empty.Root>
        {:else if launchState === "unavailable"}
          <InlineNotice tone="danger">
            <p>启动配置暂不可用，新建、编辑与删除已停用：</p>
            <p class="mt-1 font-mono text-xs">{launchError}</p>
          </InlineNotice>
          <Button variant="outline" onclick={() => loadLaunchConfigs()}
            >重新读取</Button
          >
        {:else if launchConfigs.length === 0}
          <Empty.Root variant="outline">
            <Empty.Header>
              <Empty.Media variant="icon"><IconRocket /></Empty.Media>
              <Empty.Title>还没有启动配置</Empty.Title>
              <Empty.Description
                >把常用命令、参数和环境变量存起来，在新建会话时一键选用。</Empty.Description
              >
            </Empty.Header>
            <Empty.Content>
              <Button onclick={() => editLaunchConfig()}
                ><IconPlus aria-hidden="true" />新建启动配置</Button
              >
            </Empty.Content>
          </Empty.Root>
        {:else}
          <Item.Group class="gap-2" aria-label="启动配置列表">
            {#each launchConfigs as config (config.id)}
              <Item.Root variant="outline" size="sm">
                <ListButton
                  variant="content"
                  class="flex min-w-0 flex-1 flex-col"
                  aria-label={`编辑 ${config.name}`}
                  onclick={() => editLaunchConfig(config.id)}
                >
                  <span class="flex min-w-0 items-center gap-2">
                    <span class="truncate text-xs font-medium"
                      >{config.name}</span
                    >
                    {#if config.envNames.length}<Badge variant="secondary"
                        >{config.envNames.length} 个环境变量</Badge
                      >{/if}
                  </span>
                  <span class="truncate font-mono text-xs text-muted-foreground"
                    >{joinArgs(config.cmd)}</span
                  >
                </ListButton>
                <Item.Actions class="gap-1">
                  {@render deleteButton(config.name, () => {
                    launchDeleting = config;
                    launchDeleteError = "";
                    launchDeleteOpen = true;
                  })}
                </Item.Actions>
              </Item.Root>
            {/each}
          </Item.Group>
          <div class="flex flex-wrap items-center gap-3">
            <Button
              variant="outline"
              disabled={launchFull}
              onclick={() => editLaunchConfig()}
              ><IconPlus aria-hidden="true" />新建启动配置</Button
            >
            {#if launchFull}<span class="text-xs text-muted-foreground"
                >最多 {LAUNCH_CONFIGS_LIMIT} 条，请先删除一条</span
              >{/if}
          </div>
        {/if}
      </Card.Content>
    </Card.Root>
    <Card.Root id="drafts" class="scroll-mt-4">
      <Card.Header>
        <Card.Title>草稿与暂存</Card.Title>
        <Card.Description
          >草稿正文、暂存与未确认的提交按会话保存在当前浏览器，刷新、关闭页面或认证失效后可恢复；7
          天未修改或会话已不存在时自动清理。恢复后仍需点击发送才会提交。</Card.Description
        >
      </Card.Header>
      <Card.Content class="space-y-4">
        <div class="flex items-center justify-between gap-4">
          <Label for="keep-drafts">在本机保存草稿</Label>
          <Switch
            id="keep-drafts"
            checked={keepDrafts}
            onCheckedChange={(checked) => {
              draftStorage.setEnabled(checked);
              keepDrafts = draftStorage.enabled;
              savedDrafts = draftStorage.count();
              savedScratch = draftStorage.scratchCount();
            }}
          />
        </div>
        <p class="text-xs text-muted-foreground">
          关闭后立即删除本机已保存的草稿和暂存，之后内容只保留在当前页面内存中。
        </p>
        <div class="flex flex-wrap items-center gap-3">
          <span class="text-sm" role="status"
            >本机保存了 {savedDrafts} 个会话的草稿，{savedScratch} 条暂存</span
          >
          <Button
            variant="outline"
            disabled={!savedDrafts && !savedScratch}
            onclick={() => (clearDraftsOpen = true)}>清除本机草稿</Button
          >
        </div>
      </Card.Content>
    </Card.Root>
    <TunnelSettings />
    <PushSettings />
    <Card.Root>
      <Card.Header>
        <Card.Title>会话输出预览</Card.Title>
        <Card.Description
          >从当前终端屏幕的底部向上选取非空行，可隐藏 agent
          的状态栏和输入提示符。</Card.Description
        >
      </Card.Header>
      <Card.Content class="space-y-6">
        <div class="grid gap-5 sm:grid-cols-2">
          <div class="space-y-2">
            <Label for="preview-lines">显示行数</Label>
            <Input
              id="preview-lines"
              type="number"
              min={1}
              max={50}
              step={1}
              bind:value={lines}
              aria-invalid={!validation.success}
              aria-describedby="preview-lines-help"
            />
            <p id="preview-lines-help" class="text-xs text-muted-foreground">
              1–50 行；始终保留设定高度，避免后台输出更新时会话卡片跳动。
            </p>
          </div>
          <div class="space-y-2">
            <Label for="preview-skip">跳过末尾行数</Label>
            <Input
              id="preview-skip"
              type="number"
              min={0}
              max={50}
              step={1}
              bind:value={skip}
              aria-invalid={!validation.success}
              aria-describedby="preview-skip-help"
            />
            <p id="preview-skip-help" class="text-xs text-muted-foreground">
              0–50 行；0 表示不隐藏末尾内容。
            </p>
          </div>
        </div>
        {#if !validation.success}
          <p role="alert" class="text-sm text-destructive">
            请输入有效整数：显示 1–50 行，跳过 0–50
            行。无效输入不会覆盖已保存设置。
          </p>
        {/if}
        <div class="space-y-2">
          <p class="text-sm">
            跳过末尾 {previewSettings.skip} 行，再向上显示 {previewSettings.lines}
            行。
          </p>
          <p class="text-xs text-muted-foreground">
            示例预览（12 行输出 + 2 行底栏，不是实际会话）
          </p>
          <pre
            aria-label="预览效果示例"
            style:--preview-lines={previewSettings.lines}
            class="h-preview min-w-0 overflow-hidden bg-muted p-2 font-mono text-xs leading-5 whitespace-pre text-foreground dark:bg-background">{preview ||
              "末尾行已隐藏，暂无可显示内容"}</pre>
        </div>
        <Button
          variant="outline"
          onclick={() => {
            lines = defaultPreviewSettings.lines;
            skip = defaultPreviewSettings.skip;
          }}
          >恢复默认（{defaultPreviewSettings.lines} 行，跳过 {defaultPreviewSettings.skip}
          行）</Button
        >
      </Card.Content>
    </Card.Root>
  </main>
</div>

<QuickCommandDialog bind:open={dialogOpen} initial={editing} save={upsert} />
<LaunchConfigDialog
  bind:open={launchDialogOpen}
  configId={launchEditing}
  onsaved={() => void loadLaunchConfigs()}
/>
<AlertDialog.Root bind:open={launchDeleteOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>删除启动配置？</AlertDialog.Title>
      <AlertDialog.Description
        >将删除“{launchDeleting?.name}”，此操作不能撤销；已运行的会话不受影响。</AlertDialog.Description
      >
    </AlertDialog.Header>
    {#if launchDeleting}<p
        class="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs"
      >
        {joinArgs(launchDeleting.cmd)}
      </p>{/if}
    {#if launchDeleteError}<InlineNotice tone="danger"
        >{launchDeleteError}</InlineNotice
      >{/if}
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={launchDeleteBusy}>取消</AlertDialog.Cancel>
      <Button
        variant="destructive"
        disabled={launchDeleteBusy}
        onclick={deleteLaunchConfig}>删除</Button
      >
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
<AlertDialog.Root bind:open={deleteOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>删除快捷指令？</AlertDialog.Title>
      <AlertDialog.Description
        >将删除“{deleting
          ? commandLabel(deleting)
          : ""}”，此操作不能撤销。</AlertDialog.Description
      >
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel>取消</AlertDialog.Cancel>
      <Button
        variant="destructive"
        onclick={() => {
          saveQuickCommands(
            quickCommands.list.filter((item) => item.id !== deleting?.id),
          );
          deleteOpen = false;
        }}>删除</Button
      >
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
<AlertDialog.Root bind:open={clearDraftsOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>清除本机草稿？</AlertDialog.Title>
      <AlertDialog.Description
        >删除当前浏览器保存的全部草稿、暂存和未确认提交记录，不影响终端。已打开的终端页面在刷新前仍保留其内容。此操作无法撤销。</AlertDialog.Description
      >
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel>取消</AlertDialog.Cancel>
      <AlertDialog.Action
        variant="destructive"
        onclick={() => {
          draftStorage.clear();
          savedDrafts = draftStorage.count();
          savedScratch = draftStorage.scratchCount();
          clearDraftsOpen = false;
        }}>确认清除</AlertDialog.Action
      >
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
