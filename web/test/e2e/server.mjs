import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, mkdir, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

function signalChild(child, signal) {
  try {
    child.kill(signal);
  } catch (error) {
    if (error.code !== "ESRCH") throw error;
  }
}

async function stopChild(child) {
  if (!child?.pid || child.exitCode !== null || child.signalCode !== null)
    return;
  const exited = once(child, "exit");
  signalChild(child, "SIGTERM");
  const deadline = setTimeout(() => signalChild(child, "SIGKILL"), 15_000);
  deadline.unref();
  try {
    await exited;
  } finally {
    clearTimeout(deadline);
  }
}

/**
 * @typedef {object} E2EServer
 * @property {string} baseURL
 * @property {string} apiURL
 * @property {string} workDir
 * @property {() => Promise<void>} restart
 * @property {() => string} logs
 * @property {() => Promise<void>} close
 */

// Every Playwright worker owns a real server and a fresh HOME, config and cwd.
// Port zero asks the OS to allocate ports; no local developer server is reused.
/** @param {{noGit?: boolean, token?: string}} options @returns {Promise<E2EServer>} */
export async function startE2EServer({ noGit = false, token = "" } = {}) {
  const binary = process.env.WEBMUX_E2E_BINARY;
  if (!binary)
    throw new Error(
      "Run E2E through playwright.config.ts to build the backend",
    );
  // macOS tmpdir() sits behind /var -> /private/var; the server reports
  // canonical paths, so expectations must start from the canonical root.
  const root = await realpath(await mkdtemp(join(tmpdir(), "webmux-e2e-")));
  const workDir = join(root, "work");
  const home = join(root, "home");
  const config = join(root, "config.toml");
  let backend;
  let closing;
  let backendOutput = "";
  const logs = () => `Go backend:\n${backendOutput}`;
  const close = () =>
    (closing ??= (async () => {
      try {
        await stopChild(backend);
      } finally {
        await rm(root, { recursive: true, force: true });
      }
    })());
  const interrupted = () => {
    void close().finally(() => process.exit(130));
  };
  process.once("SIGINT", interrupted);
  process.once("SIGTERM", interrupted);
  try {
    await Promise.all([
      mkdir(workDir),
      mkdir(home),
      writeFile(
        config,
        `listen = "127.0.0.1:0"\ntoken = ${JSON.stringify(token)}\ndefault_cwd = ${JSON.stringify(workDir)}\nstate_dir = ${JSON.stringify(join(root, "state"))}\nshell = "/bin/bash"\nidle_after_seconds = 30\n\n[[presets]]\nname = "shell"\nlabel = "Shell"\ncmd = ["/bin/bash", "--noprofile", "--norc", "-i"]\n`,
      ),
    ]);
    async function launchBackend(listen) {
      const outputStart = backendOutput.length;
      backend = spawn(
        binary,
        ["--config", config, ...(listen ? ["--listen", listen] : [])],
        {
          cwd: workDir,
          stdio: ["ignore", "pipe", "pipe"],
          env: {
            PATH: noGit
              ? join(root, "no-git")
              : (process.env.PATH ?? "/usr/bin:/bin"),
            HOME: home,
            XDG_CONFIG_HOME: join(home, ".config"),
            TMPDIR: root,
            SHELL: "/bin/bash",
            LANG: "C.UTF-8",
            LC_ALL: "C.UTF-8",
            PS1: "webmux-e2e$ ",
            // Apple's /bin/bash otherwise prints a zsh migration banner.
            BASH_SILENCE_DEPRECATION_WARNING: "1",
            HISTFILE: "/dev/null",
          },
        },
      );
      backend.stdout.on("data", (chunk) => (backendOutput += chunk));
      backend.stderr.on("data", (chunk) => (backendOutput += chunk));
      let spawnError;
      backend.on("error", (error) => (spawnError = error));
      const deadline = Date.now() + 30_000;
      let address;
      while (
        !(address = backendOutput
          .slice(outputStart)
          .match(/address=(127\.0\.0\.1:\d+)/)?.[1])
      ) {
        if (spawnError) throw spawnError;
        if (backend.exitCode !== null || backend.signalCode !== null)
          throw new Error("Go backend exited before listening");
        if (Date.now() > deadline)
          throw new Error("Timed out waiting for Go backend address");
        await delay(50);
      }
      const health = await fetch(`http://${address}/api/health`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
        signal: AbortSignal.timeout(10_000),
      });
      if (!health.ok)
        throw new Error(`Backend health returned ${health.status}`);
      return address;
    }
    const address = await launchBackend();
    const apiURL = `http://${address}`;
    const baseURL = apiURL;
    return {
      baseURL,
      apiURL,
      workDir,
      logs,
      async restart() {
        await stopChild(backend);
        await launchBackend(address);
      },
      async close() {
        try {
          await close();
        } finally {
          process.removeListener("SIGINT", interrupted);
          process.removeListener("SIGTERM", interrupted);
        }
      },
    };
  } catch (error) {
    await close();
    process.removeListener("SIGINT", interrupted);
    process.removeListener("SIGTERM", interrupted);
    throw new Error(`${error.message}\n${logs()}`, { cause: error });
  }
}
