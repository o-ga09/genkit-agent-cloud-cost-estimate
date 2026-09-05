// Package estimateflow は IR から成果物一式を作る決定的な処理を、
// Genkit の Flow として公開する（M4）。
//
// この処理に LLM は登場しない。IR さえあれば図と Excel が出る（NFR-3）。
// また、Genkit の preview（exp 系）API には依存しない。preview への依存は
// 対話層だけに閉じる（PRIN-6 / NFR-5）。
package estimateflow

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/cost"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/diagram"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/workbook"
)

// FlowName は Genkit に登録する Flow の名前。
const FlowName = "estimate"

// Format は成果物の形式。
type Format string

const (
	FormatSVG    Format = "svg"
	FormatPNG    Format = "png"
	FormatDrawio Format = "drawio"
	FormatXLSX   Format = "xlsx"
	FormatIR     Format = "ir"
)

// DefaultFormats は Formats を指定しなかったときに作る成果物。
// PNG は実行環境のフォントに依存するため（ADR-0014）、既定には含めない。
var DefaultFormats = []Format{FormatIR, FormatSVG, FormatDrawio, FormatXLSX}

// Request は Flow の入力。IR そのものを受け取る。
type Request struct {
	// Architecture は構成の IR。全成果物の正本（ADR-0002）。
	Architecture *ir.Architecture `json:"architecture"`
	// Formats は作る成果物。空なら DefaultFormats。
	Formats []Format `json:"formats,omitempty"`
	// BaseName は成果物のファイル名（拡張子なし）。空なら "estimate"。
	BaseName string `json:"baseName,omitempty"`
}

