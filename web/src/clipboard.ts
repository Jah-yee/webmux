export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall through to the legacy path used by insecure LAN deployments.
    }
  }
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.top = "-1000px";
  area.style.opacity = "0";
  document.body.appendChild(area);
  area.select();
  try {
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    area.remove();
  }
}

// Non-text clipboard items become uploadable files named after their type.
export async function clipboardFiles(
  items: readonly ClipboardItem[],
): Promise<File[]> {
  const files: File[] = [];
  for (const [index, item] of items.entries()) {
    const type = item.types.find((type) => !type.startsWith("text/"));
    if (!type) continue;
    const blob = await item.getType(type);
    const extension =
      type === "image/jpeg"
        ? "jpg"
        : type.split("/")[1].replace(/[^a-z0-9]/gi, "") || "bin";
    files.push(
      new File([blob], `clipboard-${Date.now()}-${index}.${extension}`, {
        type,
      }),
    );
  }
  return files;
}
