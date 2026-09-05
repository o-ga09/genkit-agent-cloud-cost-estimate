package pricing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
)

func TestPriceQuery_Key(t *testing.T) {
	a := pricing.PriceQuery{Service: "AmazonEC2", Region: "ap-northeast-1", Filters: []pricing.Filter{
		{Field: "instanceType", Value: "t3.medium"}, {Field: "tenancy", Value: "Shared"},
	}}
	b := pricing.PriceQuery{Service: "AmazonEC2", Region: "ap-northeast-1", Filters: []pricing.Filter{
		{Field: "tenancy", Value: "Shared"}, {Field: "instanceType", Value: "t3.medium"},
	}}
	if a.Key() != b.Key() {
		t.Errorf("フィルタの順序でキーが変わりました:\n %q\n %q", a.Key(), b.Key())
	}
	c := pricing.PriceQuery{Service: "AmazonEC2", Region: "us-east-1", Filters: a.Filters}
	if a.Key() == c.Key() {
		t.Error("リージョン違いが同じキーになりました")
	}
	// 一致条件が違えば別の問い合わせになる。
	d := pricing.PriceQuery{Service: "AmazonECS", Filters: []pricing.Filter{
		{Field: "usagetype", Match: pricing.MatchContains, Value: "Fargate-vCPU-Hours"},
	}}
	e := pricing.PriceQuery{Service: "AmazonECS", Filters: []pricing.Filter{
		{Field: "usagetype", Value: "Fargate-vCPU-Hours"},
	}}
	if d.Key() == e.Key() {
		t.Error("contains と equals が同じキーになりました")
	}
	if got := a.Value("instanceType"); got != "t3.medium" {
		t.Errorf("Value(instanceType) = %q", got)
	}
}

func TestStatic(t *testing.T) {
	q := pricing.PriceQuery{Service: "AmazonEC2", Region: "ap-northeast-1"}
	src := &pricing.Static{Prices: map[string]pricing.Price{
		q.Key(): {Amount: 0.0544, Currency: "USD", Unit: "Hrs", SKU: "SKU1"},
	}}
	got, err := src.Unit(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount != 0.0544 {
		t.Errorf("Amount = %v", got.Amount)
	}
	var notFound *pricing.NotFoundError
	if _, err := src.Unit(context.Background(), pricing.PriceQuery{Service: "AmazonS3"}); !errors.As(err, &notFound) {
		t.Errorf("err = %v, want *NotFoundError", err)
	}
}

// Cache は同じ問い合わせを 1 回しか元の PriceSource に流さない。
func TestCache(t *testing.T) {
	q := pricing.PriceQuery{Service: "AmazonEC2", Region: "ap-northeast-1"}
	counter := &countingSource{price: pricing.Price{Amount: 1, Unit: "Hrs"}}
	cache := pricing.NewCache(counter)

	for range 3 {
		if _, err := cache.Unit(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
	if counter.calls != 1 {
		t.Errorf("元の PriceSource の呼び出しが %d 回、want 1", counter.calls)
	}
}

// 失敗も覚える（同じ実行の中で結果がぶれないようにするため）。
func TestCache_CachesFailure(t *testing.T) {
	q := pricing.PriceQuery{Service: "AmazonEC2"}
	counter := &countingSource{err: errors.New("接続できません")}
	cache := pricing.NewCache(counter)

	for range 2 {
		if _, err := cache.Unit(context.Background(), q); err == nil {
			t.Fatal("エラーが返りませんでした")
		}
	}
	if counter.calls != 1 {
		t.Errorf("元の PriceSource の呼び出しが %d 回、want 1", counter.calls)
	}
}

type countingSource struct {
	calls int
	price pricing.Price
	err   error
}

func (c *countingSource) Unit(context.Context, pricing.PriceQuery) (pricing.Price, error) {
	c.calls++
	return c.price, c.err
}
