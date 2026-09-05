package ir_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

// FR-IR-9: IR に金額フィールドと座標フィールドを持たせない。
// 金額はコードが単価を引いて Excel の数式が計算し、座標はレイアウトエンジンが決める。
func TestIR_HasNoMoneyOrCoordinateFields(t *testing.T) {
	// 部分一致で弾く語。
	bannedSubstrings := []string{
		"price", "cost", "amount", "usd", "jpy", "total",
		"width", "height", "position", "coord", "layout",
	}
	// 完全一致でのみ弾く語（座標）。
	bannedNames := []string{"x", "y"}
	// 為替レートと割引率は前提条件であって金額ではないため対象外。
	allowed := map[string]bool{"FxRate": true, "DiscountRate": true}

	types := []reflect.Type{
		reflect.TypeOf(ir.Architecture{}),
		reflect.TypeOf(ir.Resource{}),
		reflect.TypeOf(ir.Edge{}),
		reflect.TypeOf(ir.Assumptions{}),
	}
	for _, typ := range types {
		for i := range typ.NumField() {
			f := typ.Field(i)
			if allowed[f.Name] {
				continue
			}
			name := strings.ToLower(f.Name)
			for _, b := range bannedSubstrings {
				if strings.Contains(name, b) {
					t.Errorf("%s.%s は金額または座標を表すフィールドに見えます（FR-IR-9）", typ.Name(), f.Name)
				}
			}
			if slices.Contains(bannedNames, name) {
				t.Errorf("%s.%s は座標を表すフィールドに見えます（FR-IR-9）", typ.Name(), f.Name)
			}
		}
	}
}

func TestChildren_KeepsIROrder(t *testing.T) {
	a := &ir.Architecture{Resources: []ir.Resource{
		{ID: "vpc", Service: "vpc"},
		{ID: "b", Service: "ec2", Parent: "vpc"},
		{ID: "a", Service: "ec2", Parent: "vpc"},
		{ID: "top", Service: "s3"},
	}}
	got := ids(a.Children("vpc"))
	if want := []string{"b", "a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Children(vpc) = %v, want %v", got, want)
	}
	if got, want := ids(a.Children("")), []string{"vpc", "top"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Children(\"\") = %v, want %v", got, want)
	}
}

func ids(rs []*ir.Resource) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}
