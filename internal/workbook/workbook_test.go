package workbook_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/cost"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/workbook"
)

var generatedAt = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

// stubSource は catalog の unit に合わせた固定単価を返す。
type stubSource struct {
	units  map[string]string
	prices map[string]float64
	tiered map[string]bool
	fail   map[string]bool
}

func (s *stubSource) Unit(_ context.Context, q pricing.PriceQuery) (pricing.Price, error) {
	key := q.Key()
	if s.fail[key] {
		return pricing.Price{}, &pricing.NotFoundError{Query: q}
	}
	amount, ok := s.prices[key]
	if !ok {
		amount = 0.1
	}
	return pricing.Price{
		Amount: amount, Currency: "USD", Unit: s.units[key],
		SKU: "SKU-" + q.Service, FetchedAt: generatedAt,
		Description: "テスト用の単価", Tiered: s.tiered[key], TierUpperBound: "51200",
	}, nil
}

// buildEstimate はサンプル IR から明細を作る。単価はスタブ（決定的にするため）。
func buildEstimate(t *testing.T, mutate func(*stubSource)) *cost.Estimate {
	t.Helper()
	cat, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	arch, err := ir.LoadFile("../../examples/ir/web-3tier.json")
	if err != nil {
		t.Fatal(err)
	}
	// driver ごとに期待される unit を拾って、それを返すスタブを作る。
	probe := &stubSource{units: map[string]string{}}
	first, err := cost.Build(context.Background(), arch, cat, probe)
	if err != nil {
		t.Fatal(err)
	}
	stub := &stubSource{
		units:  map[string]string{},
		prices: map[string]float64{},
		tiered: map[string]bool{},
		fail:   map[string]bool{},
	}
	for _, it := range first.Items {
		stub.units[it.Query.Key()] = it.Unit
		stub.prices[it.Query.Key()] = 0.05
	}
	if mutate != nil {
		mutate(stub)
	}
	est, err := cost.Build(context.Background(), arch, cat, stub)
	if err != nil {
		t.Fatal(err)
	}
	return est
}

