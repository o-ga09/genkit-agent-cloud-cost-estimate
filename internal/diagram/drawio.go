package diagram

import (
	"context"
	"encoding/xml"
	"fmt"
	"math"
	"sort"
	"strings"

	"oss.terrastruct.com/d2/d2target"
	"oss.terrastruct.com/d2/d2themes"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	"oss.terrastruct.com/d2/lib/geo"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

// drawioPad は図の左上に空ける余白。
const drawioPad = 40

// RenderDrawio は IR から drawio (mxGraphModel) の XML を生成する（FR-DIA-6）。
//
// 座標は D2 のレイアウトエンジンが計算した結果をそのまま使う。
// 受け取った側は「揃った状態」から手直しを始められる。
func RenderDrawio(ctx context.Context, arch *ir.Architecture, style Style) ([]byte, error) {
	diagram, err := compile(ctx, BuildD2(arch, style))
	if err != nil {
		return nil, err
	}
	return drawioXML(diagram)
}

func drawioXML(d *d2target.Diagram) ([]byte, error) {
	tl, br := d.BoundingBox()
	offsetX := float64(drawioPad - tl.X)
	offsetY := float64(drawioPad - tl.Y)

	shapes := make(map[string]d2target.Shape, len(d.Shapes))
	for _, s := range d.Shapes {
		shapes[s.ID] = s
	}

	cells := []mxCell{
		{ID: "0"},
		{ID: "1", Parent: "0"},
	}

	// 入れ子の外側から順に並べる。drawio は先に現れたセルを背面に描く。
	ordered := make([]d2target.Shape, len(d.Shapes))
	copy(ordered, d.Shapes)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Level < ordered[j].Level })

	for _, s := range ordered {
		parent := "1"
		x, y := float64(s.Pos.X)+offsetX, float64(s.Pos.Y)+offsetY
		if pid, ok := parentID(s.ID, shapes); ok {
			// 親を持つ図形の座標は親からの相対位置になる。
			p := shapes[pid]
			parent = pid
			x = float64(s.Pos.X - p.Pos.X)
			y = float64(s.Pos.Y - p.Pos.Y)
		}
		cells = append(cells, mxCell{
			ID:       s.ID,
			Value:    s.Label,
			Style:    shapeStyle(s, hasChildren(s.ID, d.Shapes)),
			Vertex:   "1",
			Parent:   parent,
			Geometry: &mxGeometry{X: round(x), Y: round(y), Width: float64(s.Width), Height: float64(s.Height), As: "geometry"},
		})
	}

	for i, c := range d.Connections {
		geo := &mxGeometry{Relative: "1", As: "geometry"}
		if pts := waypoints(c.Route, offsetX, offsetY); len(pts) > 0 {
			geo.Points = &mxPointArray{As: "points", Points: pts}
		}
		id := c.ID
		if id == "" {
			id = fmt.Sprintf("edge-%d", i+1)
		}
		cells = append(cells, mxCell{
			ID:    id,
			Value: c.Label,
			Style: fmt.Sprintf(
				"edgeStyle=orthogonalEdgeStyle;rounded=0;html=1;endArrow=block;endFill=1;strokeColor=%s;",
				themeColor(c.Stroke)),
			Edge:     "1",
			Parent:   "1",
			Source:   c.Src,
			Target:   c.Dst,
			Geometry: geo,
		})
	}

	file := mxFile{
		Host:  "genkit-agent-cloud-cost-estimate",
		Type:  "device",
		Agent: "genkit-agent-cloud-cost-estimate",
		Diagram: mxDiagram{
			ID:   "architecture",
			Name: "Architecture",
			Model: mxGraphModel{
				Dx: br.X - tl.X + drawioPad*2, Dy: br.Y - tl.Y + drawioPad*2,
				Grid: "0", GridSize: "10", Guides: "1", Tooltips: "1", Connect: "1",
				Arrows: "1", Fold: "1", Page: "1", PageScale: "1",
				PageWidth: 850, PageHeight: 1100, Math: "0", Shadow: "0",
				Root: mxRoot{Cells: cells},
			},
		},
	}

	out, err := xml.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("drawio XML の生成に失敗しました: %w", err)
	}
	return append([]byte(xml.Header), append(out, '\n')...), nil
}

// parentID は入れ子の親を返す。D2 のキーは "vpc.az.web" のようにドットで連結される。
func parentID(id string, shapes map[string]d2target.Shape) (string, bool) {
	i := strings.LastIndex(id, ".")
	if i < 0 {
		return "", false
	}
	parent := id[:i]
	if _, ok := shapes[parent]; !ok {
		return "", false
	}
	return parent, true
}

func hasChildren(id string, shapes []d2target.Shape) bool {
	prefix := id + "."
	for _, s := range shapes {
		if strings.HasPrefix(s.ID, prefix) {
			return true
		}
	}
	return false
}

