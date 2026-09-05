package ir_test

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

const exampleIR = "../../examples/ir/web-3tier.json"

func TestDecodeEncode_RoundTrip(t *testing.T) {
	a, err := ir.LoadFile(exampleIR)
	if err != nil {
		t.Fatalf("サンプル IR を読めませんでした: %v", err)
	}

	var buf bytes.Buffer
	if err := ir.Encode(&buf, a); err != nil {
		t.Fatalf("Encode に失敗しました: %v", err)
	}
	b, err := ir.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("再読み込みに失敗しました: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("往復で内容が変わりました\n got: %+v\nwant: %+v", b, a)
	}

	// 2 回目の書き出しがバイト一致すること（NFR-1）。
	var buf2 bytes.Buffer
	if err := ir.Encode(&buf2, b); err != nil {
		t.Fatalf("Encode に失敗しました: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), buf2.Bytes()) {
		t.Error("同じ IR から異なるバイト列が出ました")
	}
}

func TestDecode_SchemaVersionDefaulted(t *testing.T) {
	a, err := ir.Decode(strings.NewReader(`{"provider":"aws","region":"ap-northeast-1"}`))
	if err != nil {
		t.Fatalf("Decode に失敗しました: %v", err)
	}
	if a.SchemaVersion != ir.SchemaVersion {
		t.Errorf("schemaVersion = %q, want %q", a.SchemaVersion, ir.SchemaVersion)
	}
}

func TestDecode_UnknownFieldRejected(t *testing.T) {
	// 金額や座標のような IR にあってはならないフィールドは、
	// 黙って捨てずにエラーにする（FR-IR-9 / PRIN-1 / PRIN-2）。
	in := `{"provider":"aws","region":"ap-northeast-1",
	        "resources":[{"id":"a","service":"ec2","monthlyCostUsd":123}]}`
	if _, err := ir.Decode(strings.NewReader(in)); err == nil {
		t.Fatal("未知のフィールドがエラーになりませんでした")
	}
}

func TestSaveFile_LoadFile(t *testing.T) {
	a, err := ir.LoadFile(exampleIR)
	if err != nil {
		t.Fatalf("サンプル IR を読めませんでした: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ir.json")
	if err := ir.SaveFile(path, a); err != nil {
		t.Fatalf("SaveFile に失敗しました: %v", err)
	}
	b, err := ir.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile に失敗しました: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Error("保存して読み直すと内容が変わりました")
	}
}
