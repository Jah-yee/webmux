import { APIError } from "./api";

export const isImageType = (mediaType: string | undefined) =>
  !!mediaType?.startsWith("image/");

// Raw snapshot bytes. File snapshots ignore `side`; Diff snapshots require it.
export function codeRawPath(
  id: string,
  snapshotId: string,
  side?: "old" | "new",
  download = false,
) {
  const query = new URLSearchParams();
  if (side) query.set("side", side);
  if (download) query.set("download", "1");
  const search = query.toString();
  return `/sessions/${id}/code/snapshots/${snapshotId}/raw${search ? `?${search}` : ""}`;
}

const formats: Record<string, string> = {
  "image/png": "PNG",
  "image/jpeg": "JPEG",
  "image/gif": "GIF",
  "image/webp": "WebP",
  "image/avif": "AVIF",
  "image/bmp": "BMP",
  "image/x-icon": "ICO",
  "image/svg+xml": "SVG",
};
export const imageFormat = (mediaType: string) =>
  formats[mediaType] ?? mediaType;

export function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KiB", "MiB", "GiB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(1).replace(/\.0$/, "")} ${units[unit]}`;
}

export const basename = (path: string) =>
  path.slice(path.lastIndexOf("/") + 1) || "download";

// Explains a failed raw-bytes request. Fetch rejects with TypeError when the
// connection fails, including a response the server aborted mid-transfer.
export function rawError(error: unknown, action: "预览" | "下载") {
  if (error instanceof APIError)
    switch (error.status) {
      case 401:
      case 403:
        return `${action}失败：认证失败，请重新输入令牌`;
      case 404:
        return `${action}失败：该侧文件不存在`;
      case 409:
        return `${action}失败：内容已变化，请刷新文档`;
      case 410:
        return `${action}失败：快照已过期，请刷新文档`;
      case 413:
        return `${action}失败：文件超过 100 MiB 下载上限`;
      case 429:
        return `${action}失败：同时进行的传输过多，请稍后重试`;
      default:
        return `${action}失败：${error.message}`;
    }
  if (error instanceof TypeError) return `${action}失败：网络连接异常`;
  return `${action}失败：${error instanceof Error ? error.message : String(error)}`;
}
