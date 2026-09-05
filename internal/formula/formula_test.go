package formula_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/formula"
)

func TestEval(t *testing.T) {
	vars := map[string]float64{
		"count": 2, "hoursPerDay": 24, "daysPerMonth": 30, "ebsGb": 30, "gbPerMonth": 1000,
	}
	tests := []struct {
		src  string
		want float64
	}{
		{"count", 2},
		{"count * hoursPerDay * daysPerMonth", 1440},
		{"count * ebsGb", 60},
		{"1 + 2 * 3", 7},
		{"(1 + 2) * 3", 9},
		{"10 - 3 - 2", 5},
		{"100 / 4 / 5", 5},
		{"-count + 5", 3},
		{"gbPerMonth / 1024", 1000.0 / 1024},
		{"0.5 * count", 1},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			e, err := formula.Parse(tt.src)
			if err != nil {
				t.Fatalf("パースに失敗しました: %v", err)
			}
			got, err := e.Eval(vars)
			if err != nil {
				t.Fatalf("評価に失敗しました: %v", err)
			}
			if got != tt.want {
				t.Errorf("Eval(%q) = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestEval_Errors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		vars    map[string]float64
		wantSub string
	}{
		{"未定義の変数", "count * unknownVar", map[string]float64{"count": 1}, `変数 "unknownVar" に値がありません`},
		{"ゼロ除算", "count / zero", map[string]float64{"count": 1, "zero": 0}, "0 で除算しました"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := formula.Parse(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.Eval(tt.vars); err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("err = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func TestParse_Errors(t *testing.T) {
	for _, src := range []string{"", "1 +", "* 2", "(1 + 2", "1 + 2)", "1 $ 2", "1..2"} {
		t.Run(fmt.Sprintf("%q", src), func(t *testing.T) {
			if _, err := formula.Parse(src); err == nil {
				t.Errorf("パースがエラーになりませんでした: %q", src)
			}
		})
	}
}

func TestVars(t *testing.T) {
	e, err := formula.Parse("count * hoursPerDay * daysPerMonth + count")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"count", "daysPerMonth", "hoursPerDay"}
	if got := e.Vars(); !slices.Equal(got, want) {
		t.Errorf("Vars() = %v, want %v", got, want)
	}
}

// ADR-0013: 同じ AST から Excel の数式を作る。変数はセル参照に置き換わる。
func TestExcel(t *testing.T) {
	ref := func(name string) (string, error) {
		switch name {
		case "count":
			return "Estimate!$C$5", nil
		case "hoursPerDay":
			return "Assumptions!$B$2", nil
		case "daysPerMonth":
			return "Assumptions!$B$3", nil
		}
		return "", fmt.Errorf("参照先がありません: %q", name)
	}
	tests := []struct {
		src  string
		want string
	}{
		{"count * hoursPerDay * daysPerMonth", "Estimate!$C$5*Assumptions!$B$2*Assumptions!$B$3"},
		{"count * (hoursPerDay + daysPerMonth)", "Estimate!$C$5*(Assumptions!$B$2+Assumptions!$B$3)"},
		{"count / 2 * 3", "Estimate!$C$5/2*3"},
		{"count - (hoursPerDay - daysPerMonth)", "Estimate!$C$5-(Assumptions!$B$2-Assumptions!$B$3)"},
		{"-count + 1", "-Estimate!$C$5+1"},
		{"730", "730"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			e, err := formula.Parse(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			got, err := e.Excel(ref)
			if err != nil {
				t.Fatalf("Excel 数式化に失敗しました: %v", err)
			}
			if got != tt.want {
				t.Errorf("Excel(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestExcel_UnresolvedVar(t *testing.T) {
	e, err := formula.Parse("count * unknownVar")
	if err != nil {
		t.Fatal(err)
	}
	ref := func(name string) (string, error) {
		if name == "count" {
			return "A1", nil
		}
		return "", fmt.Errorf("参照先がありません: %q", name)
	}
	if _, err := e.Excel(ref); err == nil {
		t.Error("解決できない変数がエラーになりませんでした")
	}
}

// 同じ式から出した Go の評価結果と Excel 数式が同じ計算を表すことを、
// カッコの付き方を含めて確認する（ADR-0013 の Confirmation）。
func TestExcel_MatchesEvalStructure(t *testing.T) {
	vars := map[string]float64{"a": 7, "b": 3, "c": 2}
	ref := func(name string) (string, error) { return fmt.Sprintf("%v", vars[name]), nil }
	for _, src := range []string{
		"a - (b - c)", "a / (b * c)", "(a + b) * c", "a - b * c", "-a + b", "a / b / c",
	} {
		t.Run(src, func(t *testing.T) {
			e, err := formula.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			want, err := e.Eval(vars)
			if err != nil {
				t.Fatal(err)
			}
			// Excel 数式の変数を値に置き換えたものを、もう一度パースして評価する。
			rendered, err := e.Excel(ref)
			if err != nil {
				t.Fatal(err)
			}
			e2, err := formula.Parse(rendered)
			if err != nil {
				t.Fatalf("生成した数式 %q を再パースできませんでした: %v", rendered, err)
			}
			got, err := e2.Eval(nil)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%q: 数式 %q の値 %v が評価結果 %v と一致しません", src, rendered, got, want)
			}
		})
	}
}
