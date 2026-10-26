<script lang="ts">
  import { errorMessage } from "../error";
  import type { z } from "zod";
  import CodeFileTree from "./CodeFileTree.svelte";
  import CodeChangesList from "./CodeChangesList.svelte";
  import {
    CodeChangesSchema,
    CodeContextSchema,
    type CodeChange,
    type CodeListing,
    type ReportedCwd,
  } from "../schemas";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import { ListButton } from "$lib/components/ui/list-button";
  import * as NativeSelect from "$lib/components/ui/native-select";
  import * as Tabs from "$lib/components/ui/tabs";
  import * as Popover from "$lib/components/ui/popover";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import IconSearch from "~icons/tabler/search";
  import IconRefresh from "~icons/tabler/refresh";
  import IconFold from "~icons/tabler/fold";
  import IconDots from "~icons/tabler/dots";
  import IconFileSearch from "~icons/tabler/file-search";
  import IconPhoto from "~icons/tabler/photo";

  let {
    context,
    listing,
    rootListing,
    fetchDirectory,
    treeRefresh,
    activePath,
    changes,
    changesError,
    pending,
    changesEnabled,
    nav = $bindable("files"),
    query = $bindable(""),
    directory = $bindable(""),
    includeIgnored = $bindable(false),
    changeGroup = $bindable(""),
    changeQuery = $bindable(""),
    changeSearch = $bindable(""),
    load,
    openFile,
    openDiff,
    locate,
    sessionCwd,
    reportedCwd,
    navStatus,
    reload,
  }: {
    context: z.infer<typeof CodeContextSchema> | undefined;
    listing: CodeListing | undefined;
    rootListing: CodeListing | undefined;
    fetchDirectory: (
      path: string,
      cursor: string,
      signal: AbortSignal,
    ) => Promise<CodeListing>;
    treeRefresh: number;
    activePath: string;
    changes: z.infer<typeof CodeChangesSchema> | undefined;
    changesError: string;
    pending: boolean;
    changesEnabled: boolean;
    nav: string;
    query: string;
    directory: string;
    includeIgnored: boolean;
    changeGroup: string;
    changeQuery: string;
    changeSearch: string;
    load: (append?: boolean) => void | Promise<void>;
    openFile: (path: string, nodeId: string) => void;
    openDiff: (change: CodeChange) => void;
    locate: (input: string) => Promise<void>;
    sessionCwd: string;
    reportedCwd: ReportedCwd | null;
    navStatus: string;
    reload: () => void;
  } = $props();
  let searchOpen = $state(false);
  let searchInput = $state("");
  let locateOpen = $state(false);
  let collapse = $state(0);
  let locateInput = $state("");
  let locateError = $state("");
  const locateBase = $derived.by(() => {
    if (reportedCwd?.remote) return null;
    if (reportedCwd) return { path: reportedCwd.path, label: "Shell 报告目录" };
    return sessionCwd
      ? { path: sessionCwd, label: "启动目录（Shell 未报告）" }
      : null;
  });
  const locateHint = $derived(
    reportedCwd?.remote
      ? `Shell 报告的是远端目录（${reportedCwd.host}），不能用于定位本机文件。`
      : locateBase
        ? `基点：${locateBase.label} ${locateBase.path}`
        : "暂无可用基点。",
  );
  let locating = $state(false);
  async function submitLocate(event: SubmitEvent) {
    event.preventDefault();
    locateError = "";
    if (!context || !locateBase || locating) return;
    locating = true;
    try {
      await locate(locateInput);
      locateInput = "";
      locateOpen = false;
    } catch (err) {
      locateError = errorMessage(err);
    } finally {
      locating = false;
    }
  }
</script>

<nav
  class="code-navigation flex min-h-0 shrink-0 flex-col overflow-hidden border-r"
  aria-label="文件与修改导航"
