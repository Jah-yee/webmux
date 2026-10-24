import { mkdtemp, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import globalSetup from "./e2e/global-setup.mjs";

describe("prepared E2E runtime", () => {
  let root: string;

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), "webmux-prepared-e2e-"));
    vi.stubEnv("WEBMUX_E2E_DIST", join(root, "missing-dist"));
  });

  afterEach(async () => {
    vi.unstubAllEnvs();
    await rm(root, { recursive: true, force: true });
  });

  test("uses the supplied executable without building or taking cleanup ownership", async () => {
    const binary = join(root, "webmux");
    const contents = "#!/bin/sh\nexit 0\n";
    await writeFile(binary, contents, { mode: 0o700 });
    vi.stubEnv("WEBMUX_E2E_BINARY", binary);

    const teardown = await globalSetup();

    expect(process.env.WEBMUX_E2E_BINARY).toBe(await realpath(binary));
    expect(teardown).toBeUndefined();
    expect(await readFile(binary, "utf8")).toBe(contents);
  });

  test.each(["missing", "directory", "non-executable"])(
    "rejects a %s runtime instead of silently rebuilding",
    async (kind) => {
      const binary = kind === "directory" ? root : join(root, "webmux");
      if (kind === "non-executable")
        await writeFile(binary, "not executable", { mode: 0o600 });
      vi.stubEnv("WEBMUX_E2E_BINARY", binary);

      await expect(globalSetup()).rejects.toThrow("WEBMUX_E2E_BINARY");
    },
  );
});
