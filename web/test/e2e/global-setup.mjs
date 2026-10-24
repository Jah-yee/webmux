import { spawn } from "node:child_process";
import { once } from "node:events";
import { constants } from "node:fs";
import {
  access,
  cp,
  mkdir,
  mkdtemp,
  realpath,
  rm,
  stat,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const repoRoot = resolve(webRoot, "..");

async function build(command, args, cwd) {
  const child = spawn(command, args, {
    cwd,
    detached: true,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  child.stdout.on("data", (chunk) => (output += chunk));
  child.stderr.on("data", (chunk) => (output += chunk));
  const stop = () => {
    if (!child.pid) return;
    try {
      process.kill(-child.pid, "SIGKILL");
    } catch (error) {
      if (error.code !== "ESRCH") throw error;
    }
  };
  const deadline = setTimeout(stop, 180_000);
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
  try {
    const [code, signal] = await once(child, "exit");
    if (code !== 0)
      throw new Error(`${command} build exited ${code ?? signal}\n${output}`);
  } finally {
    clearTimeout(deadline);
    process.removeListener("SIGINT", stop);
    process.removeListener("SIGTERM", stop);
  }
}

// Ubuntu CI supplies one freshly built executable to every shard. Other runs
// build once per invocation. Workers and retries share only the executable;
// each server owns its ports, HOME, state directory, repositories and PTYs.
export default async function globalSetup() {
  const prepared = process.env.WEBMUX_E2E_BINARY;
  if (prepared) {
    try {
      const binary = await realpath(prepared);
      if (!(await stat(binary)).isFile())
        throw new Error("expected an executable file");
      await access(binary, constants.X_OK);
      process.env.WEBMUX_E2E_BINARY = binary;
      console.log("E2E build: using prepared binary");
      return;
    } catch (error) {
      throw new Error(`Invalid WEBMUX_E2E_BINARY: ${error.message}`, {
        cause: error,
      });
    }
  }
  const root = await realpath(
    await mkdtemp(join(tmpdir(), "webmux-e2e-build-")),
  );
  const binary = join(root, "webmux");
  try {
    // Keep both the embed inputs and Go source snapshot private to this run.
    // A concurrent make web / E2E run may replace the checkout's web/dist.
    await mkdir(join(root, "web"));
    await Promise.all([
      ...["cmd", "internal", "deploy", "go.mod", "go.sum"].map((name) =>
        cp(join(repoRoot, name), join(root, name), { recursive: true }),
      ),
      cp(join(webRoot, "embed.go"), join(root, "web/embed.go")),
    ]);
    const started = performance.now();
    if (process.env.WEBMUX_E2E_DIST) {
      // The combined CI job has just built this checkout's production app
      // for Go's embed. Snapshot it before starting workers; local runs build
      // from source unless the caller explicitly supplies that fresh output.
      await cp(
        await realpath(process.env.WEBMUX_E2E_DIST),
        join(root, "web/dist"),
        {
          recursive: true,
        },
      );
    } else {
      await build(
        process.execPath,
        [
          join(webRoot, "node_modules/vite/bin/vite.js"),
          "build",
          "--outDir",
          join(root, "web/dist"),
        ],
        webRoot,
      );
    }
    const frontendDone = performance.now();
    await build(
      "go",
      ["build", "-trimpath", "-o", binary, "./cmd/webmux"],
      root,
    );
    console.log(
      `E2E build: frontend ${((frontendDone - started) / 1000).toFixed(2)}s; Go ${((performance.now() - frontendDone) / 1000).toFixed(2)}s`,
    );
    process.env.WEBMUX_E2E_BINARY = binary;
  } catch (error) {
    await rm(root, { recursive: true, force: true });
    throw error;
  }
  return async () => {
    delete process.env.WEBMUX_E2E_BINARY;
    await rm(root, { recursive: true, force: true });
  };
}
