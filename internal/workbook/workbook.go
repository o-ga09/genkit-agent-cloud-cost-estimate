package workbook

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/cost"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/formula"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

// Options は生成時の設定。
type Options struct {
	// GeneratedAt はワークブックに書く生成日時。空なら現在時刻。
	GeneratedAt time.Time
}

// Build は見積もり明細から 3 シート構成のワークブックを組み立てる（FR-XLS-1）。
func Build(est *cost.Estimate, opts Options) (*excelize.File, error) {
	if opts.GeneratedAt.IsZero() {
		opts.GeneratedAt = time.Now().UTC()
	}
	b := &builder{
		f:              excelize.NewFile(),
		est:            est,
		opts:           opts,
		assumptionCell: map[string]string{},
		paramCell:      map[string]map[string]string{},
		priceRow:       map[int]int{},
	}
	if err := b.build(); err != nil {
		return nil, err
	}
	return b.f, nil
}

// Save は生成したワークブックをファイルに書き出す。
func Save(path string, est *cost.Estimate, opts Options) error {
	f, err := Build(est, opts)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.SaveAs(path); err != nil {
		return fmt.Errorf("ワークブックを保存できませんでした: %w", err)
	}
	return nil
}

type builder struct {
	f    *excelize.File
	est  *cost.Estimate
	opts Options

	st styles
	// assumptionCell は前提条件の変数名 → 絶対参照（"Assumptions!$B$6"）。
	assumptionCell map[string]string
	// paramCell はリソース ID → パラメータ名 → 絶対参照。
	paramCell map[string]map[string]string
	// priceRow は明細の添字 → Prices シートの行番号。
	priceRow map[int]int
	// discountRef と fxRef は金額の数式が参照するセル。
	discountRef string
	fxRef       string

	// err は組み立て中に起きた最初のエラー。
	err error
}

func (b *builder) build() error {
	if err := b.initSheets(); err != nil {
		return err
	}
	if err := b.writeAssumptions(); err != nil {
		return err
	}
	if err := b.writePrices(); err != nil {
		return err
	}
	if err := b.writeEstimate(); err != nil {
		return err
	}
	return b.err
}

func (b *builder) initSheets() error {
	if err := b.f.SetSheetName("Sheet1", SheetAssumptions); err != nil {
		return err
	}
	for _, name := range []string{SheetPrices, SheetEstimate} {
		if _, err := b.f.NewSheet(name); err != nil {
			return err
		}
	}
	var err error
	b.st, err = newStyles(b.f)
	return err
}

// ---- Assumptions ----

// assumptionDoc は前提条件の説明。Excel の利用者に何を書き換えてよいか伝える。
var assumptionDoc = map[string]string{
	"hoursPerDay":      "1 日あたりの稼働時間",
	"daysPerMonth":     "1 か月あたりの稼働日数",
	"requestsPerMonth": "月間リクエスト数",
	"fxRate":           "為替レート（USD/JPY）。円換算に使う",
	"discountRate":     "割引率（0〜1）。RI / Savings Plans の見込みをここで表現する",
}

func (b *builder) writeAssumptions() error {
	s := SheetAssumptions
	b.setCell(s, "A1", "見積もりの前提条件", b.st.title)
	b.setCell(s, "A2", "黄色のセルは編集できる。値を書き換えると Estimate シートの金額が再計算される。", b.st.note)

	row := 4
	b.setCell(s, cell("A", row), "全体の前提", b.st.section)
	row++
	b.setRow(s, row, b.st.header, "項目", "値", "説明")
	row++

	vars := b.est.Architecture.Assumptions.Vars()
	names := ir.AssumptionVarNames()
	names = append(names, extraNames(b.est.Architecture.Assumptions)...)
	for _, name := range names {
		b.setCell(s, cell("A", row), name, b.st.label)
		b.setCell(s, cell("B", row), vars[name], b.st.input)
		b.setCell(s, cell("C", row), assumptionDoc[name], b.st.note)
		b.assumptionCell[name] = ref(s, "B", row)
		row++
	}
	b.discountRef = b.assumptionCell["discountRate"]
	b.fxRef = b.assumptionCell["fxRate"]

	row++
	b.setCell(s, cell("A", row), "リソースごとのパラメータ", b.st.section)
	row++
	b.setRow(s, row, b.st.header, "リソース", "パラメータ", "値", "説明")
	row++

	for _, r := range b.resources() {
		params := b.paramsOf(r)
		for _, name := range slices.Sorted(maps.Keys(params)) {
			b.setCell(s, cell("A", row), r, b.st.label)
			b.setCell(s, cell("B", row), name, b.st.label)
			b.setCell(s, cell("C", row), params[name], b.st.input)
			b.setCell(s, cell("D", row), "", b.st.note)
			if b.paramCell[r] == nil {
				b.paramCell[r] = map[string]string{}
			}
			b.paramCell[r][name] = ref(s, "C", row)
			row++
		}
	}

	b.setColWidths(s, map[string]float64{"A": 22, "B": 22, "C": 16, "D": 46})
	return nil
}

