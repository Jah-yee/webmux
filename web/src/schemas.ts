import { z } from "zod";

export const WorkspaceLinkSchema = z.object({
  repositoryId: z.string(),
  worktreeId: z.string(),
  worktreePath: z.string(),
});

export const AttentionSchema = z.object({
  kind: z.enum(["question", "permission", "done", "error", "notify", "bell"]),
  source: z.enum(["agent", "terminal"]),
  since: z.iso.datetime({ offset: true }),
});
export type Attention = z.infer<typeof AttentionSchema>;

export const ReportedCwdSchema = z.object({
  path: z.string(),
  host: z.string(),
  remote: z.boolean(),
  since: z.iso.datetime({ offset: true }),
});
export type ReportedCwd = z.infer<typeof ReportedCwdSchema>;

export const SessionInfoSchema = z.object({
  id: z.string().regex(/^[a-z2-7]{8}$/),
  generation: z.string().regex(/^[0-9a-f]{16}$/),
  title: z.string(),
  cwd: z.string(),
  cmd: z.array(z.string()).min(1),
  status: z.enum(["running", "waiting", "idle", "exited"]),
  exitCode: z.number().int().nullable(),
  preview: z.string(),
  createdAt: z.iso.datetime({ offset: true }),
  lastActiveAt: z.iso.datetime({ offset: true }),
  cols: z.number().int().min(1).max(1000),
  rows: z.number().int().min(1).max(1000),
  clients: z.number().int().nonnegative(),
  workspace: WorkspaceLinkSchema.nullable(),
  attention: AttentionSchema.nullable(),
  reportedCwd: ReportedCwdSchema.nullable(),
});
export type SessionInfo = z.infer<typeof SessionInfoSchema>;

// Launch configs are server-stored launch commands. Summaries (list) carry only
// env variable names; the full config with values is fetched per id for edits.
const launchConfigFields = {
  id: z.string().regex(/^[a-z2-7]{8}$/),
  name: z.string().min(1),
  icon: z
    .string()
    .regex(/^[a-z][a-z0-9-]{0,63}$/)
    .optional(),
  cmd: z.array(z.string()).min(1),
  createdAt: z.iso.datetime({ offset: true }),
  updatedAt: z.iso.datetime({ offset: true }),
};
export const LaunchConfigSummarySchema = z.object({
  ...launchConfigFields,
  envNames: z.array(z.string()),
});
export type LaunchConfigSummary = z.infer<typeof LaunchConfigSummarySchema>;
export const LaunchConfigSchema = z.object({
  ...launchConfigFields,
  env: z.record(z.string(), z.string()),
});
export type LaunchConfig = z.infer<typeof LaunchConfigSchema>;
export const LaunchConfigsSchema = z
  .array(LaunchConfigSummarySchema)
  .max(100)
  .refine(
    (configs) =>
      new Set(configs.map((config) => config.id)).size === configs.length,
    "启动配置 ID 不能重复",
  );

