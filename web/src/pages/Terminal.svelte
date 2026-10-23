<script lang="ts">
  import { onMount, tick, untrack, type Component } from "svelte";
  import {
    IconArrowLeft,
    IconArrowDown,
    IconMaximize,
    IconMinimize,
    IconUpload,
  } from "@tabler/icons-svelte";
  import { apiBuffer, navigate, sessionStatusText } from "../api";
  import { useTerminal } from "../hooks/terminal.svelte";
  import { useViewport } from "../hooks/viewport.svelte";
  import { useUpload } from "../hooks/upload.svelte";
  import { hasTouchInput, isIOS } from "../touchInput";
  import { Button } from "$lib/components/ui/button";
  import { Textarea } from "$lib/components/ui/textarea";
  import { Label } from "$lib/components/ui/label";
  import { Slider } from "$lib/components/ui/slider";
  import * as Dialog from "$lib/components/ui/dialog";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import * as Resizable from "$lib/components/ui/resizable";
  import SoftKeys from "../components/SoftKeys.svelte";
  import QuickCommandMenu from "../components/QuickCommandMenu.svelte";
  import { quickCommands } from "../quickCommands.svelte";
  import type {
    QuickCommand,
    ReportedCwd,
    SubmissionProgress,
  } from "../schemas";
  import { newSubmissionId } from "../terminalProtocol";
  import { browserDraftStorage } from "../draftStorage";
  import CloseSessionDialog from "../components/CloseSessionDialog.svelte";
  import ScratchPanel from "../components/ScratchPanel.svelte";
  import TerminalInputComposer from "../components/TerminalInputComposer.svelte";
  import type {
    DraftOperation,
    TerminalDrafts,
    TextInsertion,
  } from "../terminal-draft.svelte";

  let {
    id,
    drafts,
    onUploadNotice,
  }: {
    id: string;
    drafts: TerminalDrafts;
    onUploadNotice: (message: string) => void;
  } = $props();
  const draft = untrack(() => drafts.get(id));
  let composer = $state<TerminalInputComposer>();
  let composerSize = $state(25);
  let terminalHeight = $state(0);
  let sending = false;
  let pasteOperation: DraftOperation | undefined;
  let container: HTMLDivElement;
  let fileInput: HTMLInputElement;
  let error = $state("");
  let notice = $state("");
  let closing = $state(false);
  let pasteOpen = $state(false);
  let pasteText = $state("");
  let copyOpen = $state(false);
  let copyText = $state("");
  let settingsOpen = $state(false);
  let scratchOpen = $state(false);
  let forceSoftKeys = $state(false);
  let fullscreen = $state(false);
  let fullscreenBusy = $state(false);
  let ownsFullscreen = false;
  let mounted = false;
  let codeShown = $state(false);
  let desktop = $state(matchMedia("(min-width:1024px)").matches);
  let CodePanel = $state<
    Component<{
      id: string;
      visible: boolean;
      connection: string;
      paste: (text: string) => TextInsertion;
      draftEnabled: boolean;
      sessionCwd: string;
      reportedCwd: ReportedCwd | null;
    }>
  >();
  let loadingCode = false;
  const terminalVisible = () => desktop || !codeShown;
  $effect(() => {
    if (codeShown && !CodePanel && !loadingCode) {
      loadingCode = true;
      void import("../components/CodePanel.svelte")
        .then((m) => {
          if (mounted) CodePanel = m.default;
        })
        .catch((e) => {
          error = (e as Error).message;
        })
        .finally(() => (loadingCode = false));
    }
    const showing = terminalVisible();
    if (showing) void tick().then(() => t.fit());
  });
  function insertReference(text: string) {
    const result = insertText(text);
    if (result === "terminal" && !desktop) {
      codeShown = false;
      void tick().then(() => t.fit());
    }
    return result;
  }
  function insertText(
    text: string,
    operation = drafts.begin(draft),
  ): TextInsertion {
    if (!mounted) return "failed";
    return drafts.insert(draft, text, operation, t.paste);
  }
  function controlNotice() {
    error = "终端控制需先退出草稿模式；草稿框内可正常复制、选择和编辑。";
  }
  function softInput(input: string) {
    if (draft.enabled) composer?.editKey(input);
    else t.send(input);
  }
  function focusInput() {
    if (draft.enabled) composer?.focus();
    else t.focus();
  }
  const draftMessages: Partial<Record<SubmissionProgress, string>> = {
    failed: "草稿未写入终端，可修改后重新发送。",
    missing:
      "服务端没有这次提交的记录：可能未送达，或记录已过期。请检查终端后决定是否重新发送。",
    partial: "草稿只有部分写入终端，提交键未写入；请检查终端后再处理草稿。",
  };
  // Draft text stays locked until the server reports the outcome.
  function reportDraft(id: string, announce: boolean) {
    return (state: SubmissionProgress) => {
      drafts.updateSubmission(draft, id, state);
      if (!mounted) return;
      if (state === "written") {
        error = "";
        if (announce) notice = "上次提交的草稿已写入终端。";
      } else if (draftMessages[state]) error = draftMessages[state];
    };
  }
  // `resend` repeats an unconfirmed submission under the same ID, which the
  // server writes at most once.
  function sendDraft(resend = false) {
    if (sending || !draft.enabled || !draft.text || !drafts.valid(draft))
      return;
    const pending = draft.submission;
    if (pending && (!resend || pending.state !== "unknown")) return;
    sending = true;
    try {
      const id = pending?.id ?? newSubmissionId();
      if (!pending) drafts.beginSubmission(draft, id);
      notice = "";
      if (t.submit(id, draft.text, true, reportDraft(id, false))) error = "";
      else {
        if (!pending) drafts.releaseSubmission(draft);
        error = "终端未就绪，草稿未发送；请恢复连接后手动重试。";
      }
    } finally {
      sending = false;
    }
  }
  function releaseDraftSubmission() {
    if (draft.submission) t.untrack(draft.submission.id);
    drafts.releaseSubmission(draft);
    error = "";
  }
  const quickMessages: Partial<Record<SubmissionProgress, string>> = {
    failed: "快捷指令未写入终端；请检查终端后重试。",
    missing: "快捷指令未送达服务端；请检查终端后重试。",
    partial: "快捷指令正文已写入，但回车未写入；请检查终端后手动提交。",
    unknown: "连接中断，快捷指令结果未知；重连后自动查询。",
  };
  function runQuickCommand(cmd: QuickCommand) {
    t.resetModifiers();
    if (cmd.enter && drafts.destination(draft) === "terminal") {
      // Text and Enter form one acknowledged submission.
      const sent = t.submit(newSubmissionId(), cmd.command, true, (state) => {
        if (!mounted) return;
        if (quickMessages[state]) error = quickMessages[state];
        else if (state === "written" && error === quickMessages.unknown)
          error = "";
      });
      error = sent ? "" : "终端未就绪，快捷指令未发送；请恢复连接后重试。";
      return;
    }
    const result = insertText(cmd.command);
    if (result === "failed")
      error = "终端未就绪，快捷指令未发送；请恢复连接后重试。";
    else error = "";
  }
  const viewport = useViewport();
  const t = useTerminal(
    () => container,
    untrack(() => id),
    {
      paste: () => {
        void paste();
      },
      copySelection: (text) => {
        void copyToClipboard(text);
      },
    },
    terminalVisible,
    {
      draftEnabled: () => draft.enabled,
      focusDraft: () => composer?.focus(),
      key: (event) => composer?.terminalKey(event),
      insertText: (text) => {
        insertText(text);
      },
    },
  );
  const sendUnavailable = $derived(
    t.connection === "missing"
      ? "会话不存在，无法发送"
      : t.exitCode !== null || t.connection === "exited"
        ? "会话已退出，可编辑或复制草稿"
        : !t.ready
          ? "终端未就绪，草稿不会自动发送"
          : "",
  );
  const quickUnavailable = $derived(
    t.exitCode !== null || t.connection === "exited"
      ? "会话已退出，无法发送"
      : !t.ready
        ? "终端未就绪，暂不能发送"
        : "",
  );
  $effect(() => {
    if (t.connection === "missing") drafts.remove(id);
  });
  $effect(() => {
    const generation = t.info?.generation;
    if (generation) drafts.bindGeneration(draft, generation);
  });
  const draftStorage = browserDraftStorage();
  const draftSaved = $derived(
    draft.persisted && draftStorage.enabled
      ? "已保存在本机"
      : draftStorage.enabled
        ? "草稿过大，仅保存在当前页面"
        : "仅当前页面，刷新后丢失",
  );
  const scratchSaved = $derived(
    draft.scratchPersisted && draftStorage.enabled
      ? "已保存在本机"
      : draftStorage.enabled
        ? "暂存超出本机保存限制，仅当前页面"
        : "仅当前页面，刷新后丢失",
  );
  function showScratch() {
    draft.scratchUpdatedWhileHidden = false;
    scratchOpen = true;
  }
  const uploads = useUpload(
    () => container,
    untrack(() => id),
    t,
    (message) => {
      if (mounted) error = message;
      else if (message) onUploadNotice(`会话 ${id}：${message}`);
    },
    {
      begin: () => drafts.begin(draft),
      destination: (operation) => drafts.destination(draft, operation),
      insert: insertText,
    },
  );

  onMount(() => {
    mounted = true;
    // A submission restored from storage may have been written before the
    // page went away; ask the server instead of guessing.
    if (draft.submission)
      t.track(draft.submission.id, reportDraft(draft.submission.id, true));
    const media = matchMedia("(min-width:1024px)");
    const layout = () => {
      desktop = media.matches;
    };
    media.addEventListener("change", layout);
    const changed = () => {
      fullscreen = Boolean(document.fullscreenElement);
      if (!fullscreen) ownsFullscreen = false;
    };
    changed();
    document.addEventListener("fullscreenchange", changed);
    return () => {
      media.removeEventListener("change", layout);
      mounted = false;
      document.removeEventListener("fullscreenchange", changed);
      if (
        ownsFullscreen &&
        document.fullscreenElement === document.documentElement
      ) {
        ownsFullscreen = false;
        void document.exitFullscreen().catch((err) => {
          console.error("离开终端时退出全屏失败", err);
        });
      }
    };
  });

  async function toggleFullscreen() {
    if (fullscreenBusy) return;
    error = "";
    if (
      !document.fullscreenElement &&
      (!document.fullscreenEnabled ||
        typeof document.documentElement.requestFullscreen !== "function")
    ) {
      error =
        "当前浏览器环境未开放网页全屏；可尝试“添加到主屏幕”以独立模式打开，减少浏览器底栏。";
      return;
    }
    fullscreenBusy = true;
    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen();
      } else {
        ownsFullscreen = true;
        // Fullscreen the document so body-level menus and dialogs remain visible.
        await document.documentElement.requestFullscreen({
          navigationUI: "hide",
        });
        if (
          !mounted &&
          ownsFullscreen &&
          document.fullscreenElement === document.documentElement
        ) {
          ownsFullscreen = false;
          await document.exitFullscreen();
        }
      }
    } catch (err) {
      if (!document.fullscreenElement) ownsFullscreen = false;
      if (mounted)
        error =
          "切换全屏失败：" + (err instanceof Error ? err.message : String(err));
      else console.error("切换全屏失败", err);
    } finally {
      if (mounted) fullscreenBusy = false;
    }
  }

  async function copyToClipboard(text: string) {
    // Application OSC 52 copies have no user gesture. writeText still succeeds
    // in a secure context when clipboard-write is allowed; otherwise the
    // existing text box is the manual copy fallback.
    if (window.isSecureContext && navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text);
        return;
      } catch {
        // Permission denied or a missing transient activation.
      }
    }
    if (!mounted) return;
    copyText = text;
    copyOpen = true;
  }
  async function copy() {
    try {
      await copyToClipboard(new TextDecoder().decode(await apiBuffer(id)));
    } catch (err) {
      error = (err as Error).message;
    }
  }

  async function paste() {
    const operation = drafts.begin(draft);
    try {
      if (
        matchMedia("(pointer: fine)").matches &&
        typeof navigator.clipboard?.read === "function"
      ) {
        const items = await navigator.clipboard.read();
        if (!mounted) return;
        const files: File[] = [];
        for (const [index, item] of items.entries()) {
          const type = item.types.find((type) => !type.startsWith("text/"));
          if (!type) continue;
          const blob = await item.getType(type);
          const extension =
            type === "image/jpeg"
              ? "jpg"
              : type.split("/")[1].replace(/[^a-z0-9]/gi, "") || "bin";
          files.push(
            new File([blob], `clipboard-${Date.now()}-${index}.${extension}`, {
              type,
            }),
          );
        }
        if (files.length) {
          if (mounted) await uploads.uploadFiles(files, operation);
          return;
        }
        const textItem = items.find((item) =>
          item.types.includes("text/plain"),
        );
        if (textItem) {
          const text = await (await textItem.getType("text/plain")).text();
          if (mounted) insertText(text, operation);
          return;
        }
      }
      const text = await navigator.clipboard.readText();
      if (mounted) insertText(text, operation);
    } catch {
      if (mounted) {
        if (drafts.destination(draft, operation) === "draft") {
          error = draft.enabled
            ? "无法读取剪贴板，请在现有草稿框内手动粘贴。"
            : "无法读取剪贴板，请重新开启草稿模式后在草稿框内手动粘贴。";
          if (draft.enabled && !hasTouchInput()) composer?.focus();
        } else if (drafts.destination(draft, operation) !== "failed") {
          pasteOperation = operation;
          pasteText = "";
          pasteOpen = true;
        }
      }
    }
  }
  async function focusTerminal(event: Event) {
    event.preventDefault();
    if (hasTouchInput()) return;
    await tick();
    focusInput();
  }