func shapeStyle(s d2target.Shape, container bool) string {
	var b strings.Builder
	b.WriteString("rounded=0;whiteSpace=wrap;html=1;")
	if container {
		// 枠の中に子を置くので、ラベルは上に寄せて中身と重ならないようにする。
		b.WriteString("verticalAlign=top;container=1;collapsible=0;")
	} else if s.Icon != nil {
		// アイコン付きの箱。drawio 側でアイコンを差し替えられるように image= で持たせる。
		b.WriteString("shape=label;imageAlign=left;imageVerticalAlign=middle;spacingLeft=40;")
		fmt.Fprintf(&b, "imageWidth=24;imageHeight=24;image=%s;", s.Icon.String())
	}
	if c := themeColor(s.Fill); c != "" {
		fmt.Fprintf(&b, "fillColor=%s;", c)
	}
	if c := themeColor(s.Stroke); c != "" {
		fmt.Fprintf(&b, "strokeColor=%s;", c)
	}
	if c := themeColor(s.GetFontColor()); c != "" {
		fmt.Fprintf(&b, "fontColor=%s;", c)
	}
	return b.String()
}

// themeColor は D2 のテーマ色コード（"B4" など）を drawio が解釈できる色に変える。
func themeColor(code string) string {
	if code == "" {
		return ""
	}
	return d2themes.ResolveThemeColor(d2themescatalog.NeutralDefault, code)
}

// waypoints は経路の中間点を返す。始点と終点は drawio が図形の縁に合わせて決める。
func waypoints(route []*geo.Point, offsetX, offsetY float64) []mxPoint {
	if len(route) <= 2 {
		return nil
	}
	pts := make([]mxPoint, 0, len(route)-2)
	for _, p := range route[1 : len(route)-1] {
		pts = append(pts, mxPoint{X: round(p.X + offsetX), Y: round(p.Y + offsetY), As: ""})
	}
	return pts
}

func round(v float64) float64 { return math.Round(v*100) / 100 }

// ---- mxGraphModel の XML 構造 ----

type mxFile struct {
	XMLName xml.Name  `xml:"mxfile"`
	Host    string    `xml:"host,attr"`
	Type    string    `xml:"type,attr"`
	Agent   string    `xml:"agent,attr"`
	Diagram mxDiagram `xml:"diagram"`
}

type mxDiagram struct {
	ID    string       `xml:"id,attr"`
	Name  string       `xml:"name,attr"`
	Model mxGraphModel `xml:"mxGraphModel"`
}

type mxGraphModel struct {
	Dx         int    `xml:"dx,attr"`
	Dy         int    `xml:"dy,attr"`
	Grid       string `xml:"grid,attr"`
	GridSize   string `xml:"gridSize,attr"`
	Guides     string `xml:"guides,attr"`
	Tooltips   string `xml:"tooltips,attr"`
	Connect    string `xml:"connect,attr"`
	Arrows     string `xml:"arrows,attr"`
	Fold       string `xml:"fold,attr"`
	Page       string `xml:"page,attr"`
	PageScale  string `xml:"pageScale,attr"`
	PageWidth  int    `xml:"pageWidth,attr"`
	PageHeight int    `xml:"pageHeight,attr"`
	Math       string `xml:"math,attr"`
	Shadow     string `xml:"shadow,attr"`
	Root       mxRoot `xml:"root"`
}

type mxRoot struct {
	Cells []mxCell `xml:"mxCell"`
}

type mxCell struct {
	ID       string      `xml:"id,attr"`
	Value    string      `xml:"value,attr,omitempty"`
	Style    string      `xml:"style,attr,omitempty"`
	Vertex   string      `xml:"vertex,attr,omitempty"`
	Edge     string      `xml:"edge,attr,omitempty"`
	Parent   string      `xml:"parent,attr,omitempty"`
	Source   string      `xml:"source,attr,omitempty"`
	Target   string      `xml:"target,attr,omitempty"`
	Geometry *mxGeometry `xml:"mxGeometry,omitempty"`
}

type mxGeometry struct {
	X        float64       `xml:"x,attr,omitempty"`
	Y        float64       `xml:"y,attr,omitempty"`
	Width    float64       `xml:"width,attr,omitempty"`
	Height   float64       `xml:"height,attr,omitempty"`
	Relative string        `xml:"relative,attr,omitempty"`
	As       string        `xml:"as,attr"`
	Points   *mxPointArray `xml:"Array,omitempty"`
}

type mxPointArray struct {
	As     string    `xml:"as,attr"`
	Points []mxPoint `xml:"mxPoint"`
}

type mxPoint struct {
	X  float64 `xml:"x,attr"`
	Y  float64 `xml:"y,attr"`
	As string  `xml:"as,attr,omitempty"`
}
