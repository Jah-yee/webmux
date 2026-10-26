import { errorMessage } from "../error";
import { onMount } from "svelte";
import { api } from "../api";
import {
  TailscaleSettingsUpdateSchema,
  TailscaleStatusSchema,
  type TailscaleSettingsUpdate,
  type TailscaleStatus,
} from "../schemas";

// Applying a Tailscale entry runs several CLI calls on the server; poll only
// while the controller reports "starting" so an idle page spawns no probes.
const STARTING_POLL_MS = 1500;
const APPLYING = "已保存，正在应用 Tailscale 设置。";

/** Reactive view of the Tailscale settings page; mutate only via the hook. */
export interface TailscaleSettingsState {
  view: TailscaleStatus | undefined;
  loading: boolean;
  busy: boolean;
  error: string;
  notice: string;
  // Bumps when the form must re-read the server values.
  editVersion: number;
}

export function useTailscaleSettings() {
  const state = $state<TailscaleSettingsState>({
    view: undefined,
    loading: true,
    busy: false,
    error: "",
    notice: "",
    editVersion: 0,
  });
  let latest = 0;
  let inflight: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let disposed = false;

  // Every response supersedes older ones: the ticket drops late replies and
  // the abort cancels the request it replaces.
  async function exchange(
    path: string,
    method = "GET",
    body?: unknown,
  ): Promise<TailscaleStatus | undefined> {
    const ticket = ++latest;
    inflight?.abort();
    const controller = (inflight = new AbortController());
    clearTimeout(timer);
    try {
      const next = await api(
        path,
        TailscaleStatusSchema,
        method,
        body,
        controller.signal,
      );
      if (disposed || ticket !== latest) return undefined;
      state.view = next;
      state.error = "";
      return next;
    } catch (cause) {
      if (!disposed && ticket === latest && !controller.signal.aborted)
        state.error = errorMessage(cause);
      throw cause;
    } finally {
      if (!disposed && ticket === latest) {
        state.loading = false;
        if (state.view?.state === "starting")
          timer = setTimeout(() => void load(), STARTING_POLL_MS);
        else if (state.notice === APPLYING) state.notice = "设置已应用。";
      }
    }
  }

  async function load(refresh = false) {
    const first = !state.view;
    try {
      await exchange(refresh ? "/tailscale?refresh=1" : "/tailscale");
      if (first && state.view) state.editVersion++;
    } catch {
      // exchange already reported the failure.
    }
  }

  async function mutate(method: "PUT" | "DELETE", body?: unknown) {
    if (state.busy || disposed) return false;
    state.busy = true;
    state.notice = "";
    try {
      const next = await exchange("/tailscale/settings", method, body);
      if (!next) return false;
      state.editVersion++;
      state.notice = next.state === "starting" ? APPLYING : "设置已保存。";
      return true;
    } catch {
      return false;
    } finally {
      if (!disposed) state.busy = false;
    }
  }

  onMount(() => {
    void load();
    return () => {
      disposed = true;
      inflight?.abort();
      clearTimeout(timer);
    };
  });

  return {
    state,
    load,
    save: (draft: TailscaleSettingsUpdate) =>
      mutate("PUT", TailscaleSettingsUpdateSchema.parse(draft)),
    reset: () => mutate("DELETE"),
  };
}
