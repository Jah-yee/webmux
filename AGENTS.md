# Repository Guidelines

## Project Overview

WebMux is a single-user Linux/macOS web terminal for persistent PTY sessions, including shells and CLI agents. Browser disconnection does not end a session; server restart does. The Go binary embeds the browser app. This is not a process sandbox, durable session store, or tmux wrapper: commands run as the service user. Keep deployments on loopback or a trusted VPN; do not expose an unrestricted shell publicly.

## Architecture & Data Flow

- Startup loads TOML configuration, converts it to `session.Options`, constructs a `session.Manager`, and injects configuration and manager into `api.New`. Keep this explicit constructor-based dependency wiring.
- `internal/session` owns PTYs, bounded byte scrollback, metadata, status detection, and process cleanup. `internal/api` owns REST routes, authentication, Origin checks, and static serving; `internal/hub` bridges session subscriptions to WebSockets.
- REST handles configuration, session operations, and filesystem requests. `/ws/sessions` delivers a snapshot followed by list events; `/ws/s/{id}` carries terminal frames. Raw PTY bytes reach xterm without server-side terminal emulation; OSC/title and preview parsing are side channels.
- Replay capture and live subscription registration share a lock: preserve gap-free reattachment. Bounded queues disconnect slow clients rather than blocking PTY readers. Each connection has one data writer; cancellation must stop connection work without killing the persistent session.
- Backend payloads and `web/src/schemas.ts` form one contract. Keep frame handling in `internal/hub` and `web/src/hooks/terminal.svelte.ts` aligned; update schemas and consumers together when changing JSON fields.

## Key Directories

| Path                                                 | Change guidance                                                                                                                                                                       |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/webmux/`                                        | CLI flags, dependency wiring, and server shutdown.                                                                                                                                    |
| `internal/session/`                                  | Session lifecycle and concurrency; process tables and cleanup are platform-specific (`_linux.go` reads `/proc`, `_darwin.go` uses sysctl/`proc_pidinfo`, shared logic in `_unix.go`). |
| `internal/api/`, `internal/hub/`, `internal/config/` | HTTP/security boundaries, streaming transport, and configuration validation.                                                                                                          |
| `web/src/pages/`, `web/src/hooks/`                   | Page state and resource-owning terminal/mobile behavior.                                                                                                                              |
| `web/src/lib/components/ui/`                         | shadcn-svelte base components; compose them in business components instead of inventing replacement dialogs or menus.                                                                 |
| `web/test/`                                          | Node unit, browser component, and isolated backend E2E suites.                                                                                                                        |
| `deploy/`, `openspec/`                               | Deployment examples and specification workflow artifacts.                                                                                                                             |

`web/dist/`, `bin/`, and browser reports are generated outputs. `acceptance.local/` is gitignored, machine-specific experimentation with fixed ports/tokens and live-session side effects—not the standard QA suite.

## Development Commands

Run from the repository root:

| Command                                    | Purpose                                                                   |
| ------------------------------------------ | ------------------------------------------------------------------------- |
| `pnpm --dir web install --frozen-lockfile` | Install locked frontend dependencies.                                     |
| `make web`                                 | Install dependencies, run Svelte/TypeScript checks, and build `web/dist`. |
| `make build`                               | Build frontend first, then the embedded Go executable `bin/webmux`.       |
| `make release`                             | Build Linux and macOS (darwin) amd64 and arm64 binaries.                  |
| `./bin/webmux --listen 127.0.0.1:7681`     | Run the backend and embedded production app.                              |
| `pnpm --dir web dev`                       | Run Vite at `http://127.0.0.1:5173`.                                      |
| `pnpm --dir web check`                     | Check Svelte/TypeScript without building.                                 |
| `make check`                               | Build frontend, check formatting, run Oxlint, `go vet` and `golangci-lint`. |
| `pnpm --dir web lint`                      | Run Oxlint on frontend code.                                            |
| `make format` / `make format-check`        | Apply/check gofmt and Oxfmt on configured targets.                        |

