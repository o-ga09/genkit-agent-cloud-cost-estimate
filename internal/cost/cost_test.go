package cost_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/cost"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
)

var fetchedAt = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

// recorder は問い合わせを記録し、単位だけ揃えた単価を返す PriceSource。
type recorder struct {
	queries []pricing.PriceQuery
	units   map[string]string // Service|属性 に対する unit の上書き
	err     error
}

func (r *recorder) Unit(_ context.Context, q pricing.PriceQuery) (pricing.Price, error) {
	r.queries = append(r.queries, q)
	if r.err != nil {
		return pricing.Price{}, r.err
	}
	unit := r.units[q.Key()]
	return pricing.Price{
		Amount: 1, Currency: "USD", Unit: unit, SKU: "SKU-" + q.Service, FetchedAt: fetchedAt,
	}, nil
}

func builtinCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatalf("catalog を読めませんでした: %v", err)
	}
	return c
}

// unitsFor は catalog が期待する unit をそのまま返す source を作る。
func unitsFor(t *testing.T, arch *ir.Architecture, cat *catalog.Catalog) *recorder {
	t.Helper()
	rec := &recorder{units: map[string]string{}}
	// 一度組み立てて、driver ごとの unit を記録する。
	probe := &recorder{units: map[string]string{}}
	est, err := cost.Build(context.Background(), arch, cat, probe)
	if err != nil {
		t.Fatalf("明細の組み立てに失敗しました: %v", err)
	}
	for _, it := range est.Items {
		rec.units[it.Query.Key()] = it.Unit
	}
	return rec
}

func TestBuild_EC2(t *testing.T) {
	cat := builtinCatalog(t)
	arch := &ir.Architecture{
		Provider: ir.ProviderAWS, Region: "ap-northeast-1",
		Assumptions: ir.Assumptions{HoursPerDay: 24, DaysPerMonth: 30, FxRate: 150},
		Resources: []ir.Resource{{
			ID: "app", Service: "ec2", Label: "App",
			Params: map[string]any{"instanceType": "t3.medium", "count": float64(2), "ebsGb": float64(30)},
		}},
	}
	rec := unitsFor(t, arch, cat)
	est, err := cost.Build(context.Background(), arch, cat, rec)
	if err != nil {
		t.Fatalf("明細の組み立てに失敗しました: %v", err)
	}
	if len(est.Items) != 2 {
		t.Fatalf("明細が %d 行、want 2（インスタンス時間と EBS）", len(est.Items))
	}

	hours := est.Items[0]
	if hours.DriverID != "instance_hours" {
		t.Fatalf("1 行目 = %q, want instance_hours", hours.DriverID)
	}
	if hours.Quantity != 2*24*30 {
		t.Errorf("数量 = %v, want %v", hours.Quantity, 2*24*30)
	}
	wantAttrs := map[string]string{
		"productFamily": "Compute Instance", "instanceType": "t3.medium", "tenancy": "Shared",
		"operatingSystem": "Linux", "preInstalledSw": "NA", "capacitystatus": "Used",
	}
	for k, want := range wantAttrs {
		if got := hours.Query.Value(k); got != want {
			t.Errorf("フィルタ %s = %q, want %q", k, got, want)
		}
	}
	if hours.Query.Region != "ap-northeast-1" {
		t.Errorf("region = %q", hours.Query.Region)
	}
	if hours.Query.Service != "AmazonEC2" {
		t.Errorf("serviceCode = %q", hours.Query.Service)
	}

	ebs := est.Items[1]
	if ebs.Quantity != 2*30 {
		t.Errorf("EBS の数量 = %v, want 60", ebs.Quantity)
	}
}

