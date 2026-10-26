import { describe, it, expect } from "vitest";
import { computeDiff } from "../src/code-diff";
import { DiffReplySchema } from "../src/code-diff-model";

function ready(old: string, next: string) {
  const result = computeDiff(old, next);
  if (result.kind !== "ready") throw new Error(result.reason);
  return result.fileDiff;
}

describe("browser text diff", () => {
  it("maps both source documents, unequal replacements, additions and deletions", () => {
    const diff = ready("same\nold\nend\n", "same\nnew\nextra\nend\n");
    expect(diff.deletionLines).toEqual(["same\n", "old\n", "end\n"]);
    expect(diff.additionLines).toEqual(["same\n", "new\n", "extra\n", "end\n"]);
    expect(diff.hunks[0]!.hunkContent).toEqual([
      { type: "context", lines: 1, additionLineIndex: 0, deletionLineIndex: 0 },
      {
        type: "change",
        additions: 2,
        deletions: 1,
        additionLineIndex: 1,
        deletionLineIndex: 1,
      },
      { type: "context", lines: 1, additionLineIndex: 3, deletionLineIndex: 2 },
    ]);
    expect(ready("", "new\n").hunks[0]).toMatchObject({
      additionLines: 1,
      deletionLines: 0,
    });
    expect(ready("old\n", "").hunks[0]).toMatchObject({
      additionLines: 0,
      deletionLines: 1,
    });
    expect(ready("", "").hunks).toEqual([]);
  });
  it("preserves CRLF, blank lines and final-newline differences", () => {
    const crlf = ready("a\r\n\r\nb\r\n", "a\n\nb\n");
    expect(crlf.hunks).toEqual([]);
    expect(crlf.deletionLines.join("")).toBe("a\r\n\r\nb\r\n");
    expect(ready("a\nlast", "a\nlast\n").hunks[0]).toMatchObject({
      noEOFCRDeletions: true,
      noEOFCRAdditions: false,
    });
    expect(ready("last", "next").hunks[0]).toMatchObject({
      noEOFCRDeletions: true,
      noEOFCRAdditions: true,
    });
  });
  it("captures complete local context with original line indexes for each hunk", () => {
    const old =
      Array.from({ length: 200 }, (_, i) => `line ${i + 1}`).join("\n") + "\n";
    const next = old
      .replace("line 100\n", "changed 100\n")
      .replace("line 150\n", "changed 150\n");
    const diff = ready(old, next);
    expect(diff.isPartial).toBe(false);
    expect(diff.deletionLines.join("")).toBe(old);
    expect(diff.additionLines.join("")).toBe(next);
    expect(
      diff.hunks.map((h) => [
        h.collapsedBefore,
        h.deletionStart,
        h.additionStart,
      ]),
    ).toEqual([
      [96, 97, 97],
      [43, 147, 147],
    ]);
  });
  it("supports 20000 lines and reports size and time limits", () => {
    const old =
      Array.from({ length: 20000 }, (_, i) => `line ${i}`).join("\n") + "\n";
    expect(ready(old, "").deletionLines).toHaveLength(20000);
    expect(computeDiff(old + "extra\n", "").kind).toBe("limited");
    expect(computeDiff("界".repeat(750000), "").kind).toBe("limited");
    expect(computeDiff(old, old.replaceAll("line", "different"), -1).kind).toBe(
      "limited",
    );
  });
  it("rejects invalid renderer metadata at the Worker boundary", () => {
    const fileDiff = ready("old\n", "new\n");
    fileDiff.hunks[0]!.additionStart = -1;
    expect(
      DiffReplySchema.safeParse({
        snapshotId: "s",
        result: { kind: "ready", fileDiff },
      }).success,
    ).toBe(false);
  });
  it("round trips empty, repeated, Unicode and control-looking source text", () => {
    const samples = [
      "",
      "\n",
      "\n\n",
      "a",
      "a\n",
      "a\r\n",
      "a\r",
      "a\rb\n",
      "same\nsame\nlast",
      "界\n🙂\n",
      "<script>x</script>\n\x1b[31m",
    ];
    for (const old of samples)
      for (const next of samples) {
        const diff = ready(old, next);
        expect(diff.deletionLines.join("")).toBe(old);
        expect(diff.additionLines.join("")).toBe(next);
      }
  });
});
