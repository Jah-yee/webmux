package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

const echoScript = `stty -echo; printf 'READY\n'; while IFS= read -r line; do if [ "$line" = SIZE ]; then printf 'SIZE:'; stty size; else printf 'ACK:%s\n' "$line"; fi; done`

func (f *fixture) interaction(n, load int) []metric {
	var ids []string
	defer func() {
		for _, id := range ids {
			f.dispose(id)
		}
	}()
	for i := 0; i < load; i++ {
		id := f.create(`while :; do printf '%0200d\n' 0; sleep 0.1; done`)
		ids = append(ids, id)
	}
	label := "single"
	if load > 0 {
		label = "loaded_8"
	}
	creation := measure("session_create_"+label, "ms", n, func(i int) float64 {
		start := time.Now()
		id := f.create(echoScript)
		elapsed := milliseconds(start)
		defer f.dispose(id)
		c := f.dial(id)
		defer c.CloseNow()
		until(c, []byte("READY\r\n"))
		return elapsed
	})
	id := f.create(echoScript)
	defer f.dispose(id)
	c := f.dial(id)
	defer c.CloseNow()
	until(c, []byte("READY\r\n"))
	for i := 0; i < 3; i++ {
		write(c, []byte(fmt.Sprintf("warmup-%d\n", i)))
		until(c, []byte(fmt.Sprintf("ACK:warmup-%d\r\n", i)))
	}
	echo := measure("input_to_output_"+label, "ms", n, func(i int) float64 {
		marker := fmt.Sprintf("%s-%04d-中文", label, i)
		start := time.Now()
		write(c, []byte(marker+"\n"))
		got := until(c, []byte("ACK:"+marker+"\r\n"))
		check(bytes.Equal(got, []byte("ACK:"+marker+"\r\n")), "echo bytes mismatch: %q", got)
		return milliseconds(start)
	})
	resize := measure("resize_"+label, "ms", n, func(i int) float64 {
		cols, rows := 100+i%20, 40+i%10
		start := time.Now()
		ctx, cancel := timeout()
		defer cancel()
		must(c.Write(ctx, websocket.MessageBinary, contract.Frame(1, []byte(fmt.Sprintf(`{"cols":%d,"rows":%d}`, cols, rows)))))
		write(c, []byte("SIZE\n"))
		until(c, []byte(fmt.Sprintf("SIZE:%d %d\r\n", rows, cols)))
		return milliseconds(start)
	})
	return []metric{creation, echo, resize}
}

func (f *fixture) reconnect(n int) metric {
	return measure("disconnect_survival_and_replay", "ms", n, func(i int) float64 {
		gate := fmt.Sprintf("offline-%d", i)
		id := f.create(fmt.Sprintf(`stty -echo; printf 'PID:%%s\nREADY\n' "$$"; while [ ! -f %s ]; do sleep 0.01; done; for ((j=0;j<200;j++)); do printf 'record-%%04d-中文\n' "$j"; done; printf 'DONE:offline\n'; while IFS= read -r line; do for ((j=0;j<200;j++)); do printf 'record-%%04d-中文\n' "$j"; done; printf 'DONE:%%s\n' "$line"; done`, gate))
		defer f.dispose(id)
		c := f.dial(id)
		defer c.CloseNow()
		initial := until(c, []byte("READY\r\n"))
		_ = c.CloseNow()
		f.await(id, func(v session.Info) bool { return v.Clients == 0 })
		must(os.WriteFile(filepath.Join(f.root, gate), nil, 0600))
		f.await(id, func(v session.Info) bool { return v.Clients == 0 && strings.Contains(v.Preview, "DONE:offline") })
		check(f.info(id).Status != session.StatusExited, "disconnect killed session")
		var expected bytes.Buffer
		expected.Write(initial)
		for j := 0; j < 200; j++ {
			fmt.Fprintf(&expected, "record-%04d-中文\r\n", j)
		}
		expected.WriteString("DONE:offline\r\n")
		start := time.Now()
		c = f.dial(id)
		defer c.CloseNow()
		p := read(c)
		check(p[0] == 1 && bytes.Equal(p[1:], expected.Bytes()), "reconnect replay differs from independent record oracle")
		elapsed := milliseconds(start)
		write(c, []byte("online\n"))
		live := until(c, []byte("DONE:online\r\n"))
		want := bytes.ReplaceAll(expected.Bytes()[len(initial):], []byte("offline"), []byte("online"))
		check(bytes.Equal(live, want), "live continuation gap, duplicate or corruption")
		return elapsed
	})
}

