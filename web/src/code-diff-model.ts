import { z } from "zod";
import type { FileDiffMetadata } from "@pierre/diffs";

const count = z.number().int().min(0).max(40002);
// The absent side of an added/deleted file uses -1 as its source index.
const lineIndex = z.number().int().min(-1).max(20000);
const content = z.discriminatedUnion("type", [
  z.object({
    type: z.literal("context"),
    lines: count,
    additionLineIndex: lineIndex,
    deletionLineIndex: lineIndex,
  }),
  z.object({
    type: z.literal("change"),
    additions: count,
    deletions: count,
    additionLineIndex: lineIndex,
    deletionLineIndex: lineIndex,
  }),
]);

// A Worker boundary is a data boundary too. Validate the renderer's model
// before passing it to the view, just as we validate the API snapshot.
export const FileDiffSchema = z.object({
  name: z.string(),
  prevName: z.string().optional(),
  type: z.enum(["change", "rename-pure", "rename-changed", "new", "deleted"]),
  hunks: z
    .array(
      z.object({
        collapsedBefore: count,
        additionStart: count,
        additionCount: count,
        additionLines: count,
        additionLineIndex: lineIndex,
        deletionStart: count,
        deletionCount: count,
        deletionLines: count,
        deletionLineIndex: lineIndex,
        hunkContent: z.array(content).max(40002),
        hunkContext: z.string().optional(),
        hunkSpecs: z.string().optional(),
        splitLineStart: count,
        splitLineCount: count,
        unifiedLineStart: count,
        unifiedLineCount: count,
        noEOFCRDeletions: z.boolean(),
        noEOFCRAdditions: z.boolean(),
      }),
    )
    .max(20000),
  splitLineCount: count,
  unifiedLineCount: count,
  isPartial: z.literal(false),
  deletionLines: z.array(z.string()).max(20000),
  additionLines: z.array(z.string()).max(20000),
}) satisfies z.ZodType<FileDiffMetadata>;

const DiffResultSchema = z.discriminatedUnion("kind", [
  z.object({ kind: z.literal("ready"), fileDiff: FileDiffSchema }),
  z.object({ kind: z.literal("limited"), reason: z.string() }),
]);
export type DiffResult = z.infer<typeof DiffResultSchema>;
export const DiffReplySchema = z.object({
  snapshotId: z.string(),
  result: DiffResultSchema,
});
