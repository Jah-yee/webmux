<script lang="ts">
  import { onMount, type Snippet } from "svelte";
  import CodeNavigator from "./CodeNavigator.svelte";
  import { api } from "../api";
  import {
    CodeContextSchema,
    CodeListingSchema,
    CodeFileSchema,
    CodeChangesSchema,
    CodeDiffSchema,
    CodeLocationSchema,
    CodeCheckSchema,
    type CodeFile,
    type CodeDiff,
    type CodeChange,
    type CodeListing,
    type ReportedCwd,
  } from "../schemas";
  import type { z } from "zod";
  import { nodeId } from "../code-reference";
  import { Button } from "$lib/components/ui/button";
  import { ListButton } from "$lib/components/ui/list-button";
  import IconLayoutSidebarLeftCollapse from "~icons/tabler/layout-sidebar-left-collapse";
  import IconLayoutSidebarLeftExpand from "~icons/tabler/layout-sidebar-left-expand";
  import IconPhoto from "~icons/tabler/photo";
  import IconPictureInPictureOff from "~icons/tabler/picture-in-picture-off";
  import IconPictureInPictureOn from "~icons/tabler/picture-in-picture-on";
  import IconRefresh from "~icons/tabler/refresh";
  import IconDots from "~icons/tabler/dots";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import { EditorView } from "@codemirror/view";
  import CodeDocumentView from "./CodeDocumentView.svelte";
  import type { TextInsertion } from "../terminal-draft.svelte";
  import { popoutAvailable, popOut, type Popout } from "./codePopout";
  let {
    id,
    visible,
    connection,
    paste,
    draftEnabled,
    sessionCwd,
    reportedCwd,
  }: {
    id: string;
    visible: boolean;
    connection: string;
    paste: (text: string) => TextInsertion;
    draftEnabled: boolean;
    sessionCwd: string;
    reportedCwd: ReportedCwd | null;
  } = $props();
  type Tab = {
    key: string;
    file?: CodeFile;
    diff?: CodeDiff;
    status: string;
    sequence: number;
    comparison?: CodeDiff;
    // Narrow-screen menu content supplied by the tab's document view.
    menu?: Snippet;
  };
  let context = $state<z.infer<typeof CodeContextSchema>>();
  let listing = $state<CodeListing>();
  let changeQuery = $state("");
  let changeSearch = $state("");
  let changeGroup = $state("");
  let changesError = $state("");
  let changes = $state<z.infer<typeof CodeChangesSchema>>();
  let tabs = $state<Tab[]>([]);
  let active = $state("");
  let nav = $state("files");
  let navOpen = $state(true);
  const navMin = 120,
    navMax = 640;
  let navWidth = $state(180);
  let section = $state<HTMLElement>();
  // Popped out, the panel stays live in its own window regardless of whether
  // the opener still shows the code pane.
  let popout = $state<Popout>();
  let popping = false;
  const canPopOut = popoutAvailable();
  const shown = $derived(visible || !!popout);
  const activeMenu = $derived(tabs.find((tab) => tab.key === active)?.menu);
  // Keep the narrow menu clear of the status bar and home indicator; env()
  // is readable only through computed custom properties.
  function safeAreaPadding() {
    const style = getComputedStyle(section ?? document.documentElement);
    const inset = (name: string) =>
      parseFloat(style.getPropertyValue(name)) || 0;
    return {
      top: 8 + inset("--safe-top"),
      bottom: 8 + inset("--safe-bottom"),
      left: 8,
      right: 8,
    };
  }
  async function togglePopout() {
    if (popout) return popout.close();
    if (!section || popping) return;
    popping = true;
    try {
      popout = await popOut(section, {
        title: `代码 · ${context?.rootPath ?? id}`,
        moved(document) {
          // CodeMirror binds to its root's window; rebind moved editors.
          for (const editor of section!.querySelectorAll(".cm-editor"))
            EditorView.findFromDOM(editor as HTMLElement)?.setRoot(document);
        },
        returned() {
          popout = undefined;
        },
      });
    } catch (e) {
      error = (e as Error).message;
    } finally {
      popping = false;
    }
  }
  let inner = $state<HTMLDivElement>();
  function setNavWidth(width: number) {
    const limit = Math.min(navMax, (inner?.clientWidth ?? navMax) - 160);
    navWidth = Math.round(Math.max(navMin, Math.min(limit, width)));
  }
  function resizeStart(event: PointerEvent) {
    if (event.button !== 0) return;
    event.preventDefault();
    const handle = event.currentTarget as HTMLElement;
    handle.setPointerCapture(event.pointerId);
    const startX = event.clientX,
      startWidth = navWidth;
    const move = (e: PointerEvent) =>
      setNavWidth(startWidth + e.clientX - startX);
    const end = () => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", end);
      handle.removeEventListener("pointercancel", end);
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", end);
    handle.addEventListener("pointercancel", end);
  }
  function resizeKey(event: KeyboardEvent) {
    const step = event.shiftKey ? 64 : 16;
    if (event.key === "ArrowLeft") setNavWidth(navWidth - step);
    else if (event.key === "ArrowRight") setNavWidth(navWidth + step);
    else if (event.key === "Home") setNavWidth(navMin);
    else if (event.key === "End") setNavWidth(navMax);
    else return;
    event.preventDefault();
  }
  let includeIgnored = $state(false);
  let directory = $state("");
  let query = $state("");
  let error = $state("");
  let navStatus = $state("");
  let pending = $state(false);
  let controller: AbortController | undefined;
  const controllers = new Map<string, AbortController>();
  let navSequence = 0;
  let mounted = false;
  let checkController: AbortController | undefined;
  let checking = false;
  let navIdentity = "";
  let base = $derived(`/sessions/${id}/code`);
  function params(extra: Record<string, string> = {}) {
    return new URLSearchParams({
      contextId: context?.contextId ?? "",
      includeIgnored: String(includeIgnored),
      ...extra,
    });
  }
  function aborted(e: unknown) {
    return e instanceof DOMException && e.name === "AbortError";
  }
  async function load(append = false) {
    if (!context || (append && pending)) return;
    controller?.abort();
    controller = new AbortController();
    const request = controller;
    const seq = ++navSequence;
    pending = true;
    error = "";
    try {
      if (nav === "changes") {
        if (!context.diffAvailable) {
          changes = undefined;
          return;
        }
        changesError = "";
        const cursor = append ? (changes?.nextCursor ?? "") : "";
        const value = await api(
          `${base}/changes?${params({ cursor, q: changeQuery, group: changeGroup })}`,
          CodeChangesSchema,
          "GET",
          undefined,
          request.signal,
        );
        if (mounted && seq === navSequence) {
          changes =
            append && changes
              ? { ...value, entries: [...changes.entries, ...value.entries] }
              : value;
        }
      } else {
        const cursor = append ? (listing?.nextCursor ?? "") : "";
        const value = await api(
          `${base}/${query ? "files" : "tree"}?${params({ nodeId: nodeId(directory), q: query, cursor })}`,
          CodeListingSchema,
          "GET",
          undefined,
          request.signal,
        );
        if (mounted && seq === navSequence) {
          listing =
            append && listing
              ? { ...value, children: [...listing.children, ...value.children] }
              : value;
          navIdentity = query ? "" : nodeId(directory);
        }
      }
      if (seq === navSequence) navStatus = "";
    } catch (e) {
      if (!aborted(e) && mounted && seq === navSequence) {
        error = (e as Error).message;
        if (nav === "changes") changesError = error;
      }
    } finally {
      if (mounted && seq === navSequence) pending = false;
    }
  }
  let selectionSequence = 0;
  const beginOpen = () => ++selectionSequence;
  function activate(key: string) {
    beginOpen();
    active = key;
  }
  function install(file?: CodeFile, diff?: CodeDiff, shouldActivate = true) {
    const d = file?.document ?? diff?.new;
    if (!context || !d) return;
    const key = `${context.contextId}:${d.nodeId ?? nodeId(d.path)}:${diff ? "diff:" + diff.change.group : d.source}`;
    const existing = tabs.find((t) => t.key === key);
    if (existing) {
      if (shouldActivate) active = key;
      if (
        file
          ? file.document.contentVersion !==
            existing.file?.document.contentVersion
          : diff?.new.contentVersion !== existing.diff?.new.contentVersion ||
            diff?.old.contentVersion !== existing.diff?.old.contentVersion
      )
        existing.status = "updated";
      return;
    }
    if (tabs.length >= 20) {
      error = "最多打开 20 个标签，请先关闭一个";
      return;
    }
    tabs.push({ key, file, diff, status: "unchanged", sequence: 0 });
    if (shouldActivate) {
      active = key;
      closeOverlayNav();
    }
  }
  // Below 1024px the navigator overlays the document, so selecting a file must
  // reveal it; side-by-side layouts keep the navigator until the user collapses it.
  function closeOverlayNav() {
    if (matchMedia("(max-width:1023px)").matches) navOpen = false;
  }
  // The server resolves typed paths against the canonical code root; shell
  // reports and launch directories may be logical paths through symlinks.
  async function locate(input: string) {
    if (!context) return;
    const target = await api(
      `${base}/locate?${params({ path: input })}`,
      CodeLocationSchema,
    );
    if (target.kind === "directory") {
      directory = target.path;
      query = "";
      await load();
    } else await openFile(target.path, target.nodeId);
  }
  async function openFile(path: string, opaqueId = nodeId(path)) {
    if (!context) return;
    await openTab(
      `${context.contextId}:${opaqueId}:worktree`,
      `${base}/file?${params({ nodeId: opaqueId })}`,
      CodeFileSchema,
      (value, activate) => install(value, undefined, activate),
    );
  }
  async function openDiff(change: CodeChange) {
    if (!context) return;
    await openTab(
      `${context.contextId}:${change.nodeId ?? nodeId(change.newPath)}:diff:${change.group}`,
      `${base}/diff?${params({ changeId: change.changeId })}`,
      CodeDiffSchema,
      (value, activate) => install(undefined, value, activate),
    );
  }
  // Focuses an already open tab, or fetches and installs it. A response only
  // activates its tab when no later open request superseded it.
  async function openTab<T extends z.ZodType>(
    key: string,
    path: string,
    schema: T,
    done: (value: z.infer<T>, activate: boolean) => void,
  ) {
    const intent = beginOpen();
    if (tabs.some((t) => t.key === key)) {
      active = key;
      closeOverlayNav();
      void check();
      return;
    }
    await read(
      key,
      () => api(path, schema, "GET", undefined, controllers.get(key)?.signal),
      (value) => done(value, intent === selectionSequence),
    );
  }
  async function read<T>(
    key: string,
    request: () => Promise<T>,
    done: (value: T) => void,
  ) {
    controllers.get(key)?.abort();
    const c = new AbortController();
    controllers.set(key, c);
    error = "";
    try {
      const value = await request();
      if (mounted && !c.signal.aborted && controllers.get(key) === c)
        done(value);
    } catch (e) {
      if (mounted && !aborted(e)) error = (e as Error).message;
    } finally {
      if (controllers.get(key) === c) controllers.delete(key);
    }
  }
  function close(key: string) {
    controllers.get(key)?.abort();
    controllers.delete(key);
    tabs = tabs.filter((t) => t.key !== key);
    if (active === key) activate(tabs.at(-1)?.key ?? "");
  }
  async function refresh(tab: Tab) {
    const seq = ++tab.sequence;
    await read(
      tab.key,
      async () => {
        const signal = controllers.get(tab.key)?.signal;
        if (tab.diff) {
          const next = await api(
            `${base}/changes?${params({ nodeId: tab.diff.change.nodeId ?? nodeId(tab.diff.change.newPath) })}`,
            CodeChangesSchema,
            "GET",
            undefined,
            signal,
          );
          const c = next.entries.find(
            (c) =>
              c.group === tab.diff!.change.group &&
              c.newPath === tab.diff!.change.newPath,
          );
          if (!c) throw new Error("此差异已不存在，请从文件导航打开完整文件");
          return {
            diff: await api(
              `${base}/diff?${params({ changeId: c.changeId })}`,
              CodeDiffSchema,
              "GET",
              undefined,
              signal,
            ),
          };
        }
        if (!tab.file) throw new Error("文件不可用");
        return {
          file: await api(
            `${base}/file?${params({ nodeId: tab.file.document.nodeId ?? nodeId(tab.file.document.path), source: tab.file.document.source })}`,
            CodeFileSchema,
            "GET",
            undefined,
            signal,
          ),
        };
      },
      (value) => {
        if (!tabs.includes(tab) || tab.sequence !== seq) return;
        tab.file = value.file;
        tab.diff = value.diff;
        tab.status = "unchanged";
        tab.comparison = undefined;
      },
    );
  }
  async function check() {
    if (
      !mounted ||
      !shown ||
      (section?.ownerDocument ?? document).hidden ||
      !context ||
      checking
    )
      return;
    checking = true;
    checkController = new AbortController();
    const c = checkController;
    const documents = tabs.map((t) => ({
      tab: t,
      sid: t.file?.snapshotId ?? t.diff!.snapshotId,
      sequence: t.sequence,
    }));
    const listingVersion = listing?.contentVersion;
    const changesVersion = changes?.contentVersion;
    const identity = navIdentity;
    try {
      const r = await api(
        `${base}/check`,
        CodeCheckSchema,
        "POST",
        {
          contextId: context.contextId,
          snapshotIds: [
            ...new Set(
              documents.flatMap((d) =>
                d.tab.comparison
                  ? [d.sid, d.tab.comparison.snapshotId]
                  : [d.sid],
              ),
            ),
          ],
          directories:
            listing && nav === "files" && !query
              ? { [identity]: listingVersion }
              : {},
          changesVersion: changesVersion ?? "",
          includeIgnored,
        },
        c.signal,
      );
      if (!mounted || c.signal.aborted) return;
      for (const d of documents) {
        if (
          tabs.includes(d.tab) &&
          d.tab.sequence === d.sequence &&
          (d.tab.file?.snapshotId ?? d.tab.diff?.snapshotId) === d.sid
        )
          d.tab.status =
            d.tab.comparison &&
            r.snapshots[d.tab.comparison.snapshotId] !== "unchanged"
              ? (r.snapshots[d.tab.comparison.snapshotId] ?? "unknown")
              : (r.snapshots[d.sid] ?? "unknown");
      }
      if (
        identity === navIdentity &&
        listingVersion === listing?.contentVersion &&
        Object.values(r.directories).some((s) => s !== "unchanged")
      )
        navStatus = "目录已更新或检测失败";
      if (
        changesVersion === changes?.contentVersion &&
        r.changes !== "unchanged"
      )
        navStatus = "修改列表已更新或检测失败";
    } catch (e) {
      if (!aborted(e) && mounted) {
        for (const d of documents) {
          if (tabs.includes(d.tab) && d.tab.sequence === d.sequence)
            d.tab.status = "unknown";
        }
        navStatus = "检测失败，状态未知";
      }
    } finally {
      checking = false;
    }
  }
  onMount(() => {
    mounted = true;
    const initial = new AbortController();
    void api(
      `${base}/context`,
      CodeContextSchema,
      "GET",
      undefined,
      initial.signal,
    )
      .then((c) => {
        if (mounted) {
          context = c;
          void load();
        }
      })
      .catch((e) => {
        if (mounted && !aborted(e)) error = (e as Error).message;
      });
    const timer = window.setInterval(() => void check(), 5000);
    const visibility = () => {
      if ((section?.ownerDocument ?? document).hidden) checkController?.abort();
      else void check();
    };
    document.addEventListener("visibilitychange", visibility);
    return () => {
      mounted = false;
      initial.abort();
      controller?.abort();
      checkController?.abort();
      for (const c of controllers.values()) c.abort();
      clearInterval(timer);
      document.removeEventListener("visibilitychange", visibility);
      popout?.close();
    };
  });
  $effect(() => {
    const show = shown;
    const state = connection;
    if (mounted) {
      if (show && state === "connected") void check();
      else checkController?.abort();
    }
  });
