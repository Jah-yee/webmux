package launch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/rainchan/webmux/internal/pathutil"
	"github.com/rainchan/webmux/internal/statefile"
)

const (
	fileName = "launch-configs.json"
	lockName = "launch-configs.lock"
)

type storedFile struct {
	V       int      `json:"v"`
	Configs []Config `json:"configs"`
}

// Service owns <state_dir>/launch-configs.json. It holds an exclusive flock
// on launch-configs.lock until Close, so a second instance on the same state
// directory reports unavailable instead of overwriting the file. A nil
// *Service is unavailable.
type Service struct {
	mu      sync.Mutex
	dir     string
	path    string
	lock    *os.File
	configs []Config
	// broken is the unavailability reason; nil when the store works.
	broken error
}

// Open loads the store. It never fails: a lock conflict, an unreadable
// directory or a corrupt file leaves the service unavailable and the file
// untouched until the server restarts. A missing file is an empty list and is
// not created until the first write.
func Open(stateDir string) *Service {
	s := &Service{}
	dir, err := pathutil.Expand(stateDir)
	if err == nil {
		dir, err = filepath.Abs(dir)
	}
	if err != nil {
		s.dir, s.path = stateDir, filepath.Join(stateDir, fileName)
		s.broken = fmt.Errorf("状态目录无效: %w", err)
		return s
	}
	s.dir, s.path = dir, filepath.Join(dir, fileName)
	if err := s.load(); err != nil {
		s.broken = err
	}
	return s
}

func (s *Service) load() error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("创建状态目录: %w", err)
	}
	if err := os.Chmod(s.dir, 0700); err != nil {
		return fmt.Errorf("设置状态目录权限: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(s.dir, lockName), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("打开锁文件: %w", err)
	}
	if err := lock.Chmod(0600); err != nil {
		_ = lock.Close()
		return fmt.Errorf("设置锁文件权限: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return fmt.Errorf("状态目录已被另一实例使用: %w", err)
	}
	s.lock = lock
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取文件: %w", err)
	}
	configs, err := parse(data)
	if err != nil {
		return err
	}
	s.configs = configs
	return nil
}

// parse accepts only what saveLocked writes. Errors name fields, never
// environment values.
func parse(data []byte) ([]Config, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var stored storedFile
	if err := d.Decode(&stored); err != nil {
		return nil, fmt.Errorf("文件无法解析: %s", describeDecodeError(err))
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("文件无法解析: 包含多余内容")
	}
	if stored.V != 1 || stored.Configs == nil {
		return nil, errors.New("文件损坏或版本不受支持")
	}
	if len(stored.Configs) > MaxConfigs {
		return nil, fmt.Errorf("文件包含超过 %d 条配置", MaxConfigs)
	}
	ids, names := map[string]bool{}, map[string]bool{}
	configs := make([]Config, 0, len(stored.Configs))
	for i, c := range stored.Configs {
		in, err := normalize(Input{Name: c.Name, Cmd: c.Cmd, Env: c.Env})
		switch {
		case err != nil:
			return nil, fmt.Errorf("第 %d 条配置: %w", i+1, err)
		case in.Name != c.Name:
			return nil, fmt.Errorf("第 %d 条配置: 名称包含首尾空白", i+1)
		case !idPattern.MatchString(c.ID) || ids[c.ID]:
			return nil, fmt.Errorf("第 %d 条配置: ID 无效或重复", i+1)
		case names[in.Name]:
			return nil, fmt.Errorf("第 %d 条配置: 名称重复", i+1)
		case c.CreatedAt.IsZero() || c.UpdatedAt.IsZero():
			return nil, fmt.Errorf("第 %d 条配置: 缺少时间", i+1)
		}
		ids[c.ID], names[in.Name] = true, true
		configs = append(configs, Config{ID: c.ID, Name: in.Name, Cmd: in.Cmd, Env: in.Env, CreatedAt: c.CreatedAt.UTC(), UpdatedAt: c.UpdatedAt.UTC()})
	}
	return configs, nil
}

// describeDecodeError drops literal values, which may be secrets, from JSON
// errors and keeps the location.
func describeDecodeError(err error) string {
	var syntax *json.SyntaxError
	var typed *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		return fmt.Sprintf("JSON 语法错误（偏移 %d）", syntax.Offset)
	case errors.As(err, &typed):
		return fmt.Sprintf("字段 %s 类型错误（偏移 %d）", typed.Field, typed.Offset)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "内容为空或不完整"
	default:
		return err.Error()
	}
}

