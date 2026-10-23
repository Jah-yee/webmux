# WinAcceptance: manual acceptance on Windows 11 (26300) + WSL 2 Debian (mirrored networking, systemd=true)

Stopped early at Main's request (01:14). Every check not listed as PASS or FAIL is NOT RUN, either because of that stop or for the reason given.
Evidence is in `/tmp/accept/WinAcceptance/` (PNGs, `tray.log`, `ps-*.txt`, `*-run.log`). I opened the screenshots marked (viewed) and checked them myself.
Builds were made from `main` d85ab21 with `-buildvcs=false -trimpath`:
- `tray-0.2.0.exe`: `-H=windowsgui`, with digests of the local `rel-0.2.0/webmux-linux-{amd64,arm64}`; `grep` confirmed both digests are in the exe.
- `tray-0.1.0.exe`: built, not used.
- `tray-0.3.0-realdigests.exe`: built, not used.
- `webmux-windows-amd64.exe` (0.1.0): PE subsystem 3 (console).
- `rel-0.2.0`/`rel-0.3.0` Linux servers.
- `webmux-contract2`: built from a `git archive` copy in /tmp with `contract.V=2`; not used.

The UI was driven through Win32 menu APIs (`MN_GETHMENU`/`GetMenuString`/`GetMenuState`), UIA for dialogs, and SendKeys with an en-US layout forced.

## 1. add-windows-console-client 4.2

The agent stand-in was `bash --norc`, because `claude` needs credentials. The "phone" was Playwright Chromium with iPhone 13 emulation on the WebMux web UI.

