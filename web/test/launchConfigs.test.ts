import { describe, expect, test } from "vitest";
import { joinArgs, parseArgs, quoteArg } from "../src/api";
import {
  launchCommandDraft,
  launchConfigIcon,
  validateLaunchDraft,
} from "../src/launchConfigs";
import { LaunchConfigSchema, LaunchConfigsSchema } from "../src/schemas";

describe("launch icons", () => {
  test.each([
    ["claude", "claudecode"],
    ["codex", "codex"],
    ["gemini", "geminicli"],
    ["opencode", "opencode"],
    ["omp", "pi"],
    ["/usr/local/bin/pi", "pi"],
    ["C:\\tools\\codex.exe", "codex"],
    ["bash", "terminal"],
  ])("matches legacy %s commands without changing argv", (program, icon) => {
    const cmd = [program, "--resume"];
    expect(launchConfigIcon({ cmd })).toBe(icon);
    expect(
      launchConfigIcon({
        cmd: ["bash", "-ic", 'exec -- "$@"', "webmux", ...cmd],
      }),
    ).toBe(icon);
    expect(cmd).toEqual([program, "--resume"]);
  });

  test("an explicit icon overrides matching and arbitrary shell scripts stay generic", () => {
    expect(launchConfigIcon({ icon: "terminal", cmd: ["claude"] })).toBe(
      "terminal",
    );
    expect(launchConfigIcon({ cmd: ["bash", "-ic", "exec claude"] })).toBe(
      "terminal",
    );
  });
});

describe("quoteArg / joinArgs", () => {
  test("leaves plain arguments readable and quotes the rest", () => {
    expect(quoteArg("claude")).toBe("claude");
    expect(quoteArg("--model=opus-4.1")).toBe("--model=opus-4.1");
    expect(quoteArg("")).toBe("''");
    expect(quoteArg("a b")).toBe("'a b'");
    expect(quoteArg("$HOME")).toBe("'$HOME'");
    expect(quoteArg("it's")).toBe(`"it's"`);
    expect(quoteArg(`it's "$x"`)).toBe(`"it's \\"\\$x\\""`);
    expect(joinArgs(["bash", "-lc", "npm run dev"])).toBe(
      "bash -lc 'npm run dev'",
    );
  });

  const tricky = [
    "",
    " ",
    "a b",
    "\t\n",
    "'",
    '"',
    "''",
    '""',
    "\\",
    "\\\\",
    "a\\b",
    "C:\\path\\",
    "$",
    "$HOME",
    "${X:-y}",
    "`id`",
    `it's "quoted" \\ $x`,
    `'"\\$`,
    "line1\nline2",
    "中文 参数",
    "emoji 😀",
    "a;b|c&d",
    "*?[]{}()<>!#~",
    "--flag=value with space",
    "=",
    "-",
  ];

  test.each(tricky)("round-trips %j", (arg) => {
    expect(parseArgs(quoteArg(arg))).toEqual([arg]);
  });

  test("round-trips every combination of tricky arguments", () => {
    for (const first of tricky)
      for (const second of tricky) {
        const argv = ["cmd", first, second, ""];
        expect(parseArgs(joinArgs(argv))).toEqual(argv);
      }
  });

  test("round-trips random argv built from tricky characters", () => {
    const alphabet = [..."ab \t\n'\"\\$`;|中😀-="];
    let seed = 1;
    const random = (n: number) => {
      seed = (seed * 1103515245 + 12345) % 2 ** 31;
      return seed % n;
    };
    for (let run = 0; run < 500; run++) {
      const argv = Array.from({ length: 1 + random(5) }, () =>
        Array.from(
          { length: random(8) },
          () => alphabet[random(alphabet.length)],
        ).join(""),
      );
      expect(parseArgs(joinArgs(argv))).toEqual(argv);
    }
  });
});

