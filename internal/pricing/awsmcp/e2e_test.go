package awsmcp_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing/awsmcp"
)

// 実際の AWS Pricing MCP Server に接続する。既定ではスキップする。
//
//	AWS_PRICING_MCP_E2E=1 go test ./internal/pricing/awsmcp/ -run E2E -v
//
// 実行には uvx と pricing:* 権限を持つ AWS 認証情報が必要（FR-PRC-5 のとおり、
// 認証情報はサービス側が持つ想定で、利用者には要求しない）。
func TestE2E_Unit(t *testing.T) {
	if os.Getenv("AWS_PRICING_MCP_E2E") == "" {
		t.Skip("AWS_PRICING_MCP_E2E が未設定のためスキップします")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	src := awsmcp.New(awsmcp.Options{})
	defer src.Close()

	price, err := src.Unit(ctx, pricing.PriceQuery{
		Service: "AmazonEC2",
		Region:  "ap-northeast-1",
		Filters: []pricing.Filter{
			{Field: "productFamily", Value: "Compute Instance"},
			{Field: "instanceType", Value: "t3.medium"},
			{Field: "tenancy", Value: "Shared"},
			{Field: "operatingSystem", Value: "Linux"},
			{Field: "preInstalledSw", Value: "NA"},
			{Field: "capacitystatus", Value: "Used"},
		},
	})
	if err != nil {
		t.Fatalf("単価を取得できませんでした: %v", err)
	}
	if price.Amount <= 0 || price.Unit != "Hrs" || price.SKU == "" {
		t.Errorf("Price = %+v, want 正の Hrs 単価と SKU", price)
	}
	t.Logf("t3.medium: %.4f %s / %s (SKU %s, %s)",
		price.Amount, price.Currency, price.Unit, price.SKU, price.Description)
}
