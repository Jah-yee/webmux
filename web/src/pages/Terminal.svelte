<script lang="ts">
  import { onMount, tick, untrack } from "svelte";
  import { apiBuffer, navigate } from "../api";
  import { clipboardFiles } from "../clipboard";
  import { useTerminal } from "../hooks/terminal.svelte";
  import { useViewport } from "../hooks/viewport.svelte";
  import { useUpload } from "../hooks/upload.svelte";
  import { useFullscreen } from "../hooks/fullscreen.svelte";
  import { hasTouchInput, isIOS } from "../touchInput";
  import QuickCommandMenu from "../components/QuickCommandMenu.svelte";
  import { quickCommands } from "../quickCommands.svelte";
  import type { QuickCommand, SubmissionProgress } from "../schemas";
  import { newSubmissionId } from "../terminalProtocol";
  import { browserDraftStorage } from "../draftStorage";
  import CloseSessionDialog from "../components/CloseSessionDialog.svelte";
  import type CodePanelComponent from "../components/CodePanel.svelte";
  import ScratchPanel from "../components/ScratchPanel.svelte";
  import TerminalCopyDialog from "../components/TerminalCopyDialog.svelte";
  import TerminalFontDialog from "../components/TerminalFontDialog.svelte";
  import TerminalHeader from "../components/TerminalHeader.svelte";
  import TerminalInputComposer from "../components/TerminalInputComposer.svelte";
  import TerminalNotices from "../components/TerminalNotices.svelte";
  import TerminalPasteDialog from "../components/TerminalPasteDialog.svelte";
  import TerminalViewport from "../components/TerminalViewport.svelte";
  import TerminalWorkspace from "../components/TerminalWorkspace.svelte";
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
  let sending = false;
  let pasteOperation: DraftOperation | undefined;
  let container = $state<HTMLDivElement>();
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
  let mounted = false;
  let codeShown = $state(false);
  let desktop = $state(matchMedia("(min-width:1024px)").matches);
  // Loaded on first use; the type-only import keeps it out of this chunk.
  let CodePanel = $state<typeof CodePanelComponent>();
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
  function toggleDraft() {
    drafts.setEnabled(draft, !draft.enabled);
    t.resetModifiers();
    if (draft.enabled && !hasTouchInput()) {
      void tick().then(() => composer?.focus());
    }
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
    () => container!,
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
  const savedLabel = (persisted: boolean, oversized: string) =>
    persisted && draftStorage.enabled
      ? "已保存在本机"
      : draftStorage.enabled
        ? oversized
        : "仅当前页面，刷新后丢失";
  const draftSaved = $derived(
    savedLabel(draft.persisted, "草稿过大，仅保存在当前页面"),
  );
  const scratchSaved = $derived(
    savedLabel(draft.scratchPersisted, "暂存超出本机保存限制，仅当前页面"),
  );
  function closeSession() {
    closing = true;
  }
  function showScratch() {
    draft.scratchUpdatedWhileHidden = false;
    scratchOpen = true;
  }
  const uploads = useUpload(
    () => container!,
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
  const fullscreen = useFullscreen((message) => {
    error = message;
  });

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
    return () => {
      media.removeEventListener("change", layout);
      mounted = false;
    };
  });

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
        const files = await clipboardFiles(items);
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
  style:--viewport-height={`${viewport.height}px`}
>
  <TerminalHeader
    terminal={t}
    {fullscreen}
    {draft}
    {draftSaved}
    {scratchSaved}
    uploading={uploads.progress !== null}
    {desktop}
    bind:codeShown
    bind:forceSoftKeys
    {quickMenu}
    upload={uploads.uploadFiles}
    {toggleDraft}
    {copy}
    {paste}
    {showScratch}
    showFontSize={() => (settingsOpen = true)}
    close={closeSession}
  />
  <TerminalNotices
    terminal={t}
    {uploads}
    {draft}
    {draftSaved}
    {error}
    bind:notice
    {showScratch}
    dismissScratch={() => (draft.scratchUpdatedWhileHidden = false)}
    remove={closeSession}
  />
  <TerminalWorkspace {codeShown} {desktop} draftOpen={draft.enabled}>
    {#snippet terminalPane()}
      <TerminalViewport
        bind:container
        terminal={t}
        visible={terminalVisible()}
        {uploads}
        {quickMenu}
        send={softInput}
        toggle={(key) => {
          if (draft.enabled) controlNotice();
          else t.toggleModifier(key);
        }}
        {paste}
        keyboard={() => {
          if (draft.enabled) composer?.keyboard();
          else t.keyboard();
        }}
      />
    {/snippet}
    {#snippet draftPane()}
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
    {/snippet}
    {#snippet codePane()}
      {#if CodePanel}<CodePanel
          {id}
          visible={codeShown}
          connection={t.connection}
          paste={insertReference}
          draftEnabled={draft.enabled}
          sessionCwd={t.info?.cwd ?? ""}
          reportedCwd={t.info?.reportedCwd ?? null}
        />{/if}
    {/snippet}
  </TerminalWorkspace>
</div>

<TerminalPasteDialog
  bind:open={pasteOpen}
  bind:text={pasteText}
  filePaste={uploads.filePaste}
  send={(text) => insertText(text, pasteOperation)}
  onCloseAutoFocus={focusTerminal}
/>
<TerminalCopyDialog
  bind:open={copyOpen}
  text={copyText}
  onCloseAutoFocus={focusTerminal}
/>
<TerminalFontDialog
  bind:open={settingsOpen}
  fontSize={t.fontSize}
  setFontSize={t.setFontSize}
  onCloseAutoFocus={focusTerminal}
/>
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
