package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

type Status string

const (
	StatusRunning Status = "running"
	StatusWaiting Status = "waiting"
	StatusIdle    Status = "idle"
	StatusExited  Status = "exited"
)

type Workspace struct {
	RepositoryID string
	WorktreeID   string
	WorktreePath string
}

// ReportedCwd is the directory a program reported through OSC 7. It is a
// navigation hint only; session ownership, file access roots and command
// execution stay bound to the immutable launch directory and Workspace.
type ReportedCwd struct {
	Path   string
	Host   string
	Remote bool
	Since  time.Time
}

type Info struct {
	ID string
	// Generation distinguishes this run from any other session that could
	// carry the same ID. Output offsets and submissions are bound to it.
	Generation   string
	Title        string
	Cwd          string
	Preset       string
	Cmd          []string
	Status       Status
	ExitCode     *int
	Preview      string
	CreatedAt    time.Time
	LastActiveAt time.Time
	Cols         uint16
	Rows         uint16
	Clients      int
	Workspace    *Workspace
	Attention    *Attention
	ReportedCwd  *ReportedCwd
}

// Message is one item queued for a terminal subscriber, in protocol order.
// Frame is raw output (type byte 0) or a replay snapshot (type byte 1),
// shared between subscribers and never modified. Otherwise exactly one of
// the other fields is set; the controller encodes it for the client.
type Message struct {
	Frame      []byte
	Info       *Info
	Exit       *int
	Position   *Position
	Submission *SubmissionStatus
	Control    *ControlState
}

type Subscriber struct {
	Out    chan Message
	Slow   chan struct{}
	failed bool
	client string
	label  string
	seq    uint64
}

// AttachOptions identifies the browser behind a connection and where its
// terminal already is. A matching resume point replays only the missing tail.
type AttachOptions struct {
	Client string
	Label  string
	Resume *ResumePoint
}
type ResumePoint struct {
	Generation string
	Offset     uint64
}
type Session struct {
	mu        sync.Mutex
	inputGate chan struct{}
	info      Info
	userTitle string
	terminal  *os.File
	cmd       *exec.Cmd
	ring      *RingBuffer
	osc       OSCScanner
	signals   *SignalHandler
	// lastInputAt and bellAttention are owned by mu, like info.Attention.
	lastInputAt   time.Time
	bellAttention bool
	preview       Preview
	subs          map[*Subscriber]struct{}
	nextSeq       uint64
	control       *Controller
	// controlSub is the controlling client's authoritative connection: the
	// most recent one. A duplicated browser tab can carry the same client ID;
	// only one of its connections may resize the PTY or answer queries.
	controlSub  *Subscriber
	submissions submissionLog
	done        chan struct{}
	readerDone  chan struct{}
	cleanupDone chan struct{}
	closeOnce   sync.Once
}

type startOptions struct {
	agentProtocol bool
	bellAttention bool
	// env is the caller's explicit launch environment.
	env map[string]string
}

func start(id, cwd, preset, name string, argv []string, capacity int, options startOptions) (*Session, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = sessionEnv(os.Environ(), id, options.agentProtocol, options.env)
	f, err := startPTY(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		return nil, err
	}
	pollable, err := pollablePTY(f)
	if err != nil {
		_ = f.Close()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
		releaseLeader(cmd.Process.Pid)
		cleanupProcesses(cmd.Process.Pid, id)
		return nil, err
	}
	f = pollable
	var raw [8]byte
	if _, err = rand.Read(raw[:]); err != nil {
		_ = f.Close()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
		releaseLeader(cmd.Process.Pid)
		cleanupProcesses(cmd.Process.Pid, id)
		return nil, err
	}
	now := time.Now()
	s := &Session{
		info: Info{
			ID:           id,
			Generation:   hex.EncodeToString(raw[:]),
			Title:        filepath.Base(cwd),
			Cwd:          cwd,
			Preset:       preset,
			Cmd:          append([]string(nil), argv...),
			Status:       StatusRunning,
			CreatedAt:    now,
			LastActiveAt: now,
			Cols:         80,
			Rows:         24,
		},
		userTitle:     name,
		terminal:      f,
		cmd:           cmd,
		ring:          NewRingBuffer(capacity),
		subs:          make(map[*Subscriber]struct{}),
		done:          make(chan struct{}),
		readerDone:    make(chan struct{}),
		bellAttention: options.bellAttention,
	}
	s.signals = s.signalHandler()
	s.inputGate = make(chan struct{}, 1)
	s.inputGate <- struct{}{}
	s.cleanupDone = make(chan struct{})
	if name != "" {
		s.info.Title = name
	}
	go s.read()
	go s.wait()
	return s, nil
}