// resources は明細に出てくるリソース ID を、明細の順序のまま重複なく返す。
func (b *builder) resources() []string {
	var out []string
	seen := map[string]bool{}
	for _, it := range b.est.Items {
		if !seen[it.ResourceID] {
			seen[it.ResourceID] = true
			out = append(out, it.ResourceID)
		}
	}
	return out
}

// paramsOf はリソースの数値パラメータを返す。数量の式が参照できるのは数値だけ。
func (b *builder) paramsOf(resourceID string) map[string]float64 {
	out := map[string]float64{}
	for _, it := range b.est.Items {
		if it.ResourceID != resourceID {
			continue
		}
		for name, v := range it.Params {
			if f, ok := cost.Numeric(v); ok {
				out[name] = f
			}
		}
	}
	return out
}

func extraNames(a ir.Assumptions) []string {
	return slices.Sorted(maps.Keys(a.Extra))
}

// ---- Prices ----

func (b *builder) writePrices() error {
	s := SheetPrices
	b.setCell(s, "A1", "単価（AWS Price List API から取得）", b.st.title)
	b.setCell(s, "A2", "SKU と取得日を載せているので、疑わしい行だけを AWS の料金ページと突き合わせられる。", b.st.note)

	row := 4
	b.setRow(s, row, b.st.header,
		"#", "リソース", "項目", "serviceCode", "リージョン", "SKU",
		"単価", "通貨", "単位", "取得日 (UTC)", "説明", "備考")
	row++

	for i, it := range b.est.Items {
		b.setCell(s, cell("A", row), i+1, b.st.label)
		b.setCell(s, cell("B", row), it.ResourceID, b.st.label)
		b.setCell(s, cell("C", row), it.DriverID, b.st.label)
		b.setCell(s, cell("D", row), it.Query.Service, b.st.label)
		b.setCell(s, cell("E", row), regionLabel(it), b.st.label)
		if it.OK() {
			b.setCell(s, cell("F", row), it.Price.SKU, b.st.label)
			b.setCell(s, cell("G", row), it.Price.Amount, b.st.unitPrice)
			b.setCell(s, cell("H", row), it.Price.Currency, b.st.label)
			b.setCell(s, cell("I", row), it.Price.Unit, b.st.label)
			b.setCell(s, cell("J", row), it.Price.FetchedAt.UTC().Format(time.RFC3339), b.st.label)
			b.setCell(s, cell("K", row), it.Price.Description, b.st.note)
			b.setCell(s, cell("L", row), tierNote(it), b.st.note)
		} else {
			b.setCell(s, cell("L", row), "単価を取得できなかった: "+it.Err.Error(), b.st.warn)
		}
		b.priceRow[i] = row
		row++
	}

	b.setColWidths(s, map[string]float64{
		"A": 4, "B": 16, "C": 18, "D": 18, "E": 16, "F": 20,
		"G": 14, "H": 6, "I": 10, "J": 22, "K": 52, "L": 42,
	})
	return nil
}

func regionLabel(it cost.LineItem) string {
	if it.Query.Region == "" {
		return "グローバル"
	}
	return it.Query.Region
}

func tierNote(it cost.LineItem) string {
	if !it.Price.Tiered {
		return ""
	}
	return fmt.Sprintf("段階課金の第 1 階層の単価（〜%s %s）。上位の階層は未計上",
		it.Price.TierUpperBound, it.Price.Unit)
}

// ---- Estimate ----

