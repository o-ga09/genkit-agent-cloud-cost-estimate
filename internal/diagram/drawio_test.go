package diagram_test

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/diagram"
)

// mxGraphModel を読み戻して構造を検証するための最小限の型。
type mxFile struct {
	XMLName xml.Name `xml:"mxfile"`
	Diagram struct {
		Model struct {
			Root struct {
				Cells []struct {
					ID       string `xml:"id,attr"`
					Value    string `xml:"value,attr"`
					Style    string `xml:"style,attr"`
					Vertex   string `xml:"vertex,attr"`
					Edge     string `xml:"edge,attr"`
					Parent   string `xml:"parent,attr"`
					Source   string `xml:"source,attr"`
					Target   string `xml:"target,attr"`
					Geometry struct {
						X      float64 `xml:"x,attr"`
						Y      float64 `xml:"y,attr"`
						Width  float64 `xml:"width,attr"`
						Height float64 `xml:"height,attr"`
					} `xml:"mxGeometry"`
				} `xml:"mxCell"`
			} `xml:"root"`
		} `xml:"mxGraphModel"`
	} `xml:"diagram"`
}

func parseDrawio(t *testing.T, b []byte) mxFile {
	t.Helper()
	var f mxFile
	if err := xml.Unmarshal(b, &f); err != nil {
		t.Fatalf("drawio XML を読めませんでした: %v", err)
	}
	return f
}

func renderDrawio(t *testing.T) []byte {
	t.Helper()
	arch, cat := loadExample(t)
	b, err := diagram.RenderDrawio(context.Background(), arch, cat)
	if err != nil {
		t.Fatalf("drawio XML の生成に失敗しました: %v", err)
	}
	return b
}

// FR-DIA-6: 同一 IR から drawio XML を出力できる。
func TestRenderDrawio_Golden(t *testing.T) {
	compareGolden(t, "web-3tier.drawio", renderDrawio(t))
}

func TestRenderDrawio_Structure(t *testing.T) {
	arch, _ := loadExample(t)
	f := parseDrawio(t, renderDrawio(t))
	cells := f.Diagram.Model.Root.Cells

	byID := map[string]int{}
	for i, c := range cells {
		if _, dup := byID[c.ID]; dup {
			t.Errorf("セル id が重複しています: %q", c.ID)
		}
		byID[c.ID] = i
	}
	// drawio の約束事: id=0 のルートと id=1 の既定レイヤーが要る。
	for _, id := range []string{"0", "1"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("id=%q のセルがありません", id)
		}
	}

	vertices, edges := 0, 0
	for _, c := range cells {
		switch {
		case c.Vertex == "1":
			vertices++
			if c.Geometry.Width <= 0 || c.Geometry.Height <= 0 {
				t.Errorf("%s に大きさがありません: %+v", c.ID, c.Geometry)
			}
			if _, ok := byID[c.Parent]; !ok {
				t.Errorf("%s の parent %q が存在しません", c.ID, c.Parent)
			}
		case c.Edge == "1":
			edges++
			if _, ok := byID[c.Source]; !ok {
				t.Errorf("エッジ %s の source %q が存在しません", c.ID, c.Source)
			}
			if _, ok := byID[c.Target]; !ok {
				t.Errorf("エッジ %s の target %q が存在しません", c.ID, c.Target)
			}
		}
	}
	if vertices != len(arch.Resources) {
		t.Errorf("図形が %d 個、want %d（IR のリソース数）", vertices, len(arch.Resources))
	}
	if edges != len(arch.Edges) {
		t.Errorf("エッジが %d 本、want %d（IR のエッジ数）", edges, len(arch.Edges))
	}
}

// FR-DIA-3 / FR-DIA-6: 入れ子は drawio 側でも入れ子として渡る。
// 親を動かせば子もついてくる状態で受け渡せる。
func TestRenderDrawio_Nesting(t *testing.T) {
	f := parseDrawio(t, renderDrawio(t))
	want := map[string]string{
		"vpc-main":                "1",
		"vpc-main.web-alb":        "vpc-main",
		"vpc-main.az-a":           "vpc-main",
		"vpc-main.az-a.app-ec2-a": "vpc-main.az-a",
		"vpc-main.az-c.app-ec2-c": "vpc-main.az-c",
		"assets-s3":               "1",
	}
	got := map[string]string{}
	for _, c := range f.Diagram.Model.Root.Cells {
		got[c.ID] = c.Parent
	}
	for id, parent := range want {
		if got[id] != parent {
			t.Errorf("%s の parent = %q, want %q", id, got[id], parent)
		}
	}
}

// 座標はすべて正の値にそろえる（drawio で開いたときに画面外に出ないように）。
func TestRenderDrawio_PositiveCoordinates(t *testing.T) {
	f := parseDrawio(t, renderDrawio(t))
	for _, c := range f.Diagram.Model.Root.Cells {
		if c.Vertex != "1" || c.Parent != "1" {
			continue
		}
		if c.Geometry.X < 0 || c.Geometry.Y < 0 {
			t.Errorf("%s の座標が負です: (%v, %v)", c.ID, c.Geometry.X, c.Geometry.Y)
		}
	}
}

// テーマ色のコード（"B4" など）ではなく、drawio が解釈できる色が入る。
func TestRenderDrawio_ResolvesThemeColors(t *testing.T) {
	f := parseDrawio(t, renderDrawio(t))
	found := false
	for _, c := range f.Diagram.Model.Root.Cells {
		if !strings.Contains(c.Style, "fillColor=") {
			continue
		}
		found = true
		for _, part := range strings.Split(c.Style, ";") {
			key, value, ok := strings.Cut(part, "=")
			if !ok || (key != "fillColor" && key != "strokeColor" && key != "fontColor") {
				continue
			}
			if !strings.HasPrefix(value, "#") {
				t.Errorf("%s の %s が色コードのままです: %q", c.ID, key, value)
			}
		}
	}
	if !found {
		t.Error("fillColor を持つセルがありません")
	}
}

// 同じ IR からの出力は決定的（FR-DIA-7 / NFR-1）。
func TestRenderDrawio_Deterministic(t *testing.T) {
	first, second := renderDrawio(t), renderDrawio(t)
	if string(first) != string(second) {
		t.Error("同じ IR から異なる drawio XML が生成されました")
	}
}
