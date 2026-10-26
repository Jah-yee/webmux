// OSC 52 is the copy sequence xterm.js and its clipboard addon deliver to
// custom handlers: the payload after "52;", not the ESC introducer.
const browserClipboardTargets = new Set(["c", "p", "s"]);

// Returns the text an application asked to copy, or undefined when the
// sequence is a query, another selection, or not valid UTF-8 base64.
export function osc52CopyText(data: string): string | undefined {
  const separator = data.indexOf(";");
  if (separator < 0) return;
  const selection = data.slice(0, separator);
  const payload = data.slice(separator + 1);
  if (payload === "?") return;
  const targets = selection ? Array.from(selection) : ["c"];
  if (!targets.some((target) => browserClipboardTargets.has(target))) return;
  try {
    return decodeOsc52Payload(payload);
  } catch {
    return;
  }
}

function decodeOsc52Payload(payload: string): string {
  const cleaned = payload.replace(/\s+/g, "");
  if (cleaned.length === 0) return "";
  if (cleaned.length % 4 === 1 || !/^[A-Za-z0-9+/]*={0,2}$/.test(cleaned)) {
    throw new Error("invalid OSC 52 base64");
  }
  const padded = cleaned.padEnd(
    cleaned.length + ((4 - (cleaned.length % 4)) % 4),
    "=",
  );
  const binary = atob(padded);
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
  return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
}
