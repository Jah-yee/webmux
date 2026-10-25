// Package launch stores user launch configs: a name, a full argv and explicit
// environment variables, persisted in the server state directory. It never
// reads the TOML presets; those stay live built-ins served by /api/presets.
package launch

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rainchan/webmux/internal/session"
)

// Limits shared with the browser editor.
const (
	MaxConfigs    = 100
	MaxNameChars  = 64
	MaxCmdItems   = 128
	MaxItemBytes  = 8192
	MaxEnvEntries = 64
)

var (
	// ErrInvalid marks input that breaks the data model (400).
	ErrInvalid = errors.New("启动配置无效")
	// ErrDuplicate marks a name already used by another config (409).
	ErrDuplicate = errors.New("启动配置名称已存在")
	// ErrNotFound marks an unknown id (404).
	ErrNotFound = errors.New("启动配置不存在")
	// ErrUnavailable marks a store that is locked, corrupt or unreadable (503).
	ErrUnavailable = errors.New("启动配置不可用")
)

// Config is a full launch config, including environment values.
type Config struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Cmd       []string          `json:"cmd"`
	Env       map[string]string `json:"env"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

// Summary is the list form: environment names only, never values.
type Summary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Cmd       []string  `json:"cmd"`
	EnvNames  []string  `json:"envNames"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Input holds the client-editable fields of a create or whole update.
type Input struct {
	Name string
	Cmd  []string
	Env  map[string]string
}

var idPattern = regexp.MustCompile(`^[a-z2-7]{8}$`)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// normalize validates in and returns a trimmed copy that shares no slice or
// map with the caller. Errors never contain environment values.
func normalize(in Input) (Input, error) {
	name := strings.TrimSpace(in.Name)
	if !utf8.ValidString(name) {
		return Input{}, invalid("名称不是有效的 UTF-8")
	}
	if n := utf8.RuneCountInString(name); n < 1 || n > MaxNameChars {
		return Input{}, invalid("名称去除首尾空白后须为 1–%d 个字符", MaxNameChars)
	}
	if len(in.Cmd) < 1 || len(in.Cmd) > MaxCmdItems {
		return Input{}, invalid("命令须包含 1–%d 项", MaxCmdItems)
	}
	if in.Cmd[0] == "" {
		return Input{}, invalid("命令第一项不能为空")
	}
	for i, arg := range in.Cmd {
		if len(arg) > MaxItemBytes {
			return Input{}, invalid("命令第 %d 项超过 %d 字节", i+1, MaxItemBytes)
		}
	}
	if len(in.Env) > MaxEnvEntries {
		return Input{}, invalid("环境变量最多 %d 项", MaxEnvEntries)
	}
	if err := session.ValidateEnv(in.Env); err != nil {
		return Input{}, invalid("%v", err)
	}
	for _, name := range slices.Sorted(maps.Keys(in.Env)) {
		if len(in.Env[name]) > MaxItemBytes {
			return Input{}, invalid("环境变量 %s 的值超过 %d 字节", name, MaxItemBytes)
		}
	}
	return Input{Name: name, Cmd: slices.Clone(in.Cmd), Env: cloneEnv(in.Env)}, nil
}

// cloneEnv copies env; nil becomes an empty map so it serializes as {}.
func cloneEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	maps.Copy(out, env)
	return out
}

func (c Config) clone() Config {
	c.Cmd = slices.Clone(c.Cmd)
	c.Env = cloneEnv(c.Env)
	return c
}

func (c Config) summary() Summary {
	names := slices.Sorted(maps.Keys(c.Env))
	if names == nil {
		names = []string{}
	}
	return Summary{
		ID:        c.ID,
		Name:      c.Name,
		Cmd:       slices.Clone(c.Cmd),
		EnvNames:  names,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// newID returns 8 lowercase base32 characters, the session ID format.
func newID(taken func(string) bool) (string, error) {
	for range 32 {
		var raw [5]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", fmt.Errorf("生成启动配置 ID: %w", err)
		}
		id := strings.ToLower(base32.StdEncoding.EncodeToString(raw[:]))
		if !taken(id) {
			return id, nil
		}
	}
	return "", errors.New("生成启动配置 ID: 多次冲突")
}