// NAT Gateway は時間課金とデータ処理料の 2 driver からなる（ADR-0018）。
func TestBuild_NATGateway(t *testing.T) {
	cat := builtinCatalog(t)
	arch := &ir.Architecture{
		Provider: ir.ProviderAWS, Region: "ap-northeast-1",
		Assumptions: ir.Assumptions{HoursPerDay: 24, DaysPerMonth: 30, FxRate: 150},
		Resources: []ir.Resource{{
			ID: "nat-a", Service: "nat_gateway", Label: "NAT Gateway",
			Params: map[string]any{"count": float64(1), "gbProcessedPerMonth": float64(300)},
		}},
	}
	rec := unitsFor(t, arch, cat)
	est, err := cost.Build(context.Background(), arch, cat, rec)
	if err != nil {
		t.Fatalf("明細の組み立てに失敗しました: %v", err)
	}
	if len(est.Items) != 2 {
		t.Fatalf("明細が %d 行、want 2（時間課金とデータ処理料）", len(est.Items))
	}

	hours := est.Items[0]
	if hours.DriverID != "nat_gateway_hours" {
		t.Fatalf("1 行目 = %q, want nat_gateway_hours", hours.DriverID)
	}
	if hours.Quantity != 1*24*30 {
		t.Errorf("数量 = %v, want %v", hours.Quantity, 1*24*30)
	}
	if got := hours.Query.Value("productFamily"); got != "NAT Gateway" {
		t.Errorf("productFamily = %q, want NAT Gateway", got)
	}
	if got := hours.Query.Value("groupDescription"); got != "Hourly charge for NAT Gateways" {
		t.Errorf("groupDescription = %q", got)
	}
	if hours.Query.Service != "AmazonEC2" {
		t.Errorf("serviceCode = %q, want AmazonEC2", hours.Query.Service)
	}

	processed := est.Items[1]
	if processed.DriverID != "nat_gateway_gb_processed" {
		t.Fatalf("2 行目 = %q, want nat_gateway_gb_processed", processed.DriverID)
	}
	if processed.Quantity != 300 {
		t.Errorf("数量 = %v, want 300", processed.Quantity)
	}
	if got := processed.Query.Value("groupDescription"); got != "Charge for per GB data processed by NatGateways" {
		t.Errorf("groupDescription = %q", got)
	}
}

// catalog の既定値が params の補完に使われる（count 未指定なら 1 台）。
func TestBuild_UsesCatalogDefaults(t *testing.T) {
	cat := builtinCatalog(t)
	arch := &ir.Architecture{
		Provider: ir.ProviderAWS, Region: "ap-northeast-1",
		Assumptions: ir.Assumptions{HoursPerDay: 24, DaysPerMonth: 30, FxRate: 150},
		Resources: []ir.Resource{{ID: "db", Service: "rds",
			Params: map[string]any{"instanceClass": "db.r6g.large", "engine": "PostgreSQL"}}},
	}
	rec := unitsFor(t, arch, cat)
	est, err := cost.Build(context.Background(), arch, cat, rec)
	if err != nil {
		t.Fatal(err)
	}
	inst := est.Items[0]
	if got := inst.Query.Value("deploymentOption"); got != "Single-AZ" {
		t.Errorf("deploymentOption = %q, want Single-AZ（catalog の既定値）", got)
	}
	if inst.Quantity != 24*30 {
		t.Errorf("数量 = %v, want 720（count の既定値 1）", inst.Quantity)
	}
	storage := est.Items[1]
	if storage.Quantity != 20 {
		t.Errorf("ストレージの数量 = %v, want 20（storageGb の既定値）", storage.Quantity)
	}
}

