package catalog_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

func TestBuiltin_LoadsAllServices(t *testing.T) {
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatalf("同梱 catalog の読み込みに失敗しました: %v", err)
	}
	// 基盤 5 サービス（ADR-0008 の MVP スコープのうち M2 で単価を引く分）と、
	// 図の入れ子に使う課金要素なしの 2 種。
	want := []string{"alb", "az", "data_transfer", "ec2", "rds", "s3", "vpc"}
	if got := c.ServiceNames(); !slices.Equal(got, want) {
		t.Errorf("ServiceNames() = %v, want %v", got, want)
	}
}

func TestBuiltin_ServiceSpec(t *testing.T) {
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := c.ServiceSpec("ec2")
	if !ok {
		t.Fatal("ec2 の定義がありません")
	}
	it, ok := spec.Params["instanceType"]
	if !ok {
		t.Fatal("ec2.params.instanceType がありません")
	}
	if it.Type != ir.ParamString || !it.Required {
		t.Errorf("instanceType = %+v, want string/required", it)
	}
	count, ok := spec.Params["count"]
	if !ok {
		t.Fatal("ec2.params.count がありません")
	}
	if count.Type != ir.ParamInt || !count.HasDefault {
		t.Errorf("count = %+v, want int/既定値あり", count)
	}
	if _, ok := c.ServiceSpec("athena"); ok {
		t.Error("定義していないサービスが引けました")
	}
}

// FR-CAT-6: データ転送はインターネット egress と AZ 間の 2 経路のみ。
func TestBuiltin_DataTransferHasTwoPaths(t *testing.T) {
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	svc, ok := c.Get("data_transfer")
	if !ok {
		t.Fatal("data_transfer の定義がありません")
	}
	want := []string{"internet_egress", "inter_az"}
	if got := svc.Params["path"].Enum; !slices.Equal(got, want) {
		t.Errorf("path の経路 = %v, want %v", got, want)
	}
}

func TestBuiltin_IconAndDisplay(t *testing.T) {
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := c.Display("rds"); !ok || d != "Amazon RDS" {
		t.Errorf("Display(rds) = %q, %v", d, ok)
	}
	icon, ok := c.Icon("ec2")
	if !ok || !strings.HasPrefix(icon, "https://") {
		t.Errorf("Icon(ec2) = %q, %v", icon, ok)
	}
	// アイコン未設定のサービスは false を返す（図はアイコン無しで描かれる）。
	if _, ok := c.Icon("az"); ok {
		t.Error("アイコン未設定の az が true を返しました")
	}
}

func TestLoad_Errors(t *testing.T) {
	tests := []struct {
		name    string
		files   fstest.MapFS
		wantSub string
	}{
		{"service がない", fstest.MapFS{
			"x.yaml": &fstest.MapFile{Data: []byte("display: X\n")},
		}, "service は必須です"},
		{"ファイル名がサービス名と違う", fstest.MapFS{
			"x.yaml": &fstest.MapFile{Data: []byte("service: ec2\ndisplay: X\n")},
		}, "ファイル名は"},
		{"未知のキー", fstest.MapFS{
			"ec2.yaml": &fstest.MapFile{Data: []byte("service: ec2\ndisplay: X\nprice: 100\n")},
		}, "パースに失敗しました"},
		{"不正なパラメータ型", fstest.MapFS{
			"ec2.yaml": &fstest.MapFile{Data: []byte("service: ec2\ndisplay: X\nparams:\n  a: {type: number}\n")},
		}, "type が不正です"},
		{"string 以外の enum", fstest.MapFS{
			"ec2.yaml": &fstest.MapFile{Data: []byte("service: ec2\ndisplay: X\nparams:\n  a: {type: int, enum: [\"1\"]}\n")},
		}, "enum を使えるのは string 型だけです"},
		{"driver の id 重複", fstest.MapFS{
			"ec2.yaml": &fstest.MapFile{Data: []byte("service: ec2\ndisplay: X\ndrivers:\n  - id: a\n  - id: a\n")},
		}, "id が重複しています"},
		{"定義が空", fstest.MapFS{}, "1 件も定義がありません"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := catalog.Load(tt.files)
			if err == nil {
				t.Fatal("エラーになりませんでした")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("err = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}

// 同梱 catalog を使って、サンプル IR がそのまま通ることを確認する。
func TestBuiltin_ValidatesExampleIR(t *testing.T) {
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	arch, err := ir.LoadFile("../../examples/ir/web-3tier.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := arch.Validate(c); err != nil {
		t.Fatalf("サンプル IR が catalog で弾かれました: %v", err)
	}
}
