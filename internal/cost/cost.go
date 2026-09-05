// Package cost は IR と catalog から見積もりの明細を組み立てる。
//
// LLM は通らない。数量は catalog の quantity_formula を評価して求め、
// 単価は catalog の price_query から組み立てた PriceQuery で引く（PRIN-1 / PRIN-3）。
// 金額そのものはここでは確定させない。Excel の数式が計算する（PRIN-4）。
package cost

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
)

var templatePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// LineItem は見積もりの 1 行。Excel の Estimate シートの 1 行に対応する。
// 金額のフィールドを持たない。金額は単価 × 数量の数式が出す。
type LineItem struct {
	ResourceID string
	Service    string
	Label      string
	DriverID   string
	Unit       string
	// Params は catalog の既定値で補完済みのパラメータ。
	// Excel では数値パラメータが入力セルになる（FR-XLS-2）。
	Params map[string]any
	Query  pricing.PriceQuery
	// Quantity は quantity_formula を評価した数量。
	Quantity float64
	// Formula は数量の式。Excel の数式に写像する（M3）。
	Formula string
	// Price は取得した単価。Err が非 nil のときは空。
	Price pricing.Price
	// Err は単価の取得（または数量の評価）に失敗した理由。
	// 1 行が失敗しても見積もり全体は失敗させない（FR-PRC-7）。
	Err error
}

// OK は単価と数量が揃っている行かを返す。
func (l LineItem) OK() bool { return l.Err == nil }

// Estimate は IR 全体の見積もり明細。
type Estimate struct {
	Architecture *ir.Architecture
	Items        []LineItem
}

// Failed は単価の取得に失敗した行を返す。Excel に取得失敗として出す。
func (e *Estimate) Failed() []LineItem {
	var out []LineItem
	for _, it := range e.Items {
		if !it.OK() {
			out = append(out, it)
		}
	}
	return out
}

// Build は IR と catalog から明細を組み立て、単価を引く。
// 単価の取得に失敗した行は Err を持ったまま明細に残る（FR-PRC-7）。
func Build(ctx context.Context, arch *ir.Architecture, cat *catalog.Catalog, src pricing.PriceSource) (*Estimate, error) {
	if err := arch.Validate(cat); err != nil {
		return nil, err
	}
	location, err := RegionLocation(arch.Region)
	if err != nil {
		return nil, err
	}
	builtins := map[string]string{"region": arch.Region, "regionLocation": location}
	assumptions := arch.Assumptions.Vars()

	est := &Estimate{Architecture: arch}
	for i := range arch.Resources {
		r := &arch.Resources[i]
		svc, ok := cat.Get(r.Service)
		if !ok {
			return nil, fmt.Errorf("catalog に定義がないサービスです: %q", r.Service)
		}
		params := svc.ResolveParams(r.Params)
		for _, d := range svc.Drivers {
			if !d.AppliesTo(params) {
				continue
			}
			est.Items = append(est.Items, buildItem(ctx, r, svc, d, params, assumptions, builtins, arch.Region, src))
		}
	}
	return est, nil
}

func buildItem(
	ctx context.Context,
	r *ir.Resource,
	svc catalog.Service,
	d catalog.Driver,
	params map[string]any,
	assumptions map[string]float64,
	builtins map[string]string,
	region string,
	src pricing.PriceSource,
) LineItem {
	item := LineItem{
		ResourceID: r.ID,
		Service:    r.Service,
		Label:      r.Label,
		DriverID:   d.ID,
		Unit:       d.Unit,
		Params:     params,
		Formula:    d.QuantityFormula,
	}
	if item.Label == "" {
		item.Label = svc.Display
	}

	quantity, err := evalQuantity(d, params, assumptions)
	if err != nil {
		item.Err = err
		return item
	}
	item.Quantity = quantity

	query, err := buildQuery(d, params, builtins, region)
	if err != nil {
		item.Err = err
		return item
	}
	item.Query = query

	price, err := src.Unit(ctx, query)
	if err != nil {
		item.Err = fmt.Errorf("%s の単価を取得できませんでした: %w", r.ID, err)
		return item
	}
	if price.Unit != d.Unit {
		// catalog が想定した単位と取得した単価の単位が食い違っている。
		// 数量の意味が変わるため、黙って計上しない。
		item.Err = fmt.Errorf("%s: 単価の単位が catalog の想定と違います（catalog: %q, 取得: %q, SKU: %s）",
			r.ID, d.Unit, price.Unit, price.SKU)
		return item
	}
	item.Price = price
	return item
}

// evalQuantity は数量を評価する。params の値が assumptions より優先される。
func evalQuantity(d catalog.Driver, params map[string]any, assumptions map[string]float64) (float64, error) {
	vars := make(map[string]float64, len(assumptions)+len(params))
	for k, v := range assumptions {
		vars[k] = v
	}
	for k, v := range params {
		if f, ok := numeric(v); ok {
			vars[k] = f
		}
	}
	return d.Quantity().Eval(vars)
}

// buildQuery は catalog の price_query から PriceQuery を組み立てる。
// テンプレートの値は params と組み込み変数からのみ解決する。
func buildQuery(d catalog.Driver, params map[string]any, builtins map[string]string, region string) (pricing.PriceQuery, error) {
	attrs := make(map[string]string, len(d.PriceQuery))
	for attr, tmpl := range d.Filters() {
		value, err := expand(tmpl, params, builtins)
		if err != nil {
			return pricing.PriceQuery{}, fmt.Errorf("price_query.%s: %w", attr, err)
		}
		attrs[attr] = value
	}
	q := pricing.PriceQuery{Service: d.ServiceCode(), Attributes: attrs}
	if !d.Global() {
		q.Region = region
	}
	return q, nil
}

func expand(tmpl string, params map[string]any, builtins map[string]string) (string, error) {
	var failure error
	out := templatePattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		name := templatePattern.FindStringSubmatch(match)[1]
		if v, ok := builtins[name]; ok {
			return v
		}
		v, ok := params[name]
		if !ok {
			failure = fmt.Errorf("パラメータ %q に値がありません", name)
			return match
		}
		s, ok := asString(v)
		if !ok {
			failure = fmt.Errorf("パラメータ %q をフィルタ値にできません: %v (%T)", name, v, v)
			return match
		}
		return s
	})
	if failure != nil {
		return "", failure
	}
	return out, nil
}

func asString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	default:
		if f, ok := numeric(v); ok {
			return strconv.FormatFloat(f, 'f', -1, 64), true
		}
		return "", false
	}
}

func numeric(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}

// Numeric は params の値を数値として返す。数値でない値（インスタンスタイプなど）は ok が false。
func Numeric(v any) (float64, bool) { return numeric(v) }
