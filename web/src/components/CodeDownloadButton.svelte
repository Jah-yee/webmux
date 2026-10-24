<script lang="ts">
  import { apiBlob, APIError } from "../api";
  import { rawError } from "../code-image";
  import { Button } from "$lib/components/ui/button";
  import IconDownload from "~icons/tabler/download";
  let {
    path,
    filename,
    disabled = false,
    onerror,
  }: {
    // Raw snapshot path relative to /api, without the download flag.
    path: string;
    filename: string;
    disabled?: boolean;
    // Called with "" when a download starts, and with the explanation and
    // HTTP status (0 for network failures) when it fails.
    onerror: (message: string, status: number) => void;
  } = $props();
  let button = $state<HTMLButtonElement | null>(null);
  let controller = $state<AbortController>();
  // Switching document/side or unmounting cancels the transfer.
  $effect(() => {
    void path;
    return () => controller?.abort();
  });
  async function download() {
    controller?.abort();
    const current = new AbortController();
    controller = current;
    onerror("", 0);
    try {
      const blob = await apiBlob(
        `${path}${path.includes("?") ? "&" : "?"}download=1`,
        current.signal,
      );
      if (current.signal.aborted) return;
      // The panel may live in a pop-out window; save from the button's own.
      const owner = button?.ownerDocument ?? document;
      const href = URL.createObjectURL(blob);
      const anchor = owner.createElement("a");
      anchor.href = href;
      anchor.download = filename;
      anchor.hidden = true;
      owner.body.append(anchor);
      anchor.click();
      anchor.remove();
      (owner.defaultView ?? window).requestAnimationFrame(() =>
        URL.revokeObjectURL(href),
      );
    } catch (error) {
      if (!current.signal.aborted)
        onerror(
          rawError(error, "下载"),
          error instanceof APIError ? error.status : 0,
        );
    } finally {
      if (controller === current) controller = undefined;
    }
  }
</script>

<Button
  bind:ref={button}
  variant="outline"
  size="sm"
  disabled={disabled || !!controller}
  onclick={() => void download()}
  ><IconDownload aria-hidden="true" />{controller ? "下载中…" : "下载"}</Button
>
