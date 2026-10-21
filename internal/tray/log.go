package tray

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultLogLimit is the size at which the tray log rotates to ".1".
const DefaultLogLimit = 1 << 20

// Log is the tray's own log: one rotated predecessor, and literal redaction
// of any secret registered while it is in use as a last line of defence.
type Log struct {
	path  string
	limit int64

	mu      sync.Mutex
	file    *os.File
	size    int64
	secrets map[string]int
}

func OpenLog(path string, limit int64) (*Log, error) {
	if limit <= 0 {
		limit = DefaultLogLimit
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	l := &Log{path: path, limit: limit, secrets: map[string]int{}}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Log) open() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	l.file, l.size = f, info.Size()
	return nil
}

// Redact registers secret until the returned release is called.
func (l *Log) Redact(secret string) (release func()) {
	if secret == "" {
		return func() {}
	}
	l.mu.Lock()
	l.secrets[secret]++
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		if l.secrets[secret]--; l.secrets[secret] <= 0 {
			delete(l.secrets, secret)
		}
		l.mu.Unlock()
	}
}

// Printf writes one timestamped line.
func (l *Log) Printf(format string, args ...any) {
	line := time.Now().Format("2006-01-02 15:04:05.000 ") + fmt.Sprintf(format, args...)
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	_, _ = l.Write([]byte(line))
}

func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := string(p)
	for secret := range l.secrets {
		text = strings.ReplaceAll(text, secret, "[redacted]")
	}
	if l.file == nil {
		return 0, os.ErrClosed
	}
	if l.size > 0 && l.size+int64(len(text)) > l.limit {
		_ = l.file.Close()
		l.file = nil
		_ = os.Remove(l.path + ".1")
		if err := os.Rename(l.path, l.path+".1"); err != nil {
			return 0, err
		}
		if err := l.open(); err != nil {
			return 0, err
		}
	}
	n, err := l.file.WriteString(text)
	l.size += int64(n)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// Path is the current log file.
func (l *Log) Path() string { return l.path }
