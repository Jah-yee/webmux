import { describe, expect, it } from "vitest";
import {
  MATCH_LIMIT,
  findMatchLines,
  matchIndex,
  matchStatus,
  stepMatch,
  type MatchCursor,
} from "../src/code-search";

const spec = (search: string, options: { regexp?: boolean } = {}) => ({
  search,
  caseSensitive: false,
  regexp: options.regexp ?? false,
  wholeWord: false,
});

describe("findMatchLines", () => {
  const lines = ["alpha\n", "beta alpha\n", "gamma\n", "ALPHA\n"];

  it("reports the 1-based line of every match, once per occurrence", () => {
    expect(findMatchLines(lines, spec("alpha"))).toEqual([1, 2, 4]);
    expect(findMatchLines(["a a\n", "a\n"], spec("a"))).toEqual([1, 1, 2]);
  });

  it("matches nothing for empty or invalid queries", () => {
    expect(findMatchLines(lines, spec(""))).toEqual([]);
    expect(findMatchLines(lines, spec("(", { regexp: true }))).toEqual([]);
  });

  it("stops at the match limit", () => {
    const many = Array.from({ length: MATCH_LIMIT + 10 }, () => "x\n");
    const matches = findMatchLines(many, spec("x"));
    expect(matches).toHaveLength(MATCH_LIMIT);
    expect(matches.at(-1)).toBe(MATCH_LIMIT);
  });
});

describe("match cursor", () => {
  const matches = [3, 7, 9];

  it("wraps forwards from the last match to the first", () => {
    let cursor: MatchCursor | undefined;
    const visited: (number | undefined)[] = [];
    for (let step = 0; step < 4; step++) {
      cursor = stepMatch(cursor, matches, 1);
      visited.push(cursor && matches[cursor.index]);
    }
    expect(visited).toEqual([3, 7, 9, 3]);
  });

  it("starts from the last match when stepping backwards first", () => {
    const last = stepMatch(undefined, matches, -1);
    expect(last?.index).toBe(2);
    const first = stepMatch({ matches, index: 0 }, matches, -1);
    expect(first?.index).toBe(2);
    expect(stepMatch(last, matches, -1)?.index).toBe(1);
  });

  it("does nothing without matches", () => {
    expect(stepMatch(undefined, [], 1)).toBeUndefined();
    expect(stepMatch({ matches: [], index: 0 }, [], -1)).toBeUndefined();
  });

  it("resets once the match list is recomputed", () => {
    const cursor = stepMatch(stepMatch(undefined, matches, 1), matches, 1);
    expect(matchIndex(cursor, matches)).toBe(1);
    // Same lines, new query or side: the old position no longer applies.
    const recomputed = [...matches];
    expect(matchIndex(cursor, recomputed)).toBe(-1);
    expect(stepMatch(cursor, recomputed, 1)?.index).toBe(0);
  });
});

describe("matchStatus", () => {
  it("labels position, total, cap and no matches", () => {
    const matches = [1, 2, 3];
    expect(matchStatus(undefined, matches)).toBe("0 / 3");
    expect(matchStatus({ matches, index: 2 }, matches)).toBe("3 / 3");
    expect(matchStatus(undefined, [])).toBe("无匹配");
    const capped = Array.from({ length: MATCH_LIMIT }, (_, i) => i + 1);
    expect(matchStatus({ matches: capped, index: 0 }, capped)).toBe(
      `1 / ${MATCH_LIMIT}+`,
    );
  });
});
