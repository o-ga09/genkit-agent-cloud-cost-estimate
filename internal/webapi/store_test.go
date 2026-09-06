package webapi_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/webapi"
)

func sampleArchitecture() *ir.Architecture {
	return &ir.Architecture{
		Provider: ir.ProviderAWS,
		Region:   "ap-northeast-1",
		Assumptions: ir.Assumptions{
			HoursPerDay: 24, DaysPerMonth: 30, RequestsPerMonth: 1_000_000, FxRate: 150,
		},
		Resources: []ir.Resource{
			{ID: "web", Service: "ec2", Params: map[string]any{
				"instanceType": "t3.medium", "count": float64(2),
			}},
		},
	}
}

func testArchitectureStore(t *testing.T, store webapi.ArchitectureStore) {
	t.Helper()
	ctx := context.Background()

	t.Run("存在しない ID を読むとエラーになる", func(t *testing.T) {
		if _, err := store.Load(ctx, "unknown"); !errors.Is(err, webapi.ErrArchitectureNotFound) {
			t.Fatalf("want ErrArchitectureNotFound, got %v", err)
		}
	})

	t.Run("保存した構成を読み込める", func(t *testing.T) {
		rec := &webapi.ArchitectureRecord{ID: "abc123", Architecture: sampleArchitecture()}
		if err := store.Save(ctx, rec); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		got, err := store.Load(ctx, "abc123")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Architecture.Region != "ap-northeast-1" {
			t.Errorf("Region = %q, want ap-northeast-1", got.Architecture.Region)
		}
		if len(got.Architecture.Resources) != 1 || got.Architecture.Resources[0].ID != "web" {
			t.Errorf("Resources = %+v", got.Architecture.Resources)
		}
	})

	t.Run("上書き保存できる", func(t *testing.T) {
		rec := &webapi.ArchitectureRecord{ID: "abc123", Architecture: sampleArchitecture()}
		rec.Architecture.Region = "us-east-1"
		if err := store.Save(ctx, rec); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		got, err := store.Load(ctx, "abc123")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Architecture.Region != "us-east-1" {
			t.Errorf("Region = %q, want us-east-1", got.Architecture.Region)
		}
	})
}

func TestMemoryArchitectureStore(t *testing.T) {
	testArchitectureStore(t, webapi.NewMemoryArchitectureStore())
}

func TestFileArchitectureStore(t *testing.T) {
	dir := t.TempDir()
	store, err := webapi.NewFileArchitectureStore(dir)
	if err != nil {
		t.Fatalf("NewFileArchitectureStore() error = %v", err)
	}
	testArchitectureStore(t, store)
}

func TestFileArchitectureStore_PersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store1, err := webapi.NewFileArchitectureStore(dir)
	if err != nil {
		t.Fatalf("NewFileArchitectureStore() error = %v", err)
	}
	if err := store1.Save(ctx, &webapi.ArchitectureRecord{ID: "keep-1", Architecture: sampleArchitecture()}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	store2, err := webapi.NewFileArchitectureStore(dir)
	if err != nil {
		t.Fatalf("NewFileArchitectureStore() error = %v", err)
	}
	got, err := store2.Load(ctx, "keep-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ID != "keep-1" {
		t.Errorf("ID = %q, want keep-1", got.ID)
	}
}

func TestFileArchitectureStore_RejectsPathTraversalID(t *testing.T) {
	dir := t.TempDir()
	store, err := webapi.NewFileArchitectureStore(dir)
	if err != nil {
		t.Fatalf("NewFileArchitectureStore() error = %v", err)
	}
	ctx := context.Background()
	if err := store.Save(ctx, &webapi.ArchitectureRecord{ID: "../escape", Architecture: sampleArchitecture()}); err == nil {
		t.Fatal("Save() with path-traversal ID should error")
	}
	if _, err := store.Load(ctx, "../"+filepath.Base(dir)); err == nil {
		t.Fatal("Load() with path-traversal ID should error")
	}
}
