import { settings, saveSettings } from "./settings.svelte";
import { QuickCommandsSchema, type QuickCommand } from "./schemas";
import { parseQuickCommands } from "./quickCommands";

export const quickCommands = {
  get list() {
    return settings.values.quickCommands ?? parseQuickCommands("");
  },
};
export function saveQuickCommands(list: QuickCommand[]) {
  return saveSettings({ quickCommands: QuickCommandsSchema.parse(list) });
}
