package awsmcp

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
)

// fixedTime は取得日時を固定する（テストを決定的にするため）。
var fixedTime = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("テストデータを読めませんでした: %v", err)
	}
	return b
}

func TestParsePrice_EC2Instance(t *testing.T) {
	q := pricing.PriceQuery{Service: "AmazonEC2", Region: "ap-northeast-1",
		Attributes: map[string]string{"instanceType": "t3.medium"}}

	got, err := parsePrice(load(t, "ec2_instance.json"), q, fixedTime)
	if err != nil {
		t.Fatalf("解析に失敗しました: %v", err)
	}
	want := pricing.Price{
		Amount:      0.0544,
		Currency:    "USD",
		Unit:        "Hrs",
		SKU:         "77PTRYZ5MAUP8HU6",
		FetchedAt:   fixedTime,
		Description: "$0.0544 per On Demand Linux t3.medium Instance Hour",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Price =\n %+v\nwant\n %+v", got, want)
	}
}

// 段階課金の SKU では最初の階層を採用し、Tiered を立てる。
func TestParsePrice_TieredTakesFirstTier(t *testing.T) {
	q := pricing.PriceQuery{Service: "AmazonS3", Region: "ap-northeast-1"}
	got, err := parsePrice(load(t, "s3_storage_tiered.json"), q, fixedTime)
	if err != nil {
		t.Fatalf("解析に失敗しました: %v", err)
	}
	if got.Amount != 0.025 {
		t.Errorf("Amount = %v, want 0.025（最初の階層）", got.Amount)
	}
	if !got.Tiered {
		t.Error("Tiered = false, want true")
	}
	if got.TierUpperBound != "51200" {
		t.Errorf("TierUpperBound = %q, want \"51200\"", got.TierUpperBound)
	}
}

// フィルタが一意でないときは、1 件目を黙って採らずにエラーにする。
// catalog の price_query の記述漏れに気づけなくなるため。
func TestParsePrice_Ambiguous(t *testing.T) {
	q := pricing.PriceQuery{Service: "AWSELB", Region: "ap-northeast-1"}
	_, err := parsePrice(load(t, "alb_ambiguous.json"), q, fixedTime)
	var ambiguous *pricing.AmbiguousError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("err = %v, want *pricing.AmbiguousError", err)
	}
	if len(ambiguous.SKUs) != 2 {
		t.Errorf("SKUs = %v, want 2 件", ambiguous.SKUs)
	}
}

func TestParsePrice_EmptyResults(t *testing.T) {
	q := pricing.PriceQuery{Service: "AWSDataTransfer"}
	_, err := parsePrice(load(t, "empty_results.json"), q, fixedTime)
	var notFound *pricing.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %v, want *pricing.NotFoundError", err)
	}
}

func TestParsePrice_InterAZ(t *testing.T) {
	q := pricing.PriceQuery{Service: "AWSDataTransfer"}
	got, err := parsePrice(load(t, "free_inter_az.json"), q, fixedTime)
	if err != nil {
		t.Fatalf("解析に失敗しました: %v", err)
	}
	if got.Amount != 0.01 || got.Unit != "GB" {
		t.Errorf("Price = %+v, want 0.01 GB", got)
	}
	if got.Tiered {
		t.Error("Tiered = true, want false（単一階層）")
	}
}

func TestParsePrice_BrokenJSON(t *testing.T) {
	if _, err := parsePrice([]byte("{"), pricing.PriceQuery{}, fixedTime); err == nil {
		t.Error("壊れた JSON がエラーになりませんでした")
	}
}

// FR-PRC-6: 参照する料金はオンデマンドのみ。
// グローバルサービスでは region を渡さない。
func TestBuildArguments(t *testing.T) {
	q := pricing.PriceQuery{
		Service: "AmazonEC2",
		Region:  "ap-northeast-1",
		Attributes: map[string]string{
			"instanceType":  "t3.medium",
			"productFamily": "Compute Instance",
		},
	}
	args := buildArguments(q)
	if args["service_code"] != "AmazonEC2" {
		t.Errorf("service_code = %v", args["service_code"])
	}
	if args["region"] != "ap-northeast-1" {
		t.Errorf("region = %v", args["region"])
	}
	opts := args["output_options"].(map[string]any)
	terms := opts["pricing_terms"].([]string)
	if len(terms) != 1 || terms[0] != "OnDemand" {
		t.Errorf("pricing_terms = %v, want [OnDemand]", terms)
	}
	filters := args["filters"].([]map[string]any)
	want := []map[string]any{
		{"Field": "instanceType", "Type": "EQUALS", "Value": "t3.medium"},
		{"Field": "productFamily", "Type": "EQUALS", "Value": "Compute Instance"},
	}
	if !reflect.DeepEqual(filters, want) {
		t.Errorf("filters = %v, want %v（属性名の昇順）", filters, want)
	}

	global := pricing.PriceQuery{Service: "AWSDataTransfer",
		Attributes: map[string]string{"transferType": "AWS Outbound"}}
	if _, ok := buildArguments(global)["region"]; ok {
		t.Error("グローバルサービスに region が渡っています")
	}
}