func (f *fixture) replay(n int) metric {
	unit := []byte("line-中文-\x1b[32mgreen\x1b[0m\r\n")
	payload := bytes.Repeat(unit, (capacity+65536)/len(unit)+1)
	file := filepath.Join(f.root, "replay.bin")
	must(os.WriteFile(file, payload, 0600))
	id := f.create(`stty raw -echo; cat replay.bin`)
	defer f.dispose(id)
	f.await(id, func(v session.Info) bool { return v.Status == session.StatusExited })
	expected := payload[len(payload)-capacity:]
	wantHash := sha256.Sum256(expected)
	return measure("replay_2mib_after_wrap", "ms", n, func(i int) float64 {
		start := time.Now()
		c := f.dial(id)
		defer c.CloseNow()
		p := read(c)
		check(p[0] == 1 && len(p)-1 == capacity && sha256.Sum256(p[1:]) == wantHash, "bounded replay hash/length mismatch")
		p = read(c)
		check(p[0] == 2, "expected state after replay")
		p = read(c)
		check(p[0] == 3, "expected exit after state")
		return milliseconds(start)
	})
}

func (f *fixture) continuity(n int) metric {
	return measure("reattach_during_live_output", "ms", n, func(i int) float64 {
		gate := fmt.Sprintf("continuous-%d", i)
		id := f.create(fmt.Sprintf(`stty -echo; printf 'READY\n'; while [ ! -f %s ]; do sleep 0.01; done; for ((j=0;j<100;j++)); do printf 'item-%%04d-中文\n' "$j"; sleep 0.002; done`, gate))
		defer f.dispose(id)
		c := f.dial(id)
		defer c.CloseNow()
		until(c, []byte("READY\r\n"))
		must(os.WriteFile(filepath.Join(f.root, gate), nil, 0600))
		until(c, []byte("item-0005-中文\r\n"))
		_ = c.CloseNow()
		start := time.Now()
		c = f.dial(id)
		defer c.CloseNow()
		ctx, cancel := timeout()
		defer cancel()
		first := readContext(ctx, c)
		check(first[0] == 1 && !bytes.Contains(first[1:], []byte("item-0099-中文")), "fixture completed before reattach; live handoff untested")
		output := append([]byte(nil), first[1:]...)
		liveFrames := 0
		for {
			p := readContext(ctx, c)
			if p[0] == 3 {
				break
			}
			check(p[0] != 1, "unexpected second replay frame")
			if p[0] == 0 {
				liveFrames++
				check(len(output)+len(p)-1 < 10000, "excess continuity output")
				output = append(output, p[1:]...)
			}
		}
		check(liveFrames > 0, "no live output after snapshot")
		var expected bytes.Buffer
		expected.WriteString("READY\r\n")
		for j := 0; j < 100; j++ {
			fmt.Fprintf(&expected, "item-%04d-中文\r\n", j)
		}
		check(bytes.Equal(output, expected.Bytes()), "snapshot/live transition lost, duplicated or reordered output")
		return milliseconds(start)
	})
}

func (f *fixture) streaming(n int) metric {
	payload := bytes.Repeat([]byte("0123456789abcdef\n"), 4*1024*1024/17)
	must(os.WriteFile(filepath.Join(f.root, "stream.bin"), payload, 0600))
	hash := sha256.Sum256(payload)
	return measure("live_stream_4mib", "MiB/s", n, func(i int) float64 {
		id := f.create(`stty raw -echo; printf READY; dd bs=1 count=1 of=/dev/null 2>/dev/null; cat stream.bin`)
		defer f.dispose(id)
		c := f.dial(id)
		defer c.CloseNow()
		until(c, []byte("READY"))
		start := time.Now()
		write(c, []byte("x"))
		ctx, cancel := timeout()
		defer cancel()
		output := make([]byte, 0, len(payload))
		for {
			p := readContext(ctx, c)
			if p[0] == 3 {
				break
			}
			if p[0] == 0 {
				check(len(output)+len(p)-1 <= len(payload), "excess streaming output")
				output = append(output, p[1:]...)
			}
		}
		elapsed := time.Since(start).Seconds()
		check(len(output) == len(payload) && sha256.Sum256(output) == hash, "live stream dropped, duplicated or changed bytes: %d/%d", len(output), len(payload))
		return float64(len(payload)) / (1024 * 1024) / elapsed
	})
}