describe("validateLaunchDraft", () => {
  test("trims the name, parses the command and collects env", () => {
    const result = validateLaunchDraft("  Claude  ", "claude --resume ''", [
      { key: 1, name: "API_KEY", value: "secret" },
      { key: 2, name: "", value: "" },
    ]);
    expect(result).toMatchObject({
      ok: true,
      value: {
        name: "Claude",
        cmd: ["claude", "--resume", ""],
        env: { API_KEY: "secret" },
      },
    });
  });

  test("rejects reserved, malformed and duplicate env names", () => {
    const result = validateLaunchDraft("x", "sh", [
      { key: 1, name: "WEBMUX_SESSION_ID", value: "1" },
      { key: 2, name: "1BAD", value: "1" },
      { key: 3, name: "OK", value: "1" },
      { key: 4, name: "OK", value: "2" },
      { key: 5, name: "", value: "orphan" },
    ]);
    expect(result.ok).toBe(false);
    expect(Object.keys(result.errors.rows)).toEqual(["1", "2", "4", "5"]);
  });

  test("enforces name, argv and value limits", () => {
    expect(validateLaunchDraft(" ", "sh", []).errors.name).toBeTruthy();
    expect(
      validateLaunchDraft("名".repeat(64), "sh", []).errors.name,
    ).toBeUndefined();
    expect(validateLaunchDraft("名".repeat(65), "sh", []).errors.name).toBe(
      "名称最多 64 个字符",
    );
    expect(validateLaunchDraft("x", "'' a", []).errors.command).toBeTruthy();
    expect(validateLaunchDraft("x", "'a", []).errors.command).toBeTruthy();
    expect(
      validateLaunchDraft("x", Array(129).fill("a").join(" "), []).errors
        .command,
    ).toBeTruthy();
    expect(
      validateLaunchDraft("x", "a".repeat(8193), []).errors.command,
    ).toBeTruthy();
    expect(
      validateLaunchDraft("x", "sh", [
        { key: 1, name: "A", value: "é".repeat(4097) },
      ]).errors.rows[1]?.value,
    ).toBeTruthy();
    expect(
      validateLaunchDraft(
        "x",
        "sh",
        Array.from({ length: 65 }, (_, key) => ({
          key,
          name: `V${key}`,
          value: "",
        })),
      ).errors.env,
    ).toBeTruthy();
  });

  test("Bash loading keeps quoted, empty and shell-looking arguments literal", () => {
    const argv = [
      "opencode",
      "two words",
      "",
      "$HOME",
      "$(touch sentinel)",
      "; exit 9",
    ];
    const result = validateLaunchDraft("Open Code", joinArgs(argv), [], true);
    expect(result.ok).toBe(true);
    if (!result.ok) throw new Error("Expected valid Bash launch");
    expect(result.value.cmd).toEqual([
      "bash",
      "-ic",
      'exec -- "$@"',
      "webmux",
      ...argv,
    ]);
    const restored = launchCommandDraft(result.value.cmd);
    expect(restored.loadBashrc).toBe(true);
    expect(parseArgs(restored.command)).toEqual(argv);
    expect(
      validateLaunchDraft("Open Code", restored.command, [], true),
    ).toEqual(result);
    const direct = validateLaunchDraft(
      "Open Code",
      restored.command,
      [],
      false,
    );
    expect(direct.ok && direct.value.cmd).toEqual(argv);
  });

  test("Bash loading cannot hide an empty command or bypass the stored argv limit", () => {
    for (const command of ["", "''", "echo 'unfinished"])
      expect(validateLaunchDraft("Agent", command, [], true).ok).toBe(false);
    expect(
      validateLaunchDraft("Agent", Array(124).fill("arg").join(" "), [], true)
        .ok,
    ).toBe(true);
    expect(
      validateLaunchDraft("Agent", Array(125).fill("arg").join(" "), [], true)
        .ok,
    ).toBe(false);
  });

  test("editing preserves a user-written Bash script that is not the checkbox wrapper", () => {
    const argv = ["bash", "-ic", 'exec opencode "$@"', "opencode"];
    const restored = launchCommandDraft(argv);
    expect(restored.loadBashrc).toBe(false);
    expect(parseArgs(restored.command)).toEqual(argv);
  });
});

describe("launch config schemas", () => {
  const summary = {
    id: "abcdefg2",
    name: "Claude",
    cmd: ["claude"],
    envNames: [],
    createdAt: "2026-10-23T00:00:00Z",
    updatedAt: "2026-10-23T00:00:00Z",
  };

  test("accepts summaries and full configs", () => {
    expect(LaunchConfigsSchema.parse([summary])).toEqual([summary]);
    const { envNames: _names, ...rest } = summary;
    expect(LaunchConfigSchema.parse({ ...rest, env: { A: "1" } }).env).toEqual({
      A: "1",
    });
  });

  test("rejects duplicate ids, more than 100 entries and bad ids", () => {
    expect(LaunchConfigsSchema.safeParse([summary, summary]).success).toBe(
      false,
    );
    expect(
      LaunchConfigsSchema.safeParse(
        Array.from({ length: 101 }, (_, i) => ({
          ...summary,
          id: `abcd${"234567"[i % 6]}${"234567"[Math.floor(i / 6) % 6]}${"abcdef"[Math.floor(i / 36)]}a`,
        })),
      ).success,
    ).toBe(false);
    expect(
      LaunchConfigsSchema.safeParse([{ ...summary, id: "ABC" }]).success,
    ).toBe(false);
  });
});
