// Generates the README screenshots in docs/images/ from a real WebMux backend.
//
//   make build && pnpm --dir web screenshots
//
// It starts bin/webmux on a free port with a throwaway config, creates a demo Git
// repository with uncommitted changes, runs real shells and the `omp` agent, then
// captures desktop and mobile (Pixel 7) viewports with Playwright.
import { execFileSync, spawn } from "node:child_process";
import { once } from "node:events";
import {
  mkdirSync,
  mkdtempSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import { chromium, devices } from "@playwright/test";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(webRoot, "..");
const outDir = join(repoRoot, "docs", "images");
const binary = join(repoRoot, "bin", "webmux");
const root = realpathSync(mkdtempSync(join(tmpdir(), "webmux-shots-")));
// A short, stable path keeps temp-directory noise out of the screenshots.
const demo = "/tmp/demo/bookshelf-api";
const config = join(root, "config.toml");

const gitEnv = {
  ...process.env,
  GIT_AUTHOR_NAME: "Rain",
  GIT_AUTHOR_EMAIL: "rain@example.com",
  GIT_COMMITTER_NAME: "Rain",
  GIT_COMMITTER_EMAIL: "rain@example.com",
};
const git = (...args) =>
  execFileSync("git", ["-C", demo, ...args], { env: gitEnv });
const write = (name, text) => {
  mkdirSync(dirname(join(demo, name)), { recursive: true });
  writeFileSync(join(demo, name), text);
};

function seedRepository() {
  rmSync(dirname(demo), { recursive: true, force: true });
  mkdirSync(demo, { recursive: true });
  git("init", "-q", "-b", "main");
  write("go.mod", "module example.com/bookshelf\n\ngo 1.26\n");
  write(
    "main.go",
    `package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /books", listBooks)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
`,
  );
  write(
    "books.go",
    `package main

import (
	"encoding/json"
	"net/http"
)

type Book struct {
	ID    int    \`json:"id"\`
	Title string \`json:"title"\`
}

var books = []Book{{1, "The Go Programming Language"}, {2, "Designing Data-Intensive Applications"}}

func listBooks(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(books)
}
`,
  );
  write("README.md", "# bookshelf-api\n\nA tiny books API.\n");
  git("add", ".");
  git("commit", "-qm", "feat: initial bookshelf API");
  // Uncommitted work shown by the diff view.
  write(
    "books.go",
    `package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type Book struct {
	ID    int    \`json:"id"\`
	Title string \`json:"title"\`
	Year  int    \`json:"year,omitempty"\`
}

var books = []Book{
	{1, "The Go Programming Language", 2015},
	{2, "Designing Data-Intensive Applications", 2017},
}

func listBooks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(books); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func getBook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	for _, b := range books {
		if b.ID == id {
			json.NewEncoder(w).Encode(b)
			return
		}
	}
	http.NotFound(w, r)
}
`,
  );
  write(
    "main.go",
    `package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /books", listBooks)
	mux.HandleFunc("GET /books/{id}", getBook)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
`,
  );
  write(
    "books_test.go",
    `package main

import (
	"net/http/httptest"
	"testing"
)

func TestGetBookNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/books/99", nil)
	req.SetPathValue("id", "99")
	getBook(rec, req)
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
`,
  );
}

async function freePort() {
  const server = createServer();
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const { port } = server.address();
  server.close();
  await once(server, "close");
  return port;
}

async function startBackend() {
  const port = await freePort();
  writeFileSync(
    config,
    `listen = "127.0.0.1:${port}"\ndefault_cwd = ${JSON.stringify(demo)}\nstate_dir = ${JSON.stringify(join(root, "state"))}\nshell = "/bin/bash"\n`,
  );
  const child = spawn(binary, ["--config", config], {
    cwd: demo,
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env, SHELL: "/bin/bash" },
  });
  let output = "";
  child.stdout.on("data", (c) => (output += c));
  child.stderr.on("data", (c) => (output += c));
  const baseURL = `http://127.0.0.1:${port}`;
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(`${baseURL}/api/sessions`)).ok)
        return { child, baseURL };
    } catch {}
    await delay(100);
  }
  throw new Error(`backend did not start:\n${output}`);
}

// Fixed colours and a short prompt keep the shell screenshots tidy.
const PROMPT =
  "export PS1='\\[\\e[1;32m\\]rain@dev\\[\\e[0m\\]:\\[\\e[1;34m\\]~/bookshelf-api\\[\\e[0m\\]\\$ '; clear";

async function createSession(page, preset, name) {
  await page.goto("/");
  await page
    .locator("header")
    .getByRole("button", { name: "＋ 新建会话", exact: true })
    .click();
  await page.getByRole("button", { name: preset, exact: true }).click();
  await page.getByLabel("工作目录", { exact: true }).fill(demo);
  await page.getByLabel("会话名称", { exact: false }).fill(name);
  await page.getByRole("button", { name: "创建并进入 →", exact: true }).click();
  await page.waitForURL(/\/s\/[a-z0-9]+$/);
  await page
    .locator(".terminal-header")
    .getByRole("status")
    .filter({ hasText: "已连接" })
    .waitFor();
  await page.locator(".xterm-helper-textarea").waitFor({ state: "attached" });
  return new URL(page.url()).pathname.split("/").at(-1);
}