| Sub-check | Result | Evidence |
|---|---|---|
| `run -- <cmd>` from `C:\` in PowerShell (conhost) attaches | PASS | `c2-ps-run-attached.png` (viewed): `cwd=/mnt/c`, TERM xterm-256color |
| Resize follows (PowerShell) | PASS | `ps-run.log`: 35x129, then 22x79 after shrinking, then 43x146 after growing; `c3-ps-resize.png` |
| Ctrl+C reaches the session (PowerShell) | PASS | `c4-ps-ctrlc.png` (viewed): `sleep 100` interrupted with `^C`, client stays attached |
| Ctrl+] detaches, phone takes over, `attach` back (PowerShell) | PASS | `c5-ps-detached.png` (viewed): "[detached from k66c7aaq; it keeps running]", exit 0. Phone `c6-phone-2-typed.png` (viewed) typed into the session (size 28x45). `wm attach` restored console size 43x146 (`ps-run.log`) |
| Same flow in Windows Terminal | PASS | `c11-wt-run-resize.png`, `c12-wt-ctrlc.png`, `c13-wt-detached.png`, `c14-wt-phone-*.png`, `c15-wt-reattach-exit.png` (viewed). `wt-run.log`: 32x116 then 21x73; Ctrl+C rc=130; phone 28x45; re-attached 28x102 |
| Exit code passthrough | PASS | `ps-exit.txt`: attach-exit=9, run-exit=42 (PowerShell); WT `exit 5` gives `$LASTEXITCODE` 5 (`c15`) |
| Service not running gives exit code 3 | PASS | `ps-nr.txt`: ls-exit=3, run-exit=3, attach-exit=3, with message "WebMux is not running; start it from the WebMux tray…" (`c17-not-running-exit3.png`, viewed) |
| `\\server\share` cwd gives an error | PASS | `ps-unc.txt`: `--cwd \\server\share` gives "directory cannot be mapped into WSL; use --cwd with a Linux path", exit 2. `\\wsl.localhost\Ubuntu\tmp` (other distro) gives exit 2; `\\wsl.localhost\Debian\tmp` maps to `/tmp`, exit 0 (`c9-ps-unc.png`) |
| Tray-selected distro honored | PASS | After choosing "Debian" in the tray, `tray.json` held `"distro":"Debian"`. With no flag or env set, the `webmux.exe` children were `wsl.exe -d Debian --cd ~ --exec ./.local/bin/webmux bridge` (Win32_Process capture) |
| No token in any output | PASS | `ps-out.txt` holds version, status, status --json, ls, ls --json, run --detach, open, help and error output. 0 matches for the token, raw or URL-encoded; only `"tokenConfigured": true` appears |
| Works with localhostForwarding off | NOT RUN | Needs a `.wslconfig` change plus a WSL restart. The distro is in mirrored mode, where the setting does not apply |

Observations, not counted as failures:
- (a) A WT window opened via `wt.exe` from WSL interop got a PATH holding only the WT package directory. `webmux.exe` then reported `cannot run wsl.exe: exec: "wsl.exe": executable file not found in %PATH%`, because it finds `wsl.exe` through PATH. It worked once I reloaded PATH from the registry. This is environment-specific.
- (b) In this mirrored setup, connecting to a closed 127.0.0.1 port hangs instead of being refused. So with WebMux stopped, `webmux status` and `wm status` print "status unknown: no response within 3s" (exit 1) instead of reporting "stopped".

## 2. add-windows-wsl-tray 4.2 / 4.3

All checks used `tray-0.2.0.exe`. There was no tray.json, no config and no `~/.local/bin/webmux` beforehand.

| Sub-check | Result | Evidence |
|---|---|---|
| First run with default settings starts WebMux | PASS | `tray.log`: "starting WebMux (托盘托管)" → "WebMux running", with no tray.json present |
| No console window | PASS | PE subsystem 2 (GUI); MainWindowHandle 0 |
| Single instance | PASS | A second launch exits, and the first shows the toast "WebMux 托盘已在运行" (`t5-single-instance-toast.png`, viewed) |
| Menu enable rules and menu does not block | PASS | Starting: start/stop/restart/open/copy greyed (`t7-starting-menu-dark.png`). Running: start greyed, stop/restart enabled. Missing webmux: only install, logs, settings and quit enabled (`t1-missing-menu.png`). The menu opened within about 1 s while starting |
| Left click opens WebMux | PASS | Edge opened `127.0.0.1:7681`, title "WebMux · 工作区" (`t6-leftclick-browser.png`, viewed) |
| Copy local link | PASS | With no token the clipboard held `http://127.0.0.1:7681` |
| Copy remote link | NOT RUN | Server listens on loopback; mirrored mode has no remote links |
| Copy Token stays out of Win+V and clears after 60 s | PASS | Clipboard sha256 equals the config token's. Formats include ExcludeClipboardContentFromMonitorProcessing and CanIncludeInClipboardHistory=0. WinRT history newest item is not the token. Still present at 30/50/57 s, empty at 63 s (`g2-copy-token-toast.png`, viewed) |
| Stop/restart confirmation when sessions exist | PASS | Dialog "停止 WebMux 将结束 2 个终端会话…" (`i1-stop-confirm.png`, `i2-restart-confirm.png`); cancel kept the sessions |
| Stop timeout forces the kill | PASS | Server was SIGSTOPped. Log: "did not stop within 15s; forcing" → "forcibly killed"; process gone |
| `wsl --shutdown` notification and auto-restart | NOT RUN | Would kill this session |
| Stops retrying after 5 consecutive crashes | PASS | 5× `kill -9`: toasts "WebMux 意外退出（退出码 9），终端会话已丢失" (`k1-crash-toast.png`, viewed); backoff 2/4/8/16 s; then "failed: 多次启动失败…", no server 30 s later, error icon (`k6-failed-menu-dark.png`) |
| Quit tray only, sessions kept 5 min, reopen resumes control (tray-managed) | PASS | Three-choice dialog (`l1-quit-dialog.png`). Quit 01:03:45; at 01:08:54 both sessions alive and `wsl.exe … webmux serve` alive. Reopened tray shows "运行中 · 2 个会话 · 托盘托管" with stop enabled (`l2-reopen-tray-mode-dark.png`) |
| Same, systemd mode | NOT RUN (partial) | Keepalive `wsl.exe --exec sleep infinity` survived the tray exit, and both sessions were alive 3 min 50 s after quitting. Stopped at user request before 5 min, and the reopen was not done |
| `auto` picks the right mode | PASS (systemd distro only) | Without the unit: "自动（当前：托盘托管）". After `webmux service install`: "自动（当前：systemd）" (`m1-auto-systemd-detected-dark.png`), and the tray started it as launcher=systemd. A distro without systemd is NOT RUN (only one distro exists) |
| Manually started instance shown as external | PASS | "运行中（外部实例）… 外部实例：由外部管理" (`n1-external-menu-dark.png`) |
| Port in use | PASS | "端口 127.0.0.1:7681 已被其他程序占用" (`o2-port-busy-menu-dark.png`) |
| Old webmux upgrade prompt | NOT RUN | Stopped (real v0.1.9 binary was downloaded for it) |
| localhostForwarding-off diagnostics | NOT RUN | Needs a WSL restart |
| Login autostart and WSL cold start | NOT RUN | Stopped; logoff and cold start are not possible here |
| Switching distro asks for confirmation | PASS | "切换发行版需要停止 WebMux，将结束 8 个终端会话…" (`r1-switch-distro-confirm.png`); after OK it restarted on Debian |
| Switching launch mode asks for confirmation | NOT RUN | Stopped |
| Init config when missing, then restart shows "复制 Token" | PASS | Prompt "已生成配置与 Token，重启 WebMux 后生效…" (`g1-configinit-restart-prompt.png`); after the restart the menu has "复制 Token" |
| NAT forwarding with phone, pause/resume, stop on quit | NOT RUN | NAT mode needs a WSL restart |
| Mirrored mode says no forwarding needed | PASS | Remote access submenu: "无需转发（镜像网络模式）" (`h1-remote-submenu-mirrored-dark.png`) |
| Forwarding disabled when no token is set | NOT RUN | Only observable in NAT mode |
| Tray log has no token | PASS | `tray.log` (136 lines): no string hashes to the token, no `token=` |
| 4.3 icon states and menus, dark taskbar | PASS | `icons-dark-montage.png` (viewed: grey stopped, orange starting, green running, red error); menus `t4-running-menu-dark.png`, `t7`, `k6`, `n1`, `o2`, `h1` (viewed) |
| 4.3 light taskbar | NOT RUN | Stopped before switching the theme; the theme was never changed |

