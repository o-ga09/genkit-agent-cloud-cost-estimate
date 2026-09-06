// Package webapi は Web サービスとしての配布形態（ADR-0010）を実装する HTTP 層。
//
// ここに置くのは HTTP の関心事（ルーティング・リクエスト/レスポンスの変換・
// 構成の永続化）だけで、見積もりロジックそのものは estimateflow に、
// 対話ロジックは intake に委譲する。
package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

// ErrArchitectureNotFound は指定 ID の構成が見つからないことを表す。
var ErrArchitectureNotFound = errors.New("構成が見つかりません")

// ArchitectureRecord は保存済み構成 1 件。
// IR JSON をサーバー側に保存し、パーマリンクから再レンダリング・再見積もり
// できるようにするための単位（FR-WEB-3）。
type ArchitectureRecord struct {
	ID           string           `json:"id"`
	Architecture *ir.Architecture `json:"architecture"`
	CreatedAt    time.Time        `json:"createdAt"`
}

// ArchitectureStore は構成の保存先。
type ArchitectureStore interface {
	Save(ctx context.Context, rec *ArchitectureRecord) error
	Load(ctx context.Context, id string) (*ArchitectureRecord, error)
}

// MemoryArchitectureStore はプロセス内に持つストア。開発とテスト用。
// プロセスを再起動すると失われる。
type MemoryArchitectureStore struct {
	mu      sync.Mutex
	records map[string]*ArchitectureRecord
}

// NewMemoryArchitectureStore は MemoryArchitectureStore を作る。
func NewMemoryArchitectureStore() *MemoryArchitectureStore {
	return &MemoryArchitectureStore{records: map[string]*ArchitectureRecord{}}
}

// Save は ArchitectureStore を実装する。
func (m *MemoryArchitectureStore) Save(_ context.Context, rec *ArchitectureRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *rec
	m.records[rec.ID] = &cp
	return nil
}

// Load は ArchitectureStore を実装する。
func (m *MemoryArchitectureStore) Load(_ context.Context, id string) (*ArchitectureRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrArchitectureNotFound, id)
	}
	cp := *rec
	return &cp, nil
}

// FileArchitectureStore はディレクトリに構成を保存するストア。
// プロセスを再起動してもパーマリンクを再読み込みできる。
type FileArchitectureStore struct {
	dir string
	mu  sync.Mutex
}

// NewFileArchitectureStore はディレクトリを作ってストアを返す。
func NewFileArchitectureStore(dir string) (*FileArchitectureStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("構成の保存先を作れませんでした: %w", err)
	}
	return &FileArchitectureStore{dir: dir}, nil
}

func (f *FileArchitectureStore) path(id string) (string, error) {
	// ID がパスを飛び出さないようにする（../ 等でのディレクトリトラバーサル対策）。
	if id == "" || id != filepath.Base(id) || id == "." || id == ".." {
		return "", fmt.Errorf("構成 ID として使えません: %q", id)
	}
	return filepath.Join(f.dir, id+".json"), nil
}

// Save は ArchitectureStore を実装する。
func (f *FileArchitectureStore) Save(_ context.Context, rec *ArchitectureRecord) error {
	path, err := f.path(rec.ID)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("構成を保存できませんでした: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return os.WriteFile(path, raw, 0o644)
}

// Load は ArchitectureStore を実装する。
func (f *FileArchitectureStore) Load(_ context.Context, id string) (*ArchitectureRecord, error) {
	path, err := f.path(id)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %q", ErrArchitectureNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	var rec ArchitectureRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("構成を読み込めませんでした: %w", err)
	}
	return &rec, nil
}

var (
	_ ArchitectureStore = (*MemoryArchitectureStore)(nil)
	_ ArchitectureStore = (*FileArchitectureStore)(nil)
)
