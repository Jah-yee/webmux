import { SvelteMap } from "svelte/reactivity";
import { previewSettings } from "./previewSettings.svelte";
import type { PreviewSettings } from "./schemas";

// Launch config previews loaded with the session list. Sessions created from a
// config use its values; everything else falls back to the browser setting.
let byConfig = $state.raw<ReadonlyMap<string, PreviewSettings>>(new Map());

export function setLaunchPreviews(
  configs: readonly {
    id: string;
    preview?: PreviewSettings | null;
  }[],
): void {
  byConfig = new SvelteMap(
    configs.flatMap((config) =>
      config.preview ? [[config.id, config.preview] as const] : [],
    ),
  );
}

export function previewFor(configId: string | undefined): PreviewSettings {
  const preview = configId ? byConfig.get(configId) : undefined;
  return preview ?? previewSettings;
}
