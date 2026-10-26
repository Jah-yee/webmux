import { errorMessage } from "../error";
import { onMount } from "svelte";
import { api, APIError } from "../api";
import {
  TunnelSettingsSchema,
  TunnelSettingsUpdateSchema,
  type TunnelSettings,
  type TunnelSettingsUpdate,
} from "../schemas";

export function useTunnelSettings() {
  let view = $state<TunnelSettings>();
  let loading = $state(true);
  let busy = $state(false);
  let error = $state("");
  let notice = $state("");
  let shutdownUnpersisted = $state(false);
  let editVersion = $state(0);
  let requestVersion = 0;
  let controller: AbortController | undefined;
  let disposed = false;

  async function refresh() {
    const version = ++requestVersion;
    controller?.abort();
    controller = new AbortController();
    try {
      const current = await api(
        "/tunnel",
        TunnelSettingsSchema,
        "GET",
        undefined,
        controller.signal,
      );
      if (disposed || version !== requestVersion) return;
      if (!view) editVersion++;
      view = current;
    } catch (cause) {
      if (!disposed && version === requestVersion && !controller.signal.aborted)
        error = errorMessage(cause);
    } finally {
      if (!disposed && version === requestVersion) loading = false;
    }
  }

  onMount(() => {
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      if (!busy && !document.hidden) await refresh();
      if (!disposed) timer = setTimeout(() => void poll(), 2000);
    }
    void poll();
    return () => {
      disposed = true;
      requestVersion++;
      controller?.abort();
      clearTimeout(timer);
    };
  });

  async function mutate(path: string, method: string, body?: unknown) {
    if (busy || disposed) return false;
    busy = true;
    error = "";
    notice = "";
    ++requestVersion;
    controller?.abort();
    controller = new AbortController();
    try {
      const current = await api(
        path,
        TunnelSettingsSchema,
        method,
        body,
        controller.signal,
      );
      if (disposed) return false;
      view = current;
      shutdownUnpersisted = false;
      editVersion++;
      notice =
        current.state === "starting"
          ? "已保存，正在应用隧道设置。"
          : "操作已完成。";
      return true;
    } catch (cause) {
      if (disposed) return false;
      error = errorMessage(cause);
      if (cause instanceof APIError && cause.notPersisted)
        shutdownUnpersisted = true;
      // A failed disk write can still have safely stopped the tunnel. Read the
      // actual state instead of leaving the switch on after an emergency stop.
      await refresh();
      return false;
    } finally {
      if (!disposed) busy = false;
    }
  }

  return {
    get view() {
      return view;
    },
    get loading() {
      return loading;
    },
    get busy() {
      return busy;
    },
    get error() {
      return error;
    },
    get notice() {
      return notice;
    },
    get editVersion() {
      return editVersion;
    },
    get shutdownUnpersisted() {
      return shutdownUnpersisted;
    },
    refresh,
    save: (draft: TunnelSettingsUpdate) =>
      mutate(
        "/tunnel/settings",
        "PUT",
        TunnelSettingsUpdateSchema.parse(draft),
      ),
    setEnabled: (enabled: boolean) =>
      mutate("/tunnel/enabled", "PUT", { enabled }),
    reset: () => mutate("/tunnel/settings", "DELETE"),
  };
}