Vite proxies `/api` and `/ws` to backend port 7681. Run the backend separately and add `allowed_origins = ["http://127.0.0.1:5173"]` to its development TOML; otherwise browser write requests are rejected. Configuration defaults to `~/.config/webmux/config.toml`; `-config`, `-listen`, and `-token` override startup settings.

## Code Conventions & Common Patterns

- Go uses gofmt. Frontend files use Oxfmt with an 80-column print width, two spaces, semicolons, trailing commas, and Svelte formatting enabled. Oxlint checks JS/TS and Svelte script blocks; keep `svelte-check` for Svelte template and type diagnostics. TypeScript is strict; `$lib` aliases `web/src/lib`.
- Follow existing names: PascalCase Svelte components, camelCase TypeScript functions, `*Schema` Zod contracts, `*.svelte.ts` rune-bearing hooks, Go `*_test.go`, and Playwright `*.spec.ts`.
- Use Svelte 5 `$state`/`$derived` for local reactive state and lifecycle cleanup for sockets, xterm, listeners, and observers. Reuse `api.ts` and `ws.ts`; do not add parallel request or reconnect conventions.
- Validate HTTP responses, WebSocket JSON, and local settings with Zod before updating state; derive types with `z.infer`. PTY data stays binary, not JSON or decoded text.
- Return contextual Go errors and API JSON errors with meaningful HTTP statuses. Frontend failures use `APIError`; HTTP 401 triggers `webmux:auth`. Preserve cancellation/deadlines for PTY writes and join spawned connection goroutines.
- Protect manager/session state with existing locks; do not hold them across blocking network or PTY operations. Shared output chunks stay immutable. Exit publication follows PTY drain and process cleanup.
- Execute commands as argv, not shell-expanded strings. Preset creation and `preset: "custom"` command creation are mutually exclusive contracts. Preserve authentication and Origin checks on API/WebSocket paths; never log terminal input/output or expose tokens through configuration responses.

## Important Files

- `cmd/webmux/main.go`, `web/src/main.ts`, `web/src/App.svelte`: server/browser entry points and top-level lifecycle/routing.
- `internal/session/manager.go`, `session.go`, `io.go`, `ring.go`: state ownership, PTY lifecycle, cancellable input, and replay.
- `web/embed.go`: embeds `web/dist`; rebuild the frontend before compiling Go to avoid missing or stale assets.
- `Makefile`, `go.mod`, `web/package.json`, `web/pnpm-lock.yaml`, `web/pnpm-workspace.yaml`: build commands, tool/dependency versions, and permitted install scripts.
- `web/vite.config.ts`, `web/tsconfig.json`, `web/components.json`, `.oxfmtrc.json`, `web/.oxlintrc.json`: proxy/alias, strict typing, UI generation, formatting, and lint configuration.
- `README.md`, `docs/` (usage, deployment, architecture, protocol), `CONTRIBUTING.md`, `SECURITY.md`: user and contributor documentation at overview level; confirm implementation details against source. Maintain `docs/user-stories.md` under the rules below; update other documentation when relevant. `deploy/config.example.toml`, `deploy/webmux.service`, `deploy/com.webmux.plist`: TOML, systemd, and launchd examples.
- `openspec/config.yaml`: spec-driven workflow context. Write new specification artifacts in Simplified Chinese, retaining English structural headings and SHALL/MUST keywords. `.agents/`, `.claude/`, and `.omp/` contain generated workflow integrations.

## User Stories Maintenance

- Read `docs/user-stories.md` before changing a capability. Every implemented
  capability MUST have one or more stories explaining the user role, intended
  action, purpose, and observable acceptance criteria.
- In the same change, update the affected stories when adding, changing, or
  removing behavior. This includes UI, REST/WebSocket APIs, CLI commands,
  configuration, security and session lifecycle, deployment, and contributor
  tooling. Pure refactors, formatting, and dependency updates with no behavior
  change do not require catalog edits.
- Reuse existing stories when their purpose still fits; add stories for new
  purposes. Keep IDs stable: do not renumber existing stories or reuse retired
  IDs. When removing a capability, mark its stories as retired and explain the
  replacement when applicable rather than silently losing their history.
- Keep acceptance criteria, per-group source references, both feature indexes,
  and any stated counts consistent. Use the existing Chinese story format and
  functional groups; add a group only when existing groups do not fit.