</script>

{#snippet quickMenu(side: "top" | "bottom")}
  <QuickCommandMenu
    commands={quickCommands.list}
    target={draft.enabled ? "draft" : "terminal"}
    unavailable={quickUnavailable}
    disabled={t.connection === "missing"}
    {side}
    run={runQuickCommand}
    manage={() => navigate(`/settings?session=${id}#quick-commands`)}
    onCloseAutoFocus={focusTerminal}
  />
{/snippet}

<div
  class="terminal-page"
  class:force-soft-keys={forceSoftKeys}
  class:ios={isIOS()}
  style:height={`${viewport.height}px`}
>
  <header
    class="app-header terminal-header flex h-(--app-header-height) shrink-0 items-center gap-2 border-b bg-card px-2"
  >
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label="返回会话列表"
      onclick={() => navigate("/")}
    >
      <IconArrowLeft class="size-4" aria-hidden="true" />
    </Button>
    <span class="min-w-0 flex-1 truncate text-sm font-medium"
      >{t.info?.title || "终端"}</span
    >
    {#if t.info}<span
        class={`status-dot status-${t.info.status}`}
        class:status-failed={t.info.status === "exited" &&
          t.info.exitCode !== null &&
          t.info.exitCode !== 0}
        role="img"
        aria-label={sessionStatusText(t.info)}
        title={sessionStatusText(t.info)}
      ></span>{/if}
    <span class="text-xs text-muted-foreground" role="status"
      >{t.connection === "connected"
        ? "已连接"
        : t.connection === "exited"
          ? "已退出"
          : t.connection === "missing"
            ? "不存在"
            : "重连中"}</span
    >
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label={fullscreen ? "退出全屏" : "全屏"}
      title={fullscreen ? "退出全屏" : "全屏"}
      aria-pressed={fullscreen}
      aria-busy={fullscreenBusy}
      disabled={fullscreenBusy}
      onclick={() => {
        void toggleFullscreen();
      }}
    >
      {#if fullscreen}<IconMinimize class="size-4" />
      {:else}<IconMaximize class="size-4" />{/if}
    </Button>
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label="上传文件"
      title="上传文件"
      disabled={uploads.progress !== null ||
        (!draft.enabled && t.connection !== "connected") ||
        t.connection === "missing" ||
        t.exitCode !== null}
      onclick={() => fileInput.click()}
      ><IconUpload class="size-4" aria-hidden="true" /></Button
    >
    <input
      bind:this={fileInput}
      type="file"
      class="hidden"
      aria-label="选择上传文件"
      onchange={() => {
        const files = Array.from(fileInput.files ?? []);
        fileInput.value = "";
        void uploads.uploadFiles(files);
      }}
    />
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label="滚动到最新位置"
      title="滚动到最新位置"
      disabled={t.restoring}
      onclick={() => {
        t.scrollToBottom();
        if (!desktop) codeShown = false;
      }}><IconArrowDown class="size-4" aria-hidden="true" /></Button
    >
    <div class="quick-commands-header flex">{@render quickMenu("bottom")}</div>
    <Button
      variant={draft.enabled ? "default" : "outline"}
      size="sm"
      aria-label={draft.scratch.length
        ? `草稿模式，${draft.scratch.length} 条暂存`
        : "草稿模式"}
      aria-pressed={draft.enabled}
      disabled={t.connection === "missing"}
      title={draft.enabled
        ? "关闭只隐藏草稿，不发送或清空"
        : draft.scratchUpdatedWhileHidden
          ? `暂存已更新，开启查看；${scratchSaved}`
          : draft.scratch.length
            ? `开启查看草稿与 ${draft.scratch.length} 条暂存；${scratchSaved}`
            : draft.updatedWhileHidden
              ? `隐藏草稿已更新，开启查看；${draftSaved}`
              : draft.text
                ? `开启查看保留的草稿；${draftSaved}`
                : "开启草稿模式"}
      onclick={() => {
        drafts.setEnabled(draft, !draft.enabled);
        t.resetModifiers();
        if (draft.enabled && !hasTouchInput()) {
          void tick().then(() => composer?.focus());
        }
      }}
      >草稿{#if !draft.enabled && draft.scratch.length}
        ·{draft.scratch
          .length}{:else if !draft.enabled && (draft.text || draft.updatedWhileHidden)}
        ·{/if}</Button
    >
    <Button
      variant="outline"
      size="sm"
      aria-pressed={codeShown}
      onclick={() => (codeShown = !codeShown)}
      >{codeShown ? (desktop ? "折叠代码" : "终端") : "代码"}</Button
    >
    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        {#snippet child({ props })}<Button
            {...props}
            variant="ghost"
            size="icon-sm"
            aria-label="终端菜单">⋯</Button
          >{/snippet}
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end">
        <DropdownMenu.Item
          onSelect={() => {
            void copy();
          }}
          >{t.historyTruncated
            ? "复制全部缓冲（更早输出已被覆盖）"
            : "复制全部缓冲"}</DropdownMenu.Item
        >
        <DropdownMenu.Item
          onSelect={() => {
            void paste();
          }}>粘贴</DropdownMenu.Item
        >
        <DropdownMenu.Item onSelect={showScratch}
          >暂存{draft.scratch.length
            ? ` (${draft.scratch.length})`
            : ""}</DropdownMenu.Item
        >
        <DropdownMenu.Item onSelect={() => t.clear()}>清屏</DropdownMenu.Item>
        <DropdownMenu.Item onSelect={() => t.resync()}
          >重新同步</DropdownMenu.Item
        >
        <DropdownMenu.Item
          onSelect={() => {
            settingsOpen = true;
          }}>字体大小</DropdownMenu.Item
        >
        <DropdownMenu.CheckboxItem bind:checked={forceSoftKeys}>
          强制显示底部工具栏
        </DropdownMenu.CheckboxItem>
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          variant="destructive"
          disabled={!t.info}
          onSelect={() => {
            closing = true;
          }}>{t.exitCode !== null ? "移除会话" : "关闭会话"}</DropdownMenu.Item
        >
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  </header>
  {#if !draft.enabled && !draft.scratchUpdatedWhileHidden && (draft.text || draft.updatedWhileHidden || draft.scratch.length)}
    <p class="sr-only" role="status">
      {#if draft.updatedWhileHidden}隐藏草稿已更新{:else if draft.text}有保留的隐藏草稿{/if}
      {#if draft.scratch.length}
        · 共 {draft.scratch.length} 条暂存{/if}
      · 开启草稿模式查看；{draftSaved}
    </p>
  {/if}
  {#if !draft.enabled && draft.scratchUpdatedWhileHidden}
    <div
      class="flex shrink-0 flex-wrap items-center gap-2 border-b bg-muted px-4 py-1.5 text-sm"
      role="status"
    >
      <span class="min-w-0 flex-1"
        >{#if draft.updatedWhileHidden}隐藏草稿已更新 ·
        {/if}暂存已更新 · 共 {draft.scratch.length} 条，不会自动发送</span
      >
      <Button variant="outline" size="sm" onclick={showScratch}>查看暂存</Button
      >
      <Button
        variant="ghost"
        size="sm"
        onclick={() => (draft.scratchUpdatedWhileHidden = false)}>知道了</Button
      >
    </div>
  {/if}
  {#if uploads.progress}
    <div
      class="shrink-0 space-y-1 border-b bg-muted px-3 py-2"
      aria-label="文件上传状态"
    >
      <div class="flex items-center gap-3 text-xs" role="status">
        <span class="min-w-0 flex-1 truncate"
          >{uploads.progress.waiting
            ? "文件已上传，等待终端恢复："
            : "正在上传 "}{uploads.progress.name}{uploads.progress.total > 1
            ? `（${uploads.progress.index}/${uploads.progress.total}）`
            : ""}</span
        >
        <span>{uploads.progress.percent}%</span>
        <Button variant="ghost" size="sm" onclick={() => uploads.cancel()}
          >取消</Button
        >
      </div>
      <progress
        aria-label="文件上传进度"
        max={100}
        value={uploads.progress.percent}
        class="block h-1 w-full accent-primary"
      ></progress>
    </div>
  {/if}
  {#if notice}<div
      class="flex shrink-0 items-center gap-2 border-b bg-muted px-4 py-1.5 text-sm"
      role="status"
    >
      <span class="min-w-0 flex-1">{notice}</span>
      <Button variant="ghost" size="sm" onclick={() => (notice = "")}
        >知道了</Button
      >
    </div>{/if}
  {#if t.control && !t.controlling && t.connection === "connected" && t.exitCode === null}
    <div
      class="flex shrink-0 items-center gap-2 border-b bg-muted px-3 py-1 text-xs"
      role="status"
      aria-label="终端控制"
    >
      <span class="min-w-0 flex-1 truncate"
        >{t.control.controller
          ? `观察中 · ${t.control.controller.label || "另一设备"}控制输入与尺寸`
          : "观察中 · 当前没有控制端"}（{t.control.cols}×{t.control
          .rows}）</span
      >
      <Button variant="outline" size="sm" onclick={() => t.claim()}
        >接管控制</Button
      >
    </div>
  {/if}
  {#if error || t.error}<p
      role="alert"
      class="shrink-0 bg-destructive/10 px-4 py-2 text-sm text-destructive"
    >
      {error || t.error}
    </p>{/if}
  {#if t.connection === "missing"}<div class="notice-warning">
      会话不存在或已被移除。<Button
        variant="outline"
        size="sm"
        onclick={() => navigate("/")}>返回列表</Button
      >
    </div>{/if}
  {#if t.exitCode !== null}
    <div
      class="flex shrink-0 flex-wrap items-center gap-3 border-b bg-muted px-4 py-2 text-sm"
      role="status"
    >
      <span>进程已退出（code {t.exitCode}）</span>
      <Button variant="outline" size="sm" onclick={() => navigate("/")}
        >返回列表</Button
      >
      <Button
        variant="outline"
        size="sm"
        onclick={() => {
          closing = true;
        }}>移除会话</Button
      >
    </div>
  {/if}
  <div
    class="terminal-workspace"
    class:code-shown={codeShown}
    class:mobile-code={!desktop && codeShown}
  >
    <Resizable.PaneGroup direction="horizontal">
      <Resizable.Pane defaultSize={50} minSize={25} class="terminal-pane">
        <div class="terminal-column" bind:clientHeight={terminalHeight}>
          <Resizable.PaneGroup direction="vertical">
            <Resizable.Pane
              defaultSize={100 - composerSize}
              minSize={20}
              order={0}
            >
              <div class="terminal-column">
                <div class="terminal-area">
                  <div class="terminal-host" bind:this={container}></div>
                  {#if uploads.dragging}
                    <div
                      class="pointer-events-none absolute inset-0 z-20 flex items-center justify-center border-2 border-dashed border-primary bg-background/90"
                      role="status"
                    >
                      <p class="text-sm">
                        {uploads.progress
                          ? "请等待当前上传完成"
                          : "松开以上传文件到临时目录"}
                      </p>
                    </div>
                  {/if}
                  {#if t.selection}
                    <div
                      class="absolute right-2 top-2 z-10 flex items-center gap-1 rounded-md border bg-popover p-1 shadow-md"
                      role="toolbar"
                      aria-label="终端文字选择"
                    >
                      <Button size="sm" onclick={() => t.copySelection()}
                        >复制选中</Button
                      >
                      <Button
                        variant="ghost"
                        size="sm"
                        onclick={() => t.clearSelection()}>取消选择</Button
                      >
                    </div>
                  {/if}
                  {#if t.restoring && t.connection !== "missing"}<div
                      class="restore-overlay"
                      role="status"
                    >
                      {t.connection === "connected" ? "正在恢复…" : "正在连接…"}
                    </div>{/if}
                </div>
                <SoftKeys
                  send={softInput}
                  modifiers={t.modifiers}
                  toggle={(key) => {
                    if (draft.enabled) controlNotice();
                    else t.toggleModifier(key);
                  }}
                  paste={() => {
                    void paste();
                  }}
                  keyboard={() => {
                    if (draft.enabled) composer?.keyboard();
                    else t.keyboard();
                  }}
                >
                  {#snippet trailing()}{@render quickMenu("top")}{/snippet}
                </SoftKeys>
              </div>
            </Resizable.Pane>
            {#if draft.enabled}
              <Resizable.Handle
                withHandle
                class="draft-divider"
                aria-label="调整草稿面板高度"
              />
              <Resizable.Pane
                order={1}
                defaultSize={composerSize}
                minSize={Math.min(
                  80,
                  (160 / Math.max(1, (terminalHeight || 500) - 8)) * 100,
                )}
                onResize={(size) => (composerSize = size)}
                class="draft-pane"
              >
                <TerminalInputComposer
                  bind:this={composer}
                  {draft}
                  {drafts}
                  unavailable={sendUnavailable}
                  saved={draftSaved}
                  target={t.info?.title || id}
                  openScratch={showScratch}
                  send={() => sendDraft()}
                  resend={() => sendDraft(true)}
                  release={releaseDraftSubmission}
                  filePaste={uploads.filePaste}
                  {controlNotice}
                />
              </Resizable.Pane>
            {/if}
          </Resizable.PaneGroup>
        </div>
      </Resizable.Pane>
      <Resizable.Handle
        withHandle
        class="code-divider"
        aria-label="调整终端与代码宽度"
      />
      <Resizable.Pane defaultSize={50} minSize={25} class="code-pane">
        {#if CodePanel}<CodePanel
            {id}
            visible={codeShown}
            connection={t.connection}
            paste={insertReference}
            draftEnabled={draft.enabled}
            sessionCwd={t.info?.cwd ?? ""}
            reportedCwd={t.info?.reportedCwd ?? null}
          />{/if}
      </Resizable.Pane>
    </Resizable.PaneGroup>
  </div>
</div>

<Dialog.Root bind:open={pasteOpen}>
  <Dialog.Content class="sm:max-w-lg" onCloseAutoFocus={focusTerminal}>
    <Dialog.Header
      ><Dialog.Title>粘贴到终端</Dialog.Title><Dialog.Description
        >当前环境无法直接读取剪贴板，请在下方粘贴。多行文本遵循应用的 bracketed
        paste 模式。</Dialog.Description
      ></Dialog.Header
    >
    <Textarea
      aria-label="粘贴内容"
      onpaste={(event) => {
        if (uploads.filePaste(event)) pasteOpen = false;
      }}
      bind:value={pasteText}
      class="max-h-[50dvh] min-h-40 overflow-y-auto font-mono"
      autocapitalize="off"
      autocorrect="off"
      spellcheck={false}
    />
    <Dialog.Footer
      ><Button
        variant="outline"
        onclick={() => {
          pasteOpen = false;
        }}>取消</Button
      ><Button
        onclick={() => {
          insertText(pasteText, pasteOperation);
          pasteOpen = false;
        }}>发送</Button
      ></Dialog.Footer
    >
  </Dialog.Content>
</Dialog.Root>
<Dialog.Root bind:open={copyOpen}>
  <Dialog.Content class="sm:max-w-lg" onCloseAutoFocus={focusTerminal}>
    <Dialog.Header
      ><Dialog.Title>复制终端文字</Dialog.Title><Dialog.Description
        >当前环境不支持剪贴板访问，请长按或全选复制。</Dialog.Description
      ></Dialog.Header
    >
    <Textarea
      aria-label="复制内容"
      readonly
      value={copyText}
      class="max-h-[50dvh] min-h-40 overflow-y-auto font-mono"
    />
    <Dialog.Footer
      ><Button
        onclick={() => {
          copyOpen = false;
        }}>关闭</Button
      ></Dialog.Footer
    >
  </Dialog.Content>
</Dialog.Root>
<Dialog.Root bind:open={settingsOpen}>
  <Dialog.Content onCloseAutoFocus={focusTerminal}>
    <Dialog.Header
      ><Dialog.Title>字体大小</Dialog.Title><Dialog.Description
        >设置保存在此设备上，也可以用双指捏合调整。</Dialog.Description
      ></Dialog.Header
    >
    <Label for="terminal-font">{t.fontSize}px</Label>
    <Slider
      id="terminal-font"
      type="single"
      min={12}
      max={20}
      step={1}
      value={t.fontSize}
      onValueChange={t.setFontSize}
      aria-label="字体大小"
    />
  </Dialog.Content>
</Dialog.Root>
{#if closing && t.info}<CloseSessionDialog
    session={t.info}
    onclose={() => {
      closing = false;
    }}
    onremoved={() => {
      drafts.remove(id);
      navigate("/");
    }}
  />{/if}
<ScratchPanel bind:open={scratchOpen} {draft} {drafts} saved={scratchSaved} />