func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.infoLocked()
}

func (s *Session) infoLocked() Info {
	i := s.info
	i.Clients = len(s.subs)
	i.Cmd = append([]string(nil), i.Cmd...)
	if i.Workspace != nil {
		workspace := *i.Workspace
		i.Workspace = &workspace
	}
	if i.ExitCode != nil {
		code := *i.ExitCode
		i.ExitCode = &code
	}
	if i.Attention != nil {
		attention := *i.Attention
		i.Attention = &attention
	}
	if i.ReportedCwd != nil {
		reported := *i.ReportedCwd
		i.ReportedCwd = &reported
	}
	return i
}
func (s *Session) Buffer() []byte        { return s.ring.Snapshot() }
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) stateLocked() Message {
	return infoMessage(s.infoLocked())
}
func (s *Session) sendLocked(sub *Subscriber, m Message) {
	if sub.failed {
		return
	}
	select {
	case sub.Out <- m:
	default:
		sub.failed = true
		close(sub.Slow)
	}
}
func (s *Session) broadcastLocked(m Message) {
	for sub := range s.subs {
		s.sendLocked(sub, m)
	}
}

// Attach registers an anonymous subscriber with a full replay.
func (s *Session) Attach() *Subscriber {
	return s.AttachClient(AttachOptions{})
}

// AttachClient atomically captures history and registers the live subscriber:
// no replay gap. The first client of a session without a controller takes
// control; everyone else observes until they send user input or claim it.
func (s *Session) AttachClient(o AttachOptions) *Subscriber {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o.Client == "" {
		o.Client = randomClientID()
	}
	s.nextSeq++
	sub := &Subscriber{Out: make(chan Message, 256), Slow: make(chan struct{}), client: o.Client, label: o.Label, seq: s.nextSeq}
	if s.control == nil {
		s.setControlLocked(sub)
	} else if s.control.ClientID == sub.client {
		// A reconnect supersedes the connection it replaces.
		s.controlSub = sub
		if s.control.Label != sub.label {
			s.control.Label = sub.label
			s.broadcastLocked(s.controlLocked())
		}
	}
	s.subs[sub] = struct{}{}
	s.replayLocked(sub, o.Resume, nil)
	return sub
}

// Position precedes replayed bytes. "append" continues a terminal at Start
// with the bytes up to End sent as ordinary output; "reset" is followed by a
// full snapshot covering Start to End.
type Position struct {
	Generation string
	Mode       string
	Start      uint64
	End        uint64
	Truncated  bool
}

// replayLocked queues the grid, position, stream and state for sub. Frames in
// held follow the state; the exit frame stays last because the connection
// closes once it is sent.
func (s *Session) replayLocked(sub *Subscriber, resume *ResumePoint, held []Message) {
	// The grid comes first: an observer must size its terminal before it
	// parses cursor-addressed history, or that history lands on a wrong grid.
	s.sendLocked(sub, s.controlLocked())
	start, end := s.ring.Range()
	position := Position{Generation: s.info.Generation, Mode: "reset", Start: start, End: end, Truncated: start > 0}
	if resume != nil && resume.Generation == s.info.Generation {
		if tail, ok := s.ring.sinceWithPrefix([]byte{0}, resume.Offset); ok {
			position.Mode, position.Start = "append", resume.Offset
			s.sendLocked(sub, positionMessage(position))
			if len(tail) > 1 {
				s.sendLocked(sub, frameMessage(tail))
			}
		}
	}
	if position.Mode == "reset" {
		s.sendLocked(sub, positionMessage(position))
		s.sendLocked(sub, frameMessage(s.ring.snapshotWithPrefix([]byte{1})))
	}
	s.sendLocked(sub, s.stateLocked())
	for _, m := range held {
		s.sendLocked(sub, m)
	}
	if s.info.ExitCode != nil {
		s.sendLocked(sub, exitMessage(*s.info.ExitCode))
	}
}