async function type(page, text, enter = true) {
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type(text, { delay: 8 });
  if (enter) await page.keyboard.press("Enter");
}

async function shoot(page, name) {
  await delay(600);
  await page.screenshot({ path: join(outDir, name) });
  console.log("wrote", name);
}

async function main() {
  mkdirSync(outDir, { recursive: true });
  seedRepository();
  const { child, baseURL } = await startBackend();
  const browser = await chromium.launch({ args: ["--disable-webgl"] });
  try {
    const contexts = {
      desktop: await browser.newContext({
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 900 },
        deviceScaleFactor: 2,
        baseURL,
        colorScheme: "dark",
      }),
      mobile: await browser.newContext({
        ...devices["Pixel 7"],
        baseURL,
        colorScheme: "dark",
      }),
    };
    // Show the demo prompt's host instead of the capturing machine's hostname.
    for (const context of Object.values(contexts)) {
      await context.route("**/api/config", async (route) => {
        const response = await route.fetch();
        const json = { ...(await response.json()), hostname: "dev" };
        await route.fulfill({ response, json });
      });
    }
    const desktop = await contexts.desktop.newPage();
    const mobile = await contexts.mobile.newPage();

    // Background sessions that make the overview interesting.
    await createSession(desktop, "Shell", "go test");
    await type(desktop, PROMPT);
    await delay(500);
    await type(desktop, "go test ./... 2>&1 | head -5");
    await delay(3000);
    await type(
      desktop,
      "echo; echo 'Proceed with deploy? [y/N]' ; read -r answer",
    );
    await createSession(desktop, "Shell", "build");
    await type(desktop, PROMPT);
    await delay(500);
    await type(
      desktop,
      'for i in $(seq 1 300); do echo "compiling package $i/300"; sleep 1; done',
    );

    // omp agent session.
    const ompId = await createSession(desktop, "omp", "omp");
    await delay(4000);
    await type(
      desktop,
      "Summarize the uncommitted changes in this repo in three short bullets and point out anything risky.",
    );
    await delay(75000);
    await shoot(desktop, "desktop-omp.png");

    // Shell session.
    const shellId = await createSession(desktop, "Shell", "shell");
    await type(desktop, PROMPT);
    await delay(500);
    for (const command of [
      "git status -sb",
      "git log --oneline --graph --decorate",
      "git diff --stat",
      "ls",
    ]) {
      await type(desktop, command);
      await delay(700);
    }
    await shoot(desktop, "desktop-terminal.png");

    // Code workspace: Git diff.
    await desktop
      .locator(".terminal-header")
      .getByRole("button", { name: "代码", exact: true })
      .click();
    await desktop.getByRole("region", { name: "代码工作区" }).waitFor();
    await desktop.getByRole("tab", { name: "修改", exact: true }).click();
    await desktop
      .getByRole("button", { name: "修改 books.go", exact: true })
      .click();
    await desktop
      .locator(".code-diff-reader:visible [data-code]")
      .first()
      .waitFor();
    await shoot(desktop, "desktop-diff.png");

    // Session overview.
    await desktop.goto("/");
    await shoot(desktop, "desktop-overview.png");

    // Mobile: the phone takes over terminal control, so omp reflows to its width.
    await mobile.goto("/");
    await shoot(mobile, "mobile-overview.png");
    await mobile.goto(`/s/${ompId}`);
    await mobile
      .locator(".terminal-header")
      .getByRole("status")
      .filter({ hasText: "已连接" })
      .waitFor();
    await delay(3000);
    await shoot(mobile, "mobile-omp.png");
    await mobile.goto(`/s/${shellId}`);
    await mobile
      .locator(".terminal-header")
      .getByRole("status")
      .filter({ hasText: "已连接" })
      .waitFor();
    await delay(1500);
    await shoot(mobile, "mobile-terminal.png");
    await mobile
      .locator(".terminal-header")
      .getByRole("button", { name: "代码", exact: true })
      .click();
    await mobile.getByRole("region", { name: "代码工作区" }).waitFor();
    await mobile.getByRole("tab", { name: "修改", exact: true }).click();
    await mobile
      .getByRole("button", { name: "修改 books.go", exact: true })
      .click();
    await mobile
      .locator(".code-diff-reader:visible [data-code]")
      .first()
      .waitFor();
    await shoot(mobile, "mobile-diff.png");
  } finally {
    await browser.close();
    child.kill("SIGTERM");
    rmSync(root, { recursive: true, force: true });
    rmSync(dirname(demo), { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
