<script lang="ts">
  import { untrack } from "svelte";
  import { apiBlob } from "../api";
  import type { CodeDocument } from "../schemas";
  import {
    basename,
    codeRawPath,
    formatBytes,
    imageFormat,
    rawError,
  } from "../code-image";
  import { Button } from "$lib/components/ui/button";
  import IconAspectRatio from "~icons/tabler/aspect-ratio";
  import IconZoomIn from "~icons/tabler/zoom-in";
  import IconZoomOut from "~icons/tabler/zoom-out";
  let {
    id,
    snapshotId,
    side,
    document: doc,
    onerror,
  }: {
    id: string;
    snapshotId: string;
    // Required for Diff snapshots; file snapshots omit it.
    side?: "old" | "new";
    document: CodeDocument;
    // Lets the parent react to a failed preview, e.g. SVG falls back to source.
    onerror?: (message: string) => void;
  } = $props();
  const minScale = 0.05,
    maxScale = 16,
    step = 1.25;
  let viewport = $state<HTMLDivElement>();
  let src = $state("");
  let phase = $state<"loading" | "ready" | "failed">("loading");
  let message = $state("");
  let natural = $state({ width: 0, height: 0 });
  let box = $state({ width: 0, height: 0 });
  let scale = $state(1);
  let x = $state(0);
  let y = $state(0);
  // While fitted, container resizes refit the image instead of keeping zoom.
  let fitted = $state(true);
  const intrinsic = $derived(natural.width > 0 && natural.height > 0);
  // SVG without an intrinsic size fills the viewport, preserving its ratio.
  const base = $derived(intrinsic ? natural : box);
  const clamp = (value: number) =>
    Math.min(maxScale, Math.max(minScale, value));

  $effect(() => {
    const path = codeRawPath(id, snapshotId, side);
    const mediaType = doc.mediaType;
    const controller = new AbortController();
    let url = "";
    untrack(() => {
      src = "";
      phase = "loading";
      message = "";
      natural = { width: 0, height: 0 };
    });
    void apiBlob(path, controller.signal)
      .then((blob) => {
        if (controller.signal.aborted) return;
        // An <img> renders SVG only with its media type; slicing is a view.
        url = URL.createObjectURL(
          blob.type === mediaType ? blob : blob.slice(0, blob.size, mediaType),
        );
        src = url;
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) fail(rawError(error, "预览"));
      });
    return () => {
      controller.abort();
      if (url) URL.revokeObjectURL(url);
    };
  });

  function fail(text: string) {
    phase = "failed";
    message = text;
    onerror?.(text);
  }
  // Keeps an image smaller than the viewport centered and a larger one
  // covering it, so panning never loses the image.
  function place() {
    const width = base.width * scale,
      height = base.height * scale;
    x =
      width <= box.width
        ? (box.width - width) / 2
        : Math.min(0, Math.max(box.width - width, x));
    y =
      height <= box.height
        ? (box.height - height) / 2
        : Math.min(0, Math.max(box.height - height, y));
  }
  function fit() {
    fitted = true;
    if (box.width <= 0 || box.height <= 0) return;
    scale = intrinsic
      ? clamp(
          Math.min(1, box.width / natural.width, box.height / natural.height),
        )
      : 1;
    place();
  }
  // Zooms keeping the image point under (px, py) fixed.
  function zoomAt(next: number, px = box.width / 2, py = box.height / 2) {
    if (phase !== "ready") return;
    const target = clamp(next);
    x = px - ((px - x) * target) / scale;
    y = py - ((py - y) * target) / scale;
    scale = target;
    fitted = false;
    place();
  }
  function pan(dx: number, dy: number) {
    if (phase !== "ready" || fitted) return;
    x += dx;
    y += dy;
    place();
  }
  function loaded(event: Event) {
    const image = event.currentTarget as HTMLImageElement;
    natural = { width: image.naturalWidth, height: image.naturalHeight };
    phase = "ready";
    fit();
  }

  // Listeners are non-passive so the browser neither scrolls, zooms the page
  // nor forwards the gesture to the terminal's touch handling.
  $effect(() => {
    const element = viewport;
    if (!element) return;
    const pointers = new Map<number, { x: number; y: number }>();
    let pinch:
      | {
          distance: number;
          scale: number;
          mid: { x: number; y: number };
          x: number;
          y: number;
        }
      | undefined;
    const local = (event: { clientX: number; clientY: number }) => {
      const rect = element.getBoundingClientRect();
      return { x: event.clientX - rect.left, y: event.clientY - rect.top };
    };
    const pair = () => {
      const [a, b] = [...pointers.values()];
      return {
        distance: Math.hypot(a.x - b.x, a.y - b.y),
        mid: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 },
      };
    };
    const startPinch = () => {
      if (pointers.size < 2) {
        pinch = undefined;
        return;
      }
      const { distance, mid } = pair();
      pinch = { distance: distance || 1, scale, mid, x, y };
    };
    const down = (event: PointerEvent) => {
      event.stopPropagation();
      if (phase !== "ready") return;
      if (event.pointerType === "mouse" && event.button !== 0) return;
      event.preventDefault();
      try {
        element.setPointerCapture(event.pointerId);
      } catch {
        /* the pointer may already be gone */
      }
      pointers.set(event.pointerId, local(event));
      startPinch();
    };
    const move = (event: PointerEvent) => {
      event.stopPropagation();
      const last = pointers.get(event.pointerId);
      if (!last) return;
      event.preventDefault();
      const point = local(event);
      pointers.set(event.pointerId, point);
      if (pinch) {
        const { distance, mid } = pair();
        const target = clamp((pinch.scale * distance) / pinch.distance);
        x = mid.x - ((pinch.mid.x - pinch.x) * target) / pinch.scale;
        y = mid.y - ((pinch.mid.y - pinch.y) * target) / pinch.scale;
        scale = target;
        fitted = false;
        place();
      } else pan(point.x - last.x, point.y - last.y);
    };
    const up = (event: PointerEvent) => {
      event.stopPropagation();
      if (!pointers.delete(event.pointerId)) return;
      startPinch();
    };
    const wheel = (event: WheelEvent) => {
      event.preventDefault();
      event.stopPropagation();
      const unit =
        event.deltaMode === WheelEvent.DOM_DELTA_LINE
          ? 16
          : event.deltaMode === WheelEvent.DOM_DELTA_PAGE
            ? box.height
            : 1;
      // Browsers report trackpad pinches as ctrl+wheel.
      if (event.ctrlKey) {
        const point = local(event);
        zoomAt(
          scale * step ** ((-event.deltaY * unit) / 100),
          point.x,
          point.y,
        );
      } else pan(-event.deltaX * unit, -event.deltaY * unit);
    };
    const stop = (event: Event) => event.stopPropagation();
    const resize = () => {
      box = { width: element.clientWidth, height: element.clientHeight };
      if (phase !== "ready") return;
      if (fitted) fit();
      else place();
    };
    const observer = new ResizeObserver(resize);
    observer.observe(element);
    untrack(resize);
    element.addEventListener("pointerdown", down);
    element.addEventListener("pointermove", move);
    element.addEventListener("pointerup", up);
    element.addEventListener("pointercancel", up);
    element.addEventListener("lostpointercapture", up);
    element.addEventListener("wheel", wheel, { passive: false });
    for (const type of ["touchstart", "touchmove", "touchend", "touchcancel"])
      element.addEventListener(type, stop);
    return () => {
      observer.disconnect();
      element.removeEventListener("pointerdown", down);
      element.removeEventListener("pointermove", move);
      element.removeEventListener("pointerup", up);
      element.removeEventListener("pointercancel", up);
      element.removeEventListener("lostpointercapture", up);
      element.removeEventListener("wheel", wheel);
      for (const type of ["touchstart", "touchmove", "touchend", "touchcancel"])
        element.removeEventListener(type, stop);
    };
  });
