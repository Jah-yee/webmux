<script lang="ts">
  import { untrack } from "svelte";
  import IconChevronDown from "~icons/tabler/chevron-down";
  import {
    api,
    APIError,
    joinArgs,
    navigate,
    parseArgs,
    recentDirs,
    store,
    stored,
    unstore,
  } from "../api";
  import type { LaunchConfigsState } from "../launchConfigs";
  import {
    NoContentSchema,
    SessionInfoSchema,
    WorktreeSchema,
    type Preset,
    type Repository,
    type Worktree,
  } from "../schemas";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import { ListButton } from "$lib/components/ui/list-button";
  import * as NativeSelect from "$lib/components/ui/native-select";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import * as Collapsible from "$lib/components/ui/collapsible";
  import DirectoryPicker from "./DirectoryPicker.svelte";
  import ResponsiveDialog from "./ResponsiveDialog.svelte";
  import WorktreeCreateFields from "./WorktreeCreateFields.svelte";

  let {
    presets,
    launchConfigs,
    defaultCwd,
    repositories = [],
    initialRepositoryId = "",
    initialWorktreeId = "",
    initialMode = "direct",
    onchange,
    onworktreecreated,
    onclose,
  }: {
    presets: Preset[];
    launchConfigs: LaunchConfigsState;
    defaultCwd: string;
    repositories?: Repository[];
    initialRepositoryId?: string;
    initialWorktreeId?: string;
    initialMode?: "direct" | "existing" | "new";
    onchange: () => void;
    onworktreecreated: (tree: Worktree) => void;
    onclose: () => void;
  } = $props();
  const recent = recentDirs();
  let cwd = $state(untrack(() => recent[0] || defaultCwd));
  // The last launch choice: `preset:<name>`, `config:<id>` or `custom`. Only
  // the choice is remembered, never config contents. A legacy `lastPreset`
  // is migrated once and then removed.
  const fallbackLaunch = untrack(() => `preset:${presets[0]?.name ?? "shell"}`);
  let launch = $state(
    untrack(() => {
      let saved = stored("lastLaunch");
      const legacy = stored("lastPreset");
      if (!saved && legacy) {
        saved = legacy === "custom" ? "custom" : `preset:${legacy}`;
        store("lastLaunch", saved);
      }
      if (legacy) unstore("lastPreset");
      return saved === "custom" ||
        saved.startsWith("config:") ||
        presets.some((p) => `preset:${p.name}` === saved)
        ? saved
        : fallbackLaunch;
    }),
  );
  let args = $state("");
  let command = $state(stored("customCommand"));
  let name = $state("");
  let mkdirOpen = $state(false);
  // Avoid initializing an unused portal on every session-form open. Keep it
  // mounted after first use so closing animations and focus restoration run.
  let mkdirMounted = $state(false);
  let error = $state("");
  let busy = $state(false);
  const chosen = $derived(presets.find((p) => `preset:${p.name}` === launch));
  const configs = $derived(
    launchConfigs.status === "ready" ? launchConfigs.configs : [],
  );
  const chosenConfig = $derived(
    configs.find((config) => `config:${config.id}` === launch),
  );
  const custom = $derived(launch === "custom");
  let temporaryOpen = $state(untrack(() => launch === "custom"));
  // A remembered config that was deleted, or a store that is unavailable,
  // falls back to the first built-in preset once the summaries settle.
  $effect(() => {
    if (
      launch.startsWith("config:") &&
      launchConfigs.status !== "loading" &&
      !chosenConfig
    )
      launch = fallbackLaunch;
  });
  let mode = $state(untrack(() => initialMode));
  let repositoryId = $state(
    untrack(() => initialRepositoryId || repositories[0]?.id || ""),
  );
  let worktreeId = $state(untrack(() => initialWorktreeId));
  let newPath = $state("");
  let branchMode = $state<"new" | "existing">("new");
  let branch = $state("");
  let base = $state("");
  let worktreeReady = $state(false);
  let createdTree = $state<import("../schemas").Worktree | null>(null);
  const repository = $derived(repositories.find((r) => r.id === repositoryId));
  const worktree = $derived(
    createdTree?.id === worktreeId
      ? createdTree
      : repository?.worktrees.find((w) => w.id === worktreeId),
  );
  const targetReady = $derived(
    mode === "direct" ||
      (mode === "existing"
        ? !!worktree?.available && worktree.kind !== "bare"
        : !!repository && worktreeReady),
  );

  async function create(makeDirectory = false, treeOnly = false) {
    busy = true;
    error = "";
    try {
      // Paint the pending state before a fast local response can immediately
      // start loading and mounting the terminal in the same interaction.
      await new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      );
      const cmd = custom && !treeOnly ? parseArgs(command) : undefined;
      if (custom && !treeOnly && (!cmd?.length || !cmd[0]))
        throw new Error("请输入可执行程序及参数");
      if (mode === "new") {
        const tree = await api(
          `/repositories/${repositoryId}/worktrees`,
          WorktreeSchema,
          "POST",
          {
            path: newPath,
            mode: branchMode,
            branch,
            ...(branchMode === "new" ? { base } : {}),
          },
        );
        createdTree = tree;
        worktreeId = tree.id;
        mode = "existing";
        onworktreecreated(tree);
        onchange();
      }
      if (treeOnly) {
        onclose();
        return;
      }
      const target =
        mode === "direct"
          ? { cwd }
          : { workspace: { repositoryId, worktreeId } };
      const payload = chosenConfig
        ? { ...target, configId: chosenConfig.id, name }
        : {
            ...target,
            preset: custom ? "custom" : chosen?.name,
            args: !custom && chosen?.allowArgs ? parseArgs(args) : [],
            ...(custom ? { cmd } : {}),
            name,
          };
      if (makeDirectory)
        await api("/fs/mkdir", NoContentSchema, "POST", { path: cwd });
      const session = await api(
        "/sessions",
        SessionInfoSchema,
        "POST",
        payload,
      );
      store(
        "recentDirs",
        JSON.stringify(
          [session.cwd, ...recent.filter((p) => p !== session.cwd)].slice(
            0,
            10,
          ),
        ),
      );
      store("lastLaunch", launch);
      if (custom) store("customCommand", command);
      navigate("/s/" + session.id);
    } catch (err) {
      if (mode !== "direct") onchange();
      if (
        mode === "direct" &&
        !makeDirectory &&
        err instanceof APIError &&
        err.status === 400 &&
        err.message.startsWith("directory:") &&
        err.message.includes("no such file or directory")
      ) {
        mkdirMounted = true;
        mkdirOpen = true;
      } else error = (err as Error).message;
    } finally {
      busy = false;
    }
  }