Observation, possible bug, not reproduced: once, at 00:58:13, a start issued by the tray stayed "运行中（外部实例）" for about 19 s before the log said "WebMux running (托盘托管)". Three later restarts became ready in about 2 s with no External state.

## 3. tray-server-version-compat 3.5

| Case | Result | Evidence |
|---|---|---|
| Matched | PASS | Status "WebMux 0.2.0"; log "tray 0.2.0 (contract v1), WSL webmux 0.2.0 (contract v1): matched"; no hint line (`t4-running-menu-dark.png`) |
| WSL older, recovers after upgrade | NOT RUN | Stopped at user request |
| WSL newer, opens the Release page | NOT RUN | Stopped at user request |
| Incompatible contract, starts and runs | NOT RUN | Stopped at user request |
| Running instance not restarted | NOT RUN | Stopped at user request |

## 4. add-tray-wsl-install-and-runtime-tunnel 6.2

| Sub-check | Result | Evidence |
|---|---|---|
| Install after `~/.local/bin/webmux` is absent | PASS | Menu: "找不到 webmux ~/.local/bin/webmux" plus "安装 WSL 中的 webmux". |
| Download source (design does not allow a local one) | as noted | The design does not allow a local download source: `fetch.GitHub` is a const, and the tray uses the default mirrors. |
| Install with no cache (digest check) | PASS | The first click downloaded the real GitHub v0.2.0 asset, and the built-in digest check rejected it from github.com, ghfast.top and gh-proxy.com ("sha256 mismatch"); nothing was installed |
| Install from local release build | PASS | I placed the local release build at `%LOCALAPPDATA%\WebMux\downloads\webmux-0.2.0-linux-amd64`, the design's verified-cache path. The second click logged "installed webmux 0.2.0 (amd64)" and "matched", and `~/.local/bin/webmux version` printed 0.2.0 |
| Upgrade prompt while running | NOT RUN | Stopped at user request |
| Tunnel on / copy link / off / kill-cloudflared notification | NOT RUN | Stopped at user request |

## 5. add-attention-web-push 10.2

| Sub-check | Result | Evidence |
|---|---|---|
| `GOOS=windows go build ./cmd/webmux-tray` | PASS | All tray builds above succeeded |
| Push hint line in the menu | PASS | "手机推送：需要 Named Tunnel" under "公网隧道（Cloudflare）" (`p1-push-hint-menu-dark.png`, viewed) |
| "手机推送设置说明…" dialog | PASS | Named Tunnel steps, local address `http://127.0.0.1:7681`, phone steps, no token (`p2-push-guide-dialog-dark.png`, viewed) |
| Light taskbar screenshots | NOT RUN | Stopped at user request |

## Cleanup done

State before the run: no `%APPDATA%\WebMux`, no `%LOCALAPPDATA%\WebMux`, no `WebMuxTray` Run value, no webmux processes, no `~/.local/bin/webmux`, no `~/.config/webmux`, no `~/.local/state/webmux`, no user unit, SystemUsesLightTheme=0.

- Ended processes:
  - all tray processes;
  - the keepalive `wsl.exe` (pid 25188) and its `sleep infinity`;
  - the systemd WebMux (`webmux service uninstall`);
  - the manual `webmux serve`;
  - the `python3 http.server`;
  - the WT and conhost test windows.

  No webmux or cloudflared processes remain on Windows or in WSL (OmpAcceptance's own server is untouched).
- Removed `~/.local/bin/webmux`, `~/.config/webmux`, `~/.local/state/webmux`, `~/.config/systemd/user/webmux.service` and its wants link.
  - Empty `~/.config/systemd` directories were removed with `rmdir`. Before the run I had confirmed only that `~/.config/systemd/user` did not exist.
  - Ran `daemon-reload`.
- Removed `%APPDATA%\WebMux` (tray.json), `%LOCALAPPDATA%\WebMux` (tray.log, downloads, keepalive.json) and the `HKCU\Control Panel\NotifyIconSettings\763529501841725883` key that I created and promoted.
  - The Run key never got `WebMuxTray`.
  - The theme was never changed (still 0).
  - I restored the user's original clipboard text (10 characters) and deleted the saved copy.
  - Deleted the Windows temp dir `webmux-accept`.
- Left behind:
  - an Edge tab at `http://127.0.0.1:7681` opened by the left-click test (now dead);
  - Windows clipboard history still holds the local-link entry `http://127.0.0.1:7681` (no token).
- Kept in /tmp/accept/WinAcceptance: build outputs and evidence.
