import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import {
  terminalFontFamilyAlt,
  terminalFontFamilyCss,
  terminalLineHeight,
} from "../fonts";
import "@xterm/xterm/css/xterm.css";
import "../fonts-terminal.css";

// Read the same theme tokens used by the shadcn surfaces, including live theme changes.
function terminalTheme() {
  const css = getComputedStyle(document.documentElement);
  return {
    background: css.getPropertyValue("--terminal-background").trim(),
    foreground: css.getPropertyValue("--terminal-foreground").trim(),
    cursor: css.getPropertyValue("--terminal-blue").trim(),
    selectionBackground: css.getPropertyValue("--terminal-selection").trim(),
    black: css.getPropertyValue("--terminal-black").trim(),
    red: css.getPropertyValue("--terminal-red").trim(),
    green: css.getPropertyValue("--terminal-green").trim(),
    yellow: css.getPropertyValue("--terminal-yellow").trim(),
    blue: css.getPropertyValue("--terminal-blue").trim(),
    magenta: css.getPropertyValue("--terminal-magenta").trim(),
    cyan: css.getPropertyValue("--terminal-cyan").trim(),
    white: css.getPropertyValue("--terminal-white").trim(),
  };
}

export interface TerminalRendererEvents {
  // The host may need a new grid: its box, DPR or measured font changed.
  layout: () => void;
  // The page theme changed and xterm already uses the new colours.
  themeChanged: () => void;
}

export interface TerminalRenderer {
  readonly term: Terminal;
  // Fits the grid to the host's current box.
  fit(): void;
  // Releases observers now and the terminal after its pending frames.
  dispose(): void;
}

// Opens xterm in `parent` with WebGL rendering, falling back to xterm's DOM
// renderer when WebGL is unavailable, loses its context or cannot measure the
// host consistently, and follows theme, size, DPR and font changes.
export function createTerminalRenderer(
  parent: HTMLElement,
  fontSize: number,
  events: TerminalRendererEvents,
): TerminalRenderer {
  let disposed = false;
  const term = new Terminal({
    fontSize,
    fontFamily: terminalFontFamilyCss,
    fontWeight: 400,
    fontWeightBold: 700,
    lineHeight: terminalLineHeight,
    scrollback: 10000,
    cursorBlink: true,
    minimumContrastRatio: 4.5,
    allowProposedApi: true,
    macOptionIsMeta: true,
    theme: terminalTheme(),
  });
  const fitAddon = new FitAddon();
  term.loadAddon(fitAddon);
  term.open(parent);
  let webgl: WebglAddon | undefined;
  const useDomRenderer = () => {
    if (!webgl) return;
    const addon = webgl;
    webgl = undefined;
    addon.dispose();
    term.refresh(0, term.rows - 1);
  };
  try {
    webgl = new WebglAddon();
    term.loadAddon(webgl);
    webgl.onContextLoss(useDomRenderer);
  } catch {
    useDomRenderer();
    // xterm's built-in DOM renderer remains available without WebGL.
  }
  const theme = () => {
    if (disposed) return;
    term.options.theme = terminalTheme();
    events.themeChanged();
  };
  const colors = new MutationObserver(theme);
  colors.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["class"],
  });
  let timer: number | undefined;
  const observer = new ResizeObserver(([entry]) => {
    if (disposed) return;
    if (webgl) {
      const pixels = entry?.devicePixelContentBoxSize?.[0];
      const css = entry?.contentRect;
      const dpr = window.devicePixelRatio || 1;
      // Chromium emulation can report physical pixels at the host DPR while
      // xterm renders at the emulated DPR. The WebGL addon then shrinks its
      // backing canvas without updating its render dimensions. DOM rendering
      // avoids that incompatible measurement; allow normal pixel rounding.
      if (
        pixels &&
        css.width > 0 &&
        css.height > 0 &&
        pixels.inlineSize > 0 &&
        pixels.blockSize > 0 &&
        (Math.abs(pixels.inlineSize - css.width * dpr) > 1 ||
          Math.abs(pixels.blockSize - css.height * dpr) > 1)
      ) {
        useDomRenderer();
      }
    }
    clearTimeout(timer);
    timer = window.setTimeout(() => events.layout(), 100);
  });
  const observeSize = () => {
    try {
      observer.observe(parent, { box: "device-pixel-content-box" });
    } catch {
      observer.observe(parent);
    }
  };
  let resolution = matchMedia(
    `(resolution: ${window.devicePixelRatio || 1}dppx)`,
  );
  const resolutionChanged = () => {
    if (disposed) return;
    resolution.removeEventListener("change", resolutionChanged);
    resolution = matchMedia(
      `(resolution: ${window.devicePixelRatio || 1}dppx)`,
    );
    resolution.addEventListener("change", resolutionChanged);
    // A DPR change need not change the CSS box. Request a fresh observation
    // rather than comparing the new DPR against stale physical dimensions.
    observer.unobserve(parent);
    observeSize();
  };
  resolution.addEventListener("change", resolutionChanged);
  observeSize();
  let remeasureFrame = 0;
  const remeasure = () => {
    cancelAnimationFrame(remeasureFrame);
    remeasureFrame = requestAnimationFrame(() => {
      if (disposed) return;
      // Later unicode-range slices must be rasterized into the cell grid
      // that was measured from the Latin face.
      webgl?.clearTextureAtlas();
      const current = term.options.fontFamily;
      term.options.fontFamily =
        current === terminalFontFamilyCss
          ? terminalFontFamilyAlt
          : terminalFontFamilyCss;
      term.options.fontFamily = current ?? terminalFontFamilyCss;
      events.layout();
    });
  };
  document.fonts.addEventListener("loadingdone", remeasure);

  return {
    term,
    fit() {
      fitAddon.fit();
    },
    dispose() {
      disposed = true;
      cancelAnimationFrame(remeasureFrame);
      document.fonts.removeEventListener("loadingdone", remeasure);
      clearTimeout(timer);
      observer.disconnect();
      resolution.removeEventListener("change", resolutionChanged);
      colors.disconnect();
      // Viewport.reset() queues syncScrollArea for the next frame, and that
      // frame reads RenderService.dimensions. Disposing in this turn removes
      // the renderer first, so the callback throws after a restore or theme
      // change. Let that frame, and the scroll refresh it may schedule, run
      // before the terminal is disposed.
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          // Older WebGL addons recreate the DOM renderer on dispose, so
          // release the addon while the terminal's services still exist.
          webgl?.dispose();
          webgl = undefined;
          term.dispose();
        });
      });
    },
  };
}
