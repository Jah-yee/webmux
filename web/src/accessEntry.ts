// accessEntryHint describes how the page was reached, for the public access
// card. Browsers report IPv6 literals with brackets (`[::1]`).
export function accessEntryHint({
  hostname,
  protocol,
}: Pick<URL, "hostname" | "protocol">): string {
  if (
    hostname === "127.0.0.1" ||
    hostname === "[::1]" ||
    hostname === "localhost" ||
    hostname.endsWith(".localhost")
  )
    return "当前通过本机回环地址访问；公网入口主要用于手机等外部设备。";
  if (hostname.endsWith(".trycloudflare.com"))
    return "当前是 Quick Tunnel 地址，每次启动都会变化；手机推送需要 Named Tunnel。";
  if (protocol === "https:")
    return "当前通过 HTTPS 访问；如果没有使用 Named Tunnel，手机推送仍可能不可用。";
  return "当前不是 HTTPS 地址；手机推送需要从 Named Tunnel 的 HTTPS 地址打开。";
}
