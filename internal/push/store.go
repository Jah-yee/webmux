// Package push delivers session attention alerts to browsers over Web Push
// (RFC 8030) with message encryption (RFC 8291) and VAPID (RFC 8292).
package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rainchan/webmux/internal/statefile"
)

// MaxSubscriptions bounds the devices one instance alerts.
const MaxSubscriptions = 32

const storeFile = "push.json"

var ErrFull = errors.New("已有 32 台设备开启推送，请先在其他设备关闭推送")

// Subscription is one browser push endpoint. Origin is the page origin it was
// registered from and anchors the notification's link.
type Subscription struct {
	ID        string    `json:"id"`
	Endpoint  string    `json:"endpoint"`
	P256dh    string    `json:"p256dh"`
	Auth      string    `json:"auth"`
	Origin    string    `json:"origin"`
	CreatedAt time.Time `json:"createdAt"`
}

type storedFile struct {
	V     int `json:"v"`
	VAPID struct {
		Private string `json:"private"`
	} `json:"vapid"`
	Subscriptions []Subscription `json:"subscriptions"`
}

// Store keeps the VAPID key and subscriptions in <state_dir>/push.json. The
// file is small and written rarely, so every change rewrites it under mu.
type Store struct {
	mu   sync.Mutex
	dir  string
	key  *ecdsa.PrivateKey
	subs []Subscription
}

var b64 = base64.RawURLEncoding

// OpenStore loads or creates the store. An unparsable file is kept aside and
// replaced by a fresh key with no subscriptions; an inaccessible directory or
// file is an error, which disables push without affecting sessions.
func OpenStore(dir string, log *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	path := filepath.Join(dir, storeFile)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		parseErr := s.parse(data)
		if parseErr == nil {
			if err := os.Chmod(path, 0600); err != nil {
				return nil, err
			}
			return s, nil
		}
		aside := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
		if err := os.Rename(path, aside); err != nil {
			return nil, err
		}
		log.Warn("push store unreadable; kept aside and recreated, devices must enable push again", "file", aside, "error", parseErr)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	s.key, s.subs = key, nil
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) parse(data []byte) error {
	var f storedFile
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f.V != 1 {
		return fmt.Errorf("unsupported version %d", f.V)
	}
	raw, err := b64.DecodeString(f.VAPID.Private)
	if err != nil {
		return fmt.Errorf("vapid key: %w", err)
	}
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return fmt.Errorf("vapid key: %w", err)
	}
	ids, endpoints := map[string]bool{}, map[string]bool{}
	for _, sub := range f.Subscriptions {
		if sub.ID == "" || ids[sub.ID] || endpoints[sub.Endpoint] {
			return errors.New("duplicate or empty subscription")
		}
		if _, _, err := decodeClientKeys(sub.P256dh, sub.Auth); err != nil {
			return err
		}
		if err := checkEndpoint(sub.Endpoint); err != nil {
			return err
		}
		ids[sub.ID], endpoints[sub.Endpoint] = true, true
	}
	if len(f.Subscriptions) > MaxSubscriptions {
		return errors.New("too many subscriptions")
	}
	s.key, s.subs = key, f.Subscriptions
	return nil
}

// PublicKey is the base64url uncompressed P-256 point browsers pass as
// applicationServerKey.
func (s *Store) PublicKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publicKeyLocked()
}

func (s *Store) publicKeyLocked() string {
	raw, _ := s.key.PublicKey.Bytes()
	return b64.EncodeToString(raw)
}

func (s *Store) signer() (*ecdsa.PrivateKey, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.key, s.publicKeyLocked()
}

// Add registers sub or, for a known endpoint, refreshes its keys and origin
// and returns the existing ID.
func (s *Store) Add(sub Subscription) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := append([]Subscription(nil), s.subs...)
	found := false
	for i := range next {
		if next[i].Endpoint == sub.Endpoint {
			next[i].P256dh, next[i].Auth, next[i].Origin = sub.P256dh, sub.Auth, sub.Origin
			sub.ID, found = next[i].ID, true
			break
		}
	}
	if !found {
		if len(next) >= MaxSubscriptions {
			return "", ErrFull
		}
		sub.ID = newID()
		if sub.CreatedAt.IsZero() {
			sub.CreatedAt = time.Now().UTC()
		}
		next = append(next, sub)
	}
	previous := s.subs
	s.subs = next
	if err := s.saveLocked(); err != nil {
		s.subs = previous
		return "", err
	}
	return sub.ID, nil
}

// Remove deletes id; a missing id is not an error.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.subs {
		if s.subs[i].ID == id {
			previous := s.subs
			s.subs = append(append([]Subscription(nil), s.subs[:i]...), s.subs[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.subs = previous
				return err
			}
			return nil
		}
	}
	return nil
}

func (s *Store) Get(id string) (Subscription, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subs {
		if sub.ID == id {
			return sub, true
		}
	}
	return Subscription{}, false
}

func (s *Store) List() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Subscription(nil), s.subs...)
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

func (s *Store) saveLocked() error {
	var f storedFile
	f.V = 1
	raw, err := s.key.Bytes()
	if err != nil {
		return err
	}
	f.VAPID.Private = b64.EncodeToString(raw)
	f.Subscriptions = s.subs
	if f.Subscriptions == nil {
		f.Subscriptions = []Subscription{}
	}
	return statefile.WriteJSON(filepath.Join(s.dir, storeFile), f, statefile.Options{TempPattern: ".push-*"})
}

func newID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:]))
}
