package diagram_test

import (
	"context"
	"encoding/xml"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/diagram"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

var update = flag.Bool("update", false, "ゴールデンファイルを更新する")

const exampleIR = "../../examples/ir/web-3tier.json"

func loadExample(t *testing.T) (*ir.Architecture, *catalog.Catalog) {
	t.Helper()
	arch, err := ir.LoadFile(exampleIR)
	if err != nil {
		t.Fatalf("サンプル IR を読めませんでした: %v", err)
	}
	cat, err := catalog.Builtin()
	if err != nil {
		t.Fatalf("catalog を読めませんでした: %v", err)
	}
	if err := arch.Validate(cat); err != nil {
		t.Fatalf("サンプル IR が不正です: %v", err)
	}
	return arch, cat
}

// FR-DIA-3: parent が D2 のコンテナのネストに写像される。
func TestBuildD2_Golden(t *testing.T) {
	arch, cat := loadExample(t)
	got := diagram.BuildD2(arch, cat)
	compareGolden(t, "web-3tier.d2", []byte(got))
}

func TestBuildD2_Structure(t *testing.T) {
	arch := &ir.Architecture{Resources: []ir.Resource{
		{ID: "vpc", Service: "vpc", Label: "VPC"},
		{ID: "az", Service: "az", Label: "AZ-a", Parent: "vpc"},
		{ID: "web", Service: "ec2", Parent: "az"},
		{ID: "bucket", Service: "s3", Label: `bucket "assets"`},
	}, Edges: []ir.Edge{
		{From: "web", To: "bucket", Label: "PUT"},
		{From: "bucket", To: "web"},
	}}
	got := diagram.BuildD2(arch, stubStyle{})

	want := strings.Join([]string{
		`direction: right`,
		``,
		`vpc: "VPC" {`,
		`  az: "AZ-a" {`,
		`    web: "Amazon EC2" {`,
		`      icon: https://example.test/ec2.svg`,
		`    }`,
		`  }`,
		`}`,
		`bucket: "bucket \"assets\"" {`,
		`  icon: https://example.test/s3.svg`,
		`}`,
		`vpc.az.web -> bucket: "PUT"`,
		`bucket -> vpc.az.web`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("生成された D2 ソースが期待と違います\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// FR-DIA-2 / PRIN-2: 座標は D2 ソースに現れない。配置はレイアウトエンジンが決める。
func TestBuildD2_HasNoCoordinates(t *testing.T) {
	arch, cat := loadExample(t)
	src := diagram.BuildD2(arch, cat)
	for _, banned := range []string{"top:", "left:", "near:", "position", "width:", "height:"} {
		if strings.Contains(src, banned) {
			t.Errorf("D2 ソースに座標指定 %q が含まれています", banned)
		}
	}
}

func TestRenderSVG_Golden(t *testing.T) {
	arch, cat := loadExample(t)
	svg, err := diagram.RenderSVG(context.Background(), arch, cat)
	if err != nil {
		t.Fatalf("レンダリングに失敗しました: %v", err)
	}
	if err := xml.Unmarshal(svg, new(any)); err != nil {
		t.Fatalf("出力が XML として不正です: %v", err)
	}
	for _, want := range []string{"App EC2 t3.medium x2", "ap-northeast-1a", "Amazon-EC2.svg", "SQL"} {
		if !strings.Contains(string(svg), want) {
			t.Errorf("SVG に %q が含まれていません", want)
		}
	}
	compareGolden(t, "web-3tier.svg", svg)
}

// FR-DIA-7 / NFR-1: 同一 IR からの再レンダリングは決定的である。
func TestRenderSVG_Deterministic(t *testing.T) {
	arch, cat := loadExample(t)
	ctx := context.Background()
	first, err := diagram.RenderSVG(ctx, arch, cat)
	if err != nil {
		t.Fatal(err)
	}
	second, err := diagram.RenderSVG(ctx, arch, cat)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("同じ IR から異なる SVG が生成されました")
	}
}

func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("ゴールデンファイルを更新できませんでした: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ゴールデンファイルを読めませんでした（-update で生成できます）: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("%s がゴールデンと一致しません。差分を確認し、意図した変更なら -update で更新してください", name)
	}
}

// stubStyle は catalog に依存せず writer の挙動だけを見るためのスタイル。
type stubStyle struct{}

func (stubStyle) Icon(service string) (string, bool) {
	switch service {
	case "ec2":
		return "https://example.test/ec2.svg", true
	case "s3":
		return "https://example.test/s3.svg", true
	}
	return "", false
}

func (stubStyle) Display(service string) (string, bool) {
	switch service {
	case "ec2":
		return "Amazon EC2", true
	case "s3":
		return "Amazon S3", true
	}
	return "", false
}