</script>

{#snippet fields()}
  <form
    class="space-y-5"
    onsubmit={(event) => {
      event.preventDefault();
      void create();
    }}
  >
    <fieldset disabled={busy} class="min-w-0 space-y-5">
      <fieldset class="space-y-2" disabled={busy}>
        <legend class="mb-2 text-sm font-medium">启动位置</legend>
        <div class="flex flex-wrap gap-2">
          {#each [{ value: "direct", label: "直接目录" }, { value: "existing", label: "已有工作树" }, { value: "new", label: "新工作树" }] as option}
            <Button
              type="button"
              variant={mode === option.value ? "default" : "outline"}
              aria-pressed={mode === option.value}
              disabled={option.value !== "direct" && !repositories.length}
              onclick={() => {
                mode = option.value as typeof mode;
                error = "";
              }}>{option.label}</Button
            >
          {/each}
        </div>
      </fieldset>
      {#if mode !== "direct"}
        <div class="space-y-2">
          <Label for="session-repository">仓库</Label>
          <NativeSelect.Root
            id="session-repository"
            bind:value={repositoryId}
            onchange={() => {
              worktreeId = "";
              createdTree = null;
            }}
            disabled={busy}
            class="w-full"
          >
            <NativeSelect.Option value="">选择仓库</NativeSelect.Option>
            {#each repositories as repo (repo.id)}<NativeSelect.Option
                value={repo.id}
                disabled={repo.availability !== "available"}
                >{repo.name}</NativeSelect.Option
              >{/each}
          </NativeSelect.Root>
        </div>
        {#if mode === "existing"}
          <div class="space-y-2">
            <Label for="session-worktree">工作树</Label>
            <NativeSelect.Root
              id="session-worktree"
              bind:value={worktreeId}
              disabled={busy}
              class="w-full"
            >
              <NativeSelect.Option value="">选择工作树</NativeSelect.Option>
              {#each repository?.worktrees || [] as tree (tree.id)}<NativeSelect.Option
                  value={tree.id}
                  disabled={!tree.available || tree.kind === "bare"}
                  >{tree.kind === "main" ? "主工作区 · " : ""}{tree.branch ||
                    tree.head?.slice(0, 12) ||
                    "无提交"} · {tree.path}</NativeSelect.Option
                >{/each}
              {#if createdTree && !repository?.worktrees.some((tree) => tree.id === createdTree?.id)}<NativeSelect.Option
                  value={createdTree.id}
                  >{createdTree.branch} · {createdTree.path}</NativeSelect.Option
                >{/if}
            </NativeSelect.Root>
            {#if worktree}<p
                class="font-mono text-xs break-all whitespace-pre-wrap"
              >
                {worktree.path}
              </p>{/if}
            <p class="text-xs text-muted-foreground">
              同一工作树中的会话共享文件，各自使用独立终端。锁定只保护 Git
              工作树，不表示会话独占。
            </p>
            {#if createdTree}<p role="status" class="text-sm text-primary">
                工作树已创建。再次提交只启动会话，不会重复创建分支或目录。
              </p>{/if}
          </div>
        {:else if repository}
          {#key repository.id}<WorktreeCreateFields
              {repository}
              disabled={busy}
              bind:path={newPath}
              bind:mode={branchMode}
              bind:branch
              bind:base
              bind:ready={worktreeReady}
            />{/key}
        {/if}
      {:else}
        <div class="space-y-2">
          <Label for="cwd">工作目录</Label>
          <div class="flex gap-2">
            <Input
              id="cwd"
              bind:value={cwd}
              placeholder="~/projects"
              required
              autocomplete="off"
              autocapitalize="off"
              autocorrect="off"
              spellcheck={false}
            />
            <DirectoryPicker bind:value={cwd} />
          </div>
          {#if recent.length}
            <div class="flex flex-wrap gap-1" aria-label="最近目录">
              {#each recent.slice(0, 5) as dir (dir)}<Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  class="max-w-full"
                  title={dir}
                  onclick={() => {
                    cwd = dir;
                  }}
                  ><span class="min-w-0 truncate"
                    >{dir.split("/").pop() || "/"}</span
                  ></Button
                >{/each}
            </div>
          {/if}
        </div>
      {/if}
      <fieldset class="min-w-0 space-y-4">
        <legend class="mb-2 text-sm font-medium">启动命令</legend>
        <div class="space-y-2" role="group" aria-labelledby="launch-builtin">
          <p id="launch-builtin" class="text-xs text-muted-foreground">
            内置预设
          </p>
          <div class="flex flex-wrap gap-2">
            {#each presets as p (p.name)}
              <Button
                type="button"
                variant={launch === `preset:${p.name}` ? "default" : "outline"}
                aria-pressed={launch === `preset:${p.name}`}
                onclick={() => {
                  launch = `preset:${p.name}`;
                  args = "";
                }}>{p.label}</Button
              >
            {/each}
          </div>
        </div>
        <div
          class="min-w-0 space-y-2"
          role="group"
          aria-labelledby="launch-saved"
          aria-busy={launchConfigs.status === "loading"}
        >
          <p id="launch-saved" class="text-xs text-muted-foreground">
            我的启动配置
          </p>
          {#if launchConfigs.status === "loading"}
            <p role="status" class="text-xs text-muted-foreground">
              正在读取启动配置…
            </p>
          {:else if launchConfigs.status === "unavailable"}
            <p class="text-xs text-destructive wrap-anywhere">
              启动配置暂不可用：{launchConfigs.error}
            </p>
          {:else if !configs.length}
            <p class="text-xs text-muted-foreground">
              还没有启动配置。<a
                class="text-foreground underline underline-offset-2"
                href="/settings#launch-configs">前往设置创建启动配置</a
              >
            </p>
          {:else}
            <div class="grid max-h-60 min-w-0 gap-2 overflow-y-auto p-0.5">
              {#each configs as config (config.id)}
                <ListButton
                  variant="option"
                  class="grid min-w-0"
                  aria-label={`启动配置 ${config.name}`}
                  aria-pressed={launch === `config:${config.id}`}
                  onclick={() => {
                    launch = `config:${config.id}`;
                    args = "";
                  }}
                >
                  <span class="truncate text-sm font-medium">{config.name}</span
                  >
                  <span class="truncate font-mono text-xs text-muted-foreground"
                    >{joinArgs(config.cmd)}</span
                  >
                  {#if config.envNames.length}<span
                      class="text-xs text-muted-foreground"
                      >{config.envNames.length} 个环境变量</span
                    >{/if}
                </ListButton>
              {/each}
            </div>
          {/if}
        </div>
        <Collapsible.Root bind:open={temporaryOpen} class="min-w-0 space-y-2">
          <Collapsible.Trigger
            class="group flex items-center gap-1 text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
          >
            <IconChevronDown
              class="size-4 -rotate-90 transition-transform group-data-[state=open]:rotate-0"
              aria-hidden="true"
            />临时命令（不保存）
          </Collapsible.Trigger>
          <Collapsible.Content class="space-y-2">
            <Button
              type="button"
              variant={custom ? "default" : "outline"}
              aria-pressed={custom}
              onclick={() => {
                launch = "custom";
                args = "";
              }}>自定义</Button
            >
            {#if custom}
              <Label for="custom-command">自定义启动命令</Label>
              <Input
                id="custom-command"
                bind:value={command}
                placeholder="omp --resume 或 bash -lc 'npm run dev'"
                required
                autocomplete="off"
                autocapitalize="off"
                autocorrect="off"
                spellcheck={false}
              />
              <p class="text-xs text-muted-foreground">
                支持引号和转义；直接执行程序，不展开 shell 变量或管道。需要
                shell 语法时请使用 bash -lc
                '命令'。只在本浏览器记住，需要长期使用请在设置中保存为启动配置。
              </p>
            {/if}
          </Collapsible.Content>
        </Collapsible.Root>
        {#if chosen}
          <p class="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">
            {joinArgs(chosen.cmd)}{args ? " " + args : ""}
          </p>
        {:else if chosenConfig}
          <p class="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">
            {joinArgs(chosenConfig.cmd)}
          </p>
        {/if}
      </fieldset>
      {#if chosen?.allowArgs}
        <div class="space-y-2">
          <Label for="args">追加参数</Label><Input
            id="args"
            bind:value={args}
            placeholder="--resume 或带引号的参数"
            autocapitalize="off"
            autocorrect="off"
            spellcheck={false}
          />
        </div>
      {/if}
      <div class="space-y-2">
        <Label for="session-name"
          >会话名称 <span class="text-muted-foreground">可选</span></Label
        ><Input
          id="session-name"
          bind:value={name}
          placeholder="留空则跟随终端标题"
        />
      </div>
      {#if error}<p role="alert" class="text-destructive">{error}</p>{/if}
      <div class="flex flex-wrap justify-end gap-2 pt-2">
        <Button
          type="button"
          variant="outline"
          disabled={busy}
          onclick={onclose}>取消</Button
        >
        {#if mode === "new"}<Button
            type="button"
            variant="outline"
            disabled={busy || !targetReady}
            onclick={() => create(false, true)}>仅创建工作树</Button
          >{/if}
        <Button
          type="submit"
          disabled={busy ||
            !targetReady ||
            (!chosen && !custom && !chosenConfig)}
          >{busy ? "正在启动…" : "创建并进入 →"}</Button
        >
      </div>
    </fieldset>
  </form>
{/snippet}

<ResponsiveDialog
  title="新建会话与工作树"
  description="选择目录和命令。浏览器断开后，会话仍会继续。"
  drawerDescription="选择目录和命令，创建持久终端会话。"
  class="p-6"
  pinDrawerHeader
  {busy}
  {onclose}
>
  {@render fields()}
</ResponsiveDialog>

{#if mkdirMounted}<AlertDialog.Root bind:open={mkdirOpen}>
    <AlertDialog.Content>
      <AlertDialog.Header
        ><AlertDialog.Title>目录不存在，是否创建？</AlertDialog.Title
        ><AlertDialog.Description class="break-all"
          >将在 {cwd} 创建目录，然后启动会话。</AlertDialog.Description
        ></AlertDialog.Header
      >
      <AlertDialog.Footer>
        <AlertDialog.Cancel>取消</AlertDialog.Cancel>
        <AlertDialog.Action
          onclick={() => {
            void create(true);
          }}>创建目录并启动</AlertDialog.Action
        >
      </AlertDialog.Footer>
    </AlertDialog.Content>
  </AlertDialog.Root>{/if}
