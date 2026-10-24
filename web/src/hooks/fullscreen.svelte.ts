import { onMount } from "svelte";

export interface Fullscreen {
  readonly active: boolean;
  readonly busy: boolean;
  toggle(): Promise<void>;
}

// Document fullscreen for the terminal page. Fullscreen this page entered is
// left again when the page unmounts, including a request still in flight.
export function useFullscreen(setError: (message: string) => void): Fullscreen {
  let active = $state(false);
  let busy = $state(false);
  let owned = false;
  let mounted = false;

  onMount(() => {
    mounted = true;
    const changed = () => {
      active = Boolean(document.fullscreenElement);
      if (!active) owned = false;
    };
    changed();
    document.addEventListener("fullscreenchange", changed);
    return () => {
      mounted = false;
      document.removeEventListener("fullscreenchange", changed);
      if (owned && document.fullscreenElement === document.documentElement) {
        owned = false;
        void document.exitFullscreen().catch((err) => {
          console.error("离开终端时退出全屏失败", err);
        });
      }
    };
  });

  async function toggle() {
    if (busy) return;
    setError("");
    if (
      !document.fullscreenElement &&
      (!document.fullscreenEnabled ||
        typeof document.documentElement.requestFullscreen !== "function")
    ) {
      setError(
        "当前浏览器环境未开放网页全屏；可尝试“添加到主屏幕”以独立模式打开，减少浏览器底栏。",
      );
      return;
    }
    busy = true;
    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen();
      } else {
        owned = true;
        // Fullscreen the document so body-level menus and dialogs remain visible.
        await document.documentElement.requestFullscreen({
          navigationUI: "hide",
        });
        if (
          !mounted &&
          owned &&
          document.fullscreenElement === document.documentElement
        ) {
          owned = false;
          await document.exitFullscreen();
        }
      }
    } catch (err) {
      if (!document.fullscreenElement) owned = false;
      if (mounted)
        setError(
          "切换全屏失败：" + (err instanceof Error ? err.message : String(err)),
        );
      else console.error("切换全屏失败", err);
    } finally {
      if (mounted) busy = false;
    }
  }

  return {
    get active() {
      return active;
    },
    get busy() {
      return busy;
    },
    toggle,
  };
}