func build(t *testing.T, est *cost.Estimate) *excelize.File {
	t.Helper()
	f, err := workbook.Build(est, workbook.Options{GeneratedAt: generatedAt})
	if err != nil {
		t.Fatalf("ワークブックの生成に失敗しました: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// FR-XLS-1: 3 シート構成。
func TestBuild_HasThreeSheets(t *testing.T) {
	f := build(t, buildEstimate(t, nil))
	want := []string{"Assumptions", "Prices", "Estimate"}
	got := f.GetSheetList()
	if len(got) != len(want) {
		t.Fatalf("シート = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("シート[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// FR-XLS-4 / PRIN-4: Estimate の金額セルはすべて数式。定数を焼き込まない。
func TestBuild_MoneyCellsAreFormulas(t *testing.T) {
	est := buildEstimate(t, nil)
	f := build(t, est)
	checked := 0
	// 明細行と合計行だけを見る（見出しや注記の行は対象外）。
	rows := make([]int, 0, len(est.Items)+1)
	for i := range est.Items {
		rows = append(rows, 5+i)
	}
	rows = append(rows, totalRow(t, f))
	for _, row := range rows {
		for _, col := range []string{"F", "G", "H"} { // 単価 / 月額(USD) / 月額(JPY)
			addr := col + strconv.Itoa(row)
			expr, err := f.GetCellFormula("Estimate", addr)
			if err != nil {
				t.Fatal(err)
			}
			if expr != "" {
				if !strings.HasPrefix(expr, "=") {
					t.Errorf("%s の数式が = で始まっていません: %q", addr, expr)
				}
				checked++
				continue
			}
			// 数式でないなら、空セル（単価を取得できなかった行）でなければならない。
			value, err := f.GetCellValue("Estimate", addr, excelize.Options{RawCellValue: true})
			if err != nil {
				t.Fatal(err)
			}
			if value != "" {
				t.Errorf("%s に定数 %q が入っています（金額セルは数式のみ）", addr, value)
			}
		}
	}
	if checked == 0 {
		t.Fatal("金額セルが 1 つも見つかりませんでした")
	}
}

// FR-XLS-3: Prices シートの各行に SKU・取得日・リージョンが埋まっている。
func TestBuild_PricesHasSKUAndFetchedAt(t *testing.T) {
	est := buildEstimate(t, nil)
	f := build(t, est)
	for i := range est.Items {
		row := 5 + i
		for col, name := range map[string]string{"E": "リージョン", "F": "SKU", "J": "取得日"} {
			v, err := f.GetCellValue("Prices", col+strconv.Itoa(row))
			if err != nil {
				t.Fatal(err)
			}
			if v == "" {
				t.Errorf("Prices!%s%d（%s）が空です", col, row, name)
			}
		}
	}
}

// FR-XLS-5 / FR-XLS-6: Assumptions を書き換えると Estimate の合計が変わる。
func TestBuild_TotalFollowsAssumptions(t *testing.T) {
	est := buildEstimate(t, nil)
	f := build(t, est)

	totalAddr := "G" + strconv.Itoa(totalRow(t, f))
	before := calcFloat(t, f, "Estimate", totalAddr)
	if before <= 0 {
		t.Fatalf("合計 = %v, want 正の値", before)
	}

	// 稼働時間を半分にすると、時間課金の行が減って合計も下がる。
	hours := assumptionCell(t, f, "hoursPerDay")
	if err := f.SetCellValue("Assumptions", hours, 12); err != nil {
		t.Fatal(err)
	}
	after := calcFloat(t, f, "Estimate", totalAddr)
	if after >= before {
		t.Errorf("hoursPerDay を 24→12 にしても合計が減りませんでした: %v → %v", before, after)
	}

	// 割引率を 0.2 にすると合計はさらに減る。
	discount := assumptionCell(t, f, "discountRate")
	if err := f.SetCellValue("Assumptions", discount, 0.2); err != nil {
		t.Fatal(err)
	}
	discounted := calcFloat(t, f, "Estimate", totalAddr)
	if diff := after*0.8 - discounted; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("割引率 0.2 の合計 = %v, want %v", discounted, after*0.8)
	}
}

// 円換算は為替レートのセルを参照する。
func TestBuild_YenUsesFxRate(t *testing.T) {
	f := build(t, buildEstimate(t, nil))
	row := totalRow(t, f)
	usd := calcFloat(t, f, "Estimate", "G"+strconv.Itoa(row))
	jpy := calcFloat(t, f, "Estimate", "H"+strconv.Itoa(row))
	if diff := usd*150 - jpy; diff > 1e-3 || diff < -1e-3 {
		t.Errorf("円換算 = %v, want %v（fxRate 150）", jpy, usd*150)
	}
}

// FR-PRC-7: 単価を取得できなかった行があっても Excel は生成でき、
// その行は金額セルが空で、理由が書かれる。
func TestBuild_FailedRowIsMarked(t *testing.T) {
	var failedKey string
	est := buildEstimate(t, func(s *stubSource) {
		for key := range s.units {
			if failedKey == "" || key < failedKey {
				failedKey = key
			}
		}
		s.fail[failedKey] = true
	})
	if len(est.Failed()) == 0 {
		t.Fatal("失敗した行が作れませんでした")
	}
	f := build(t, est)

	failedIndex := -1
	for i, it := range est.Items {
		if !it.OK() {
			failedIndex = i
			break
		}
	}
	row := strconv.Itoa(5 + failedIndex)
	for _, col := range []string{"F", "G", "H"} {
		v, err := f.GetCellValue("Estimate", col+row, excelize.Options{RawCellValue: true})
		if err != nil {
			t.Fatal(err)
		}
		if v != "" {
			t.Errorf("取得失敗した行の %s%s に値 %q が入っています", col, row, v)
		}
	}
	note, err := f.GetCellValue("Estimate", "I"+row)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "単価を取得できなかった") {
		t.Errorf("備考 = %q, want 取得失敗の理由", note)
	}
	// 合計は残りの行で計算できる。
	if v := calcFloat(t, f, "Estimate", "G"+strconv.Itoa(totalRow(t, f))); v <= 0 {
		t.Errorf("合計 = %v, want 正の値（失敗行以外は計上される）", v)
	}
}

// FR-XLS-7 / FR-XLS-8: 未計上の項目とオンデマンドの注記が入る。
func TestBuild_Notes(t *testing.T) {
	est := buildEstimate(t, func(s *stubSource) {
		for key := range s.units {
			s.tiered[key] = true
		}
	})
	f := build(t, est)
	rows, err := f.GetRows("Estimate")
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, r := range rows {
		text.WriteString(strings.Join(r, " "))
		text.WriteString("\n")
	}
	all := text.String()
	for _, want := range []string{
		"オンデマンド単価に基づく", "Savings Plans",
		"未計上の項目", "NAT Gateway", "段階課金の上位階層",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("Estimate シートに %q が含まれていません", want)
		}
	}
}

// 数量も数式にする。台数を変えれば数量と金額が追随する。
func TestBuild_QuantityIsFormula(t *testing.T) {
	est := buildEstimate(t, nil)
	f := build(t, est)
	for i, it := range est.Items {
		addr := "E" + strconv.Itoa(5+i)
		got, err := f.GetCellFormula("Estimate", addr)
		if err != nil {
			t.Fatal(err)
		}
		if got == "" {
			t.Errorf("%s（%s/%s の数量）が数式ではありません", addr, it.ResourceID, it.DriverID)
			continue
		}
		if !strings.Contains(got, "Assumptions!") {
			t.Errorf("%s の数量が Assumptions を参照していません: %q", addr, got)
		}
	}
}

// ---- ヘルパー ----

func calcFloat(t *testing.T, f *excelize.File, sheet, addr string) float64 {
	t.Helper()
	v, err := f.CalcCellValue(sheet, addr, excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatalf("%s!%s を計算できませんでした: %v", sheet, addr, err)
	}
	got, err := strconv.ParseFloat(v, 64)
	if err != nil {
		t.Fatalf("%s!%s = %q を数値として読めませんでした", sheet, addr, v)
	}
	return got
}

// totalRow は Estimate シートの合計行を探す。
func totalRow(t *testing.T, f *excelize.File) int {
	t.Helper()
	rows, err := f.GetRows("Estimate")
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		if len(r) > 0 && r[0] == "合計" {
			return i + 1
		}
	}
	t.Fatal("合計行が見つかりませんでした")
	return 0
}

// assumptionCell は Assumptions シートの指定した項目の値セルを探す。
func assumptionCell(t *testing.T, f *excelize.File, name string) string {
	t.Helper()
	rows, err := f.GetRows("Assumptions")
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		if len(r) > 0 && r[0] == name {
			return "B" + strconv.Itoa(i+1)
		}
	}
	t.Fatalf("Assumptions に %q がありません", name)
	return ""
}