- Check behavior against the implementation and relevant tests. Distinguish
  implemented capabilities from OpenSpec plans; investigate disagreements
  before updating the catalog. Keep verification notes accurate and never claim
  that tests ran or the whole catalog was reverified without that evidence.
- Before completing the change, verify that every added or changed capability
  maps to a story, IDs are unique, index references resolve, and linked source
  paths exist. Run the applicable Markdown formatting and whitespace checks.
- In the delivery or PR summary, identify the affected story IDs, or briefly
  explain why the change has no user-story impact.

## Runtime/Tooling Preferences

Use Go 1.26+, Node.js 24 LTS, pnpm 12.11.2, and GNU Make. Use pnpm—not npm, Yarn, or Bun—to manage frontend dependencies; retain the frozen lockfile workflow and the `esbuild`-only dependency build-script allowlist.

Production needs only the Linux or macOS binary, a shell, and any chosen CLI agents; it does not require Node or Go. `deploy/webmux.service` (systemd) and `deploy/com.webmux.plist` (launchd) are the service examples. Release builds use `CGO_ENABLED=0`. `make check` needs a separately installed `golangci-lint`; current CI does not run that lint target. OpenSpec 1.13.0 is optional workflow tooling, not a frontend runtime dependency.

## Testing & QA

```sh
make test                         # Go race tests + all frontend suites
make test-unit                    # Vitest in Node
make test-browser                 # Vitest in headless Chromium
make test-e2e                     # Playwright: desktop + Pixel 7 Chromium
make test-browsers-install        # Chromium (+ Linux libraries; may require sudo)
```

- Go tests use the standard `testing` package. Target a package with `go test -race ./internal/session`; Linux/macOS lifecycle tests cover child-process cleanup, waiting detection, and input cancellation; subreaper reaping is Linux-only. macOS temp paths sit behind `/var -> /private/var`: compare against canonical paths (packages' `TestMain` canonicalizes `TMPDIR`; E2E uses `realpath`).
- Browser tests exercise real Svelte/Tailwind/xterm rendering without a backend. E2E builds one production frontend and embedded Go executable per invocation in a private temporary build directory. Workers share only the immutable executable, with independent backends, HOME/config/workdirs and dynamic ports (one worker locally, two on Ubuntu CI and four on macOS CI), and automatic session/process cleanup. Do not connect tests to an existing developer service.
- Target a frontend test with `pnpm --dir web exec vitest run --config vitest.config.ts test/input.test.ts`, or an E2E spec with `pnpm --dir web exec playwright test test/e2e/terminal.spec.ts`.
- Preserve behavioral coverage for authentication/Origin rejection, argv boundaries, replay continuity, exit ordering, cancellation, terminal input/rendering, and touch interactions. No numeric coverage threshold is configured.
- `.github/workflows/test.yml` runs parallel jobs: formatting/frontend build, Go race, Vitest, Storybook and two E2E shards on Ubuntu; one macOS job runs Go race and E2E concurrently, checks both exit codes, and keeps the platform-specific suites. `Test result` aggregates every job. PR runs restore caches read-only and superseded PR runs are cancelled; caches are saved from `main`. Failure diagnostics live in `web/playwright-report/`, `web/test-results/`, and `web/.vitest/attachments/` (uploaded per job); open the E2E HTML report with `pnpm --dir web test:e2e:report`.
- Chromium mobile emulation does not replace real-device iOS Safari/Android keyboard and native selection-menu checks.

## UI Pull Request Evidence

- PRs that change visible UI (layout, styling, components, or interaction states) MUST embed screenshots of the final implementation in the PR description. Capture the running app or its real Storybook components; mockups and test summaries do not replace screenshots.
- Show before/after views for changes to an existing interface. Include the affected light/dark themes and desktop/mobile layouts when the change spans them, and label the viewport and any sample data.
- Use GitHub attachments or committed image URLs that reviewers can open; local filesystem paths and localhost URLs are not valid PR image links. Check that the images render in the published PR and refresh them when later changes alter the UI. Use isolated fixtures or demo sessions without credentials or private terminal output.
