// Entry-point availability that depends on the platform. The predicates take
// the probed capabilities as arguments so every combination is testable;
// Playwright cannot emulate a Home Screen web app (display-mode: standalone).

export function isStandalone() {
  return matchMedia("(display-mode: standalone)").matches;
}

export function fullscreenSupported() {
  return (
    document.fullscreenEnabled &&
    typeof document.documentElement.requestFullscreen === "function"
  );
}

// Without the Fullscreen API (iPhone), a browser tab keeps the entry to
// suggest adding to the Home Screen; a Home Screen app has nothing to offer.
export function fullscreenOffered(platform: {
  supported: boolean;
  standalone: boolean;
}) {
  return platform.supported || !platform.standalone;
}

// Mobile browsers open popups as tabs, and a touch device's Home Screen app
// opens them as an in-app browser sheet; either would cover the terminal the
// panel belongs to. Without Document Picture-in-Picture, offer the popup only
// with a mouse, and not in a Home Screen app on a touch-capable device (an
// iPad with a trackpad has a fine primary pointer). Desktop Safari web apps
// keep the popup.
export function popoutOffered(platform: {
  documentPictureInPicture: boolean;
  finePointer: boolean;
  touch: boolean;
  standalone: boolean;
}) {
  return (
    platform.documentPictureInPicture ||
    (platform.finePointer && !(platform.standalone && platform.touch))
  );
}