export const SessionEventSchema = z.discriminatedUnion("type", [
  z.object({
    type: z.literal("snapshot"),
    sessions: z.array(SessionInfoSchema),
  }),
  z.object({ type: z.literal("created"), session: SessionInfoSchema }),
  z.object({ type: z.literal("updated"), session: SessionInfoSchema }),
  z.object({ type: z.literal("removed"), id: SessionInfoSchema.shape.id }),
]);
export const ExitSchema = z.object({ exitCode: z.number().int() });
// Terminal stream frames 0x04–0x06; see docs/protocol.md.
const OffsetSchema = z.number().int().nonnegative();
export const StreamPositionSchema = z.object({
  generation: SessionInfoSchema.shape.generation,
  mode: z.enum(["reset", "append"]),
  start: OffsetSchema,
  end: OffsetSchema,
  truncated: z.boolean(),
});
export type StreamPosition = z.infer<typeof StreamPositionSchema>;
export const SubmissionIdSchema = z.string().regex(/^[A-Za-z0-9_-]{8,64}$/);
export const SubmissionStatusSchema = z.object({
  id: SubmissionIdSchema,
  state: z.enum(["received", "written", "partial", "failed", "missing"]),
  error: z.string().optional(),
});
export type SubmissionStatus = z.infer<typeof SubmissionStatusSchema>;
export const ControlStateSchema = z.object({
  controller: z
    .object({
      clientId: z.string(),
      label: z.string(),
      since: z.iso.datetime({ offset: true }),
    })
    .nullable(),
  cols: SessionInfoSchema.shape.cols,
  rows: SessionInfoSchema.shape.rows,
  // The program enabled DEC mode 2031 (color scheme change reports).
  colorSchemeReports: z.boolean(),
});
export type ControlState = z.infer<typeof ControlStateSchema>;
// Client-side progress of one submission. "saved" exists only in this
// browser; "unknown" means the acknowledgement was lost and not yet resolved.
export const SubmissionProgressSchema = z.enum([
  "saved",
  "sending",
  "received",
  "written",
  "partial",
  "failed",
  "missing",
  "unknown",
]);
export type SubmissionProgress = z.infer<typeof SubmissionProgressSchema>;
export const StoredDraftSchema = z.object({
  v: z.literal(1),
  sessionId: SessionInfoSchema.shape.id,
  generation: SessionInfoSchema.shape.generation.nullable(),
  text: z.string(),
  savedAt: z.number().int().nonnegative(),
  submission: z
    .object({
      id: SubmissionIdSchema,
      state: SubmissionProgressSchema,
      at: z.number().int().nonnegative(),
    })
    .nullable(),
});
export type StoredDraft = z.infer<typeof StoredDraftSchema>;
export const ScratchEntrySchema = z.object({
  id: z.string().regex(/^[A-Za-z0-9_-]{8,64}$/),
  text: z.string(),
  createdAt: z.number().int().nonnegative(),
  updatedAt: z.number().int().nonnegative(),
});
export type ScratchEntry = z.infer<typeof ScratchEntrySchema>;
export const StoredScratchSchema = z
  .object({
    v: z.literal(1),
    sessionId: SessionInfoSchema.shape.id,
    generation: SessionInfoSchema.shape.generation.nullable(),
    entries: z.array(ScratchEntrySchema),
    savedAt: z.number().int().nonnegative(),
  })
  .refine(
    (scratch) =>
      new Set(scratch.entries.map((entry) => entry.id)).size ===
      scratch.entries.length,
    "暂存 ID 不能重复",
  );
export type StoredScratch = z.infer<typeof StoredScratchSchema>;
export const HealthSchema = z.object({
  ok: z.literal(true),
  sessions: z.number().int().nonnegative(),
});
export const WorkspaceCapabilitySchema = z.object({
  gitAvailable: z.boolean(),
  registryAvailable: z.boolean(),
  gitError: z.string(),
  registryError: z.string(),
});
export const TunnelInfoSchema = z.object({
  enabled: z.boolean(),
  state: z.enum(["off", "starting", "ready", "failed"]),
  named: z.boolean(),
  hostname: z.string(),
  publicUrl: z.string().optional(),
});
export type TunnelInfo = z.infer<typeof TunnelInfoSchema>;

