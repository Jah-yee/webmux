import { onMount } from "svelte";
import { uploadFile } from "../api";
import type { Connection } from "../ws";
import { hasTouchInput } from "../touchInput";
import type {
  DraftOperation,
  InputDestination,
  TextInsertion,
} from "../terminal-draft.svelte";

// Connection state is only required when the operation still targets the PTY.
interface UploadTerminal {
  readonly exitCode: number | null;
  readonly connection: Connection;
  readonly restoring: boolean;
  focus(): void;
}

export interface UploadInput {
  begin(): DraftOperation;
  destination(operation: DraftOperation): InputDestination;
  insert(text: string, operation: DraftOperation): TextInsertion;
}

export interface UploadProgress {
  name: string;
  percent: number;
  index: number;
  total: number;
  waiting: boolean;
}

// All upload sources share the same operation identity and quoted-path insertion.
export function useUpload(
  getHost: () => HTMLElement,
  id: string,
  terminal: UploadTerminal,
  setError: (message: string) => void,
  input: UploadInput,
) {
  let progress = $state<UploadProgress | null>(null);
  let dragging = $state(false);
  let foreground = $state(!document.hidden);
  let controller: AbortController | undefined;
  let waiter = $state<{
    resolve: () => void;
    reject: (error: Error) => void;
    operation: DraftOperation;
  } | null>(null);
  let mounted = false;

  $effect(() => {
    const current = waiter;
    if (!current) return;
    const target = input.destination(current.operation);
    if (
      target === "failed" ||
      terminal.exitCode !== null ||
      terminal.connection === "exited" ||
      terminal.connection === "missing"
    ) {
      waiter = null;
      current.reject(new Error("终端会话已退出或不存在"));
    } else if (
      target === "draft" ||
      (foreground && terminal.connection === "connected" && !terminal.restoring)
    ) {
      waiter = null;
      current.resolve();
    }
  });

  async function waitForDestination(
    signal: AbortSignal,
    operation: DraftOperation,
  ) {
    if (signal.aborted) throw new DOMException("操作已取消", "AbortError");
    const { promise, resolve, reject } = Promise.withResolvers<void>();
    const abort = () => reject(new DOMException("操作已取消", "AbortError"));
    const timeout = window.setTimeout(
      () => reject(new Error("等待终端恢复超时")),
      30_000,
    );
    signal.addEventListener("abort", abort, { once: true });
    waiter = { resolve, reject, operation };
    try {
      await promise;
    } finally {
      clearTimeout(timeout);
      signal.removeEventListener("abort", abort);
      waiter = null;
    }
  }

  async function uploadFiles(files: File[], operation = input.begin()) {
    if (!files.length) return;
    if (!mounted || input.destination(operation) === "failed") return;
    if (progress) {
      setError("正在上传，请等待当前上传完成。");
      return;
    }
    if (
      terminal.exitCode !== null ||
      terminal.connection === "exited" ||
      terminal.connection === "missing"
    ) {
      setError("终端会话已退出或不存在，无法上传文件。");
      return;
    }
    setError("");
    const current = new AbortController();
    controller = current;
    let pendingPath = "";
    let lastTarget: TextInsertion = "failed";
    try {
      for (const [index, file] of files.entries()) {
        progress = {
          name: file.name,
          percent: 0,
          index: index + 1,
          total: files.length,
          waiting: false,
        };
        const { path } = await uploadFile(
          id,
          file,
          (percent) => {
            if (progress) progress.percent = percent;
          },
          current.signal,
        );
        pendingPath = path;
        if (!mounted || current.signal.aborted)
          throw new Error(`文件已上传至 ${path}，但路径未插入：操作已取消`);
        progress.waiting = true;
        progress.percent = 100;
        try {
          await waitForDestination(current.signal, operation);
          if (!mounted || current.signal.aborted)
            throw new DOMException("操作已取消", "AbortError");
          // Quote arbitrary filenames safely. Uploading never runs a command.
          lastTarget = input.insert(
            "'" + path.replaceAll("'", "'\\''") + "' ",
            operation,
          );
          if (lastTarget === "failed") throw new Error("输入目标未就绪");
          pendingPath = "";
        } catch (err) {
          throw new Error(
            `文件已上传至 ${path}，但路径未插入：${(err as Error).message}`,
          );
        }
      }
      if (
        lastTarget === "terminal" &&
        input.destination(operation) === "terminal" &&
        !hasTouchInput()
      )
        terminal.focus();
    } catch (err) {
      if (mounted || pendingPath) setError((err as Error).message);
    } finally {
      controller = undefined;
      if (mounted) progress = null;
    }
  }

  // Returns whether the paste carried files and was taken over as an upload.
  function filePaste(event: ClipboardEvent) {
    const files = Array.from(event.clipboardData?.files ?? []);
    if (!files.length) return false;
    event.preventDefault();
    event.stopImmediatePropagation();
    void uploadFiles(files);
    return true;
  }

  onMount(() => {
    mounted = true;
    // bind:this is cleared before onMount teardown; retain the listener owner.
    const host = getHost();
    let foregroundFrame = 0;
    const visibilityChanged = () => {
      cancelAnimationFrame(foregroundFrame);
      foreground = false;
      // Let foreground reconnect handlers invalidate the old socket first.
      if (!document.hidden)
        foregroundFrame = requestAnimationFrame(() => {
          foreground = !document.hidden;
        });
    };
    document.addEventListener("visibilitychange", visibilityChanged);
    // Capture before xterm consumes paste/drop events; text still uses xterm.
    let dragDepth = 0;
    const dragEnter = (event: DragEvent) => {
      if (!event.dataTransfer?.types.includes("Files")) return;
      event.preventDefault();
      dragging = true;
      dragDepth++;
    };
    const dragOver = (event: DragEvent) => {
      if (!event.dataTransfer?.types.includes("Files")) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = progress ? "none" : "copy";
    };
    const dragLeave = () => {
      if (--dragDepth <= 0) {
        dragDepth = 0;
        dragging = false;
      }
    };
    const drop = (event: DragEvent) => {
      if (!event.dataTransfer?.types.includes("Files")) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      dragDepth = 0;
      dragging = false;
      void uploadFiles(Array.from(event.dataTransfer.files));
    };
    host.addEventListener("paste", filePaste, true);
    host.addEventListener("dragenter", dragEnter, true);
    host.addEventListener("dragover", dragOver, true);
    host.addEventListener("dragleave", dragLeave, true);
    host.addEventListener("drop", drop, true);
    return () => {
      document.removeEventListener("visibilitychange", visibilityChanged);
      cancelAnimationFrame(foregroundFrame);
      mounted = false;
      controller?.abort();
      host.removeEventListener("paste", filePaste, true);
      host.removeEventListener("dragenter", dragEnter, true);
      host.removeEventListener("dragover", dragOver, true);
      host.removeEventListener("dragleave", dragLeave, true);
      host.removeEventListener("drop", drop, true);
    };
  });

  return {
    get progress() {
      return progress;
    },
    get dragging() {
      return dragging;
    },
    uploadFiles,
    filePaste,
    cancel() {
      controller?.abort();
    },
  };
}
