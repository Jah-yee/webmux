package tray

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rainchan/webmux/internal/fetch"
)

// installRunner plays a distribution: uname, wslpath, install, version.
type installRunner struct {
	mu        sync.Mutex
	machine   string
	noMount   bool
	installed string // version reported after install
	calls     [][]string
}

func (r *installRunner) Run(_ context.Context, argv, _ []string) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, argv)
	exec := slices.Index(argv, "--exec")
	switch argv[exec+1] {
	case "uname":
		return Result{Stdout: []byte(r.machine + "\n")}, nil
	case "wslpath":
		if r.noMount {
			return Result{Stderr: []byte("wslpath: not mounted"), Code: 1}, nil
		}
		return Result{Stdout: []byte("/mnt/c/dl/" + filepath.Base(argv[len(argv)-1]) + "\n")}, nil
	case "install":
		return Result{}, nil
	}
	if slices.Equal(argv[len(argv)-2:], []string{"version", "--json"}) {
		return Result{Stdout: []byte(`{"v":1,"version":"` + r.installed + `","contract":1}` + "\n")}, nil
	}
	return Result{Code: 1}, nil
}

func (r *installRunner) Start([]string, []string) (Process, error) { return nil, errors.New("unused") }

func (r *installRunner) program(name string) [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out [][]string
	for _, argv := range r.calls {
		if exec := slices.Index(argv, "--exec"); exec >= 0 && argv[exec+1] == name {
			out = append(out, argv)
		}
	}
	return out
}

type releaseServer struct {
	*httptest.Server
	hits  atomic.Int32
	paths chan string
}

func newReleaseServer(t *testing.T, body []byte) *releaseServer {
	s := &releaseServer{paths: make(chan string, 8)}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.paths <- r.URL.Path
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

var serverBinary = []byte("#!/bin/sh\necho webmux 0.2.0\n")

func testInstaller(t *testing.T, runner *installRunner, base string, digests map[string]string) Installer {
	return Installer{
		Runner:     runner,
		Target:     Target{Distro: "Ubuntu", Webmux: "~/my bin/web;mux"},
		Version:    "0.2.0",
		Dir:        filepath.Join(t.TempDir(), "downloads"),
		Digests:    digests,
		Downloader: fetch.Downloader{Name: "webmux", GitHubBase: base, Mirrors: []string{}},
	}
}

func TestInstallerInstallsVerifiedRelease(t *testing.T) {
	srv := newReleaseServer(t, serverBinary)
	runner := &installRunner{machine: "aarch64", installed: "0.2.0"}
	in := testInstaller(t, runner, srv.URL, map[string]string{"amd64": "unused", "arm64": sum(serverBinary)})
	if err := in.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := <-srv.paths; got != "/"+ReleaseRepo+"/releases/download/v0.2.0/webmux-linux-arm64" {
		t.Fatalf("release path %q", got)
	}
	installs := runner.program("install")
	want := []string{"wsl.exe", "-d", "Ubuntu", "--cd", "~", "--exec", "install", "-D", "-m", "0755", "/mnt/c/dl/webmux-0.2.0-linux-arm64", "./my bin/web;mux"}
	if len(installs) != 1 || !slices.Equal(installs[0], want) {
		t.Fatalf("install argv %q", installs)
	}
	// A verified download is reused for the next install.
	if err := in.Install(context.Background()); err != nil || srv.hits.Load() != 1 {
		t.Fatalf("reinstall: %v, %d downloads", err, srv.hits.Load())
	}
}

func TestInstallerRefusesUnverifiedFiles(t *testing.T) {
	srv := newReleaseServer(t, []byte("tampered"))
	runner := &installRunner{machine: "x86_64", installed: "0.2.0"}
	in := testInstaller(t, runner, srv.URL, map[string]string{"amd64": sum(serverBinary), "arm64": sum(serverBinary)})
	err := in.Install(context.Background())
	var sources *fetch.SourcesError
	if !errors.As(err, &sources) || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("tampered download: %v", err)
	}
	if len(runner.program("install")) != 0 {
		t.Fatal("installed an unverified file")
	}
	entries, _ := os.ReadDir(in.Dir)
	if len(entries) != 0 {
		t.Fatalf("download left behind: %v", entries)
	}
}

func TestInstallerPreconditions(t *testing.T) {
	digests := map[string]string{"amd64": sum(serverBinary), "arm64": sum(serverBinary)}
	for name, tc := range map[string]struct {
		runner  *installRunner
		digests map[string]string
		version string
		want    string
	}{
		"development tray": {&installRunner{machine: "x86_64"}, nil, "0.2.0", ErrInstallUnsupported.Error()},
		"dev version":      {&installRunner{machine: "x86_64"}, digests, "dev", ErrInstallUnsupported.Error()},
		"architecture":     {&installRunner{machine: "riscv64"}, digests, "0.2.0", "不支持的 WSL 架构 riscv64"},
		"no automount":     {&installRunner{machine: "x86_64", noMount: true}, digests, "0.2.0", ErrNoWindowsFiles.Error()},
		"wrong version":    {&installRunner{machine: "x86_64", installed: "0.1.0"}, digests, "0.2.0", `报告版本 "0.1.0"`},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newReleaseServer(t, serverBinary)
			in := testInstaller(t, tc.runner, srv.URL, tc.digests)
			in.Version = tc.version
			err := in.Install(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want %q", err, tc.want)
			}
			if tc.want == ErrInstallUnsupported.Error() && (len(tc.runner.calls) != 0 || srv.hits.Load() != 0) {
				t.Fatal("unsupported tray touched WSL or the network")
			}
			if tc.runner.noMount || tc.runner.machine == "riscv64" {
				if len(tc.runner.program("install")) != 0 {
					t.Fatal("install ran after a failed precondition")
				}
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	for in, want := range map[string]string{"webmux 0.2.0\n": "0.2.0", "webmux dev": "dev", "usage: webmux": "", "": ""} {
		if got := ParseVersion(in); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
