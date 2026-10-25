import { z } from "zod";
import {
  ErrorSchema,
  RecentDirsSchema,
  TextSettingSchema,
  UploadSchema,
  type SessionInfo,
} from "./schemas";

export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
    // Machine-readable cause some endpoints add, such as push 409 reasons.
    public reason?: string,
  ) {
    super(message);
  }
}
export const stored = (key: string, fallback = "") => {
  try {
    return TextSettingSchema.parse(
      localStorage.getItem("webmux." + key) ?? fallback,
    );
  } catch {
    return fallback;
  }
};
export function store(key: string, value: string) {
  try {
    localStorage.setItem("webmux." + key, value);
  } catch {
    /* private-mode storage is optional */
  }
}
export function unstore(key: string) {
  try {
    localStorage.removeItem("webmux." + key);
  } catch {
    /* private-mode storage is optional */
  }
}
const tokenFragment = "#token=";

// Adopts a `#token=` link: fragments never reach the server, and the token is
// removed from the address bar and current history entry before any request.
// Returns true when a token was stored.
export function consumeTokenFragment(): boolean {
  const hash = location.hash;
  if (!hash.startsWith(tokenFragment)) return false;
  history.replaceState(history.state, "", location.pathname + location.search);
  let token: string;
  try {
    token = decodeURIComponent(hash.slice(tokenFragment.length));
  } catch {
    return false;
  }
  if (!token) return false;
  store("token", token);
  return true;
}
function responseError(status: number, body: unknown, fallback: string) {
  const detail = ErrorSchema.safeParse(body);
  if (status === 401) window.dispatchEvent(new Event("webmux:auth"));
  return detail.success
    ? new APIError(status, detail.data.error, detail.data.reason)
    : new APIError(status, fallback);
}

async function request(
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<Response> {
  const response = await fetch("/api" + path, {
    method,
    signal,
    headers: {
      Authorization: "Bearer " + stored("token"),
      "Content-Type": "application/json",
    },
    // oxlint-disable-next-line unicorn/no-invalid-fetch-options -- Callers supply the method; GET requests omit the optional body.
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok)
    throw responseError(
      response.status,
      await response.json().catch(() => null),
      response.statusText,
    );
  return response;
}
export async function api<T extends z.ZodType>(
  path: string,
  schema: T,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<z.output<T>> {
  const response = await request(path, method, body, signal);
  return schema.parse(
    response.status === 204 ? undefined : await response.json(),
  );
}
export async function apiBuffer(id: string): Promise<Uint8Array> {
  return new Uint8Array(
    await (await request(`/sessions/${id}/buffer`)).arrayBuffer(),
  );
}
// Raw bytes share the JSON transport's Bearer header, APIError and 401 event.
export async function apiBlob(
  path: string,
  signal?: AbortSignal,
): Promise<Blob> {
  return (await request(path, "GET", undefined, signal)).blob();
}
// XHR exposes upload progress; fetch does not. Authentication and errors stay
// shared with the JSON transport, and multipart boundaries belong to the browser.
export function uploadFile(
  id: string,
  file: File,
  progress: (percent: number) => void,
  signal: AbortSignal,
): Promise<z.infer<typeof UploadSchema>> {
  const { promise, resolve, reject } =
    Promise.withResolvers<z.infer<typeof UploadSchema>>();
  if (signal.aborted) {
    reject(new DOMException("上传已取消", "AbortError"));
    return promise;
  }
  const xhr = new XMLHttpRequest();
  const abort = () => xhr.abort();
  const cleanup = () => signal.removeEventListener("abort", abort);
  xhr.open("POST", `/api/sessions/${id}/upload`);
  xhr.setRequestHeader("Authorization", "Bearer " + stored("token"));
  xhr.responseType = "json";
  xhr.timeout = 5 * 60 * 1000;
  xhr.upload.onprogress = (event) => {
    if (event.lengthComputable)
      progress(Math.round((event.loaded / event.total) * 100));
  };
  xhr.onload = () => {
    cleanup();
    try {
      if (xhr.status < 200 || xhr.status >= 300)
        throw responseError(xhr.status, xhr.response, xhr.statusText);
      resolve(UploadSchema.parse(xhr.response));
    } catch (err) {
      reject(err);
    }
  };
  xhr.onerror = () => {
    cleanup();
    reject(new APIError(0, "上传失败：网络连接异常"));
  };
  xhr.ontimeout = () => {
    cleanup();
    reject(new APIError(0, "上传超时"));
  };
  xhr.onabort = () => {
    cleanup();
    reject(new DOMException("上传已取消", "AbortError"));
  };
  signal.addEventListener("abort", abort, { once: true });
  const form = new FormData();
  form.append("file", file);
  xhr.send(form);
  return promise;
}
export function navigate(path: string) {
  history.pushState(null, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
}
const statusText = {
  running: "运行中",
  waiting: "可能在等你",
  idle: "空闲",
  exited: "已退出",
};
const attentionText = {
  question: "需要回答",
  permission: "等待授权",
  done: "已完成",
  error: "出错停止",
  notify: "有通知",
  bell: "响铃提醒",
};
// An agent-reported signal is more specific than the heuristic status label.
export function sessionStatusText(
  session: Pick<SessionInfo, "status" | "attention">,
) {
  return session.attention
    ? attentionText[session.attention.kind]
    : statusText[session.status];
}
export function recentDirs(): string[] {
  try {
    return RecentDirsSchema.parse(JSON.parse(stored("recentDirs", "[]")));
  } catch {
    return [];
  }
}
// Split extra argv without executing a shell. Quoted paths and empty arguments are preserved.
export function parseArgs(input: string): string[] {
  const result: string[] = [];
  let token = "",
    quote = "",
    escaped = false,
    started = false;
  for (const c of input) {
    if (escaped) {
      token += c;
      escaped = false;
      started = true;
    } else if (c === "\\" && quote !== "'") {
      escaped = true;
      started = true;
    } else if (quote) {
      if (c === quote) quote = "";
      else token += c;
    } else if (c === '"' || c === "'") {
      quote = c;
      started = true;
    } else if (/\s/.test(c)) {
      if (started) {
        result.push(token);
        token = "";
        started = false;
      }
    } else {
      token += c;
      started = true;
    }
  }
  if (escaped || quote) throw new Error("参数引号或反斜杠未闭合");
  if (started) result.push(token);
  return result;
}
// Characters that need quoting for parseArgs (whitespace, quotes, backslash)
// plus shell metacharacters, so the text also reads unambiguously as argv.
const plainArg = /^[^\s'"\\$`;&|<>()*?![\]{}#~]+$/u;
// Renders one argument so parseArgs reads it back unchanged.
export function quoteArg(arg: string): string {
  if (plainArg.test(arg)) return arg;
  if (!arg.includes("'")) return `'${arg}'`;
  return `"${arg.replace(/["\\$`]/g, "\\$&")}"`;
}
// Inverse of parseArgs: parseArgs(joinArgs(argv)) equals argv.
export function joinArgs(argv: readonly string[]): string {
  return argv.map(quoteArg).join(" ");
}
