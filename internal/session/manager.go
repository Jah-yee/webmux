package session

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rainchan/webmux/internal/pathutil"
)

type Preset struct {
	Name      string
	Label     string
	Cmd       []string
	AllowArgs bool
}
type Options struct {
	ScrollbackBytes int
	DefaultCwd      string
	Presets         []Preset
	IdleAfter       time.Duration
	PromptPatterns  []string
	// Zero values keep both signal sources enabled.
	DisableAgentProtocol bool
	DisableBellAttention bool
}
type Event struct {
	Type     string
	Sessions []Info
	Session  *Info
	ID       string
}
type ListSubscriber struct {
	Out    chan Event
	Slow   chan struct{}
	failed bool
}
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	subs     map[*ListSubscriber]struct{}
	options  Options
	patterns []*regexp.Regexp
	done     chan struct{}
	stopOnce sync.Once
	stopped  bool
	initErr  error
}

var ErrNotFound = errors.New("session not found")
var ErrRunning = errors.New("session is still running")

func NewManager(o Options) *Manager {
	if o.ScrollbackBytes == 0 {
		o.ScrollbackBytes = 2 * 1024 * 1024
	}
	if o.IdleAfter == 0 {
		o.IdleAfter = 30 * time.Second
	}
	if o.PromptPatterns == nil {
		o.PromptPatterns = []string{`[$#%>❯›?:]\s*$`, `(?i)\(y/n\)|\[y/n\]|yes.*no|press enter`}
	}
	m := &Manager{
		sessions: make(map[string]*Session),
		subs:     make(map[*ListSubscriber]struct{}),
		options:  o,
		done:     make(chan struct{}),
	}
	m.initErr = enableReaper()
	for _, pattern := range o.PromptPatterns {
		m.patterns = append(m.patterns, regexp.MustCompile(pattern))
	}
	go m.updates()
	return m
}

// Create starts a session. env holds explicit variables added to the
// server's environment for this session only (see ValidateEnv).
func (m *Manager) Create(cwd, preset string, cmd, args []string, name string, env map[string]string) (*Session, error) {
	if m.initErr != nil {
		return nil, fmt.Errorf("process reaper: %w", m.initErr)
	}
	if err := ValidateEnv(env); err != nil {
		return nil, err
	}
	if cwd == "" {
		cwd = m.options.DefaultCwd
	}
	cwd, err := pathutil.Expand(cwd)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(cwd)
	if err != nil {
		return nil, fmt.Errorf("directory: %w", err)
	}
	if !stat.IsDir() {
		return nil, errors.New("path is not a directory")
	}
	var argv []string
	if preset == "custom" {
		if len(cmd) == 0 || cmd[0] == "" {
			return nil, errors.New("custom command requires an executable")
		}
		if len(args) > 0 {
			return nil, errors.New("custom command does not accept preset arguments")
		}
		argv = cmd
	} else {
		if cmd != nil {
			return nil, errors.New("command is only allowed with the custom preset")
		}
		for _, p := range m.options.Presets {
			if p.Name == preset {
				if len(args) > 0 && !p.AllowArgs {
					return nil, errors.New("preset does not allow arguments")
				}
				argv = make([]string, len(p.Cmd)+len(args))
				copy(argv, p.Cmd)
				copy(argv[len(p.Cmd):], args)
				break
			}
		}
		if len(argv) == 0 || argv[0] == "" {
			return nil, errors.New("unknown or empty preset")
		}
	}
	var raw [5]byte
	if _, err = rand.Read(raw[:]); err != nil {
		return nil, err
	}
	id := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:]))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return nil, errors.New("server is shutting down")
	}
	s, err := start(id, cwd, preset, name, argv, m.options.ScrollbackBytes, startOptions{
		agentProtocol: !m.options.DisableAgentProtocol,
		bellAttention: !m.options.DisableBellAttention,
		env:           env,
	})
	if err != nil {
		return nil, err
	}
	m.sessions[id] = s
	info := s.Info()
	m.publishLocked(Event{Type: "created", Session: &info})
	slog.Info("session created", "id", id, "preset", preset)
	return s, nil
}