// when の条件に合う driver だけが計上される。
func TestBuild_DriverWhenCondition(t *testing.T) {
	cat := builtinCatalog(t)
	arch := &ir.Architecture{
		Provider: ir.ProviderAWS, Region: "ap-northeast-1",
		Assumptions: ir.Assumptions{HoursPerDay: 24, DaysPerMonth: 30, FxRate: 150},
		Resources: []ir.Resource{{ID: "egress", Service: "data_transfer",
			Params: map[string]any{"path": "internet_egress", "gbPerMonth": float64(1000)}}},
	}
	rec := unitsFor(t, arch, cat)
	est, err := cost.Build(context.Background(), arch, cat, rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(est.Items) != 1 {
		t.Fatalf("明細が %d 行、want 1（internet_egress のみ）", len(est.Items))
	}
	item := est.Items[0]
	if item.DriverID != "internet_egress" {
		t.Errorf("driver = %q, want internet_egress", item.DriverID)
	}
	// グローバルサービスは region を渡さず、location で絞る。
	if item.Query.Region != "" {
		t.Errorf("region = %q, want 空（グローバルサービス）", item.Query.Region)
	}
	if got := item.Query.Value("fromLocation"); got != "Asia Pacific (Tokyo)" {
		t.Errorf("fromLocation = %q, want Asia Pacific (Tokyo)", got)
	}
}

// FR-PRC-7: 単価の取得に失敗しても、見積もり全体は失敗しない。
func TestBuild_PriceFailureKeepsOtherItems(t *testing.T) {
	cat := builtinCatalog(t)
	arch := &ir.Architecture{
		Provider: ir.ProviderAWS, Region: "ap-northeast-1",
		Assumptions: ir.Assumptions{HoursPerDay: 24, DaysPerMonth: 30, FxRate: 150},
		Resources: []ir.Resource{{ID: "app", Service: "ec2",
			Params: map[string]any{"instanceType": "t3.medium"}}},
	}
	rec := &recorder{err: errors.New("MCP に接続できません")}
	est, err := cost.Build(context.Background(), arch, cat, rec)
	if err != nil {
		t.Fatalf("行の失敗で見積もり全体が失敗しました: %v", err)
	}
	if len(est.Items) != 2 {
		t.Fatalf("明細が %d 行、want 2", len(est.Items))
	}
	if len(est.Failed()) != 2 {
		t.Errorf("失敗した行が %d 件、want 2", len(est.Failed()))
	}
	if !strings.Contains(est.Items[0].Err.Error(), "MCP に接続できません") {
		t.Errorf("失敗の理由が伝わっていません: %v", est.Items[0].Err)
	}
}

// 単価の単位が catalog の想定と違う行は計上しない（数量の意味が変わるため）。
func TestBuild_UnitMismatch(t *testing.T) {
	cat := builtinCatalog(t)
	arch := &ir.Architecture{
		Provider: ir.ProviderAWS, Region: "ap-northeast-1",
		Assumptions: ir.Assumptions{HoursPerDay: 24, DaysPerMonth: 30, FxRate: 150},
		Resources: []ir.Resource{{ID: "app", Service: "ec2",
			Params: map[string]any{"instanceType": "t3.medium"}}},
	}
	rec := &recorder{units: map[string]string{}} // すべて unit が空文字列になる
	est, err := cost.Build(context.Background(), arch, cat, rec)
	if err != nil {
		t.Fatal(err)
	}
	if est.Items[0].OK() {
		t.Error("単位が食い違う行が計上されました")
	}
	if !strings.Contains(est.Items[0].Err.Error(), "単位") {
		t.Errorf("理由が単位の不一致になっていません: %v", est.Items[0].Err)
	}
}

func TestBuild_UnknownRegion(t *testing.T) {
	cat := builtinCatalog(t)
	arch := &ir.Architecture{
		Provider: ir.ProviderAWS, Region: "moon-central-1",
		Assumptions: ir.Assumptions{HoursPerDay: 24, DaysPerMonth: 30, FxRate: 150},
		Resources:   []ir.Resource{{ID: "app", Service: "ec2", Params: map[string]any{"instanceType": "t3.medium"}}},
	}
	if _, err := cost.Build(context.Background(), arch, cat, &recorder{}); err == nil {
		t.Error("未登録のリージョンがエラーになりませんでした")
	}
}

// サンプル IR の全行でフィルタが解決でき、テンプレートが残らないこと。
func TestBuild_ExampleIR(t *testing.T) {
	for _, name := range []string{"web-3tier", "serverless-api"} {
		t.Run(name, func(t *testing.T) { buildExample(t, name) })
	}
}

func buildExample(t *testing.T, name string) {
	t.Helper()
	cat := builtinCatalog(t)
	arch, err := ir.LoadFile("../../examples/ir/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	rec := unitsFor(t, arch, cat)
	est, err := cost.Build(context.Background(), arch, cat, rec)
	if err != nil {
		t.Fatalf("明細の組み立てに失敗しました: %v", err)
	}
	if len(est.Failed()) != 0 {
		t.Fatalf("失敗した行があります: %v", est.Failed()[0].Err)
	}
	for _, it := range est.Items {
		for _, f := range it.Query.Filters {
			if strings.Contains(f.Value, "{{") {
				t.Errorf("%s/%s のフィルタ %s にテンプレートが残っています: %q",
					it.ResourceID, it.DriverID, f.Field, f.Value)
			}
		}
		if it.Quantity < 0 {
			t.Errorf("%s/%s の数量が負です: %v", it.ResourceID, it.DriverID, it.Quantity)
		}
	}
	// VPC / AZ は課金要素を持たないため明細に出ない。
	for _, it := range est.Items {
		if it.Service == "vpc" || it.Service == "az" {
			t.Errorf("%s が明細に出ています", it.Service)
		}
	}
}
