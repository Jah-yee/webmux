<script lang="ts">
  import { untrack } from "svelte";
  import {
    api,
    APIError,
    joinArgs,
    navigate,
    parseArgs,
    recentDirs,
    store,
    stored,
  } from "../api";
  import { launchConfigIcon, type LaunchConfigsState } from "../launchConfigs";
  import LaunchConfigIcon from "./LaunchConfigIcon.svelte";
  import {
    NoContentSchema,
    SessionInfoSchema,
    WorktreeSchema,
    type Repository,
    type Worktree,
  } from "../schemas";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import * as Select from "$lib/components/ui/select";
  import * as Tabs from "$lib/components/ui/tabs";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import DirectoryPicker from "./DirectoryPicker.svelte";
  import ResponsiveDialog from "./ResponsiveDialog.svelte";
  import WorktreeCreateFields from "./WorktreeCreateFields.svelte";

  let {
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
  // Preserve the existing lastLaunch format, independently of the inactive tab.
  const lastLaunch = untrack(() => stored("lastLaunch"));
  let commandSource = $state(
    !lastLaunch || (lastLaunch.startsWith("config:") && lastLaunch.length > 7)
      ? "saved"
      : "custom",
  );
  let selectedConfigId = $state(
    lastLaunch.startsWith("config:") ? lastLaunch.slice(7) : "",
  );
  let launchInitialized = $state(false);
  let command = $state(stored("customCommand"));
  let name = $state("");
  let mkdirOpen = $state(false);
  // Avoid initializing an unused portal on every session-form open. Keep it
  // mounted after first use so closing animations and focus restoration run.
  let mkdirMounted = $state(false);
  let error = $state("");
  let busy = $state(false);
  const configs = $derived(
    launchConfigs.status === "ready" ? launchConfigs.configs : [],
  );
  const chosenConfig = $derived(
    configs.find((config) => config.id === selectedConfigId),
  );
  const custom = $derived(commandSource === "custom");
  $effect(() => {
    if (launchConfigs.status === "loading") return;
    if (!launchInitialized) {
      launchInitialized = true;
      if (!lastLaunch && !selectedConfigId) {
        selectedConfigId = configs[0]?.id || "";
        if (!selectedConfigId) commandSource = "custom";
      }
    }
    // Never silently replace a deleted or unavailable remembered selection.
    if (selectedConfigId && !chosenConfig) {
      selectedConfigId = "";
      commandSource = "custom";
    }
  });
  const commandReady = $derived.by(() => {
    if (!custom) return !!chosenConfig;
    try {
      const argv = parseArgs(command);
      return !!argv.length && !!argv[0];
    } catch {
      return false;
    }
  });
  let mode = $state(untrack(() => initialMode));
  let newPanelMounted = $state(untrack(() => initialMode === "new"));
  $effect(() => {
    if (mode === "new") newPanelMounted = true;
  });
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
  const worktreeChoices = $derived.by(() => {
    const trees = repository?.worktrees || [];
    return createdTree && !trees.some((tree) => tree.id === createdTree?.id)
      ? [...trees, createdTree]
      : trees;
  });
  function worktreeLabel(tree: Worktree) {
    return `${tree.kind === "main" ? "主工作区 · " : ""}${tree.branch || tree.head?.slice(0, 12) || "无提交"}`;
  }
  const targetReady = $derived(
    mode === "direct" ||
      (mode === "existing"
        ? !!worktree?.available && worktree.kind !== "bare"
        : !!repository && worktreeReady),
  );

  async function create(makeDirectory = false, treeOnly = false) {
    if (!treeOnly && !commandReady) {
      error = "请选择有效启动配置，或输入可执行程序及参数。";
      return;
    }
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
      const payload =
        !custom && chosenConfig
          ? { ...target, configId: chosenConfig.id, name }
          : {
              ...target,
              cmd,
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
      store("lastLaunch", custom ? "custom" : `config:${selectedConfigId}`);
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

{#snippet repositoryPicker(id: string, active: boolean)}
  <div class="space-y-2">
    <Label for={id}>仓库</Label>
    <Select.Root
      type="single"
      bind:value={repositoryId}
      onValueChange={() => {
        worktreeId = "";
        createdTree = null;
      }}
      disabled={busy || !active}
      items={repositories.map((repo) => ({
        value: repo.id,
        label: repo.name,
      }))}
    >
      <Select.Trigger {id}>
        {#if repository}
          {@render choiceDetails(repository.name, repository.anchorPath)}
        {:else}
          <span class="text-muted-foreground">选择仓库</span>
        {/if}
      </Select.Trigger>
      <Select.Content>
        {#each repositories as repo (repo.id)}
          <Select.Item
            value={repo.id}
            label={repo.name}
            aria-label={repo.name}
            disabled={repo.availability !== "available"}
          >
            {@render choiceDetails(repo.name, repo.anchorPath)}
          </Select.Item>
        {/each}
      </Select.Content>
    </Select.Root>
  </div>
{/snippet}

{#snippet choiceDetails(label: string, description: string, envCount = 0)}
  <span class="grid min-w-0 flex-1 gap-0.5">
    <span class="truncate text-sm font-medium" title={label}>{label}</span>
    <span
      class="line-clamp-2 font-mono text-xs break-all whitespace-pre-wrap text-muted-foreground"
      title={description}>{description}</span
    >
    {#if envCount}
      <span class="text-xs text-muted-foreground">{envCount} 个环境变量</span>
    {/if}
  </span>
{/snippet}

{#snippet fields()}
  <form
    class="space-y-5"
    onsubmit={(event) => {
      event.preventDefault();
      void create();
    }}
  >
    <fieldset disabled={busy} class="min-w-0 space-y-5">
      <Tabs.Root
        bind:value={mode}
        onValueChange={() => {
          error = "";
        }}
      >
        <Tabs.List aria-label="启动位置" size="form" class="w-full">
          <Tabs.Trigger value="direct">直接目录</Tabs.Trigger>
          <Tabs.Trigger value="existing" disabled={!repositories.length}
            >已有工作树</Tabs.Trigger
          >
          <Tabs.Trigger value="new" disabled={!repositories.length}
            >新工作树</Tabs.Trigger
          >
        </Tabs.List>
        <Tabs.Content value="existing" class="space-y-3 pt-2">
          <fieldset
            disabled={busy || mode !== "existing"}
            class="min-w-0 space-y-3"
          >
            {@render repositoryPicker(
              "session-repository-existing",
              mode === "existing",
            )}
            <div class="space-y-2">
              <Label for="session-worktree">工作树</Label>
              <Select.Root
                type="single"
                bind:value={worktreeId}
                disabled={busy || mode !== "existing"}
                items={worktreeChoices.map((tree) => ({
                  value: tree.id,
                  label: worktreeLabel(tree),
                }))}
              >
                <Select.Trigger id="session-worktree">
                  {#if worktree}
                    {@render choiceDetails(
                      worktreeLabel(worktree),
                      worktree.path,
                    )}
                  {:else}
                    <span class="text-muted-foreground">选择工作树</span>
                  {/if}
                </Select.Trigger>
                <Select.Content>
                  {#each worktreeChoices as tree (tree.id)}
                    <Select.Item
                      value={tree.id}
                      label={worktreeLabel(tree)}
                      aria-label={worktreeLabel(tree)}
                      disabled={!tree.available || tree.kind === "bare"}
                    >
                      {@render choiceDetails(worktreeLabel(tree), tree.path)}
                    </Select.Item>
                  {/each}
                </Select.Content>
              </Select.Root>
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
          </fieldset>
        </Tabs.Content>
        <Tabs.Content value="new" class="space-y-3 pt-2">
          <fieldset disabled={busy || mode !== "new"} class="min-w-0 space-y-3">
            {@render repositoryPicker("session-repository-new", mode === "new")}
            {#if newPanelMounted && repository}
              {#key repository.id}<WorktreeCreateFields
                  {repository}
                  disabled={busy || mode !== "new"}
                  bind:path={newPath}
                  bind:mode={branchMode}
                  bind:branch
                  bind:base
                  bind:ready={worktreeReady}
                />{/key}
            {/if}
          </fieldset>
        </Tabs.Content>
        <Tabs.Content value="direct" class="pt-2">
          <fieldset disabled={busy || mode !== "direct"} class="min-w-0">
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
          </fieldset>
        </Tabs.Content>
      </Tabs.Root>
      <fieldset class="min-w-0 space-y-4">
        <legend class="mb-2 text-sm font-medium">启动命令</legend>
        <Tabs.Root bind:value={commandSource}>
          <Tabs.List aria-label="启动命令来源" size="form" class="w-full">
            <Tabs.Trigger value="saved">预先输入</Tabs.Trigger>
            <Tabs.Trigger value="custom">手动填写</Tabs.Trigger>
          </Tabs.List>
          <Tabs.Content
            value="saved"
            class="min-w-0 space-y-2 pt-2"
            aria-busy={launchConfigs.status === "loading"}
          >
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
              <Select.Root
                type="single"
                bind:value={selectedConfigId}
                disabled={busy || custom}
                items={configs.map((config) => ({
                  value: config.id,
                  label: config.name,
                }))}
              >
                <Select.Trigger aria-label="选择启动配置">
                  {#if chosenConfig}
                    <LaunchConfigIcon icon={launchConfigIcon(chosenConfig)} />
                    {@render choiceDetails(
                      chosenConfig.name,
                      joinArgs(chosenConfig.cmd),
                      chosenConfig.envNames.length,
                    )}
                  {:else}
                    <span class="text-muted-foreground">选择启动配置</span>
                  {/if}
                </Select.Trigger>
                <Select.Content>
                  {#each configs as config (config.id)}
                    <Select.Item
                      value={config.id}
                      label={config.name}
                      aria-label={config.name}
                    >
                      <LaunchConfigIcon icon={launchConfigIcon(config)} />
                      {@render choiceDetails(
                        config.name,
                        joinArgs(config.cmd),
                        config.envNames.length,
                      )}
                    </Select.Item>
                  {/each}
                </Select.Content>
              </Select.Root>
            {/if}
          </Tabs.Content>
          <Tabs.Content value="custom" class="min-w-0 space-y-2 pt-2">
            <fieldset disabled={busy || !custom} class="min-w-0 space-y-2">
              <Label for="custom-command">自定义启动命令</Label>
              <Input
                id="custom-command"
                bind:value={command}
                placeholder="omp --resume 或 bash -lc 'npm run dev'"
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
            </fieldset>
          </Tabs.Content>
        </Tabs.Root>
      </fieldset>
      <div class="space-y-2">
        <Label for="session-name"
          >会话名称 <span class="text-muted-foreground">可选</span></Label
        ><Input
          id="session-name"
          bind:value={name}
          placeholder="留空则跟随终端标题"
        />
        {#if !commandReady}
          <p role="status" class="text-xs text-muted-foreground">
            请选择有效启动配置，或输入可执行程序及参数。
          </p>
        {/if}
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
        <Button type="submit" disabled={busy || !targetReady || !commandReady}
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
