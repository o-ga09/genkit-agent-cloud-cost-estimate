package ir_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

// fakeSchema は catalog に依存せずバリデーションを検証するためのスキーマ。
type fakeSchema map[string]ir.ServiceSpec

func (f fakeSchema) ServiceNames() []string {
	names := make([]string, 0, len(f))
	for n := range f {
		names = append(names, n)
	}
	return names
}

func (f fakeSchema) ServiceSpec(service string) (ir.ServiceSpec, bool) {
	s, ok := f[service]
	return s, ok
}

func testSchema() fakeSchema {
	return fakeSchema{
		"ec2": {Params: map[string]ir.ParamSpec{
			"instanceType": {Type: ir.ParamString, Required: true},
			"count":        {Type: ir.ParamInt, HasDefault: true},
			"ebsGb":        {Type: ir.ParamFloat, HasDefault: true},
			"detailedMon":  {Type: ir.ParamBool, HasDefault: true},
		}},
		"data_transfer": {Params: map[string]ir.ParamSpec{
			"path": {Type: ir.ParamString, Required: true, Enum: []string{"internet_egress", "inter_az"}},
		}},
		"vpc": {},
	}
}

func validArch() *ir.Architecture {
	return &ir.Architecture{
		SchemaVersion: ir.SchemaVersion,
		Provider:      ir.ProviderAWS,
		Region:        "ap-northeast-1",
		Assumptions: ir.Assumptions{
			HoursPerDay: 24, DaysPerMonth: 30, RequestsPerMonth: 1_000_000,
			FxRate: 150, DiscountRate: 0,
		},
		Resources: []ir.Resource{
			{ID: "vpc-main", Service: "vpc"},
			{ID: "app", Service: "ec2", Parent: "vpc-main",
				Params: map[string]any{"instanceType": "t3.medium", "count": float64(2)}},
			{ID: "egress", Service: "data_transfer",
				Params: map[string]any{"path": "internet_egress"}},
		},
		Edges: []ir.Edge{{From: "app", To: "egress", Label: "out"}},
	}
}

func TestValidate_OK(t *testing.T) {
	if err := validArch().Validate(testSchema()); err != nil {
		t.Fatalf("正しい IR がエラーになりました: %v", err)
	}
}

func TestValidate_NilSchemaSkipsServiceChecks(t *testing.T) {
	a := validArch()
	a.Resources[1].Service = "not-in-catalog"
	if err := a.Validate(nil); err != nil {
		t.Fatalf("schema なしでサービス検証が走りました: %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ir.Architecture)
		wantSub string
	}{
		{"id が重複", func(a *ir.Architecture) {
			a.Resources[2].ID = "app"
		}, "id が重複しています"},
		{"id が空", func(a *ir.Architecture) {
			a.Resources[1].ID = ""
		}, "resources[1].id"},
		{"id に使えない文字", func(a *ir.Architecture) {
			a.Resources[1].ID = "app.web"
		}, "英数字で始まり"},
		{"catalog にないサービス", func(a *ir.Architecture) {
			a.Resources[1].Service = "athena"
		}, "catalog に定義がないサービスです"},
		{"必須パラメータ欠落", func(a *ir.Architecture) {
			delete(a.Resources[1].Params, "instanceType")
		}, "必須のパラメータです"},
		{"パラメータの型不一致", func(a *ir.Architecture) {
			a.Resources[1].Params["instanceType"] = 3
		}, "文字列である必要があります"},
		{"int に小数", func(a *ir.Architecture) {
			a.Resources[1].Params["count"] = 1.5
		}, "整数である必要があります"},
		{"bool でない", func(a *ir.Architecture) {
			a.Resources[1].Params["detailedMon"] = "yes"
		}, "真偽値である必要があります"},
		{"catalog にないパラメータ", func(a *ir.Architecture) {
			a.Resources[1].Params["spotPrice"] = 0.01
		}, "catalog に定義がないパラメータです"},
		{"enum 外の値", func(a *ir.Architecture) {
			a.Resources[2].Params["path"] = "inter_region"
		}, "次のいずれかである必要があります"},
		{"存在しない parent", func(a *ir.Architecture) {
			a.Resources[1].Parent = "vpc-sub"
		}, "resources[1].parent"},
		{"自分自身が parent", func(a *ir.Architecture) {
			a.Resources[1].Parent = "app"
		}, "自分自身を親にできません"},
		{"parent が循環", func(a *ir.Architecture) {
			a.Resources[0].Parent = "app"
		}, "循環しています"},
		{"存在しない edge.from", func(a *ir.Architecture) {
			a.Edges[0].From = "ghost"
		}, "edges[0].from"},
		{"存在しない edge.to", func(a *ir.Architecture) {
			a.Edges[0].To = "ghost"
		}, "edges[0].to"},
		{"自己ループの edge", func(a *ir.Architecture) {
			a.Edges[0].To = "app"
		}, "from と to が同じリソースです"},
		{"未対応のプロバイダ", func(a *ir.Architecture) {
			a.Provider = "azure"
		}, "未対応のプロバイダ"},
		{"region が空", func(a *ir.Architecture) {
			a.Region = ""
		}, "region"},
		{"リソースが空", func(a *ir.Architecture) {
			a.Resources = nil
			a.Edges = nil
		}, "1 つ以上のリソースが必要です"},
		{"未対応のスキーマバージョン", func(a *ir.Architecture) {
			a.SchemaVersion = "99"
		}, "未対応のスキーマバージョン"},
		{"稼働時間が範囲外", func(a *ir.Architecture) {
			a.Assumptions.HoursPerDay = 25
		}, "assumptions.hoursPerDay"},
		{"稼働日数が範囲外", func(a *ir.Architecture) {
			a.Assumptions.DaysPerMonth = 32
		}, "assumptions.daysPerMonth"},
		{"為替レートが 0", func(a *ir.Architecture) {
			a.Assumptions.FxRate = 0
		}, "assumptions.fxRate"},
		{"割引率が範囲外", func(a *ir.Architecture) {
			a.Assumptions.DiscountRate = 1
		}, "assumptions.discountRate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := validArch()
			tt.mutate(a)
			err := a.Validate(testSchema())
			if err == nil {
				t.Fatalf("エラーになりませんでした")
			}
			var ve *ir.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("ValidationError ではありません: %T", err)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("理由が伝わっていません\n got: %v\nwant substring: %q", err, tt.wantSub)
			}
		})
	}
}
