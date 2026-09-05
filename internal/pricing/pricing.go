// Package pricing は単価取得を抽象化する（ADR-0004）。
//
// PriceQuery は catalog の price_query 定義と IR の params からコードが組み立てる。
// LLM がフィルタ値を渡す経路は存在しない（PRIN-3 / FR-PRC-2）。
// 第一実装は AWS Pricing MCP Server 経由（サブパッケージ awsmcp）で、
// MCP への依存はそのパッケージ内に閉じている。
package pricing

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// PriceQuery は 1 つの単価を特定するための問い合わせ。
type PriceQuery struct {
	// Service は Price List API のサービスコード（"AmazonEC2" など）。
	Service string
	// Region はリージョン。データ転送のようなグローバルサービスでは空にする。
	Region string
	// Attributes は Price List API の属性フィルタ。
	Attributes map[string]string
}

// Key は問い合わせを一意に表す文字列。キャッシュとテストのスタブに使う。
func (q PriceQuery) Key() string {
	var b strings.Builder
	b.WriteString(q.Service)
	b.WriteByte('|')
	b.WriteString(q.Region)
	for _, k := range slices.Sorted(maps.Keys(q.Attributes)) {
		fmt.Fprintf(&b, "|%s=%s", k, q.Attributes[k])
	}
	return b.String()
}

// Price は取得した単価。SKU と取得日時を必ず伴わせ、Excel の Prices シートに
// 出力して事後検証できるようにする（FR-PRC-4 / NFR-2）。
type Price struct {
	Amount    float64
	Currency  string
	Unit      string // "Hrs", "GB-Mo", "Requests" など
	SKU       string
	FetchedAt time.Time

	// Description は料金の説明文（"$0.0544 per On Demand Linux t3.medium Instance Hour" など）。
	// 検証時に AWS の料金ページと突き合わせるために持つ。
	Description string
	// Tiered は段階課金の SKU から最初の階層を採用したことを示す。
	// 上位の階層は未計上であり、Excel に注記する（FR-XLS-7）。
	Tiered bool
	// TierUpperBound は採用した階層の上限（段階課金でないときは空）。
	TierUpperBound string
}

// PriceSource は単価の取得元。フェイク実装に差し替えて見積もりロジックを
// テストできるようにするための抽象（FR-PRC-1）。
type PriceSource interface {
	Unit(ctx context.Context, q PriceQuery) (Price, error)
}

// NotFoundError は条件に合う単価が見つからなかったことを表す。
type NotFoundError struct {
	Query PriceQuery
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("条件に合う単価が見つかりませんでした: %s", e.Query.Key())
}

// AmbiguousError はフィルタが一意に絞れていないことを表す。
// catalog の price_query の記述漏れを示すので、黙って 1 件目を採らずにエラーにする。
type AmbiguousError struct {
	Query PriceQuery
	SKUs  []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("フィルタが一意ではありません（%d 件一致: %s）: %s",
		len(e.SKUs), strings.Join(e.SKUs, ", "), e.Query.Key())
}

// Static は固定の単価を返す PriceSource。テストとオフライン実行に使う。
type Static struct {
	// Prices は PriceQuery.Key() から単価への対応。
	Prices map[string]Price
}

// Unit は PriceSource を実装する。
func (s *Static) Unit(_ context.Context, q PriceQuery) (Price, error) {
	p, ok := s.Prices[q.Key()]
	if !ok {
		return Price{}, &NotFoundError{Query: q}
	}
	return p, nil
}

// Cache は同じ問い合わせの結果を使い回す PriceSource。
// 1 回の見積もりの中で同じ単価を何度も引かないために使う。
// 結果は成功・失敗の両方を覚える（同じ実行の中で結果がぶれないようにするため）。
type Cache struct {
	src PriceSource

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	price Price
	err   error
}

// NewCache は src の結果をメモ化する PriceSource を返す。
func NewCache(src PriceSource) *Cache {
	return &Cache{src: src, entries: make(map[string]cacheEntry)}
}

// Unit は PriceSource を実装する。
func (c *Cache) Unit(ctx context.Context, q PriceQuery) (Price, error) {
	key := q.Key()
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()
	if ok {
		return e.price, e.err
	}
	price, err := c.src.Unit(ctx, q)
	c.mu.Lock()
	c.entries[key] = cacheEntry{price: price, err: err}
	c.mu.Unlock()
	return price, err
}

var (
	_ PriceSource = (*Static)(nil)
	_ PriceSource = (*Cache)(nil)
)
