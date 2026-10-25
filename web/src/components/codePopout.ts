// Moves a live element into a separate window and back. The element keeps its
// component state and listeners; only its document changes. Chromium desktop
// browsers in secure contexts get an always-on-top Document Picture-in-Picture
// window; other desktop browsers (Safari, Firefox, plain-HTTP origins) get an
// ordinary same-origin popup.

import { mount, unmount } from "svelte";
import PopoutEventRoot from "./PopoutEventRoot.svelte";
import { isStandalone, popoutOffered } from "../capabilities";

type DocumentPictureInPicture = {
  requestWindow(options?: { width?: number; height?: number }): Promise<Window>;
  readonly window: Window | null;
};

function documentPictureInPicture(): DocumentPictureInPicture | undefined {
  if (!isSecureContext || !("documentPictureInPicture" in window)) return;
  // Missing from TypeScript's DOM library; the shape is the spec's interface.
  const api = window.documentPictureInPicture as DocumentPictureInPicture;
  return api;
}
export function popoutAvailable() {
  return popoutOffered({
    documentPictureInPicture: !!documentPictureInPicture(),
    finePointer: matchMedia("(pointer: fine)").matches,
    touch: matchMedia("(any-pointer: coarse)").matches,
    standalone: isStandalone(),
  });
}
async function openWindow(width: number, height: number) {
  const api = documentPictureInPicture();
  if (api) return api.requestWindow({ width, height });
  const popup = window.open(
    "",
    "_blank",
    `popup,width=${width},height=${height}`,
  );
  if (!popup) throw new Error("弹出窗口被浏览器拦截，请允许本站弹出窗口");
  return popup;
}

// Constructed stylesheets belong to the document that created them, and a
// shadow root drops its adopted sheets when moved to another document. Record
// what each shadow root adopted so the sheets can be rebuilt after a move.
const adopted = new WeakMap<ShadowRoot, readonly CSSStyleSheet[]>();
let recording = false;
function recordAdoptedSheets() {
  if (recording) return;
  recording = true;
  const property = Object.getOwnPropertyDescriptor(
    ShadowRoot.prototype,
    "adoptedStyleSheets",
  );
  if (!property?.set || !property.get) return;
  const { get, set } = property;
  Object.defineProperty(ShadowRoot.prototype, "adoptedStyleSheets", {
    ...property,
    get,
    set(this: ShadowRoot, sheets: CSSStyleSheet[]) {
      adopted.set(this, [...sheets]);
      set.call(this, sheets);
    },
  });
}
// Node type checks, not instanceof: nodes from either window's realm qualify.
function* shadowRoots(root: Node): Generator<ShadowRoot> {
  if (
    root.nodeType !== Node.ELEMENT_NODE &&
    root.nodeType !== Node.DOCUMENT_FRAGMENT_NODE
  )
    return;
  const parent = root as Element | DocumentFragment;
  const elements = [
    ...(root.nodeType === Node.ELEMENT_NODE ? [root as Element] : []),
    ...parent.querySelectorAll("*"),
  ];
  for (const element of elements) {
    if (!element.shadowRoot) continue;
    yield element.shadowRoot;
    yield* shadowRoots(element.shadowRoot);
  }
}
function captureSheets(root: Node) {
  for (const shadow of shadowRoots(root))
    if (shadow.adoptedStyleSheets.length && !adopted.has(shadow))
      adopted.set(shadow, [...shadow.adoptedStyleSheets]);
}
type Realm = Window & { CSSStyleSheet: typeof CSSStyleSheet };
const clones = new WeakMap<Window, WeakMap<CSSStyleSheet, CSSStyleSheet>>();
function sheetFor(sheet: CSSStyleSheet, win: Realm) {
  if (Object.getPrototypeOf(sheet) === win.CSSStyleSheet.prototype)
    return sheet;
  let cache = clones.get(win);
  if (!cache) clones.set(win, (cache = new WeakMap()));
  let clone = cache.get(sheet);
  if (!clone) {
    clone = new win.CSSStyleSheet();
    clone.replaceSync(
      [...sheet.cssRules].map((rule) => rule.cssText).join("\n"),
    );
    cache.set(sheet, clone);
  }
  return clone;
}
function restoreSheets(root: Node, win: Realm) {
  for (const shadow of shadowRoots(root)) {
    const sheets = adopted.get(shadow);
    if (!sheets?.length) continue;
    const wanted = sheets.map((sheet) => sheetFor(sheet, win));
    const current = shadow.adoptedStyleSheets;
    if (
      current.length === wanted.length &&
      current.every((sheet, index) => sheet === wanted[index])
    )
      continue;
    // Bypass the recorder: the recorded sheets stay the originals.
    shadowSet.call(shadow, wanted);
  }
}
const shadowSet = Object.getOwnPropertyDescriptor(
  ShadowRoot.prototype,
  "adoptedStyleSheets",
)!.set!;