// Replay discards queued stream frames, which the new snapshot supersedes,
// but keeps submission statuses: they are not part of the stream and a lost
// one would leave a draft locked.
func (s *Session) Replay(sub *Subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var statuses []Message
	for {
		select {
		case m := <-sub.Out:
			if m.Submission != nil {
				statuses = append(statuses, m)
			}
		default:
			s.replayLocked(sub, nil, statuses)
			return
		}
	}
}
func (s *Session) Detach(sub *Subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, sub)
	// The newest remaining connection of the same client inherits control.
	var heir *Subscriber
	for other := range s.subs {
		if other.client == sub.client && (heir == nil || other.seq > heir.seq) {
			heir = other
		}
	}
	if heir == nil {
		s.submissions.forgetWatcher(sub.client)
	}
	if s.controlSub != sub {
		return
	}
	// Another connection of the same client takes over; otherwise control is
	// not handed to the remaining observers: their layout must not change
	// because another device went away. The next input claims it.
	s.controlSub = heir
	if heir == nil {
		s.control = nil
		s.broadcastLocked(s.controlLocked())
		return
	}
	// The promoted connection's earlier resizes were refused; the control
	// frame makes it send its grid again.
	s.sendLocked(heir, s.controlLocked())
}
func (s *Session) Write(p []byte) error {
	return s.WriteContext(context.Background(), p)
}
func (s *Session) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 || cols > 1000 || rows > 1000 {
		return errors.New("invalid terminal size")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resizeLocked(cols, rows)
}

// ResizeFrom resizes the PTY only if the subscriber is the controlling
// connection, checked in the same critical section as the resize so that a
// takeover cannot interleave between them.
func (s *Session) ResizeFrom(sub *Subscriber, cols, rows uint16) error {
	if cols == 0 || rows == 0 || cols > 1000 || rows > 1000 {
		return errors.New("invalid terminal size")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.controlSub != sub {
		return ErrNotController
	}
	return s.resizeLocked(cols, rows)
}

func (s *Session) resizeLocked(cols, rows uint16) error {
	if cols == s.info.Cols && rows == s.info.Rows {
		return nil
	}
	err := pty.Setsize(s.terminal, &pty.Winsize{Cols: cols, Rows: rows})
	if err == nil {
		s.info.Cols = cols
		s.info.Rows = rows
		s.info.Preview = s.preview.Resize(cols, rows)
		// Observers follow the controller's grid instead of fitting their own.
		s.broadcastLocked(s.controlLocked())
	}
	return err
}
func (s *Session) read() {
	defer close(s.readerDone)
	buf := make([]byte, 32768)
	for {
		n, err := s.terminal.Read(buf)
		if n > 0 {
			p := buf[:n]
			s.mu.Lock()
			s.ring.Write(p)
			s.osc.Feed(p, s.signals)
			reports := s.preview.colorSchemeReports
			s.info.Preview = s.preview.Feed(p)
			s.info.LastActiveAt = time.Now()
			// Latched attention outlives redraws such as spinners and timers.
			if s.info.Attention != nil {
				s.info.Status = StatusWaiting
			} else {
				s.info.Status = StatusRunning
			}
			if len(s.subs) > 0 {
				s.broadcastLocked(outputMessage(p))
				// After the output that toggled it, so the mode applies from there.
				if s.preview.colorSchemeReports != reports {
					s.broadcastLocked(s.controlLocked())
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}
func (s *Session) wait() {
	err := s.cmd.Wait()
	releaseLeader(s.cmd.Process.Pid)
	code := 0
	if err != nil {
		code = s.cmd.ProcessState.ExitCode()
		if code < 0 {
			if status, ok := s.cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
				code = 128 + int(status.Signal())
			}
		}
	} // Drain final PTY bytes before publishing exit.
	s.Close()
	<-s.cleanupDone
	select {
	case <-s.readerDone:
	case <-time.After(time.Second):
		_ = s.terminal.Close()
		<-s.readerDone
	}
	_ = s.terminal.Close()
	s.mu.Lock()
	s.info.Status = StatusExited
	s.info.Attention = nil
	// A finished program can no longer stand behind its last directory report.
	s.info.ReportedCwd = nil
	s.info.ExitCode = &code
	s.broadcastLocked(s.stateLocked())
	s.broadcastLocked(exitMessage(code))
	close(s.done)
	s.mu.Unlock()
	slog.Info("session exited", "id", s.info.ID, "exitCode", code)
}
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGHUP)
		go func() {
			defer close(s.cleanupDone)
			cleanupProcesses(s.cmd.Process.Pid, s.info.ID)
		}()
	})
}