export const TunnelSettingsSchema = z.object({
  enabled: z.boolean(),
  mode: z.enum(["quick", "named"]),
  hostname: z.string(),
  tokenConfigured: z.boolean(),
  source: z.enum(["web", "config"]),
  state: TunnelInfoSchema.shape.state,
  publicUrl: z.string().optional(),
  error: z.string().optional(),
  canEnable: z.boolean(),
  enableBlockedReason: z.string().optional(),
  storageAvailable: z.boolean(),
  storageError: z.string().optional(),
});
export type TunnelSettings = z.infer<typeof TunnelSettingsSchema>;
export const TunnelSettingsUpdateSchema = z.object({
  mode: z.enum(["quick", "named"]),
  hostname: z.string(),
  token: z.string().optional(),
});
export type TunnelSettingsUpdate = z.infer<typeof TunnelSettingsUpdateSchema>;
export const ConfigSchema = z.object({
  defaultCwd: z.string(),
  // Older servers do not send this display-only field.
  hostname: z.string().default(""),
  workspace: WorkspaceCapabilitySchema,
  // Older servers do not send the runtime tunnel state.
  tunnel: TunnelInfoSchema.optional(),
});
export const WorktreeSchema = z.object({
  id: z.string(),
  repositoryId: z.string(),
  path: z.string(),
  kind: z.enum(["main", "linked", "bare"]),
  branch: z.string().nullable(),
  head: z.string().nullable(),
  detached: z.boolean(),
  locked: z.boolean(),
  lockReason: z.string(),
  prunable: z.boolean(),
  available: z.boolean(),
  dirty: z.boolean().nullable(),
  hasUntracked: z.boolean(),
  hasIgnored: z.boolean(),
  error: z.string(),
});
export type Worktree = z.infer<typeof WorktreeSchema>;
export const RepositorySchema = z.object({
  id: z.string(),
  name: z.string(),
  commonDir: z.string(),
  anchorPath: z.string(),
  availability: z.enum(["available", "unavailable"]),
  error: z.string(),
  checkedAt: z.iso.datetime({ offset: true }),
  worktrees: z.array(WorktreeSchema),
});
export type Repository = z.infer<typeof RepositorySchema>;
export const RepositoriesSchema = z.array(RepositorySchema);
// Browser-local choice of worktrees shown on the home page, keyed by
// repository ID. A repository without an entry was never curated.
export const HomeWorktreesSchema = z.record(
  z.string().min(1),
  z.array(z.string().min(1)).max(1000),
);
export type HomeWorktrees = z.infer<typeof HomeWorktreesSchema>;
export const BranchesSchema = z.object({
  branches: z.array(
    z.object({
      name: z.string(),
      head: z.string(),
      worktreePaths: z.array(z.string()),
    }),
  ),
  bases: z.array(z.object({ name: z.string(), head: z.string() })),
  defaultBase: z.string().nullable(),
  defaultParent: z.string(),
});
export const RemovalPreviewSchema = z.object({
  confirmationId: z.string(),
  expiresAt: z.iso.datetime({ offset: true }),
  worktree: WorktreeSchema,
  sessions: z.array(SessionInfoSchema),
  blockers: z.array(z.string()),
  forceAllowed: z.boolean(),
});
export type RemovalPreview = z.infer<typeof RemovalPreviewSchema>;
export const DirectoriesSchema = z.array(z.string());
// `reason` accompanies push 409s; other errors omit it.
export const ErrorSchema = z.object({
  error: z.string(),
  reason: z.string().optional(),
  notPersisted: z.boolean().optional(),
});
export const UploadSchema = z.object({ path: z.string().startsWith("/") });
export const RecentDirsSchema = z
  .array(z.string())
  .transform((dirs) => dirs.slice(0, 10));
export const FontSizeSchema = z.coerce.number().int().min(12).max(20);
export const ThemeSchema = z.enum(["light", "dark"]);
export type Theme = z.infer<typeof ThemeSchema>;
export const TextSettingSchema = z.string();
export const PreviewSettingsSchema = z.object({
  lines: z.number().int().min(1).max(50),
  skip: z.number().int().min(0).max(50),
});
export type PreviewSettings = z.infer<typeof PreviewSettingsSchema>;
export const QuickCommandSchema = z.object({
  id: z.string().min(1).max(64),
  name: z.string().max(40),
  command: z
    .string()
    .max(4096)
    .refine((value) => value.trim() !== "", "指令不能为空"),
  enter: z.boolean(),
});
export type QuickCommand = z.infer<typeof QuickCommandSchema>;
export const QuickCommandsSchema = z
  .array(QuickCommandSchema)
  .max(50)
  .refine(
    (commands) =>
      new Set(commands.map((cmd) => cmd.id)).size === commands.length,
    "快捷指令 ID 不能重复",
  );
