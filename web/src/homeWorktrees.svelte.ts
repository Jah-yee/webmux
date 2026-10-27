import { settings, changeSettings } from "./settings.svelte";
import { HomeWorktreesSchema, type HomeWorktrees } from "./schemas";

export const homeWorktrees = {
  get value(): HomeWorktrees {
    return settings.values.homeWorktrees ?? {};
  },
  save(next: HomeWorktrees) {
    changeSettings({ homeWorktrees: HomeWorktreesSchema.parse(next) });
  },
};