</script>

<!-- The section is the part that moves into the pop-out window; the wrapper
     stays in the opener and holds the placeholder. -->
<div class="flex h-full min-h-0 min-w-0 flex-col">
  {#if popout}<div
      class="flex flex-1 flex-col items-center justify-center gap-3 p-4 text-sm text-muted-foreground"
    >
      <p>代码已在独立窗口中显示</p>
      <Button variant="outline" size="sm" onclick={() => popout?.close()}
        ><IconPictureInPictureOff />收回到此处</Button
      >
    </div>{/if}
  <section
    bind:this={section}
    class="code-panel flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-background"
    aria-label="代码工作区"
  >
    <div class="flex shrink-0 items-stretch border-b">
      <div class="flex shrink-0 items-center px-2 py-1">
        <Button
          variant="outline"
          size="icon-sm"
          class="relative"
          aria-label={navOpen ? "收起导航" : "文件导航"}
          aria-expanded={navOpen}
          title={navOpen
            ? "收起导航"
            : navStatus
              ? `文件导航（${navStatus}）`
              : "文件导航"}
          onclick={() => (navOpen = !navOpen)}
          >{#if navOpen}<IconLayoutSidebarLeftCollapse
            />{:else}<IconLayoutSidebarLeftExpand
            />{/if}{#if navStatus && !navOpen}<span
              class="absolute top-0.5 right-0.5 size-1.5 rounded-full bg-warning"
              aria-hidden="true"
            ></span>{/if}</Button
        >
      </div>
      <div
        class="flex min-w-0 flex-1 overflow-x-auto scrollbar-thin"
        role="tablist"
        aria-label="文件标签"
      >
        {#each tabs as tab (tab.key)}
          {@const path = tab.file?.document.path ?? tab.diff?.new.path ?? ""}
          {@const meta = `${tab.diff ? tab.diff.change.group : tab.file?.document.source}${tab.status !== "unchanged" ? " ●" : ""}`}
          {@const image = [
            tab.file?.document.mediaType,
            tab.diff?.new.mediaType,
            tab.diff?.old.mediaType,
          ].some((type) => type?.startsWith("image/"))}
          <div class="flex shrink-0 items-center border-r first:border-l">
            <ListButton
              variant="tab"
              role="tab"
              aria-selected={active === tab.key}
              aria-label={`${path} · ${meta}`}
              title={path}
              onclick={() => activate(tab.key)}
              >{#if image}<IconPhoto
                  class="mr-1 inline size-3.5 align-[-0.125em]"
                  aria-hidden="true"
                />{/if}{path.slice(path.lastIndexOf("/") + 1)} · {meta}</ListButton
            >
            <ListButton
              variant="tab-close"
              aria-label={`关闭 ${path}`}
              onclick={() => close(tab.key)}>×</ListButton
            >
          </div>{/each}
      </div>
      <div class="flex shrink-0 items-center gap-1 px-2 py-1">
        <Button
          variant="outline"
          size="icon-sm"
          aria-label="刷新全部"
          title={context?.rootPath
            ? `刷新全部（${context.rootPath}）`
            : "刷新全部"}
          disabled={pending}
          onclick={() => {
            void load();
            for (const tab of tabs) void refresh(tab);
          }}><IconRefresh /></Button
        >
        {#if canPopOut}<Button
            variant="outline"
            size="icon-sm"
            class="max-sm:hidden"
            aria-label={popout ? "收回代码窗口" : "在独立窗口中显示代码"}
            aria-pressed={!!popout}
            title={popout ? "收回代码窗口" : "在独立窗口中显示代码"}
            onclick={() => void togglePopout()}
            >{#if popout}<IconPictureInPictureOff
              />{:else}<IconPictureInPictureOn />{/if}</Button
          >{/if}
        {#if activeMenu}<DropdownMenu.Root>
            <DropdownMenu.Trigger>
              {#snippet child({ props })}<Button
                  {...props}
                  variant="outline"
                  size="icon-sm"
                  class="sm:hidden"
                  aria-label="代码菜单"
                  title="代码菜单"><IconDots aria-hidden="true" /></Button
                >{/snippet}
            </DropdownMenu.Trigger>
            <DropdownMenu.Content
              align="end"
              collisionPadding={safeAreaPadding()}
              class="w-72 max-w-[calc(100vw-1rem)]"
            >
              {@render activeMenu()}
            </DropdownMenu.Content>
          </DropdownMenu.Root>{/if}
      </div>
    </div>
    {#if error}<p
        role="alert"
        class="shrink-0 px-2 py-1 text-sm break-all text-destructive"
      >
        {error}
      </p>{/if}
    {#if context?.reason}<p
        class="shrink-0 px-2 py-1 text-xs text-muted-foreground"
      >
        {context.reason}
      </p>{/if}
    <div
      class="code-inner flex min-h-0 flex-1 overflow-hidden"
      style:--code-nav-width={`${navWidth}px`}
      bind:this={inner}
    >
      {#if navOpen}<CodeNavigator
          {context}
          {listing}
          {changes}
          {changesError}
          {pending}
          {sessionCwd}
          {reportedCwd}
          {navStatus}
          reload={() => void load()}
          changesEnabled={shown && !changesError && !navStatus}
          bind:nav
          bind:query
          bind:directory
          bind:includeIgnored
          bind:changeGroup
          bind:changeQuery
          bind:changeSearch
          load={(append) => void load(append)}
          openFile={(path, opaqueId) => void openFile(path, opaqueId)}
          {locate}
          openDiff={(change) => void openDiff(change)}
        />
        <!-- A focusable separator with aria-valuenow is an interactive widget (WAI-ARIA window splitter). -->
        <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
        <div
          class="code-nav-divider"
          role="separator"
          aria-orientation="vertical"
          aria-label="调整导航宽度"
          aria-valuemin={navMin}
          aria-valuemax={navMax}
          aria-valuenow={navWidth}
          tabindex="0"
          onpointerdown={resizeStart}
          onkeydown={resizeKey}
        ></div>{/if}
      <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        {#each tabs as tab (tab.key)}<div
            class="min-h-0 flex-1 overflow-hidden"
            class:hidden={active !== tab.key}
            role="tabpanel"
          >
            <CodeDocumentView
              {id}
              active={shown && active === tab.key}
              file={tab.file}
              diff={tab.diff}
              status={tab.status}
              {paste}
              {draftEnabled}
              refresh={() => void refresh(tab)}
              contextId={context?.contextId ?? ""}
              comparison={tab.comparison}
              onComparison={(value) => (tab.comparison = value)}
              onState={(state) => (tab.status = state)}
              {beginOpen}
              openSide={(file, intent) =>
                install(file, undefined, intent === selectionSequence)}
              bind:menu={tab.menu}
            />
          </div>{/each}
        {#if !tabs.length}<p class="p-4 text-sm text-muted-foreground">
            选择文件阅读完整内容，或从修改列表打开 Diff。
          </p>{/if}
      </div>
    </div>
  </section>
</div>