export const NoContentSchema = z.undefined();
export const PushReasonSchema = z.enum([
  "disabled",
  "storage",
  "quick_tunnel",
  "unsupported_origin",
]);
export type PushReason = z.infer<typeof PushReasonSchema>;
export const PushStatusSchema = z.object({
  enabled: z.boolean(),
  available: z.boolean(),
  reason: PushReasonSchema.optional(),
  // Base64url uncompressed P-256 VAPID key, present only when available.
  publicKey: z.string().optional(),
  subscriptions: z.number().int().min(0),
});
export type PushStatus = z.infer<typeof PushStatusSchema>;
export const PushSubscriptionResultSchema = z.object({ id: z.string() });
// A test notification goes to every device; failures name only the push
// service host, and removed lists subscriptions the service reported expired.
export const PushTestResultSchema = z.object({
  devices: z.number().int().min(0),
  accepted: z.number().int().min(0),
  removed: z.array(z.string()),
  errors: z.array(z.string()),
});
export type PushTestResult = z.infer<typeof PushTestResultSchema>;

const CodeSourceSchema = z.enum(["worktree", "index", "head", "empty"]);
export const CodeContextSchema = z.object({
  contextId: z.string(),
  rootPath: z.string(),
  rootKind: z.enum(["directory", "worktree"]),
  browseAvailable: z.boolean(),
  diffAvailable: z.boolean(),
  ignoreAvailable: z.boolean(),
  reason: z.string(),
});
export const CodeNodeSchema = z.object({
  nodeId: z.string(),
  path: z.string(),
  name: z.string(),
  kind: z.enum(["file", "directory", "symlink", "boundary", "special"]),
  gitStatus: z.string(),
  ignored: z.boolean(),
  referenceable: z.boolean(),
  mediaType: z.string(),
  linkTarget: z.string().optional(),
});
export const CodeListingSchema = z.object({
  children: z.array(CodeNodeSchema),
  nextCursor: z.string(),
  limited: z.boolean(),
  complete: z.boolean(),
  contentVersion: z.string(),
  checkedAt: z.string(),
});
export const CodeLocationSchema = z.object({
  nodeId: z.string(),
  path: z.string(),
  kind: z.enum(["file", "directory"]),
});
export const CodeDocumentSchema = z.object({
  nodeId: z.string().optional(),
  path: z.string(),
  absolutePath: z.string(),
  source: CodeSourceSchema,
  oid: z.string(),
  contentVersion: z.string(),
  kind: z.enum([
    "text",
    "binary",
    "unsupported",
    "too-large",
    "symlink",
    "image",
  ]),
  text: z.string(),
  lineCount: z.number().int().nonnegative().max(20000),
  referenceable: z.boolean(),
  reason: z.string(),
  mediaType: z.string(),
  size: z.number().int().nonnegative(),
  downloadable: z.boolean(),
});
export const CodeFileSchema = z.object({
  snapshotId: z.string(),
  document: CodeDocumentSchema,
  checkedAt: z.string(),
});
export const CodeChangeSchema = z.object({
  nodeId: z.string().optional(),
  changeId: z.string(),
  group: z.enum(["unstaged", "staged", "untracked"]),
  oldPath: z.string(),
  newPath: z.string(),
  status: z.string(),
  oldOid: z.string(),
  newOid: z.string(),
  oldMode: z.string(),
  newMode: z.string(),
});
export const CodeChangesSchema = z.object({
  nextCursor: z.string(),
  total: z.number().int().nonnegative(),
  entries: z.array(CodeChangeSchema).max(200),
  contentVersion: z.string(),
  checkedAt: z.string(),
});
export const CodeDiffSchema = z.object({
  snapshotId: z.string(),
  old: CodeDocumentSchema,
  new: CodeDocumentSchema,
  kind: z.enum(["text", "metadata", "image"]),
  reason: z.string(),
  checkedAt: z.string(),
  change: CodeChangeSchema,
});
const CodeStateSchema = z.enum([
  "updated",
  "unchanged",
  "missing",
  "expired",
  "unknown",
]);
export const CodeCheckSchema = z.object({
  snapshots: z.record(z.string(), CodeStateSchema),
  directories: z.record(z.string(), CodeStateSchema),
  changes: CodeStateSchema,
  checkedAt: z.string(),
});
export type CodeDocument = z.infer<typeof CodeDocumentSchema>;
export type CodeFile = z.infer<typeof CodeFileSchema>;
export type CodeDiff = z.infer<typeof CodeDiffSchema>;
export type CodeChange = z.infer<typeof CodeChangeSchema>;
export type CodeListing = z.infer<typeof CodeListingSchema>;
