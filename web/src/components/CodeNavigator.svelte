<script lang="ts">
  import type { z } from "zod";
  import CodeChangesList from "./CodeChangesList.svelte";
  import {
    CodeChangesSchema,
    CodeContextSchema,
    type CodeChange,
    type CodeListing,
    type ReportedCwd,
  } from "../schemas";
  import { Button } from "$lib/components/ui/button";
  import { Checkbox } from "$lib/components/ui/checkbox";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import { ListButton } from "$lib/components/ui/list-button";
  import * as NativeSelect from "$lib/components/ui/native-select";
  import * as Tabs from "$lib/components/ui/tabs";
  import IconPhoto from "~icons/tabler/photo";

  let {
    context,
    listing,
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
    load: (append?: boolean) => void;
    openFile: (path: string, nodeId: string) => void;
    openDiff: (change: CodeChange) => void;
    locate: (input: string) => Promise<void>;
    sessionCwd: string;
    reportedCwd: ReportedCwd | null;
    navStatus: string;
    reload: () => void;
  } = $props();
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
    } catch (err) {
      locateError = (err as Error).message;
    } finally {
      locating = false;
    }
  }
</script>

<nav
  class="code-navigation flex min-h-0 shrink-0 flex-col overflow-hidden border-r"
  aria-label="文件与修改导航"
>
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
  {:else}<p
      class="shrink-0 truncate px-2 pt-1 text-left text-xs text-muted-foreground [direction:rtl]"
      title={context?.rootPath}
    >
      <bdi>{context?.rootPath ?? "读取代码目录…"}</bdi>
    </p>{/if}
  <Tabs.Root
    value={nav}
    onValueChange={(v) => {
      nav = v;
      load();
    }}
  >
    <Tabs.List class="w-full"
      ><Tabs.Trigger value="files">文件</Tabs.Trigger><Tabs.Trigger
        value="changes">修改</Tabs.Trigger
      ></Tabs.List
    >
  </Tabs.Root>
  {#if nav === "files"}
    <form
      class="flex flex-col gap-1 p-2"
      onsubmit={(e) => {
        e.preventDefault();
        load();
      }}
    >
      <Input
        size="sm"
        aria-label="搜索文件路径"
        placeholder="文件名或路径"
        bind:value={query}
      />
      <Button size="sm" variant="outline" type="submit">搜索路径</Button>
      <Label size="sm"
        ><Checkbox
          checked={includeIgnored}
          disabled={!context?.ignoreAvailable}
          onCheckedChange={(checked) => {
            includeIgnored = checked;
            load();
          }}
        />显示忽略项</Label
      >
    </form>
    <details class="px-2 pb-2 text-xs">
      <summary class="cursor-pointer text-muted-foreground">按路径打开</summary>
      <form class="mt-1 flex flex-col gap-1" onsubmit={submitLocate}>
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
          disabled={!locateBase || locating || !locateInput.trim()}>打开</Button
        >
      </form>
      <p class="mt-1 break-all text-muted-foreground">{locateHint}</p>
      {#if locateError}<p role="alert" class="mt-1 break-all text-destructive">
          {locateError}
        </p>{/if}
    </details>
    <div class="flex flex-wrap gap-1 p-2 text-xs" aria-label="路径导航">
      <ListButton
        onclick={() => {
          directory = "";
          query = "";
          load();
        }}>根目录</ListButton
      >
      {#each directory.split("/").filter(Boolean) as part, i}<span>/</span
        ><ListButton
          class="break-all"
          onclick={() => {
            directory = directory
              .split("/")
              .slice(0, i + 1)
              .join("/");
            query = "";
            load();
          }}>{part}</ListButton
        >{/each}
    </div>
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
              load();
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
  {:else if !context?.diffAvailable}<p class="p-2 text-xs">Git 差异不可用</p>
  {:else}
    <Label size="sm" class="mx-2 mt-2"
      >来源
      <NativeSelect.Root
        size="sm"
        aria-label="修改来源"
        class="min-w-0 flex-1"
        bind:value={changeGroup}
        onchange={() => load()}
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
        load();
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
    {#if changesError}<Button variant="outline" size="sm" onclick={() => load()}
        >刷新修改列表</Button
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
</nav>
