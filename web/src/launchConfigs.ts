import { joinArgs, parseArgs } from "./api";
import type { LaunchConfigSummary } from "./schemas";

// Client-side mirror of the server limits; the server stays authoritative.
export const LAUNCH_CONFIGS_LIMIT = 100;
const LAUNCH_NAME_MAX = 64;
const LAUNCH_ARGS_MAX = 128;
export const LAUNCH_ENV_MAX = 64;
const LAUNCH_VALUE_BYTES = 8192;
const envName = /^[A-Za-z_][A-Za-z0-9_]*$/;
const encoder = new TextEncoder();
const bytes = (value: string) => encoder.encode(value).length;
const BASHRC_PREFIX = ["bash", "-ic", 'exec -- "$@"', "webmux"] as const;

export type EnvRow = { key: number; name: string; value: string };
export type LaunchConfigInput = {
  name: string;
  icon?: string;
  cmd: string[];
  env: Record<string, string>;
};
export type LaunchDraftErrors = {
  name?: string;
  command?: string;
  env?: string;
  rows: Record<number, { name?: string; value?: string }>;
};
/** Summaries as seen by the new-session sheet; values are never loaded. */
export type LaunchConfigsState =
  | { status: "loading" }
  | { status: "ready"; configs: LaunchConfigSummary[] }
  | { status: "unavailable"; error: string };

/** Restore only our exact wrapper; arbitrary user-written shell commands stay intact. */
export function launchCommandDraft(cmd: readonly string[]) {
  const loadBashrc = hasBashrcWrapper(cmd);
  return {
    command: joinArgs(loadBashrc ? cmd.slice(BASHRC_PREFIX.length) : cmd),
    loadBashrc,
  };
}

function hasBashrcWrapper(cmd: readonly string[]) {
  return (
    cmd.length > BASHRC_PREFIX.length &&
    BASHRC_PREFIX.every((arg, index) => cmd[index] === arg)
  );
}

export const LAUNCH_ICON_OPTIONS = [
  { value: "terminal", label: "终端" },
  { value: "claudecode", label: "Claude Code" },
  { value: "codex", label: "Codex" },
  { value: "geminicli", label: "Gemini CLI" },
  { value: "opencode", label: "OpenCode" },
  { value: "pi", label: "Pi / omp" },
] as const;

/** Old configs get a display default without rewriting their saved commands. */
export function launchConfigIcon(config: {
  icon?: string;
  cmd: readonly string[];
}) {
  if (config.icon) return config.icon;
  const executable = config.cmd[
    hasBashrcWrapper(config.cmd) ? BASHRC_PREFIX.length : 0
  ]
    ?.split(/[\\/]/)
    .pop()
    ?.toLowerCase()
    .replace(/\.exe$/, "");
  switch (executable) {
    case "claude":
      return "claudecode";
    case "codex":
      return "codex";
    case "gemini":
      return "geminicli";
    case "opencode":
      return "opencode";
    case "omp":
    case "pi":
      return "pi";
    default:
      return "terminal";
  }
}

/** Error for one env variable name, or "" when valid. */
function envNameError(name: string): string {
  if (!name) return "请输入变量名";
  if (!envName.test(name))
    return "变量名只能包含字母、数字和下划线，且不能以数字开头";
  if (name === "WEBMUX_SESSION_ID") return "WEBMUX_SESSION_ID 由 WebMux 保留";
  return "";
}

/**
 * Validates the editor fields with the server's limits. Rows whose name and
 * value are both empty are ignored so an added-but-unused row never blocks.
 */
export function validateLaunchDraft(
  name: string,
  command: string,
  rows: readonly EnvRow[],
  loadBashrc = false,
):
  | { ok: true; value: LaunchConfigInput; errors: LaunchDraftErrors }
  | { ok: false; errors: LaunchDraftErrors } {
  const errors: LaunchDraftErrors = { rows: {} };
  const trimmed = name.trim();
  if (!trimmed) errors.name = "请输入名称";
  else if ([...trimmed].length > LAUNCH_NAME_MAX)
    errors.name = `名称最多 ${LAUNCH_NAME_MAX} 个字符`;

  let cmd: string[] = [];
  try {
    cmd = parseArgs(command);
    if (!cmd.length || !cmd[0]) errors.command = "请输入可执行程序及参数";
    else {
      if (loadBashrc) cmd = [...BASHRC_PREFIX, ...cmd];
      if (cmd.length > LAUNCH_ARGS_MAX)
        errors.command = loadBashrc
          ? `加载 .bashrc 后命令最多 ${LAUNCH_ARGS_MAX} 个参数（包含 Bash 启动参数）`
          : `命令最多 ${LAUNCH_ARGS_MAX} 个参数`;
      else if (cmd.some((arg) => bytes(arg) > LAUNCH_VALUE_BYTES))
        errors.command = `单个参数不能超过 ${LAUNCH_VALUE_BYTES} 字节`;
    }
  } catch (err) {
    errors.command = (err as Error).message;
  }

  const env: Record<string, string> = {};
  const used = rows.filter((row) => row.name || row.value);
  if (used.length > LAUNCH_ENV_MAX)
    errors.env = `最多 ${LAUNCH_ENV_MAX} 个环境变量`;
  for (const row of used) {
    const rowErrors: { name?: string; value?: string } = {};
    const nameError = envNameError(row.name);
    if (nameError) rowErrors.name = nameError;
    else if (Object.hasOwn(env, row.name)) rowErrors.name = "变量名重复";
    if (row.value.includes("\0")) rowErrors.value = "变量值不能包含 NUL 字符";
    else if (bytes(row.value) > LAUNCH_VALUE_BYTES)
      rowErrors.value = `变量值不能超过 ${LAUNCH_VALUE_BYTES} 字节`;
    if (rowErrors.name || rowErrors.value) errors.rows[row.key] = rowErrors;
    else env[row.name] = row.value;
  }

  if (
    errors.name ||
    errors.command ||
    errors.env ||
    Object.keys(errors.rows).length
  )
    return { ok: false, errors };
  return { ok: true, value: { name: trimmed, cmd, env }, errors };
}