</script>

<div class="flex min-h-0 flex-1 flex-col">
  <div class="flex shrink-0 items-center gap-2 border-b px-2 py-1 text-xs">
    <p
      class="min-w-0 flex-1 truncate text-muted-foreground"
      aria-label="图片信息"
    >
      {imageFormat(doc.mediaType)} · {phase === "ready"
        ? intrinsic
          ? `${natural.width} × ${natural.height}`
          : "无固有尺寸"
        : phase === "loading"
          ? "读取尺寸…"
          : "尺寸未知"} · {formatBytes(doc.size)}
    </p>
    <div class="flex shrink-0 items-center gap-1">
      <output class="min-w-10 text-right tabular-nums" aria-label="缩放比例"
        >{phase === "ready" ? `${Math.round(scale * 100)}%` : ""}</output
      >
      <Button
        variant="outline"
        size="icon-sm"
        aria-label="适应窗口"
        title="适应窗口"
        aria-pressed={phase === "ready" && fitted}
        disabled={phase !== "ready"}
        onclick={fit}><IconAspectRatio aria-hidden="true" /></Button
      >
      <Button
        variant="outline"
        size="sm"
        aria-label="原始尺寸 1:1"
        title="原始尺寸 1:1"
        disabled={phase !== "ready"}
        onclick={() => zoomAt(1)}>1:1</Button
      >
      <!-- Narrow touch screens zoom by pinching; keep the row to one line. -->
      <Button
        variant="outline"
        size="icon-sm"
        class="max-sm:any-pointer-coarse:hidden"
        aria-label="缩小"
        title="缩小"
        disabled={phase !== "ready" || scale <= minScale}
        onclick={() => zoomAt(scale / step)}
        ><IconZoomOut aria-hidden="true" /></Button
      >
      <Button
        variant="outline"
        size="icon-sm"
        class="max-sm:any-pointer-coarse:hidden"
        aria-label="放大"
        title="放大"
        disabled={phase !== "ready" || scale >= maxScale}
        onclick={() => zoomAt(scale * step)}
        ><IconZoomIn aria-hidden="true" /></Button
      >
    </div>
  </div>
  <div
    bind:this={viewport}
    class="code-image-viewport relative min-h-0 flex-1 overflow-hidden bg-muted/40 select-none"
    class:cursor-grab={phase === "ready" && !fitted}
    role="group"
    aria-label="图片预览"
  >
    {#if src && phase !== "failed"}<img
        {src}
        alt={basename(doc.path)}
        draggable="false"
        class="absolute top-0 left-0 h-(--image-height) w-(--image-width) max-w-none origin-top-left transform-(--image-transform) object-contain"
        class:invisible={phase !== "ready"}
        style:--image-width={`${base.width}px`}
        style:--image-height={`${base.height}px`}
        style:--image-transform={`translate(${x}px, ${y}px) scale(${scale})`}
        onload={loaded}
        onerror={() =>
          fail("图片无法解码：文件可能已损坏，或当前浏览器不支持该格式")}
      />{/if}
    {#if phase === "loading"}<p
        class="absolute inset-0 grid place-items-center text-sm text-muted-foreground"
        role="status"
      >
        正在加载图片…
      </p>{:else if phase === "failed"}<p
        class="absolute inset-0 grid place-items-center p-4 text-center text-sm text-destructive"
        role="alert"
      >
        {message}
      </p>{/if}
  </div>
</div>

<style>
  .code-image-viewport {
    touch-action: none;
    overscroll-behavior: contain;
  }
</style>
