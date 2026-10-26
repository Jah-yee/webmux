import { onMount } from "svelte";

export type KeyboardVisibility = "visible" | "hidden" | "unknown";

// An on-screen keyboard shrinks the visible viewport by much more than browser
// chrome does. Keep a per-width baseline so rotation and split-screen resizes,
// which also change the width, cannot masquerade as an open keyboard.
const KEYBOARD_MIN_SHRINK = 120;
const KEYBOARD_HIDE_SHRINK = 60;
const WIDTH_TOLERANCE = 40;

export function useViewport() {
  let height = $state(
    Math.round(window.visualViewport?.height ?? window.innerHeight),
  );
  let keyboard = $state<KeyboardVisibility>("hidden");
  let keyboardDismissed = $state(false);
  onMount(() => {
    const viewport = window.visualViewport;
    const root = document.documentElement;
    const previousOverflow = root.style.overflow;
    // A scrollable document lets iOS pan the page to chase the focused caret.
    root.style.overflow = "hidden";
    let composing = false;
    let frame = 0;
    let closedHeight = height;
    let width = window.innerWidth;
    let keyboardState: KeyboardVisibility = "hidden";
    let sawKeyboard = false;
    const apply = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        // Composition updates move the caret every key; snapping the scroll
        // offset back on each of them is the shake. Height still follows the
        // keyboard: Android keyboards compose every word and may hide without
        // ending the composition.
        if (!composing && (window.scrollX || window.scrollY))
          window.scrollTo(0, 0);
        const next = Math.round(viewport?.height ?? window.innerHeight);
        if (next !== height) height = next;
        const nextWidth = window.innerWidth;
        if (Math.abs(nextWidth - width) > WIDTH_TOLERANCE) {
          width = nextWidth;
          closedHeight = next;
          keyboardState = "unknown";
          // The old height baseline cannot classify the new width. If the IME
          // was open, rotation may have closed it; require a fresh open/close
          // cycle instead of inventing a dismissal from the new height.
          sawKeyboard = false;
        } else if ((viewport?.scale ?? 1) > 1.01) {
          // Pinch zoom also shrinks the visual viewport, but not by an IME.
          keyboardState = "unknown";
        } else {
          if (next > closedHeight) closedHeight = next;
          const shrink = closedHeight - next;
          if (shrink >= KEYBOARD_MIN_SHRINK) keyboardState = "visible";
          else if (shrink <= KEYBOARD_HIDE_SHRINK) keyboardState = "hidden";
          // Between the thresholds, retain the previous state: keyboard
          // animations and browser chrome changes are not stable yet.
        }
        if (keyboardState === "visible") {
          sawKeyboard = true;
          keyboardDismissed = false;
        } else if (keyboardState === "hidden" && sawKeyboard) {
          // The IME was open and is now gone. Android Back leaves the
          // editable focused, so the next touch must release that focus and
          // the keyboard button must restart focus to reopen it. A keyboard
          // never observed open (headless, hardware keys) keeps focus toggling.
          sawKeyboard = false;
          keyboardDismissed = true;
        }
        if (keyboardState !== keyboard) keyboard = keyboardState;
      });
    };
    const start = () => {
      composing = true;
    };
    // A blur or a hidden page can abandon a composition without compositionend.
    const end = () => {
      if (!composing) return;
      composing = false;
      apply();
    };
    const hidden = () => {
      if (document.hidden) end();
    };
    viewport?.addEventListener("resize", apply);
    viewport?.addEventListener("scroll", apply);
    window.addEventListener("resize", apply);
    document.addEventListener("compositionstart", start, true);
    document.addEventListener("compositionend", end, true);
    document.addEventListener("focusout", end, true);
    document.addEventListener("visibilitychange", hidden);
    return () => {
      cancelAnimationFrame(frame);
      root.style.overflow = previousOverflow;
      viewport?.removeEventListener("resize", apply);
      viewport?.removeEventListener("scroll", apply);
      window.removeEventListener("resize", apply);
      document.removeEventListener("compositionstart", start, true);
      document.removeEventListener("compositionend", end, true);
      document.removeEventListener("focusout", end, true);
      document.removeEventListener("visibilitychange", hidden);
    };
  });
  return {
    get height() {
      return height;
    },
    get keyboard() {
      return keyboard;
    },
    get keyboardDismissed() {
      return keyboardDismissed;
    },
  };
}
