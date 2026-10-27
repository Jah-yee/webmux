import { describe, expect, test } from "vitest";
import { resolveE2EWorkers } from "../playwright.config";

describe("E2E workers", () => {
  test("uses up to two workers locally", () => {
    expect(resolveE2EWorkers({}, "linux", 8)).toBe(2);
    expect(resolveE2EWorkers({}, "linux", 1)).toBe(1);
  });

  test("keeps CI defaults per platform", () => {
    expect(resolveE2EWorkers({ CI: "1" }, "linux", 8)).toBe(2);
    expect(resolveE2EWorkers({ CI: "1" }, "darwin", 8)).toBe(4);
  });

  test("honours WEBMUX_E2E_WORKERS everywhere", () => {
    expect(resolveE2EWorkers({ WEBMUX_E2E_WORKERS: "3" }, "linux", 8)).toBe(3);
    expect(
      resolveE2EWorkers({ CI: "1", WEBMUX_E2E_WORKERS: "1" }, "darwin", 8),
    ).toBe(1);
  });

  test.each(["0", "1.5", "two"])(
    "rejects invalid WEBMUX_E2E_WORKERS %s",
    (value) => {
      expect(() => resolveE2EWorkers({ WEBMUX_E2E_WORKERS: value })).toThrow(
        "positive integer",
      );
    },
  );
});