// Close releases the file lock; the service is unavailable afterwards.
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock != nil {
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		_ = s.lock.Close()
		s.lock = nil
	}
	if s.broken == nil {
		s.broken = errors.New("服务已关闭")
	}
	s.configs = nil
}

// Path is the absolute path of launch-configs.json.
func (s *Service) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Unavailable returns an ErrUnavailable error naming the reason and the file
// path, or nil when the store works.
func (s *Service) Unavailable() error {
	if s == nil {
		return fmt.Errorf("%w: 服务未启用", ErrUnavailable)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unavailableLocked()
}

func (s *Service) unavailableLocked() error {
	if s.broken == nil {
		return nil
	}
	return fmt.Errorf("%w: %v（%s）", ErrUnavailable, s.broken, s.path)
}

// lockAvailable locks s.mu when the store works; callers must unlock it.
func (s *Service) lockAvailable() error {
	if s == nil {
		return fmt.Errorf("%w: 服务未启用", ErrUnavailable)
	}
	s.mu.Lock()
	if err := s.unavailableLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	return nil
}

// List returns summaries in creation order.
func (s *Service) List() ([]Summary, error) {
	if err := s.lockAvailable(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	out := make([]Summary, 0, len(s.configs))
	for _, c := range s.configs {
		out = append(out, c.summary())
	}
	return out, nil
}

// Get returns a copy of one full config.
func (s *Service) Get(id string) (Config, error) {
	if err := s.lockAvailable(); err != nil {
		return Config{}, err
	}
	defer s.mu.Unlock()
	if i := s.indexLocked(id); i >= 0 {
		return s.configs[i].clone(), nil
	}
	return Config{}, ErrNotFound
}

// Create validates in, saves a new config and returns a copy of it.
func (s *Service) Create(in Input) (Config, error) {
	in, err := normalize(in)
	if err != nil {
		return Config{}, err
	}
	if err := s.lockAvailable(); err != nil {
		return Config{}, err
	}
	defer s.mu.Unlock()
	if len(s.configs) >= MaxConfigs {
		return Config{}, invalid("最多保存 %d 条启动配置", MaxConfigs)
	}
	if s.nameTakenLocked(in.Name, "") {
		return Config{}, ErrDuplicate
	}
	id, err := newID(func(id string) bool { return s.indexLocked(id) >= 0 })
	if err != nil {
		return Config{}, err
	}
	now := time.Now().UTC()
	c := Config{ID: id, Name: in.Name, Cmd: in.Cmd, Env: in.Env, CreatedAt: now, UpdatedAt: now}
	next := append(slices.Clip(s.configs), c)
	if err := s.saveLocked(next); err != nil {
		return Config{}, err
	}
	s.configs = next
	return c.clone(), nil
}

// Update replaces name, cmd and env of id; id and createdAt are kept.
func (s *Service) Update(id string, in Input) (Config, error) {
	in, err := normalize(in)
	if err != nil {
		return Config{}, err
	}
	if err := s.lockAvailable(); err != nil {
		return Config{}, err
	}
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return Config{}, ErrNotFound
	}
	if s.nameTakenLocked(in.Name, id) {
		return Config{}, ErrDuplicate
	}
	c := s.configs[i]
	c.Name, c.Cmd, c.Env, c.UpdatedAt = in.Name, in.Cmd, in.Env, time.Now().UTC()
	next := slices.Clone(s.configs)
	next[i] = c
	if err := s.saveLocked(next); err != nil {
		return Config{}, err
	}
	s.configs = next
	return c.clone(), nil
}

// Delete removes id.
func (s *Service) Delete(id string) error {
	if err := s.lockAvailable(); err != nil {
		return err
	}
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return ErrNotFound
	}
	next := slices.Delete(slices.Clone(s.configs), i, i+1)
	if err := s.saveLocked(next); err != nil {
		return err
	}
	s.configs = next
	return nil
}

func (s *Service) indexLocked(id string) int {
	return slices.IndexFunc(s.configs, func(c Config) bool { return c.ID == id })
}

func (s *Service) nameTakenLocked(name, except string) bool {
	return slices.ContainsFunc(s.configs, func(c Config) bool { return c.Name == name && c.ID != except })
}

// saveLocked atomically replaces the file with configs; the caller swaps the
// in-memory list only after it succeeds.
func (s *Service) saveLocked(configs []Config) error {
	if configs == nil {
		configs = []Config{}
	}
	err := statefile.WriteJSON(s.path, storedFile{V: 1, Configs: configs}, statefile.Options{TempPattern: ".launch-configs-*"})
	if err != nil {
		return fmt.Errorf("保存启动配置: %w", err)
	}
	return nil
}
