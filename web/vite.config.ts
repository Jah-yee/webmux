import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";
import { embedAssets } from "./scripts/embed-assets.ts";
export default defineConfig({
  plugins: [svelte(), tailwindcss(), embedAssets()],
  resolve: {
    alias: { $lib: fileURLToPath(new URL("./src/lib", import.meta.url)) },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // Preserve the browser baseline used before the Vite 8 upgrade.
    target: ["chrome107", "edge107", "firefox104", "safari16"],
  },
  // The terminal page is lazy. Without pre-bundling, the dev server discovers
  // xterm on the first terminal visit, re-optimizes and force-reloads the page.
  optimizeDeps: {
    include: [
      "@xterm/xterm",
      "@xterm/addon-fit",
      "@xterm/addon-webgl",
      "paneforge",
      "@codemirror/state",
      "@codemirror/view",
      "@codemirror/search",
      "@codemirror/language",
      "@codemirror/lang-javascript",
      "@codemirror/lang-python",
      "@codemirror/lang-json",
      "@codemirror/lang-go",
      "@codemirror/lang-rust",
      "@codemirror/lang-html",
      "@codemirror/lang-css",
      "@lezer/highlight",
      "diff",
      "@pierre/diffs",
      "@pierre/diffs/worker",
      "@pierre/diffs/worker/worker.js",
    ],
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:7681",
      "/ws": { target: "ws://127.0.0.1:7681", ws: true },
    },
  },
});
