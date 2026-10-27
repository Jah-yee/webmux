import { settings } from "./settings.svelte";
import { defaultPreview } from "./settings-intent";

export const defaultPreviewSettings = defaultPreview;
export const previewSettings = {
  get lines() {
    return (settings.values.preview ?? defaultPreview).lines;
  },
  get skip() {
    return (settings.values.preview ?? defaultPreview).skip;
  },
};
