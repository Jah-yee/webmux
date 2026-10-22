package push

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// browserKeys returns a valid p256dh/auth pair and the private key to decrypt.
func browserKeys(t *testing.T) (string, string, *ecdh.PrivateKey) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return b64.EncodeToString(key.PublicKey().Bytes()), b64.EncodeToString(auth), key
}

func testSubscription(t *testing.T, endpoint string) Subscription {
	p256dh, auth, _ := browserKeys(t)
	return Subscription{Endpoint: endpoint, P256dh: p256dh, Auth: auth, Origin: "https://term.example.com"}
}

func TestStorePersistsKeyAndSubscriptions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s, err := OpenStore(dir, discard())
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0700, filepath.Join(dir, storeFile): 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s mode %v, %v; want %v", path, info.Mode().Perm(), err, want)
		}
	}
	first := testSubscription(t, "https://push.example/a")
	id, err := s.Add(first)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := testSubscription(t, "https://push.example/a")
	again, err := s.Add(refreshed)
	if err != nil || again != id {
		t.Fatalf("re-registration id %q, %v; want %q", again, err, id)
	}
	reopened, err := OpenStore(dir, discard())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.PublicKey() != s.PublicKey() || len(reopened.PublicKey()) != 87 {
		t.Fatalf("public key changed across restart: %q vs %q", reopened.PublicKey(), s.PublicKey())
	}
	subs := reopened.List()
	if len(subs) != 1 || subs[0].ID != id || subs[0].P256dh != refreshed.P256dh || subs[0].Origin != "https://term.example.com" {
		t.Fatalf("subscriptions after restart: %+v", subs)
	}
	if err := reopened.Remove(id); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Remove(id); err != nil || reopened.Count() != 0 {
		t.Fatalf("second remove %v, count %d", err, reopened.Count())
	}
}

func TestStoreLimitsSubscriptions(t *testing.T) {
	s, err := OpenStore(t.TempDir(), discard())
	if err != nil {
		t.Fatal(err)
	}
	for i := range MaxSubscriptions {
		if _, err := s.Add(testSubscription(t, fmt.Sprintf("https://push.example/%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Add(testSubscription(t, "https://push.example/extra")); err != ErrFull {
		t.Fatalf("33rd subscription: %v, want ErrFull", err)
	}
	if _, err := s.Add(testSubscription(t, "https://push.example/0")); err != nil {
		t.Fatalf("known endpoint refused at the limit: %v", err)
	}
}

func TestStoreRebuildsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, storeFile)
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	s, err := OpenStore(dir, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if s.Count() != 0 || s.PublicKey() == "" {
		t.Fatalf("rebuilt store: %d subscriptions, key %q", s.Count(), s.PublicKey())
	}
	aside, _ := filepath.Glob(path + ".corrupt-*")
	if len(aside) != 1 {
		t.Fatalf("corrupt file not kept aside: %v", aside)
	}
	if kept, _ := os.ReadFile(aside[0]); string(kept) != "{not json" {
		t.Fatalf("kept content %q", kept)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("no warning logged: %s", logs.String())
	}
}

func TestStoreUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0700) })
	if _, err := OpenStore(filepath.Join(parent, "state"), discard()); err == nil {
		t.Fatal("store opened in an unwritable directory")
	}
}
