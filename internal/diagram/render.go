package diagram

import (
	"context"
	"fmt"
	"log/slog"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2target"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	d2log "oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

// layoutEngine は座標計算に使うレイアウトエンジン。
// 入れ子コンテナを含む構成図では ELK のほうが破綻しにくい。
const layoutEngine = "elk"

// RenderSVG は IR から構成図の SVG を生成する。
// 同じ IR からは常に同じバイト列が出る（FR-DIA-7 / NFR-1）。
func RenderSVG(ctx context.Context, arch *ir.Architecture, style Style) ([]byte, error) {
	return renderSVG(ctx, BuildD2(arch, style))
}

// compile は D2 ソースをレイアウト済みの図に変換する。
// 座標はここで初めて決まる。決めるのは ELK であって、IR でもコードでもない（PRIN-2）。
func compile(ctx context.Context, src string) (*d2target.Diagram, error) {
	ctx = d2log.With(ctx, slog.New(slog.DiscardHandler))
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, fmt.Errorf("テキスト計測の初期化に失敗しました: %w", err)
	}
	layout := layoutEngine
	compileOpts := &d2lib.CompileOptions{
		Ruler:  ruler,
		Layout: &layout,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2elklayout.DefaultLayout, nil
		},
	}
	themeID := d2themescatalog.NeutralDefault.ID
	pad := int64(d2svg.DEFAULT_PADDING)
	renderOpts := &d2svg.RenderOpts{ThemeID: &themeID, Pad: &pad}

	diagram, _, err := d2lib.Compile(ctx, src, compileOpts, renderOpts)
	if err != nil {
		return nil, fmt.Errorf("D2 のコンパイルに失敗しました: %w", err)
	}
	return diagram, nil
}

func renderSVG(ctx context.Context, src string) ([]byte, error) {
	// D2 は context に logger が無いと警告とスタックトレースを標準エラーに吐く。
	// ここはライブラリ呼び出しなので黙らせる。エラーは戻り値で返す。
	ctx = d2log.With(ctx, slog.New(slog.DiscardHandler))
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, fmt.Errorf("テキスト計測の初期化に失敗しました: %w", err)
	}
	layout := layoutEngine
	compileOpts := &d2lib.CompileOptions{
		Ruler:  ruler,
		Layout: &layout,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2elklayout.DefaultLayout, nil
		},
	}
	themeID := d2themescatalog.NeutralDefault.ID
	pad := int64(d2svg.DEFAULT_PADDING)
	renderOpts := &d2svg.RenderOpts{ThemeID: &themeID, Pad: &pad}

	diagram, _, err := d2lib.Compile(ctx, src, compileOpts, renderOpts)
	if err != nil {
		return nil, fmt.Errorf("D2 のコンパイルに失敗しました: %w", err)
	}
	svg, err := d2svg.Render(diagram, renderOpts)
	if err != nil {
		return nil, fmt.Errorf("SVG のレンダリングに失敗しました: %w", err)
	}
	return svg, nil
}