// Done closes when the manager shuts down. Consumers use it to release resources.
func (m *Manager) Done() <-chan struct{} { return m.done }

func (m *Manager) Get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked()
}

func (m *Manager) listLocked() []Info {
	list := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s.Info())
	}
	sort.Slice(list, func(i, j int) bool { return list[i].LastActiveAt.After(list[j].LastActiveAt) })
	return list
}
func (m *Manager) Close(id string) error {
	s, err := m.Get(id)
	if err != nil {
		return err
	}
	s.Close()
	return nil
}
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	if s.Info().Status != StatusExited {
		return ErrRunning
	}
	delete(m.sessions, id)
	m.publishLocked(Event{Type: "removed", ID: id})
	return nil
}
func (m *Manager) Subscribe() *ListSubscriber {
	m.mu.Lock()
	defer m.mu.Unlock()
	sub := &ListSubscriber{Out: make(chan Event, 256), Slow: make(chan struct{})}
	sub.Out <- Event{Type: "snapshot", Sessions: m.listLocked()}
	m.subs[sub] = struct{}{}
	return sub
}
func (m *Manager) Unsubscribe(sub *ListSubscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subs, sub)
}
func (m *Manager) publishLocked(e Event) {
	for sub := range m.subs {
		if sub.failed {
			continue
		}
		select {
		case sub.Out <- e:
		default:
			sub.failed = true
			close(sub.Slow)
		}
	}
}
func (m *Manager) updates() {
	timer := time.NewTicker(500 * time.Millisecond)
	defer timer.Stop()
	previous := make(map[string]Info)
	previewSent := make(map[string]time.Time)
	var snapshot processSnapshot
	probeTick := false
	for {
		select {
		case <-m.done:
			return
		case now := <-timer.C:
			probeTick = !probeTick
			if probeTick {
				table := processes()
				reapAdopted(table)
				snapshot = newProcessSnapshot(table)
			}
			m.mu.Lock()
			for id, s := range m.sessions {
				// Classify every 500 ms using the once-per-second process table.
				// This leaves headroom for delivering the 3-second waiting signal.
				s.evaluate(now, snapshot, m.options.IdleAfter, m.patterns)
				info := s.Info()
				old, known := previous[id]
				if known && now.Sub(previewSent[id]) < 2*time.Second {
					info.Preview = old.Preview
				}
				if !known || !sameDisplay(old, info) {
					m.publishLocked(Event{Type: "updated", Session: &info})
					s.publishState()
					if !known || old.Preview != info.Preview {
						previewSent[id] = now
					}
					previous[id] = info
				}
			}
			for id := range previous {
				if _, exists := m.sessions[id]; !exists {
					delete(previous, id)
					delete(previewSent, id)
				}
			}
			m.mu.Unlock()
		}
	}
}
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	m.stopped = true
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	for _, s := range list {
		s.Close()
	}
	for _, s := range list {
		select {
		case <-s.Done():
		case <-ctx.Done():
			forceSession(s.cmd.Process.Pid, s.info.ID)
		}
	}
	reapAdopted(processes())
	m.stopOnce.Do(func() { close(m.done) })
}

// SetWorkspace commits a separately resolved association only for the same
// session and immutable launch directory. No filesystem or Git work runs here.
func (m *Manager) SetWorkspace(id, cwd string, workspace *Workspace) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return
	}
	s.mu.Lock()
	if s.info.Cwd != cwd || sameWorkspace(s.info.Workspace, workspace) {
		s.mu.Unlock()
		return
	}
	if workspace == nil {
		s.info.Workspace = nil
	} else {
		value := *workspace
		s.info.Workspace = &value
	}
	info := s.infoLocked()
	s.broadcastLocked(s.stateLocked())
	s.mu.Unlock()
	m.publishLocked(Event{Type: "updated", Session: &info})
}
