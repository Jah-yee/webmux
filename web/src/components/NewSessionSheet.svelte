<script lang="ts">
  import { MediaQuery } from "svelte/reactivity";
  import { untrack } from "svelte";
  import {
    api,
    APIError,
    navigate,
    parseArgs,
    recentDirs,
    store,
    stored,
  } from "../api";
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
  import * as Dialog from "$lib/components/ui/dialog";
  import * as Drawer from "$lib/components/ui/drawer";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import DirectoryPicker from "./DirectoryPicker.svelte";
  import WorktreeCreateFields from "./WorktreeCreateFields.svelte";

  let {
    presets,
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
    defaultCwd: string;
    repositories?: Repository[];
    initialRepositoryId?: string;
    initialWorktreeId?: string;
    initialMode?: "direct" | "existing" | "new";
    onchange: () => void;
    onworktreecreated: (tree: Worktree) => void;
    onclose: () => void;
  } = $props();
  const desktop = new MediaQuery("(min-width: 640px)");
  const recent = recentDirs();
  let open = $state(true);
  let cwd = $state(untrack(() => recent[0] || defaultCwd));
  let preset = $state(
    untrack(
      () =>
        (stored("lastPreset") === "custom"
          ? "custom"
          : presets.find((p) => p.name === stored("lastPreset"))?.name) ||
        presets[0]?.name ||
        "shell",
    ),
  );
  let args = $state("");
  let command = $state(stored("customCommand"));
  let name = $state("");
  let mkdirOpen = $state(false);
  let error = $state("");
  let busy = $state(false);
  const chosen = $derived(presets.find((p) => p.name === preset));
  const custom = $derived(preset === "custom");
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
      const payload = {
        ...(mode === "direct"
          ? { cwd }
          : { workspace: { repositoryId, worktreeId } }),
        preset,
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
      store("lastPreset", preset);
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
          <select
            id="session-repository"
            bind:value={repositoryId}
            onchange={() => {
              worktreeId = "";
              createdTree = null;
            }}
            disabled={busy}
            class="h-10 w-full min-w-0 rounded-md border bg-background px-3 text-sm"
          >
            <option value="">选择仓库</option>
            {#each repositories as repo (repo.id)}<option
                value={repo.id}
                disabled={repo.availability !== "available"}>{repo.name}</option
              >{/each}
          </select>
        </div>
        {#if mode === "existing"}
          <div class="space-y-2">
            <Label for="session-worktree">工作树</Label>
            <select
              id="session-worktree"
              bind:value={worktreeId}
              disabled={busy}
              class="h-10 w-full min-w-0 rounded-md border bg-background px-3 text-sm"
            >
              <option value="">选择工作树</option>
              {#each repository?.worktrees || [] as tree (tree.id)}<option
                  value={tree.id}
                  disabled={!tree.available || tree.kind === "bare"}
                  >{tree.kind === "main" ? "主工作区 · " : ""}{tree.branch ||
                    tree.head?.slice(0, 12) ||
                    "无提交"} · {tree.path}</option
                >{/each}
              {#if createdTree && !repository?.worktrees.some((tree) => tree.id === createdTree?.id)}<option
                  value={createdTree.id}
                  >{createdTree.branch} · {createdTree.path}</option
                >{/if}
            </select>
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
                  class="max-w-full truncate text-xs"
                  title={dir}
                  onclick={() => {
                    cwd = dir;
                  }}>{dir.split("/").pop() || "/"}</Button
                >{/each}
            </div>
          {/if}
        </div>
      {/if}
      <fieldset class="space-y-2">
        <legend class="mb-2 text-sm font-medium">启动命令</legend>
        <div class="flex flex-wrap gap-2">
          {#each presets as p (p.name)}
            <Button
              type="button"
              variant={preset === p.name ? "default" : "outline"}
              aria-pressed={preset === p.name}
              onclick={() => {
                preset = p.name;
                args = "";
              }}>{p.label}</Button
            >
          {/each}
          <Button
            type="button"
            variant={custom ? "default" : "outline"}
            aria-pressed={custom}
            onclick={() => {
              preset = "custom";
              args = "";
            }}>自定义</Button
          >
        </div>
        {#if custom}
          <div class="space-y-2">
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
              支持引号和转义；直接执行程序，不展开 shell 变量或管道。需要 shell
              语法时请使用 bash -lc '命令'。
            </p>
          </div>
        {:else}
          <p class="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">
            {chosen?.cmd.join(" ")}{args ? " " + args : ""}
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
          disabled={busy || !targetReady || (!chosen && !custom)}
          >{busy ? "正在启动…" : "创建并进入 →"}</Button
        >
      </div>
    </fieldset>
  </form>
{/snippet}

{#if desktop.current}
  <Dialog.Root
    bind:open
    onOpenChange={(value) => {
      if (!value && !busy) onclose();
      else if (!value) open = true;
    }}
  >
    <Dialog.Content
      class="max-h-[90dvh] overflow-y-auto p-6 sm:max-w-xl"
      onEscapeKeydown={(event) => {
        if (busy) event.preventDefault();
      }}
      onInteractOutside={(event) => {
        if (busy) event.preventDefault();
      }}
    >
      <Dialog.Header
        ><Dialog.Title>新建会话与工作树</Dialog.Title><Dialog.Description
          >选择目录和命令。浏览器断开后，会话仍会继续。</Dialog.Description
        ></Dialog.Header
      >
      {@render fields()}
    </Dialog.Content>
  </Dialog.Root>
{:else}
  <Drawer.Root
    bind:open
    dismissible={!busy}
    onOpenChange={(value) => {
      if (!value && !busy) onclose();
      else if (!value) open = true;
    }}
  >
    <Drawer.Content class="data-[vaul-drawer-direction=bottom]:max-h-[95dvh]">
      <Drawer.Header
        ><Drawer.Title>新建会话与工作树</Drawer.Title><Drawer.Description
          >选择目录和命令，创建持久终端会话。</Drawer.Description
        ></Drawer.Header
      >
      <div
        class="overflow-y-auto px-4 pt-1 pb-[max(1rem,env(safe-area-inset-bottom))]"
      >
        {@render fields()}
      </div>
    </Drawer.Content>
  </Drawer.Root>
{/if}

<AlertDialog.Root bind:open={mkdirOpen}>
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
</AlertDialog.Root>
