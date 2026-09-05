package intake

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/firebase/genkit/go/ai"
)

// Session は 1 つの見積もり相談の状態。
// 会話履歴だけを持ち、これを保存しておけば中断した対話を再開できる（FR-CHT-6）。
type Session struct {
	ID       string        `json:"id"`
	Messages []*ai.Message `json:"messages"`
}

// pending は未回答の質問（interrupt された ask_user の呼び出し）を返す。
// 保存した会話履歴から復元できるので、セッションに別途持たせない。
func (s *Session) pending() []*ai.Part {
	if len(s.Messages) == 0 {
		return nil
	}
	var out []*ai.Part
	for _, p := range s.Messages[len(s.Messages)-1].Content {
		if p.IsInterrupt() {
			out = append(out, p)
		}
	}
	return out
}

// SessionStore は対話の保存先。
type SessionStore interface {
	Load(ctx context.Context, id string) (*Session, error)
	Save(ctx context.Context, s *Session) error
}

// ErrSessionNotFound はセッションが見つからないことを表す。
var ErrSessionNotFound = fmt.Errorf("セッションが見つかりません")

// MemoryStore はプロセス内に持つセッションストア。開発とテスト用。
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string][]byte
}

// NewMemoryStore は MemoryStore を作る。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: map[string][]byte{}}
}

// Load は SessionStore を実装する。
func (m *MemoryStore) Load(_ context.Context, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	return decodeSession(raw)
}

// Save は SessionStore を実装する。
func (m *MemoryStore) Save(_ context.Context, s *Session) error {
	raw, err := encodeSession(s)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = raw
	return nil
}

// FileStore はディレクトリにセッションを保存するストア。
// プロセスを再起動しても対話を再開できる。
type FileStore struct {
	dir string
	mu  sync.Mutex
}

// NewFileStore はディレクトリを作ってストアを返す。
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("セッションの保存先を作れませんでした: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

func (f *FileStore) path(id string) (string, error) {
	// セッション ID がパスを飛び出さないようにする。
	if id == "" || id != filepath.Base(id) || id == "." || id == ".." {
		return "", fmt.Errorf("セッション ID として使えません: %q", id)
	}
	return filepath.Join(f.dir, id+".json"), nil
}

// Load は SessionStore を実装する。
func (f *FileStore) Load(_ context.Context, id string) (*Session, error) {
	path, err := f.path(id)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return decodeSession(raw)
}

// Save は SessionStore を実装する。
func (f *FileStore) Save(_ context.Context, s *Session) error {
	path, err := f.path(s.ID)
	if err != nil {
		return err
	}
	raw, err := encodeSession(s)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return os.WriteFile(path, raw, 0o644)
}

func encodeSession(s *Session) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("セッションを保存できませんでした: %w", err)
	}
	return raw, nil
}

func decodeSession(raw []byte) (*Session, error) {
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("セッションを読み込めませんでした: %w", err)
	}
	return &s, nil
}

var (
	_ SessionStore = (*MemoryStore)(nil)
	_ SessionStore = (*FileStore)(nil)
)
