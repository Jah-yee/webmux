package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/workspace"
)

func TestWorkspaceDegradedServerProcess(t *testing.T) {
	if configPath := os.Getenv("WEBMUX_TEST_DEGRADED_CONFIG"); configPath != "" {
		if err := run([]string{"--config", configPath}); err != nil {
			t.Fatal(err)
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, brokenState := range []bool{false, true} {
		t.Run(fmt.Sprintf("brokenStorage=%v", brokenState), func(t *testing.T) {
			dir := t.TempDir()
			state := filepath.Join(dir, "state")
			if brokenState {
				if err := os.WriteFile(state, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			configPath := filepath.Join(dir, "config.toml")
			content := fmt.Sprintf("listen = '127.0.0.1:0'\ndefault_cwd = %q\nstate_dir = %q\nshell = '/bin/sh'\n", dir, state)
			if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestWorkspaceDegradedServerProcess$")
			cmd.Env = append(os.Environ(), "WEBMUX_TEST_DEGRADED_CONFIG="+configPath, "PATH="+filepath.Join(dir, "no-git"), "HOME="+dir)
			output, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			ended := make(chan error, 1)
			defer func() {
				_ = cmd.Process.Signal(syscall.SIGTERM)
				select {
				case err := <-ended:
					if err != nil && ctx.Err() == nil {
						t.Errorf("server exit: %v", err)
					}
				case <-time.After(8 * time.Second):
					_ = cmd.Process.Kill()
					<-ended
					t.Error("server shutdown timed out")
				}
			}()
			address := make(chan string, 1)
			go func() {
				scanner := bufio.NewScanner(output)
				for scanner.Scan() {
					text := scanner.Text()
					if strings.Contains(text, "webmux listening") {
						for _, field := range strings.Fields(text) {
							if strings.HasPrefix(field, "address=") {
								address <- strings.TrimPrefix(field, "address=")
							}
						}
					}
				}
			}()
			go func() { ended <- cmd.Wait() }()
			var url string
			select {
			case addr := <-address:
				url = "http://" + addr
			case <-ctx.Done():
				t.Fatal("real server did not start in degraded environment")
			}
			client := &http.Client{Timeout: 3 * time.Second}
			response, err := client.Get(url + "/api/config")
			if err != nil {
				t.Fatal(err)
			}
			var settings struct {
				Workspace workspace.Capability `json:"workspace"`
			}
			err = json.NewDecoder(response.Body).Decode(&settings)
			_ = response.Body.Close()
			if err != nil || settings.Workspace.GitAvailable || settings.Workspace.RegistryAvailable == brokenState {
				t.Fatalf("degraded capability: %+v %v", settings, err)
			}
			response, err = client.Post(url+"/api/sessions", "application/json", strings.NewReader(`{"preset":"custom","cmd":["/bin/sh","-c","printf REAL_DEGRADED"]}`))
			if err != nil {
				t.Fatal(err)
			}
			var info contract.SessionInfo
			err = json.NewDecoder(response.Body).Decode(&info)
			_ = response.Body.Close()
			if err != nil || response.StatusCode != 201 || info.Cwd != dir || info.Workspace != nil {
				t.Fatalf("real degraded launch: status %d info %+v err %v", response.StatusCode, info, err)
			}
		})
	}
}
