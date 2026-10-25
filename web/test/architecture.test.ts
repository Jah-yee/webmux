// Frontend architecture guard: the gaps that ESLint, dependency-cruiser,
// jscpd and knip do not cover. See docs/frontend-architecture.md.
import { globSync, readFileSync } from "node:fs";
import { basename, dirname, extname } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, test } from "vitest";

const root = fileURLToPath(new URL("..", import.meta.url));
const UI_DIR = "src/lib/components/ui/";

const files = globSync("src/**/*.svelte", { cwd: root })
  .map((file) => file.replaceAll("\\", "/"))
  .sort();

/** SessionRow / session-row / session_row / session-row/index → "sessionrow" */
function componentKey(file: string): string {
  let name = basename(file, extname(file));
  if (name === "index") name = basename(dirname(file));
  return name.toLowerCase().replace(/[-_.]/g, "");
}

function groupBy<T>(items: T[], key: (item: T) => string): Map<string, T[]> {
  const groups = new Map<string, T[]>();
  for (const item of items) {
    const k = key(item);
    groups.set(k, [...(groups.get(k) ?? []), item]);
  }
  return groups;
}

// Static class strings: class="…", class='…' and cn("…").
const CLASS_PATTERNS = [
  /\bclass\s*=\s*"([^"{}$]+)"/g,
  /\bclass\s*=\s*'([^'{}$]+)'/g,
  /\bcn\(\s*["'`]([^"'`$]+)["'`]/g,
];

function classStrings(source: string): string[] {
  return CLASS_PATTERNS.flatMap((pattern) =>
    [...source.matchAll(pattern)].map((match) => match[1]),
  );
}

// Placement classes are a call site's business; only appearance is compared.
const LAYOUT_CLASS =
  /^(?:[a-z0-9-]+:)*(?:m[trblxyse]?-|w-|h-|min-|max-|col-|row-|order-|self-|justify-self-|place-self-|grow|shrink|basis-|flex-1|hidden|block)/;

function appearanceKey(classString: string): string {
  return classString
    .split(/\s+/)
    .filter((name) => name && !LAYOUT_CLASS.test(name))
    .sort()
    .join(" ");
}

const MIN_CLASSES = 6;
const MAX_FILES = 2;

/** Accepted repeats; each needs a reason and review. */
const CLASS_STRING_ALLOWLIST = new Set<string>([]);

describe("frontend architecture", () => {
  test("scans component files", () => {
    expect(files.length).toBeGreaterThan(0);
    expect(files.some((file) => file.startsWith(UI_DIR))).toBe(true);
  });

  test("each business component name has one implementation", () => {
    const duplicates = [
      ...groupBy(
        files.filter((file) => !file.startsWith(UI_DIR)),
        componentKey,
      ),
    ]
      .filter(([, list]) => list.length > 1)
      .map(([key, list]) => `${key}: ${list.join(", ")}`);

    expect(
      duplicates,
      "Keep one implementation in src/components and import it elsewhere.",
    ).toEqual([]);
  });

  test("business code does not re-create design-system components", () => {
    const uiKeys = new Map(
      files
        .filter((file) => file.startsWith(UI_DIR))
        .map((file) => [componentKey(file), file] as const),
    );
    const shadowing = files
      .filter(
        (file) => !file.startsWith(UI_DIR) && uiKeys.has(componentKey(file)),
      )
      .map((file) => `${file} → ${uiKeys.get(componentKey(file))}`);

    expect(
      shadowing,
      "Use the design-system component, adding a variant if needed.",
    ).toEqual([]);
  });

  test(`appearance of ${MIN_CLASSES}+ classes is not copied into more than ${MAX_FILES} files`, () => {
    const seen = new Map<string, Set<string>>();
    for (const file of files) {
      if (file.startsWith(UI_DIR)) continue;
      const source = readFileSync(`${root}/${file}`, "utf8");
      for (const raw of classStrings(source)) {
        const key = appearanceKey(raw);
        if (key.split(" ").length < MIN_CLASSES) continue;
        if (CLASS_STRING_ALLOWLIST.has(key)) continue;
        seen.set(key, (seen.get(key) ?? new Set()).add(file));
      }
    }
    const copied = [...seen]
      .filter(([, where]) => where.size > MAX_FILES)
      .map(([key, where]) => `"${key}": ${[...where].join(", ")}`);

    expect(
      copied,
      "Extract a component into src/components or add a design-system variant.",
    ).toEqual([]);
  });
});
