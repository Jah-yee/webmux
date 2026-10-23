# OmpAcceptance — agent-attention-signals task 5.3 (manual omp acceptance)

Repo: /home/rainchan/webmux @ d85ab21, binary `bin/webmux` 0.1.0 (built 2026-10-24 00:27; contains `WARP_CLI_AGENT_PROTOCOL_VERSION`). omp v18.12.6 (`~/.local/bin/omp`, real HOME credentials).
Server: `bin/webmux serve --config /tmp/accept/OmpAcceptance/config.toml` on 127.0.0.1:17730, `state_dir=/tmp/accept/OmpAcceptance/state`, token empty (loopback), presets `shell` and `omp` (`["bash","-ic","exec omp \"$@\"","omp"]`, as in deploy/config.example.toml).
`make test` was not run here; the parent runs it.

## omp setup required by the docs
docs/usage.md says: "**omp**: no configuration needed". WebMux injects `WARP_CLI_AGENT_PROTOCOL_VERSION=1` itself. The omp binary confirms this: `packages/coding-agent/src/modes/warp-events.ts` emits `question_asked` on ask tool start, `permission_request`/`permission_replied` on tool approval, and `stop`/`stop_failure` on `agent_end`, but only when that variable is ≥1. No extension, snippet or omp config change was installed. Approval and error were triggered with per-session CLI flags only (`--approval-mode always-ask` and `--api-key <bogus>`), so nothing needed restoring.

Common args for every omp session: `--model anthropic/claude-haiku-5-5 --thinking off --no-session --no-title <prompt>`, cwd `/tmp/accept/OmpAcceptance/work`. Sessions were created via `POST /api/sessions {"preset":"omp","args":[...]}`.

## Results (agent_protocol = true)

| # | Check | Result | Evidence |
|---|-------|--------|----------|
| 1 | ask → “需要回答” | PASS | Session `ask` (prompt "Call the ask tool once to ask me to choose A or B. Do nothing else."). API: `status: waiting, attention: {kind: question, source: agent, since: 00:29:26}`. The omp Ask dialog "Choose A or B." is visible in the preview. The card shows **需要回答** in the screenshot. |
| 2 | tool approval → “等待授权” | PASS | Session `permission` (`--approval-mode always-ask "Run the bash command: echo hi"`). API: `waiting, {kind: permission, source: agent}`. The preview shows the "Allow tool: bash / Command: echo hi" dialog while the omp spinner/timer keeps redrawing (`⠦ 36s` → `50s`). The card shows **等待授权**. |
| 3 | normal completion → “已完成” | PASS | Session `done` ("Reply with just: OK"). API: `waiting, {kind: done, source: agent}`. The preview shows `OK` followed by the idle prompt. The card shows **已完成**. |
| 4 | error stop → “出错停止” | PASS | Session `error` (`--api-key sk-ant-invalid-acceptance-test "Reply with just: OK"`). API: `waiting, {kind: error, source: agent}`. The preview shows `✘ 401 … authentication_error … invalid x-api-key`. The card shows **出错停止**. |

- Screenshot (viewed; all 4 cards in one full-page list screenshot): `/tmp/accept/OmpAcceptance/list-protocol-on.png`. Playwright page-text counts: 需要回答 1, 等待授权 1, 已完成 1, 出错停止 1, 响铃提醒 0, 可能在等你 0.
- Raw `/api/sessions` JSON: `/tmp/accept/OmpAcceptance/sessions-protocol-on.json`.
- Extra clearing check (not required):
  - I sent `\r` as a `0x00` input frame over `/ws/s/{id}` to `ask` (selects A) and to `permission` (Approve).
  - Immediately afterwards both sessions showed `status: running, attention: null`.
  - About 15 s later both showed `{kind: done, source: agent}`. Previews: "You selected A." and "The command ran and printed hi."
- Privacy: `grep -c -E 'Choose A|invalid-acceptance|You selected'` over both server logs gives 0. Attention JSON holds only kind/source/since. The bogus `--api-key` value does appear in `SessionInfo.cmd`, because it is a CLI argument (expected behaviour; the key is fake).

## Results (agent_protocol = false, after server restart)

| # | Check | Result | Evidence |
|---|-------|--------|----------|
| 5 | ask → “响铃提醒” | PASS | Session `ask-noproto` (same prompt). API: `waiting, {kind: bell, source: terminal, since: 00:31:54}`. The Ask dialog is visible. The card shows **响铃提醒**. |
| 6 | completion → “响铃提醒” | PASS | Session `done-noproto`. API: `waiting, {kind: bell, source: terminal}`. The preview shows `OK`. The card shows **响铃提醒**. |

- Screenshot (viewed): `/tmp/accept/OmpAcceptance/list-protocol-off.png`. Page-text counts: 响铃提醒 2, 需要回答/等待授权/已完成/出错停止 0.
- JSON: `/tmp/accept/OmpAcceptance/sessions-protocol-off.json`.
- Env check: custom session `env` ran `env | grep -E '^(WARP|WEBMUX|TERM|COLORTERM)' | sed 's/=.*//'`. Output: `COLORTERM`, `TERM`, `WEBMUX_SESSION_ID`, with no `WARP_*` and no `TERM_PROGRAM`. It exited 0.

## Bugs found
None.

## Cleanup
- Every session was closed with `DELETE /api/sessions/{id}`; they exited with code 129 (SIGHUP), and the env session exited 0. Each was then removed with `?remove=1`, and `/api/sessions` returned `[]` both times.
- Both servers were stopped with `webmux stop --config …` ("WebMux stopped"). Port 17730 is no longer listening (`ss -ltn | grep -c 17730` = 0), and no leftover omp/webmux processes match.
- Nothing was installed or changed in omp config, `~`, Windows, systemd, the registry or the repo (`git status --short` is empty). omp ran with `--no-session`.
- Left in place as evidence: `/tmp/accept/OmpAcceptance/` (config.toml, state/, empty work/, server1.log, server2.log, PNGs, JSONs, and the helper scripts shot.cjs (Playwright screenshot plus text count) and send.mjs (WS input frame)).