func (b *builder) writeEstimate() error {
	s := SheetEstimate
	arch := b.est.Architecture
	b.setCell(s, "A1", fmt.Sprintf("月額見積もり（%s / オンデマンド）", arch.Region), b.st.title)
	b.setCell(s, "A2", "生成日時: "+b.opts.GeneratedAt.UTC().Format(time.RFC3339), b.st.note)

	row := 4
	b.setRow(s, row, b.st.header,
		"リソース", "サービス", "項目", "単位", "数量", "単価 (USD)", "月額 (USD)", "月額 (JPY)", "備考")
	row++

	first := row
	for i, it := range b.est.Items {
		b.setCell(s, cell("A", row), itemLabel(it), b.st.label)
		b.setCell(s, cell("B", row), it.Service, b.st.label)
		b.setCell(s, cell("C", row), it.DriverID, b.st.label)
		b.setCell(s, cell("D", row), it.Unit, b.st.label)

		note := ""
		if qty, err := b.quantityFormula(it); err == nil {
			b.setFormula(s, cell("E", row), qty, b.st.quantity)
		} else {
			// 式をセル参照に写せない場合だけ、評価済みの数量を定数で置く。
			b.setCell(s, cell("E", row), it.Quantity, b.st.quantity)
			note = "数量を数式にできなかった: " + err.Error()
		}

		switch {
		case it.OK():
			b.setFormula(s, cell("F", row), fmt.Sprintf("%s!G%d", SheetPrices, b.priceRow[i]), b.st.unitPrice)
			b.setFormula(s, cell("G", row),
				fmt.Sprintf("E%d*F%d*(1-%s)", row, row, b.discountRef), b.st.money)
			b.setFormula(s, cell("H", row), fmt.Sprintf("G%d*%s", row, b.fxRef), b.st.yen)
			if n := tierNote(it); n != "" {
				note = joinNotes(note, n)
			}
		default:
			// 金額セルは空のままにする。定数を焼き込まない（PRIN-4）。
			note = joinNotes(note, "単価を取得できなかった: "+it.Err.Error())
		}
		b.setCell(s, cell("I", row), note, b.st.warn)
		row++
	}
	last := row - 1
	row++
	b.setCell(s, cell("A", row), "合計", b.st.section)
	if last >= first {
		b.setFormula(s, cell("G", row), fmt.Sprintf("SUM(G%d:G%d)", first, last), b.st.total)
		b.setFormula(s, cell("H", row), fmt.Sprintf("SUM(H%d:H%d)", first, last), b.st.totalYen)
	}

	row += 2
	b.setCell(s, cell("A", row), "注記", b.st.section)
	row++
	b.setCell(s, cell("A", row), onDemandNote, b.st.note)
	row += 2

	b.setCell(s, cell("A", row), "未計上の項目（この見積もりに含まれていないコスト）", b.st.section)
	row++
	for _, n := range b.notAccountedItems() {
		b.setCell(s, cell("A", row), "・"+n, b.st.note)
		row++
	}

	b.setColWidths(s, map[string]float64{
		"A": 26, "B": 14, "C": 18, "D": 10, "E": 14, "F": 14, "G": 14, "H": 14, "I": 52,
	})
	return nil
}

func itemLabel(it cost.LineItem) string {
	if it.Label != "" && it.Label != it.ResourceID {
		return fmt.Sprintf("%s (%s)", it.ResourceID, it.Label)
	}
	return it.ResourceID
}

func joinNotes(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " / " + b
	}
}

// quantityFormula は数量の式を、Assumptions シートのセル参照に写した数式にする。
func (b *builder) quantityFormula(it cost.LineItem) (string, error) {
	expr, err := formula.Parse(it.Formula)
	if err != nil {
		return "", err
	}
	return expr.Excel(func(name string) (string, error) {
		if cells, ok := b.paramCell[it.ResourceID]; ok {
			if c, ok := cells[name]; ok {
				return c, nil
			}
		}
		if c, ok := b.assumptionCell[name]; ok {
			return c, nil
		}
		return "", fmt.Errorf("変数 %q に対応する入力セルがありません", name)
	})
}

// notAccountedItems は未計上の項目に、この見積もり固有の事情を足して返す。
func (b *builder) notAccountedItems() []string {
	out := slices.Clone(notAccounted)
	for _, it := range b.est.Items {
		if it.OK() && it.Price.Tiered {
			out = append(out, fmt.Sprintf("%s / %s の段階課金の上位階層（第 1 階層 〜%s %s の単価で計算している）",
				it.ResourceID, it.DriverID, it.Price.TierUpperBound, it.Price.Unit))
		}
	}
	for _, it := range b.est.Failed() {
		out = append(out, fmt.Sprintf("%s / %s（単価を取得できなかったため未計上）", it.ResourceID, it.DriverID))
	}
	return out
}
