import { SearchQuery } from "@codemirror/search";
import { Text } from "@codemirror/state";

export type SearchSpec = {
  search: string;
  caseSensitive: boolean;
  regexp: boolean;
  wholeWord: boolean;
};

/** Matching stops here; the status then reads `n / 5000+`. */
export const MATCH_LIMIT = 5000;

/**
 * The 1-based line of every match of `spec` in `lines` (each keeping its own
 * line terminator, as in FileDiffMetadata), in document order and capped at
 * MATCH_LIMIT. Empty or invalid queries match nothing.
 */
export function findMatchLines(
  lines: readonly string[],
  spec: SearchSpec,
): number[] {
  if (!spec.search) return [];
  const query = new SearchQuery(spec);
  if (!query.valid) return [];
  const doc = Text.of(lines.join("").split("\n"));
  const cursor = query.getCursor(doc);
  const result: number[] = [];
  for (
    let match = cursor.next();
    !match.done && result.length < MATCH_LIMIT;
    match = cursor.next()
  ) {
    result.push(doc.lineAt(match.value.from).number);
  }
  return result;
}

/**
 * The current position within one particular match list. A cursor only
 * applies to the list it was created for: once the query or searched text
 * changes, a new list is computed and the position starts over.
 */
export type MatchCursor = { matches: readonly number[]; index: number };

/** The current match index in `matches`, or -1 before the first step. */
export function matchIndex(
  cursor: MatchCursor | undefined,
  matches: readonly number[],
): number {
  return cursor?.matches === matches ? cursor.index : -1;
}

/**
 * Moves to the next (1) or previous (-1) match, wrapping at both ends. Before
 * the first step, next selects the first match and previous the last one.
 * Returns undefined when there is nothing to move to.
 */
export function stepMatch(
  cursor: MatchCursor | undefined,
  matches: readonly number[],
  direction: 1 | -1,
): MatchCursor | undefined {
  if (!matches.length) return;
  const index = matchIndex(cursor, matches);
  const current = index < 0 && direction < 0 ? 0 : index;
  return {
    matches,
    index: (current + direction + matches.length) % matches.length,
  };
}

/** `n / total` (with `+` once capped), or 无匹配. */
export function matchStatus(
  cursor: MatchCursor | undefined,
  matches: readonly number[],
): string {
  if (!matches.length) return "无匹配";
  const capped = matches.length === MATCH_LIMIT ? "+" : "";
  return `${matchIndex(cursor, matches) + 1} / ${matches.length}${capped}`;
}