func (f *fixture) access() metric {
	type test struct {
		method, path, credential, origin string
		status                           int
	}
	var cases []test
	for _, p := range []string{"/api/health", "/api/sessions", "/api/config", "/api/repositories", "/ws/sessions", "/ws/s/missing"} {
		cases = append(cases, test{"GET", p, "", "", 401}, test{"GET", p, "wrong", "", 401}, test{"GET", p, token, "https://evil.example", 403})
	}
	cases = append(cases, test{"GET", "/api/health", token, "", 200}, test{"GET", "/api/health", token, f.server.URL, 200}, test{"POST", "/api/sessions", token, "https://evil.example", 403}, test{"POST", "/api/repositories", token, "https://evil.example", 403})
	return measure("auth_origin_contracts", "case", len(cases), func(i int) float64 {
		v := cases[i]
		r, err := http.NewRequest(v.method, f.server.URL+v.path, nil)
		must(err)
		if v.credential != "" {
			r.Header.Set("Authorization", "Bearer "+v.credential)
		}
		r.Header.Set("Origin", v.origin)
		if strings.HasPrefix(v.path, "/ws/") {
			ctx, cancel := timeout()
			defer cancel()
			c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+v.path, &websocket.DialOptions{HTTPHeader: r.Header})
			if c != nil {
				defer c.CloseNow()
			}
			check(err != nil && resp != nil && resp.StatusCode == v.status, "WebSocket upgrade %s: expected HTTP %d, got %v, err=%v", v.path, v.status, resp, err)
			return 1
		}
		resp, err := f.client.Do(r)
		must(err)
		defer resp.Body.Close()
		check(resp.StatusCode == v.status, "%s %s: got %d want %d", v.method, v.path, resp.StatusCode, v.status)
		return 1
	})
}

func (f *fixture) worktrees(n int) metric {
	repo := filepath.Join(f.root, "repository")
	must(os.Mkdir(repo, 0700))
	f.git("-C", repo, "init", "-b", "main")
	f.git("-C", repo, "commit", "--allow-empty", "-m", "fixture")
	r := decode[workspace.Repository](f.request("POST", "/api/repositories", map[string]string{"path": repo}, 201))
	return measure("worktree_create_launch_remove", "ms", n, func(i int) float64 {
		start := time.Now()
		path := filepath.Join(f.root, fmt.Sprintf("tree-%d", i))
		base := "/api/repositories/" + r.ID
		w := decode[workspace.Worktree](f.request("POST", base+"/worktrees", workspace.CreateRequest{Path: path, Mode: "new", Branch: fmt.Sprintf("baseline-%d", i), Base: "main"}, 201))
		s := decode[session.Info](f.request("POST", "/api/sessions", map[string]any{"preset": "custom", "cmd": []string{"/bin/bash", "-c", "pwd; exit 7"}, "workspace": workspace.Reference{RepositoryID: r.ID, WorktreeID: w.ID}}, 201))
		defer f.dispose(s.ID)
		info := f.await(s.ID, func(v session.Info) bool { return v.Status == session.StatusExited })
		check(info.Cwd == path && info.ExitCode != nil && *info.ExitCode == 7, "workspace cwd/exit mismatch")
		check(bytes.Equal(f.request("GET", "/api/sessions/"+s.ID+"/buffer", nil, 200), []byte(path+"\r\n")), "worktree command did not run in expected cwd")
		preview := decode[workspace.Preview](f.request("POST", base+"/worktrees/"+w.ID+"/removal-preview", struct{}{}, 200))
		f.request("POST", base+"/worktrees/"+w.ID+"/remove", workspace.RemoveRequest{ConfirmationID: preview.ConfirmationID, Mode: "safe", ConfirmPath: path}, 204)
		_, err := os.Stat(path)
		check(os.IsNotExist(err), "removed worktree still exists")
		return milliseconds(start)
	})
}
