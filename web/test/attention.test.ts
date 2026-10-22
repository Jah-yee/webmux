import { describe, expect, test } from "vitest";
import { sessionStatusText } from "../src/api";
import { SessionInfoSchema } from "../src/schemas";

const session = {
  id: "abcdefgh",
  generation: "0123456789abcdef",
  title: "omp",
  cwd: "/work",
  preset: "omp",
  cmd: ["omp"],
  status: "waiting",
  exitCode: null,
  preview: "",
  createdAt: "2026-10-16T00:00:00Z",
  lastActiveAt: "2026-10-16T00:00:01Z",
  cols: 80,
  rows: 24,
  clients: 0,
  workspace: null,
  attention: null,
  reportedCwd: null,
};

describe("session attention contract", () => {
  test("accepts a null or well-formed attention", () => {
    expect(SessionInfoSchema.parse(session).attention).toBeNull();
    const attention = {
      kind: "permission",
      source: "agent",
      since: "2026-10-16T00:00:02.123456789+08:00",
    };
    expect(
      SessionInfoSchema.parse({ ...session, attention }).attention,
    ).toEqual(attention);
  });

  test("rejects unknown kinds and sources", () => {
    for (const attention of [
      { kind: "busy", source: "agent", since: "2026-10-16T00:00:02Z" },
      { kind: "done", source: "screen", since: "2026-10-16T00:00:02Z" },
      { kind: "done", source: "agent", since: "yesterday" },
    ])
      expect(
        SessionInfoSchema.safeParse({ ...session, attention }).success,
      ).toBe(false);
  });

  test("accepts a shell-reported directory", () => {
    const reportedCwd = {
      path: "/work/sub",
      host: "",
      remote: false,
      since: "2026-10-16T00:00:02Z",
    };
    expect(
      SessionInfoSchema.parse({ ...session, reportedCwd }).reportedCwd,
    ).toEqual(reportedCwd);
    expect(
      SessionInfoSchema.safeParse({
        ...session,
        reportedCwd: { ...reportedCwd, remote: "no" },
      }).success,
    ).toBe(false);
  });

  test("labels prefer the reported attention over the heuristic status", () => {
    expect(sessionStatusText({ status: "waiting", attention: null })).toBe(
      "可能在等你",
    );
    expect(
      sessionStatusText({
        status: "waiting",
        attention: {
          kind: "question",
          source: "agent",
          since: "2026-10-16T00:00:02Z",
        },
      }),
    ).toBe("需要回答");
    expect(sessionStatusText({ status: "running", attention: null })).toBe(
      "运行中",
    );
  });
});