function copyStyles(from: Document, to: Document) {
  for (const node of from.head.querySelectorAll(
    'link[rel="stylesheet"], style',
  ))
    to.head.append(to.importNode(node, true));
}
function mirrorAttributes(from: Element, to: Element) {
  while (to.attributes.length) to.removeAttribute(to.attributes[0].name);
  for (const { name, value } of from.attributes) to.setAttribute(name, value);
}

export type Popout = {
  readonly window: Window;
  // Closes the window; the element returns through the window's pagehide.
  close(): void;
};

// Must be called from a user gesture: both window kinds need activation.
export async function popOut(
  element: HTMLElement,
  options: {
    title: string;
    // Called after the element moved into `document`, or back to the opener.
    moved(document: Document): void;
    returned(): void;
  },
): Promise<Popout> {
  const source = element.ownerDocument;
  const rect = element.getBoundingClientRect();
  const pip = await openWindow(
    Math.max(480, Math.round(rect.width)),
    Math.max(360, Math.round(rect.height)),
  );
  const target = pip.document;
  target.title = options.title;
  copyStyles(source, target);
  // Theme and font settings live on <html>; stylesheets added later (lazy
  // chunks, editor themes) land in <head>.
  mirrorAttributes(source.documentElement, target.documentElement);
  const layout = target.createElement("style");
  layout.textContent =
    "html,body{height:100%}body{margin:0;display:flex;flex-direction:column}";
  target.head.append(layout);
  const mirror = new MutationObserver((records) => {
    for (const record of records) {
      if (record.type === "attributes")
        mirrorAttributes(source.documentElement, target.documentElement);
      else
        for (const node of record.addedNodes)
          if (
            node instanceof HTMLStyleElement ||
            (node instanceof HTMLLinkElement && node.rel === "stylesheet")
          )
            target.head.append(target.importNode(node, true));
    }
  });
  mirror.observe(source.documentElement, { attributes: true });
  mirror.observe(source.head, { childList: true });

  recordAdoptedSheets();
  captureSheets(element);
  const marker = source.createComment("code-popout");
  element.before(marker);
  target.body.append(element);
  restoreSheets(element, pip as Realm);
  // Components keep creating shadow roots in the opener's document and
  // inserting them here; rebuild their sheets as they arrive.
  const inserted = new MutationObserver((records) => {
    for (const record of records)
      for (const node of record.addedNodes) restoreSheets(node, pip as Realm);
  });
  inserted.observe(target.body, { childList: true, subtree: true });
  // Svelte delegates handlers such as onclick to listeners on its roots and
  // the opener's document; events here bubble to neither. A root mounted in
  // this body attaches those listeners for the moved components.
  const events = mount(PopoutEventRoot, { target: target.body });
  options.moved(target);

  // An ordinary popup outlives its opener; close it with the opener so the
  // panel is never stranded in an orphaned window.
  const opener = source.defaultView!;
  const closeWithOpener = () => pip.close();
  opener.addEventListener("pagehide", closeWithOpener);
  pip.addEventListener(
    "pagehide",
    () => {
      opener.removeEventListener("pagehide", closeWithOpener);
      mirror.disconnect();
      inserted.disconnect();
      void unmount(events);
      if (marker.isConnected) {
        captureSheets(element);
        marker.replaceWith(element);
        restoreSheets(element, opener as Realm);
        options.moved(source);
      } else marker.remove();
      options.returned();
    },
    { once: true },
  );
  return { window: pip, close: () => pip.close() };
}
