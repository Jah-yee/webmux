<script lang="ts">
  import { api } from "../api";
  import { suggestWorktreeBranch } from "../branchName";
  import { BranchesSchema, type Repository } from "../schemas";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import * as Command from "$lib/components/ui/command";
  import { ScrollArea } from "$lib/components/ui/scroll-area";
  import type { z } from "zod";
  let {
    repository,
    path = $bindable(""),
    mode = $bindable<"new" | "existing">("new"),
    branch = $bindable(""),
    base = $bindable(""),
    ready = $bindable(false),
    disabled = false,
  }: {
    repository: Repository;
    path: string;
    mode: "new" | "existing";
    branch: string;
    base: string;
    ready: boolean;
    disabled?: boolean;
  } = $props();
  let data = $state<z.infer<typeof BranchesSchema> | null>(null);
  let error = $state("");
  let loading = $state(false);
  let pathEdited = $state(false);
  let branchEdited = $state(false);

  function suggestedBranch(avoid?: string) {
    if (!data) return "";
    return suggestWorktreeBranch({
      branchNames: data.branches.map((item) => item.name),
      worktreePaths: repository.worktrees.map((tree) => tree.path),
      avoid,
    });
  }
  function fillSuggestedBranch() {
    if (mode !== "new" || branchEdited || branch || !data) return;
    branch = suggestedBranch();
  }

  $effect(() => {
    const id = repository.id;
    let active = true;
    data = null;
    error = "";
    loading = true;
    pathEdited = false;
    branchEdited = false;
    branch = "";
    base = "";
    void api(`/repositories/${id}/branches`, BranchesSchema)
      .then((value) => {
        if (!active) return;
        data = value;
        base = value.defaultBase || "";
        fillSuggestedBranch();
      })
      .catch((err) => {
        if (active) error = (err as Error).message;
      })
      .finally(() => {
        if (active) loading = false;
      });
    return () => {
      active = false;
    };
  });
  $effect(() => {
    if (!pathEdited)
      path = data
        ? `${data.defaultParent}/${branch.split("/").at(-1) || ""}`
        : "";
  });
  $effect(() => {
    ready =
      !!data &&
      !loading &&
      !!branch &&
      !!path &&
      (mode === "existing" || !!base);
  });
</script>

<div class="space-y-4 rounded-lg border p-3">
  <fieldset class="space-y-2">
    <legend class="mb-2 text-sm font-medium">工作树分支</legend>
    <div class="flex flex-wrap gap-2">
      <Button
        type="button"
        variant={mode === "new" ? "default" : "outline"}
        aria-pressed={mode === "new"}
        onclick={() => {
          if (mode === "new") return;
          mode = "new";
          branchEdited = false;
          branch = "";
          fillSuggestedBranch();
        }}>创建新分支</Button
      >
      <Button
        type="button"
        variant={mode === "existing" ? "default" : "outline"}
        aria-pressed={mode === "existing"}
        onclick={() => {
          if (mode === "existing") return;
          mode = "existing";
          branch = "";
        }}>使用已有分支</Button
      >
    </div>
  </fieldset>
  {#if loading}<p role="status" class="text-sm">正在读取本地分支…</p>{/if}
  {#if error}<p role="alert" class="break-all text-sm text-destructive">
      {error}
    </p>{/if}
  {#if mode === "new"}
    <div class="space-y-2">
      <div class="flex items-center justify-between gap-2">
        <Label for="worktree-branch">新分支名称</Label>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!data || loading}
          onclick={() => {
            branch = suggestedBranch(branch || undefined);
          }}>换一个</Button
        >
      </div>
      <Input
        id="worktree-branch"
        bind:value={branch}
        oninput={() => {
          branchEdited = true;
        }}
        required
        placeholder="feature/my-task"
        autocomplete="off"
        autocapitalize="off"
        autocorrect="off"
        spellcheck={false}
      />
      <p class="text-xs text-muted-foreground">
        建议名避开已有本地分支和已有工作树目录段，可直接修改。
      </p>
    </div>
    <div class="space-y-2">
      <Label for="worktree-base">起始基线</Label>
      <Input
        id="worktree-base"
        bind:value={base}
        list="worktree-bases"
        required
        placeholder="本地分支、远程跟踪引用或提交 OID"
        autocomplete="off"
        autocapitalize="off"
        autocorrect="off"
        spellcheck={false}
      />
      <datalist id="worktree-bases"
        >{#each data?.bases || [] as item}<option value={item.name}
            >{item.head.slice(0, 12)}</option
          >{/each}</datalist
      >
      {#if data && !data.bases.length}<p class="text-sm text-muted-foreground">
          尚无可用基线。请先在终端创建提交；仍可在主工作区启动会话。
        </p>{/if}
    </div>
  {:else}
    <div class="space-y-2">
      <Label for="existing-branch">已有本地分支</Label>
      <Command.Root
        label="已有本地分支"
        variant="outline"
        class="h-auto min-w-0"
        filter={(value, search) =>
          value.toLowerCase().includes(search.trim().toLowerCase()) ? 1 : 0}
        disableInitialScroll
      >
        <Command.Input
          id="existing-branch"
          placeholder="搜索本地分支…"
          aria-describedby="existing-branch-selection"
          disabled={disabled || loading || !data}
          autocomplete="off"
          autocapitalize="off"
          autocorrect="off"
          spellcheck={false}
        />
        <ScrollArea
          type="always"
          class="h-48 min-w-0 overflow-hidden"
          scrollbarYClasses="bg-muted/50 [&_[data-slot=scroll-area-thumb]]:bg-muted-foreground"
        >
          <Command.List
            aria-label="本地分支列表"
            class="max-h-none overflow-visible pr-3"
          >
            <Command.Empty>
              {loading
                ? "正在读取本地分支…"
                : error
                  ? "无法读取本地分支"
                  : "没有匹配的本地分支"}
            </Command.Empty>
            {#each data?.branches || [] as item (item.name)}
              <Command.Item
                value={item.name}
                disabled={disabled || item.worktreePaths.length > 0}
                data-checked={branch === item.name}
                onSelect={() => {
                  branch = item.name;
                }}
              >
                <span class="min-w-0 flex-1 break-all">
                  {item.name}
                  {#if item.worktreePaths.length}
                    <span class="block text-muted-foreground"
                      >已被工作树占用</span
                    >
                  {/if}
                </span>
              </Command.Item>
            {/each}
          </Command.List>
        </ScrollArea>
      </Command.Root>
      <p
        id="existing-branch-selection"
        role="status"
        class="break-all text-xs text-muted-foreground"
      >
        {branch
          ? `已选择：${branch}`
          : "搜索后点击分支，或用 ↑↓ 和 Enter 选择未被占用的分支。"}
      </p>
    </div>
  {/if}
  <div class="space-y-2">
    <Label for="worktree-path">新工作树完整路径</Label>
    <Input
      id="worktree-path"
      bind:value={path}
      oninput={() => {
        pathEdited = true;
      }}
      required
      autocomplete="off"
      autocapitalize="off"
      autocorrect="off"
      spellcheck={false}
    />
    <p
      class="font-mono text-xs break-all whitespace-pre-wrap text-muted-foreground"
    >
      {path}
    </p>
    <p class="text-xs text-muted-foreground">
      默认放在仓库旁，目录名取分支最后一段；可以修改完整路径，不覆盖已有目录。
    </p>
  </div>
  <p class="text-xs text-muted-foreground">
    仅检出版本控制内容；不会 fetch、复制 .env
    或安装依赖。创建成功后即使命令启动失败，工作树与分支仍保留。
  </p>
</div>
