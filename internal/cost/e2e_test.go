package cost_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/cost"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing/awsmcp"
)

// サンプル IR の全 driver を実際の AWS Pricing MCP Server で引く。既定ではスキップする。
//
//	AWS_PRICING_MCP_E2E=1 go test ./internal/cost/ -run E2E -v
//
// catalog の price_query が実データで一意に解決できることの確認に使う（FR-CAT-5）。
func TestE2E_ExampleIR(t *testing.T) {
	if os.Getenv("AWS_PRICING_MCP_E2E") == "" {
		t.Skip("AWS_PRICING_MCP_E2E が未設定のためスキップします")
	}
	for _, name := range []string{"web-3tier", "serverless-api"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			cat := builtinCatalog(t)
			arch, err := ir.LoadFile("../../examples/ir/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			src := awsmcp.New(awsmcp.Options{})
			defer src.Close()

			est, err := cost.Build(ctx, arch, cat, pricing.NewCache(src))
			if err != nil {
				t.Fatalf("明細の組み立てに失敗しました: %v", err)
			}
			for _, it := range est.Items {
				if !it.OK() {
					t.Errorf("%s/%s: %v", it.ResourceID, it.DriverID, it.Err)
					continue
				}
				t.Logf("%-12s %-24s %12.10f %s/%s  qty=%.0f  SKU=%s  tier=%s〜%s",
					it.ResourceID, it.DriverID, it.Price.Amount, it.Price.Currency, it.Price.Unit,
					it.Quantity, it.Price.SKU, it.Price.TierLowerBound, it.Price.TierUpperBound)
			}
		})
	}
}
