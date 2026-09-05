package workbook

import (
	"fmt"
	"maps"
	"slices"

	"github.com/xuri/excelize/v2"
)

// styles はシートで使う書式。入力セルを色分けして編集可能であることを示す（FR-XLS-2）。
type styles struct {
	title     int
	section   int
	header    int
	label     int
	note      int
	warn      int
	input     int
	quantity  int
	unitPrice int
	money     int
	yen       int
	total     int
	totalYen  int
}

const (
	inputFill  = "FFF2CC" // 編集できる入力セル（黄色）
	headerFill = "E7E6E6"
	moneyFmt   = "#,##0.00"
	yenFmt     = "#,##0"
	qtyFmt     = "#,##0.###"
	priceFmt   = "0.00000000"
)

func newStyles(f *excelize.File) (styles, error) {
	var st styles
	var err error

	def := func(s *excelize.Style) int {
		if err != nil {
			return 0
		}
		var id int
		id, err = f.NewStyle(s)
		return id
	}
	border := []excelize.Border{
		{Type: "left", Color: "BFBFBF", Style: 1},
		{Type: "right", Color: "BFBFBF", Style: 1},
		{Type: "top", Color: "BFBFBF", Style: 1},
		{Type: "bottom", Color: "BFBFBF", Style: 1},
	}
	fill := func(color string) excelize.Fill {
		return excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{color}}
	}

	st.title = def(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	st.section = def(&excelize.Style{Font: &excelize.Font{Bold: true}})
	st.header = def(&excelize.Style{
		Font: &excelize.Font{Bold: true}, Fill: fill(headerFill), Border: border,
	})
	st.label = def(&excelize.Style{Border: border})
	st.note = def(&excelize.Style{Font: &excelize.Font{Color: "595959"}})
	st.warn = def(&excelize.Style{Font: &excelize.Font{Color: "C00000"}})
	st.input = def(&excelize.Style{
		Fill: fill(inputFill), Border: border, Font: &excelize.Font{Bold: true},
		CustomNumFmt: ptr(qtyFmt),
	})
	st.quantity = def(&excelize.Style{Border: border, CustomNumFmt: ptr(qtyFmt)})
	st.unitPrice = def(&excelize.Style{Border: border, CustomNumFmt: ptr(priceFmt)})
	st.money = def(&excelize.Style{Border: border, CustomNumFmt: ptr(moneyFmt)})
	st.yen = def(&excelize.Style{Border: border, CustomNumFmt: ptr(yenFmt)})
	st.total = def(&excelize.Style{
		Font: &excelize.Font{Bold: true}, Border: border, CustomNumFmt: ptr(moneyFmt),
	})
	st.totalYen = def(&excelize.Style{
		Font: &excelize.Font{Bold: true}, Border: border, CustomNumFmt: ptr(yenFmt),
	})
	if err != nil {
		return styles{}, fmt.Errorf("書式を作成できませんでした: %w", err)
	}
	return st, nil
}

func ptr[T any](v T) *T { return &v }

// ---- セル操作のヘルパー ----

func cell(col string, row int) string { return fmt.Sprintf("%s%d", col, row) }

// ref はシートをまたいで参照する絶対参照を返す。
func ref(sheet, col string, row int) string {
	return fmt.Sprintf("%s!$%s$%d", sheet, col, row)
}

// fail は最初のエラーだけを覚える。書き込みの都度エラーを返すと
// シートを組み立てるコードが読めなくなるため、まとめて build() で返す。
func (b *builder) fail(err error) {
	if err != nil && b.err == nil {
		b.err = err
	}
}

func (b *builder) setCell(sheet, addr string, value any, style int) {
	if err := b.f.SetCellValue(sheet, addr, value); err != nil {
		b.fail(fmt.Errorf("%s!%s に値を書けませんでした: %w", sheet, addr, err))
		return
	}
	b.applyStyle(sheet, addr, style)
}

func (b *builder) setFormula(sheet, addr, expr string, style int) {
	if err := b.f.SetCellFormula(sheet, addr, "="+expr); err != nil {
		b.fail(fmt.Errorf("%s!%s に数式を書けませんでした: %w", sheet, addr, err))
		return
	}
	b.applyStyle(sheet, addr, style)
}

func (b *builder) applyStyle(sheet, addr string, style int) {
	if style == 0 {
		return
	}
	b.fail(b.f.SetCellStyle(sheet, addr, addr, style))
}

// setRow は 1 行分の値を A 列から順に書く。
func (b *builder) setRow(sheet string, row, style int, values ...any) {
	for i, v := range values {
		col, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			b.fail(err)
			return
		}
		b.setCell(sheet, cell(col, row), v, style)
	}
}

func (b *builder) setColWidths(sheet string, widths map[string]float64) {
	for _, col := range slices.Sorted(maps.Keys(widths)) {
		b.fail(b.f.SetColWidth(sheet, col, col, widths[col]))
	}
}
