package diagram

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/kanrichan/resvg-go"
	"golang.org/x/image/font/sfnt"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

// IconLoader は図に貼るアイコンを取得する。PNG では外部 URL を解決できないため、
// 取得したデータを SVG に埋め込んでからラスタライズする。
// nil を渡すとアイコンなしで描画する。
type IconLoader func(ctx context.Context, url string) (data []byte, contentType string, err error)

// PNGOptions は PNG 変換の設定。
type PNGOptions struct {
	// FontData は描画に使うフォント（TTF / OTF / TTC）。
	// 空なら FontPaths（既定は OS ごとの候補）から最初に見つかったものを使う。
	FontData []byte
	// FontPaths はフォントの探索先。空なら既定の候補を使う。
	FontPaths []string
	// IconLoader はアイコンの取得方法。nil ならアイコンを描画しない。
	IconLoader IconLoader
}

// defaultFontPaths は日本語を含むラベルを描ける可能性が高いフォントの候補。
// コンテナに載せる場合は Noto Sans CJK を入れておく想定。
var defaultFontPaths = []string{
	// Linux
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/opentype/noto/NotoSansJP-Regular.otf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	// macOS
	"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
	"/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc",
	"/Library/Fonts/Arial Unicode.ttf",
}

// RenderPNG は IR から構成図の PNG を生成する（FR-DIA-5）。
// SVG と同じ D2 のレイアウト結果をラスタライズするので、両者の内容は一致する。
func RenderPNG(ctx context.Context, arch *ir.Architecture, style Style, opts PNGOptions) ([]byte, error) {
	svg, err := RenderSVG(ctx, arch, style)
	if err != nil {
		return nil, err
	}
	return PNGFromSVG(ctx, svg, opts)
}

// PNGFromSVG は d2 が出力した SVG を PNG に変換する。
func PNGFromSVG(ctx context.Context, svg []byte, opts PNGOptions) ([]byte, error) {
	font, err := loadFont(opts)
	if err != nil {
		return nil, err
	}
	family, err := fontFamily(font)
	if err != nil {
		return nil, err
	}

	prepared := useFontFamily(svg, family)
	prepared, err = inlineIcons(ctx, prepared, opts.IconLoader)
	if err != nil {
		return nil, err
	}

	worker, err := resvg.NewContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("SVG ラスタライザの初期化に失敗しました: %w", err)
	}
	defer worker.Close()
	renderer, err := worker.NewRenderer()
	if err != nil {
		return nil, fmt.Errorf("SVG ラスタライザの初期化に失敗しました: %w", err)
	}
	defer renderer.Close()

	if err := renderer.LoadFontData(font); err != nil {
		return nil, fmt.Errorf("フォントを読み込めませんでした: %w", err)
	}
	if err := renderer.SetFontFamily(family); err != nil {
		return nil, fmt.Errorf("フォントを設定できませんでした: %w", err)
	}
	png, err := renderer.Render(prepared)
	if err != nil {
		return nil, fmt.Errorf("PNG への変換に失敗しました: %w", err)
	}
	return png, nil
}

func loadFont(opts PNGOptions) ([]byte, error) {
	if len(opts.FontData) > 0 {
		return opts.FontData, nil
	}
	paths := opts.FontPaths
	if len(paths) == 0 {
		paths = defaultFontPaths
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("PNG 変換に使えるフォントが見つかりませんでした（探索先: %v）。"+
		"PNGOptions.FontData か FontPaths でフォントを指定してください", paths)
}

// fontFamily はフォントデータからファミリ名を読む。
// SVG 側の font-family をこの名前に揃えるために使う。
func fontFamily(data []byte) (string, error) {
	sfntFont, err := sfnt.Parse(data)
	if err != nil {
		collection, cerr := sfnt.ParseCollection(data)
		if cerr != nil {
			return "", fmt.Errorf("フォントを解析できませんでした: %w", err)
		}
		sfntFont, err = collection.Font(0)
		if err != nil {
			return "", fmt.Errorf("フォントを解析できませんでした: %w", err)
		}
	}
	name, err := sfntFont.Name(nil, sfnt.NameIDFamily)
	if err != nil {
		return "", fmt.Errorf("フォント名を読めませんでした: %w", err)
	}
	return name, nil
}

// d2FontFamily は d2 が埋め込む独自のフォントファミリ名。
// 実体は @font-face の woff で、ラスタライザからは使えない。
var d2FontFamily = regexp.MustCompile(`d2-\d+-font-[a-z]+`)

// useFontFamily は d2 独自のフォントファミリ名を、実際に読み込んだフォントに差し替える。
func useFontFamily(svg []byte, family string) []byte {
	return d2FontFamily.ReplaceAll(svg, []byte(family))
}

// imageHref は SVG 内の外部画像（アイコン）参照。
var imageHref = regexp.MustCompile(`href="(https?://[^"]+)"`)

// inlineIcons は外部 URL のアイコンを data URI に置き換える。
// ラスタライザはネットワークを見にいかないため、埋め込まないとアイコンが消える。
// 取得に失敗したアイコンは、図全体を失敗させずにそのまま残す（描画されないだけ）。
func inlineIcons(ctx context.Context, svg []byte, load IconLoader) ([]byte, error) {
	if load == nil {
		return svg, nil
	}
	cache := map[string]string{}
	var failure error
	out := imageHref.ReplaceAllFunc(svg, func(match []byte) []byte {
		url := string(imageHref.FindSubmatch(match)[1])
		if cached, ok := cache[url]; ok {
			if cached == "" {
				return match
			}
			return []byte(`href="` + cached + `"`)
		}
		data, contentType, err := load(ctx, url)
		if err != nil {
			cache[url] = ""
			return match
		}
		if contentType == "" {
			contentType = "image/svg+xml"
		}
		encoded := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
		cache[url] = encoded
		return []byte(`href="` + encoded + `"`)
	})
	return out, failure
}

// HTTPIconLoader は HTTP でアイコンを取得する IconLoader を返す。
// client が nil なら 10 秒のタイムアウトを持つクライアントを使う。
func HTTPIconLoader(client *http.Client) IconLoader {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return func(ctx context.Context, url string) ([]byte, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, "", err
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("アイコンを取得できませんでした（%s）: %s", res.Status, url)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(res.Body); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), res.Header.Get("Content-Type"), nil
	}
}