>
  {#if context?.rootPath}<p
      class="shrink-0 px-2 pt-1 text-xs break-all text-muted-foreground"
    >
      {context.rootPath}
    </p>{/if}
  {#if navStatus}<div class="flex shrink-0 items-center gap-1 px-2 pt-1">
      <p
        role="status"
        class="min-w-0 flex-1 truncate text-xs text-warning"
        title={navStatus}
      >
        {navStatus}
      </p>
      <Button variant="outline" size="sm" onclick={reload}>刷新导航</Button>
    </div>
  {/if}
  <Tabs.Root
    value={nav}
    onValueChange={(v) => {
      nav = v;
      if (v === "changes" || query || !rootListing) void load();
    }}
  >
    <Tabs.List class="w-full"
      ><Tabs.Trigger value="files">文件</Tabs.Trigger><Tabs.Trigger
        value="changes">修改</Tabs.Trigger
      ></Tabs.List
    >
  </Tabs.Root>
  <div class="flex min-h-0 flex-1 flex-col" class:hidden={nav !== "files"}>
    <div class="flex shrink-0 items-center gap-0.5 px-2 py-1">
      <span
        class="min-w-0 flex-1 truncate text-xs font-medium text-muted-foreground"
        >资源管理器</span
      >
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label="切换路径搜索"
        title="搜索文件路径"
        aria-expanded={searchOpen}
        onclick={() => {
          searchOpen = !searchOpen;
          if (!searchOpen && query) {
            query = "";
            void load();
          }
        }}><IconSearch aria-hidden="true" /></Button
      >
      <Popover.Root bind:open={locateOpen}>
        <Popover.Trigger>
          {#snippet child({ props })}<Button
              {...props}
              variant="ghost"
              size="icon-xs"
              aria-label="按路径打开"
              title="按路径打开"><IconFileSearch aria-hidden="true" /></Button
            >{/snippet}
        </Popover.Trigger>
        <Popover.Content align="start" class="w-72">
          <Popover.Header
            ><Popover.Title>按路径打开</Popover.Title><Popover.Description
              >{locateHint}</Popover.Description
            ></Popover.Header
          >
          <form class="mt-2 flex flex-col gap-2" onsubmit={submitLocate}>
            <Input
              size="sm"
              aria-label="定位文件路径"
              placeholder="相对路径，如 src/main.go"
              bind:value={locateInput}
              disabled={!locateBase}
            />
            <Button
              size="sm"
              variant="outline"
              type="submit"
              disabled={!locateBase || locating || !locateInput.trim()}
              >打开</Button
            >
          </form>
          {#if locateError}<p
              role="alert"
              class="mt-1 break-all text-xs text-destructive"
            >
              {locateError}
            </p>{/if}
        </Popover.Content>
      </Popover.Root>
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label="刷新文件树"
        title="刷新文件树"
        disabled={pending}
        onclick={reload}><IconRefresh aria-hidden="true" /></Button
      >
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label="全部折叠"
        title="全部折叠"
        onclick={() => {
          collapse++;
          query = "";
          searchInput = "";
          directory = "";
          void load();
        }}><IconFold aria-hidden="true" /></Button
      >
      <DropdownMenu.Root>
        <DropdownMenu.Trigger>
          {#snippet child({ props })}<Button
              {...props}
              variant="ghost"
              size="icon-xs"
              aria-label="文件导航选项"
              title="文件导航选项"><IconDots aria-hidden="true" /></Button
            >{/snippet}
        </DropdownMenu.Trigger>
        <DropdownMenu.Content align="end">
          <DropdownMenu.CheckboxItem
            closeOnSelect
            checked={includeIgnored}
            disabled={!context?.ignoreAvailable}
            onCheckedChange={(checked) => {
              includeIgnored = checked;
              void load();
            }}>显示忽略项</DropdownMenu.CheckboxItem
          >
        </DropdownMenu.Content>
      </DropdownMenu.Root>
    </div>
    {#if searchOpen}
      <form
        class="flex shrink-0 gap-1 px-2 pb-2"
        onsubmit={(event) => {
          event.preventDefault();
          query = searchInput;
          directory = "";
          void load();
        }}
      >
        <Input
          size="sm"
          aria-label="搜索文件路径"
          placeholder="文件名或路径"
          bind:value={searchInput}
        />
        <Button size="sm" variant="outline" type="submit">搜索路径</Button>
      </form>
    {/if}
    {#if includeIgnored}<p class="px-2 pb-1 text-xs text-muted-foreground">
        包含忽略项
      </p>{/if}
    <div class:hidden={!!query} class="flex min-h-0 flex-1 flex-col">
      {#key context?.contextId}
        <CodeFileTree
          listing={rootListing}
          rootPath={context?.rootPath ?? ""}
          {pending}
          {activePath}
          {directory}
          refresh={treeRefresh}
          {collapse}
          {fetchDirectory}
          {openFile}
          loadMore={() => load(true)}
        />
      {/key}
    </div>
    {#if query}
      <div class="min-h-0 flex-1 overflow-auto p-1">
        {#each listing?.children ?? [] as node (node.nodeId)}
          <ListButton
            variant="row"
            class="block w-full"
            title={node.path}
            disabled={node.kind === "special" ||
              node.kind === "boundary" ||
              node.kind === "symlink"}
            onclick={() => {
              if (node.kind === "directory") {
                directory = node.path;
                query = "";
                void load();
              } else openFile(node.path, node.nodeId);
            }}
          >
            {#if node.mediaType.startsWith("image/")}<IconPhoto
                class="mr-1 inline size-3.5 align-[-0.125em] text-muted-foreground"
                aria-hidden="true"
              />{/if}<span class="break-all"
              >{node.kind === "directory"
                ? "▸ "
                : node.kind === "boundary"
                  ? "边界 "
                  : ""}{query ? node.path : node.name}</span
            >
            {node.gitStatus}{node.ignored ? " · 忽略" : ""}{node.linkTarget
              ? ` → ${node.linkTarget}`
              : ""}
          </ListButton>
        {/each}
        {#if listing?.nextCursor}<Button
            size="sm"
            variant="outline"
            onclick={() => load(true)}>下一页</Button
          >{/if}
        {#if listing?.limited || listing?.complete === false}<p
            role="status"
            class="p-2 text-xs"
          >
            结果受限，请收窄路径；未列出全部文件。
          </p>{/if}
        {#if listing?.children.length === 0 && !pending}<p class="p-2 text-xs">
            没有匹配项
          </p>{/if}
      </div>
    {/if}
  </div>
  {#if nav === "changes"}
    {#if !context?.diffAvailable}<p class="p-2 text-xs">Git 差异不可用</p>
    {:else}
      <Label size="sm" class="mx-2 mt-2"
        >来源
        <NativeSelect.Root
          size="sm"
          aria-label="修改来源"
          class="min-w-0 flex-1"
          bind:value={changeGroup}
          onchange={() => void load()}
        >
          <NativeSelect.Option value="">全部来源</NativeSelect.Option
          ><NativeSelect.Option value="unstaged">未暂存</NativeSelect.Option
          ><NativeSelect.Option value="staged">已暂存</NativeSelect.Option
          ><NativeSelect.Option value="untracked">未跟踪</NativeSelect.Option>
        </NativeSelect.Root>
      </Label>
      <form
        class="flex gap-1 p-2"
        onsubmit={(e) => {
          e.preventDefault();
          changeQuery = changeSearch;
          void load();
        }}
      >
        <Input
          size="sm"
          aria-label="筛选修改路径"
          class="flex-1"
          bind:value={changeSearch}
          placeholder="筛选修改路径"
        />
        <Button type="submit" variant="outline" size="sm">筛选修改</Button>
      </form>
      {#if changesError}<Button
          variant="outline"
          size="sm"
          onclick={() => load()}>刷新修改列表</Button
        >{/if}
      <CodeChangesList
        entries={changes?.entries ?? []}
        total={changes?.total ?? 0}
        nextCursor={changes?.nextCursor ?? ""}
        version={`${changes?.contentVersion ?? ""}:${changeQuery}:${changeGroup}`}
        group={changeGroup}
        {pending}
        enabled={changesEnabled}
        onmore={() => load(true)}
        onopen={openDiff}
      />
    {/if}
  {/if}
</nav>
