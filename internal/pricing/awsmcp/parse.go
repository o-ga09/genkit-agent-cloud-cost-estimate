// Package awsmcp は AWS Pricing MCP Server 経由の PriceSource 実装（ADR-0004）。
//
// MCP への依存はこのパッケージに閉じている。LLM のツールとしては公開しない（FR-PRC-3）。
package awsmcp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
)

// toolResponse は get_pricing ツールが返す JSON。
type toolResponse struct {
	Result struct {
		Status    string    `json:"status"`
		Message   string    `json:"message"`
		ErrorType string    `json:"error_type"`
		Data      []product `json:"data"`
	} `json:"result"`
}

type product struct {
	Product struct {
		SKU           string            `json:"sku"`
		ProductFamily string            `json:"productFamily"`
		Attributes    map[string]string `json:"attributes"`
	} `json:"product"`
	Terms map[string]json.RawMessage `json:"terms"`
}

type term struct {
	SKU             string                    `json:"sku"`
	OfferTermCode   string                    `json:"offerTermCode"`
	EffectiveDate   string                    `json:"effectiveDate"`
	PriceDimensions map[string]priceDimension `json:"priceDimensions"`
}

type priceDimension struct {
	Unit         string            `json:"unit"`
	Description  string            `json:"description"`
	BeginRange   string            `json:"beginRange"`
	EndRange     string            `json:"endRange"`
	RateCode     string            `json:"rateCode"`
	PricePerUnit map[string]string `json:"pricePerUnit"`
}

// parsePrice は get_pricing のレスポンスから単価を 1 つ取り出す。
//
// 複数の SKU が該当した場合はエラーにする。1 件目を黙って採ると catalog の
// フィルタ漏れに気づけないため（AmbiguousError）。
// 段階課金の SKU では最初の階層（beginRange = 0）を採用し、Tiered を立てる。
func parsePrice(raw []byte, q pricing.PriceQuery, now time.Time) (pricing.Price, error) {
	var resp toolResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return pricing.Price{}, fmt.Errorf("単価レスポンスの解析に失敗しました: %w", err)
	}
	if resp.Result.Status == "error" {
		if resp.Result.ErrorType == "empty_results" {
			return pricing.Price{}, &pricing.NotFoundError{Query: q}
		}
		return pricing.Price{}, fmt.Errorf("単価の取得に失敗しました: %s", resp.Result.Message)
	}
	switch len(resp.Result.Data) {
	case 0:
		return pricing.Price{}, &pricing.NotFoundError{Query: q}
	case 1:
	default:
		skus := make([]string, 0, len(resp.Result.Data))
		for _, p := range resp.Result.Data {
			skus = append(skus, p.Product.SKU)
		}
		slices.Sort(skus)
		return pricing.Price{}, &pricing.AmbiguousError{Query: q, SKUs: skus}
	}

	p := resp.Result.Data[0]
	onDemand, ok := p.Terms["OnDemand"]
	if !ok {
		return pricing.Price{}, fmt.Errorf("SKU %s にオンデマンドの料金がありません", p.Product.SKU)
	}
	// オンデマンドは offerTermCode をキーにした map。通常 1 件だが、
	// 複数あってもキー順で決定的に選ぶ。
	var terms map[string]term
	if err := json.Unmarshal(onDemand, &terms); err != nil {
		return pricing.Price{}, fmt.Errorf("SKU %s のオンデマンド料金の解析に失敗しました: %w", p.Product.SKU, err)
	}
	if len(terms) == 0 {
		return pricing.Price{}, fmt.Errorf("SKU %s にオンデマンドの料金がありません", p.Product.SKU)
	}
	termKeys := make([]string, 0, len(terms))
	for k := range terms {
		termKeys = append(termKeys, k)
	}
	slices.Sort(termKeys)
	t := terms[termKeys[0]]

	dim, tiered, err := billableTier(t.PriceDimensions)
	if err != nil {
		return pricing.Price{}, fmt.Errorf("SKU %s: %w", p.Product.SKU, err)
	}
	amount, currency, err := amountOf(dim)
	if err != nil {
		return pricing.Price{}, fmt.Errorf("SKU %s: %w", p.Product.SKU, err)
	}

	price := pricing.Price{
		Amount:      amount,
		Currency:    currency,
		Unit:        dim.Unit,
		SKU:         p.Product.SKU,
		FetchedAt:   now,
		Description: dim.Description,
		Tiered:      tiered,
	}
	if tiered {
		price.TierLowerBound = dim.BeginRange
		price.TierUpperBound = dim.EndRange
	}
	return price, nil
}

// billableTier は課金が始まる最初の階層を返す。
//
// 段階課金では下の階層ほど安いとは限らない。DynamoDB のストレージのように
// 第 1 階層が無料枠（$0）の SKU があり、それを採ると費用が丸ごと消えてしまう。
// そこで「単価が 0 でない最初の階層」を採る。無料枠は未計上として扱う
// （見積もりが過小になるより過大になるほうが安全なため）。
func billableTier(dims map[string]priceDimension) (priceDimension, bool, error) {
	if len(dims) == 0 {
		return priceDimension{}, false, fmt.Errorf("価格次元がありません")
	}
	keys := make([]string, 0, len(dims))
	for k := range dims {
		keys = append(keys, k)
	}
	// beginRange の数値順、同値なら rateCode 順で決定的に並べる。
	slices.SortFunc(keys, func(a, b string) int {
		ra, rb := rangeValue(dims[a].BeginRange), rangeValue(dims[b].BeginRange)
		switch {
		case ra < rb:
			return -1
		case ra > rb:
			return 1
		}
		return cmpString(a, b)
	})
	for _, k := range keys {
		if amount, _, err := amountOf(dims[k]); err == nil && amount > 0 {
			return dims[k], len(dims) > 1, nil
		}
	}
	// すべて 0 円（無料の SKU）。
	return dims[keys[0]], len(dims) > 1, nil
}

func rangeValue(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// amountOf は単価と通貨を返す。USD を優先する。
func amountOf(dim priceDimension) (float64, string, error) {
	if len(dim.PricePerUnit) == 0 {
		return 0, "", fmt.Errorf("単価がありません")
	}
	currencies := make([]string, 0, len(dim.PricePerUnit))
	for c := range dim.PricePerUnit {
		currencies = append(currencies, c)
	}
	slices.Sort(currencies)
	currency := currencies[0]
	if _, ok := dim.PricePerUnit["USD"]; ok {
		currency = "USD"
	}
	amount, err := strconv.ParseFloat(dim.PricePerUnit[currency], 64)
	if err != nil {
		return 0, "", fmt.Errorf("単価 %q を数値として読めませんでした", dim.PricePerUnit[currency])
	}
	return amount, currency, nil
}