// Artifact は生成した成果物 1 つ。
type Artifact struct {
	Format      Format `json:"format"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	// Content は中身。JSON では base64 になる。
	Content []byte `json:"content"`
}

// Line は見積もり明細 1 行の要約。
//
// 単価・数量・SKU という「取得した事実」だけを載せ、月額や合計は持たない。
// 金額は Excel の数式が出すものであり、ここで確定させない（PRIN-4）。
type Line struct {
	ResourceID string    `json:"resourceId"`
	Service    string    `json:"service"`
	DriverID   string    `json:"driverId"`
	Unit       string    `json:"unit"`
	Quantity   float64   `json:"quantity"`
	UnitPrice  float64   `json:"unitPrice,omitempty"`
	Currency   string    `json:"currency,omitempty"`
	SKU        string    `json:"sku,omitempty"`
	FetchedAt  time.Time `json:"fetchedAt,omitzero"`
	// Tiered は段階課金の第 1 階層の単価を使っていることを示す。
	Tiered bool `json:"tiered,omitempty"`
	// Error は単価を取得できなかった行の理由。
	Error string `json:"error,omitempty"`
}

// Response は Flow の出力。
type Response struct {
	Region    string     `json:"region"`
	Lines     []Line     `json:"lines"`
	Artifacts []Artifact `json:"artifacts"`
	// FailedLines は単価を取得できなかった行数。0 でなくても成果物は作る（FR-PRC-7）。
	FailedLines int `json:"failedLines"`
}

// Deps は Flow が使う外部依存。テストではフェイクに差し替える。
type Deps struct {
	// Catalog はサービス定義。nil なら同梱の catalog を読む。
	Catalog *catalog.Catalog
	// Prices は単価の取得元。必須。
	Prices pricing.PriceSource
	// PNG は PNG 変換の設定（フォントとアイコンの取得方法）。
	PNG diagram.PNGOptions
	// Now は生成日時の取得方法。nil なら time.Now。
	Now func() time.Time
}

func (d Deps) catalog() (*catalog.Catalog, error) {
	if d.Catalog != nil {
		return d.Catalog, nil
	}
	return catalog.Builtin()
}

func (d Deps) now() time.Time {
	if d.Now == nil {
		return time.Now().UTC()
	}
	return d.Now()
}

// Define は estimate Flow を Genkit に登録する。
func Define(g *genkit.Genkit, deps Deps) *core.Flow[*Request, *Response, struct{}] {
	return genkit.DefineFlow(g, FlowName, func(ctx context.Context, req *Request) (*Response, error) {
		return Run(ctx, deps, req)
	})
}

// Run は Flow の中身。Genkit を初期化せずに呼べる（NFR-3 / テスト用）。
func Run(ctx context.Context, deps Deps, req *Request) (*Response, error) {
	if req == nil || req.Architecture == nil {
		return nil, fmt.Errorf("architecture は必須です")
	}
	if deps.Prices == nil {
		return nil, fmt.Errorf("単価の取得元（Deps.Prices）が設定されていません")
	}
	cat, err := deps.catalog()
	if err != nil {
		return nil, err
	}
	arch := req.Architecture

	est, err := cost.Build(ctx, arch, cat, pricing.NewCache(deps.Prices))
	if err != nil {
		return nil, err
	}

	res := &Response{
		Region:      arch.Region,
		Lines:       summarize(est),
		FailedLines: len(est.Failed()),
	}
	formats := req.Formats
	if len(formats) == 0 {
		formats = DefaultFormats
	}
	base := req.BaseName
	if base == "" {
		base = "estimate"
	}
	for _, format := range formats {
		artifact, err := build(ctx, deps, cat, arch, est, format, base)
		if err != nil {
			return nil, err
		}
		res.Artifacts = append(res.Artifacts, artifact)
	}
	return res, nil
}

func build(
	ctx context.Context,
	deps Deps,
	cat *catalog.Catalog,
	arch *ir.Architecture,
	est *cost.Estimate,
	format Format,
	base string,
) (Artifact, error) {
	switch format {
	case FormatIR:
		var buf bytes.Buffer
		if err := ir.Encode(&buf, arch); err != nil {
			return Artifact{}, err
		}
		return Artifact{Format: format, Filename: base + ".json",
			ContentType: "application/json", Content: buf.Bytes()}, nil

	case FormatSVG:
		svg, err := diagram.RenderSVG(ctx, arch, cat)
		if err != nil {
			return Artifact{}, err
		}
		return Artifact{Format: format, Filename: base + ".svg",
			ContentType: "image/svg+xml", Content: svg}, nil

	case FormatPNG:
		png, err := diagram.RenderPNG(ctx, arch, cat, deps.PNG)
		if err != nil {
			return Artifact{}, err
		}
		return Artifact{Format: format, Filename: base + ".png",
			ContentType: "image/png", Content: png}, nil

	case FormatDrawio:
		x, err := diagram.RenderDrawio(ctx, arch, cat)
		if err != nil {
			return Artifact{}, err
		}
		return Artifact{Format: format, Filename: base + ".drawio",
			ContentType: "application/vnd.jgraph.mxfile", Content: x}, nil

	case FormatXLSX:
		f, err := workbook.Build(est, workbook.Options{GeneratedAt: deps.now()})
		if err != nil {
			return Artifact{}, err
		}
		defer f.Close()
		buf, err := f.WriteToBuffer()
		if err != nil {
			return Artifact{}, fmt.Errorf("Excel を書き出せませんでした: %w", err)
		}
		return Artifact{Format: format, Filename: base + ".xlsx",
			ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			Content:     buf.Bytes()}, nil

	default:
		return Artifact{}, fmt.Errorf("未対応の形式です: %q", format)
	}
}

func summarize(est *cost.Estimate) []Line {
	lines := make([]Line, 0, len(est.Items))
	for _, it := range est.Items {
		line := Line{
			ResourceID: it.ResourceID,
			Service:    it.Service,
			DriverID:   it.DriverID,
			Unit:       it.Unit,
			Quantity:   it.Quantity,
		}
		if it.OK() {
			line.UnitPrice = it.Price.Amount
			line.Currency = it.Price.Currency
			line.SKU = it.Price.SKU
			line.FetchedAt = it.Price.FetchedAt
			line.Tiered = it.Price.Tiered
		} else {
			line.Error = it.Err.Error()
		}
		lines = append(lines, line)
	}
	return lines
}
