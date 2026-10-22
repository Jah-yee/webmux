import {
  CodeChangeSchema,
  SessionInfoSchema,
  WorktreeSchema,
} from "../src/schemas";

export const now = Date.parse("2026-01-01T12:00:00Z");
export const longToken = "repository_" + "unbroken_identifier_".repeat(18);
export const longPath = `/home/developer/${longToken}/多语言目录/工作区/${longToken}`;
export const longError = `无法访问工作区：${longPath}。请检查目录权限后重试。`;

export const session = SessionInfoSchema.parse({
  id: "abcdefgh",
  generation: "0123456789abcdef",
  title: `正在检查终端任务 · ${longToken}`,
  cwd: longPath,
  preset: "shell",
  cmd: ["/bin/sh"],
  status: "waiting",
  exitCode: null,
  preview: `${longToken}\n等待确认权限…`,
  createdAt: "2026-01-01T10:00:00Z",
  lastActiveAt: "2026-01-01T11:59:30Z",
  cols: 80,
  rows: 24,
  clients: 12345,
  workspace: null,
  attention: null,
  reportedCwd: {
    path: `${longPath}/internal/session`,
    host: "developer-mac.local",
    remote: false,
    since: "2026-01-01T11:58:00Z",
  },
});

export const worktree = WorktreeSchema.parse({
  id: "storybook-worktree",
  repositoryId: "storybook-repository",
  path: longPath,
  kind: "linked",
  branch: `feature/${longToken}`,
  head: "1234567890abcdef",
  detached: false,
  locked: true,
  lockReason: `另一个终端正在处理 ${longToken}`,
  prunable: true,
  available: false,
  dirty: true,
  hasUntracked: true,
  hasIgnored: true,
  error: longError,
});

export const changes = ["unstaged", "staged", "untracked"].map((group, i) =>
  CodeChangeSchema.parse({
    changeId: `change-${i}`,
    group,
    oldPath: `${longPath}/old-${longToken}.ts`,
    newPath: `${longPath}/new-${longToken}.ts`,
    status: i === 2 ? "A" : "R100",
    oldOid: "a".repeat(40),
    newOid: "b".repeat(40),
    oldMode: "100644",
    newMode: "100644",
  }),
);
