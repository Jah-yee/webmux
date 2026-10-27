import { SvelteSet } from "svelte/reactivity";
import { api, APIError, stored, store } from "./api";
import { connect, type SocketConnection } from "./ws";
import {
  SettingsSnapshotSchema,
  SettingsEventSchema,
  SettingsValuesSchema,
  type SettingsSnapshot,
  type SettingsValues,
} from "./schemas";
import {
  emptySettings,
  settingsIntent,
  type SettingsIntent,
} from "./settings-intent";

function cached(): SettingsSnapshot {
  try {
    return SettingsSnapshotSchema.parse(JSON.parse(stored("settingsCache")));
  } catch {
    try {
      localStorage.removeItem("webmux.settingsCache");
    } catch {
      /* optional cache */
    }
    return {
      revision: 0,
      updatedAt: "",
      values: structuredClone(emptySettings),
    };
  }
}
let confirmed = cached();
export const settings = $state({
  values: confirmed.values,
  revision: confirmed.revision,
  ready: false,
  saving: false,
  error: "",
  connected: false,
});
let socket: SocketConnection | undefined;
let generation = 0;
let serial: Promise<unknown> = Promise.resolve();
let pending: SettingsIntent[] = [];
const observers = new SvelteSet<() => void>();
function render() {
  let values = confirmed.values;
  for (const operation of pending) values = { ...values, ...operation(values) };
  settings.values = values;
  settings.revision = confirmed.revision;
  settings.saving = pending.length > 0;
  for (const fn of observers) fn();
}
export function observeSettings(fn: () => void) {
  observers.add(fn);
  fn();
  return () => {
    observers.delete(fn);
  };
}
function adopt(snapshot: SettingsSnapshot, replace = false) {
  if (!replace && snapshot.revision < confirmed.revision) return;
  confirmed = snapshot;
  settings.ready = true;
  store("settingsCache", JSON.stringify(snapshot));
  render();
}
export async function reloadSettings() {
  const epoch = generation;
  try {
    const snapshot = await api("/settings", SettingsSnapshotSchema);
    if (epoch !== generation) return;
    adopt(snapshot, !settings.ready);
    settings.error = "";
  } catch (err) {
    if (epoch !== generation) return;
    settings.error = (err as Error).message;
    settings.ready = false;
  }
}
export function stopSettings() {
  generation++;
  socket?.close();
  socket = undefined;
  pending = [];
  serial = Promise.resolve();
  settings.ready = false;
  settings.connected = false;
  render();
}
export function startSettings() {
  stopSettings();
  const epoch = generation;
  void reloadSettings();
  socket = connect(
    "/ws/settings",
    (raw) => {
      if (epoch !== generation || typeof raw !== "string") return;
      const event = SettingsEventSchema.parse(JSON.parse(raw));
      if (event.type === "settings") {
        // Each subscription begins with an authoritative snapshot, including
        // revision 0 after an operator restores or resets the state file.
        adopt(event.settings, true);
        settings.error = "";
      } else if (event.revision === confirmed.revision + 1)
        adopt({
          revision: event.revision,
          updatedAt: event.updatedAt,
          values: SettingsValuesSchema.parse({
            ...confirmed.values,
            ...event.changes,
          }),
        });
      else if (event.revision > confirmed.revision + 1) void reloadSettings();
    },
    (state) => {
      settings.connected = state === "connected";
      if (state === "reconnecting") void reloadSettings();
    },
    (message) => {
      settings.error = message;
    },
  );
}
export function saveSettings(changes: Partial<SettingsValues>): Promise<void> {
  if (!settings.ready)
    return Promise.reject(
      new Error(settings.error || "设置尚未连接，请重新读取"),
    );
  const epoch = generation;
  const operation = settingsIntent($state.snapshot(settings.values), changes);
  pending.push(operation);
  render();
  const task = serial.then(async () => {
    if (epoch !== generation) throw new Error("连接已改变，请重新保存");
    try {
      for (let attempt = 0; attempt < 2; attempt++) {
        try {
          const snapshot = await api(
            "/settings",
            SettingsSnapshotSchema,
            "PATCH",
            {
              baseRevision: confirmed.revision,
              changes: operation(confirmed.values),
            },
          );
          if (epoch !== generation) throw new Error("连接已改变，请重新保存");
          adopt(snapshot);
          settings.error = "";
          return;
        } catch (err) {
          if (!(err instanceof APIError) || err.status !== 409 || attempt)
            throw err;
          const latest = await api("/settings", SettingsSnapshotSchema);
          if (epoch !== generation) throw new Error("连接已改变，请重新保存");
          adopt(latest, true);
        }
      }
    } catch (err) {
      if (epoch === generation) {
        settings.error = (err as Error).message;
        if (err instanceof APIError && err.status === 503)
          settings.ready = false;
      }
      throw err;
    } finally {
      if (epoch === generation) {
        pending = pending.filter((x) => x !== operation);
        render();
      }
    }
  });
  serial = task.catch(() => undefined);
  return task;
}
// Shortcut controls surface failures in the shared status instead of creating
// unhandled promises. Dialog forms await saveSettings and retain their draft.
export function changeSettings(changes: Partial<SettingsValues>) {
  void saveSettings(changes).catch((err: Error) => {
    settings.error = err.message;
  });
}
