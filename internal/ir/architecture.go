// Package ir は構成の中間表現（Intermediate Representation）を定義する。
//
// Architecture は構成図・見積もり Excel・drawio XML すべての正本であり、
// 成果物の生成は LLM を通さずにこの型だけから決定的に行われる（ADR-0002）。
//
// この型に金額フィールドと座標フィールドを持たせてはならない。
// 金額はコードが単価を引いて Excel の数式が計算し、座標はレイアウトエンジンが
// 計算する（ADR-0001 / PRIN-1 / PRIN-2）。
package ir

// SchemaVersion は現在の IR スキーマのバージョン。
// 保存済み IR の移行判断に使うため、JSON にも書き出す。
const SchemaVersion = "1"

// Provider はクラウドプロバイダ。
type Provider string

const (
	ProviderAWS Provider = "aws"
	ProviderGCP Provider = "gcp"
)

// Architecture は構成そのもの。全成果物の唯一の正本。
type Architecture struct {
	// SchemaVersion は IR のスキーマ版。読み込み時に空なら現行版として扱うため、
	// 入力では省略できる（LLM に出力させるスキーマからも外れる）。
	// 保存するときは Encode が必ず埋める。
	SchemaVersion string      `json:"schemaVersion,omitempty"`
	Provider      Provider    `json:"provider"`
	Region        string      `json:"region"`
	Assumptions   Assumptions `json:"assumptions"`
	Resources     []Resource  `json:"resources"`
	Edges         []Edge      `json:"edges"`
}

// Resource は構成に含まれる 1 つのリソース。
type Resource struct {
	// ID は構成内で一意な識別子。図のノードキーにもなる。
	ID string `json:"id"`
	// Service は catalog が定義するサービス名。値域は catalog に制約される。
	Service string `json:"service"`
	// Label は図に出す表示名。空なら catalog の表示名にフォールバックする。
	Label string `json:"label,omitempty"`
	// Parent は入れ子の親リソース ID（VPC / AZ など）。空可。
	Parent string `json:"parent,omitempty"`
	// Params は catalog の params 定義でバリデーションされるパラメータ。
	Params map[string]any `json:"params,omitempty"`
}

// Edge はリソース間の接続。
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

// Assumptions は見積もりの前提条件。Excel の Assumptions シートの入力セルと
// 1:1 で対応する（FR-IR-7 / FR-XLS-2）。
type Assumptions struct {
	// HoursPerDay は 1 日あたりの稼働時間。
	HoursPerDay float64 `json:"hoursPerDay"`
	// DaysPerMonth は 1 か月あたりの稼働日数。
	DaysPerMonth float64 `json:"daysPerMonth"`
	// RequestsPerMonth は月間リクエスト数。
	RequestsPerMonth float64 `json:"requestsPerMonth"`
	// FxRate は USD/JPY の為替レート。
	FxRate float64 `json:"fxRate"`
	// DiscountRate は 0.0〜1.0 の割引率。RI / Savings Plans の効果は
	// 正確な価格計算ではなくこのセルで表現する（ADR-0009）。
	DiscountRate float64 `json:"discountRate"`
	// Extra は上記に収まらない前提条件。Excel の入力セルとして追加される。
	Extra map[string]float64 `json:"extra,omitempty"`
}

// ResourceByID は指定 ID のリソースを返す。
func (a *Architecture) ResourceByID(id string) (*Resource, bool) {
	for i := range a.Resources {
		if a.Resources[i].ID == id {
			return &a.Resources[i], true
		}
	}
	return nil, false
}

// Children は親 ID を持つ子リソースを、IR での出現順で返す。
// parent が空文字列ならトップレベルのリソースを返す。
func (a *Architecture) Children(parent string) []*Resource {
	var out []*Resource
	for i := range a.Resources {
		if a.Resources[i].Parent == parent {
			out = append(out, &a.Resources[i])
		}
	}
	return out
}

// AssumptionVarNames は Assumptions が式に供給する変数名（JSON フィールド名）を返す。
// catalog の quantity_formula はこの名前で前提条件を参照する（ADR-0013）。
func AssumptionVarNames() []string {
	return []string{"hoursPerDay", "daysPerMonth", "requestsPerMonth", "fxRate", "discountRate"}
}

// Vars は式エンジンに渡す変数表を返す。Extra のキーもそのまま変数になる。
func (a Assumptions) Vars() map[string]float64 {
	vars := map[string]float64{
		"hoursPerDay":      a.HoursPerDay,
		"daysPerMonth":     a.DaysPerMonth,
		"requestsPerMonth": a.RequestsPerMonth,
		"fxRate":           a.FxRate,
		"discountRate":     a.DiscountRate,
	}
	for k, v := range a.Extra {
		vars[k] = v
	}
	return vars
}
