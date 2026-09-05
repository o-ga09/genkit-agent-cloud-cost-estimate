package diagram_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/diagram"
)

// pngMagic は PNG のシグネチャ。
var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// testFont は PNG のテストに使うフォントを返す。
// 実行環境にフォントが無ければテストをスキップする（ADR-0014 のとおり、
// PNG 変換は実行環境のフォントに依存する）。
func testFont(t *testing.T) []byte {
	t.Helper()
	for _, p := range []string{
		"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/Library/Fonts/Arial Unicode.ttf",
	} {
		if data, err := os.ReadFile(p); err == nil {
			return data
		}
	}
	t.Skip("PNG 変換に使えるフォントが見つからないためスキップします")
	return nil
}

// stubIcons はネットワークを使わずにアイコンを返す IconLoader。
func stubIcons(calls *[]string) diagram.IconLoader {
	icon := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16">` +
		`<rect width="16" height="16" fill="#ff9900"/></svg>`)
	return func(_ context.Context, url string) ([]byte, string, error) {
		*calls = append(*calls, url)
		return icon, "image/svg+xml", nil
	}
}

// FR-DIA-5: 同一 IR から PNG が生成される。
func TestRenderPNG(t *testing.T) {
	font := testFont(t)
	arch, cat := loadExample(t)
	var calls []string

	png, err := diagram.RenderPNG(context.Background(), arch, cat, diagram.PNGOptions{
		FontData:   font,
		IconLoader: stubIcons(&calls),
	})
	if err != nil {
		t.Fatalf("PNG の生成に失敗しました: %v", err)
	}
	if !bytes.HasPrefix(png, pngMagic) {
		t.Fatalf("PNG のシグネチャがありません: %x", png[:min(8, len(png))])
	}
	if len(png) < 5000 {
		t.Errorf("PNG が %d バイトしかありません（描画されていない可能性）", len(png))
	}
	if len(calls) == 0 {
		t.Error("アイコンが 1 つも取得されませんでした")
	}
}

// FR-DIA-7 / NFR-1: 同一 IR からの PNG は決定的。
func TestRenderPNG_Deterministic(t *testing.T) {
	font := testFont(t)
	arch, cat := loadExample(t)
	var calls []string
	opts := diagram.PNGOptions{FontData: font, IconLoader: stubIcons(&calls)}

	first, err := diagram.RenderPNG(context.Background(), arch, cat, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := diagram.RenderPNG(context.Background(), arch, cat, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("同じ IR から異なる PNG が生成されました")
	}
}

// アイコンを取得できなくても、図全体は生成する。
func TestRenderPNG_IconFailureIsTolerated(t *testing.T) {
	font := testFont(t)
	arch, cat := loadExample(t)
	png, err := diagram.RenderPNG(context.Background(), arch, cat, diagram.PNGOptions{
		FontData: font,
		IconLoader: func(context.Context, string) ([]byte, string, error) {
			return nil, "", errors.New("取得できません")
		},
	})
	if err != nil {
		t.Fatalf("アイコンの取得失敗で PNG 全体が失敗しました: %v", err)
	}
	if !bytes.HasPrefix(png, pngMagic) {
		t.Error("PNG のシグネチャがありません")
	}
}

// フォントが見つからない環境では、原因の分かるエラーを返す（ADR-0014 の Confirmation）。
func TestPNGFromSVG_NoFont(t *testing.T) {
	_, err := diagram.PNGFromSVG(context.Background(), []byte("<svg/>"), diagram.PNGOptions{
		FontPaths: []string{"/nonexistent/font.ttf"},
	})
	if err == nil {
		t.Fatal("フォントが無くてもエラーになりませんでした")
	}
	if !strings.Contains(err.Error(), "フォント") {
		t.Errorf("err = %v, want フォントについての説明", err)
	}
}
