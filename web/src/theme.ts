import { settings, changeSettings, observeSettings } from "./settings.svelte";
import { type Theme } from "./schemas";

const system = matchMedia("(prefers-color-scheme: light)");
let choice: Theme | undefined;

function applyTheme() {
  const dark = choice ? choice === "dark" : !system.matches;
  const root = document.documentElement;
  root.classList.toggle("dark", dark);
  const meta = document.querySelector<HTMLMetaElement>(
    'meta[name="theme-color"]',
  );
  if (meta)
    meta.content = getComputedStyle(root)
      .getPropertyValue("--background")
      .trim();
}

export function initializeTheme() {
  observeSettings(() => {
    choice = settings.values.theme ?? undefined;
    applyTheme();
  });
  system.addEventListener("change", () => {
    if (!choice) applyTheme();
  });
}

export function toggleTheme() {
  const next = document.documentElement.classList.contains("dark")
    ? "light"
    : "dark";
  changeSettings({ theme: next });
}
