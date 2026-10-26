//go:build linux

package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestAdoptedZombieReapedWhileLeaderLives(t *testing.T) {
	t.Parallel()
	m := NewManager(Options{DefaultCwd: t.TempDir()})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	s, err := m.Create("", []string{"/bin/bash", "-c", `setsid /bin/bash -c 'sleep 0.05 & printf "DAEMON_CHILD=%s\n" "$!"'; read -r stop; exit 23`}, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`DAEMON_CHILD=(\d+)`)
	var match []string
	until := time.Now().Add(time.Second)
	for len(match) == 0 {
		match = pattern.FindStringSubmatch(string(s.Buffer()))
		if time.Now().After(until) {
			t.Fatal("daemon child was not announced")
		}
		time.Sleep(10 * time.Millisecond)
	}
	child, _ := strconv.Atoi(match[1])
	defer func() { var status syscall.WaitStatus; _, _ = syscall.Wait4(child, &status, syscall.WNOHANG, nil) }()
	until = time.Now().Add(3 * time.Second)
	for {
		_, err = os.Stat("/proc/" + strconv.Itoa(child))
		if os.IsNotExist(err) {
			break
		}
		if time.Now().After(until) {
			t.Fatal("adopted new-session zombie remains while manager is alive")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s.Info().Status == StatusExited {
		t.Fatal("reaper consumed the live session leader")
	}
	if err = s.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("leader wait did not finish")
	}
	if info := s.Info(); info.ExitCode == nil || *info.ExitCode != 23 {
		t.Fatalf("leader wait result stolen: %+v", info)
	}
}

func TestReaperLeavesServiceCommandExitForItsOwner(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 17")
	if err := startChild(cmd); err != nil {
		t.Fatal(err)
	}
	defer releaseLeader(cmd.Process.Pid)
	deadline := time.Now().Add(time.Second)
	for {
		table := processes()
		if state, ok := table[cmd.Process.Pid]; ok && state.state == 'Z' {
			reapAdopted(table)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command did not become waitable")
		}
		time.Sleep(time.Millisecond)
	}
	err := cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 17 {
		t.Fatalf("service child Wait result stolen: %v", err)
	}
}
