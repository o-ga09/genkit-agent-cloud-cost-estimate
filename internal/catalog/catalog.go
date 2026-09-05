// Package catalog はサービスごとの表示情報・パラメータ定義・コストモデルを
// YAML から読み込む（ADR-0005）。
//
// catalog は 3 つの役割を兼ねる。
//   - Resource.Service の値域（enum）の定義
//   - Resource.Params のバリデーションスキーマ
//   - 図に当てるアイコンと表示名の供給元
//
// price_query は「コードが組み立てて使う」ものであり、LLM には渡さない（PRIN-3）。
package catalog

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/formula"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
)

// price_query の予約キー。これ以外のキーは Price List API の属性名として扱う。
const (
	keyServiceCode = "serviceCode"
	keyScope       = "scope"

	scopeRegional = "regional"
	scopeGlobal   = "global"
)

// builtinVars は params 以外に price_query のテンプレートで使える変数。
var builtinVars = map[string]bool{
	"region":         true, // "ap-northeast-1"
	"regionLocation": true, // "Asia Pacific (Tokyo)"
}

// templatePattern は price_query の "{{name}}" を取り出す。
var templatePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

//go:embed data/*.yaml
var builtinFS embed.FS

// Param は catalog の params 定義 1 件。
type Param struct {
	Type     ir.ParamType `yaml:"type"`
	Required bool         `yaml:"required"`
	Default  any          `yaml:"default"`
	Enum     []string     `yaml:"enum"`
	Doc      string       `yaml:"doc"`
}

// Driver はコスト要素 1 件。1 driver が Excel の 1 行になる。
type Driver struct {
	ID   string `yaml:"id"`
	Unit string `yaml:"unit"`
	Doc  string `yaml:"doc"`
	// When はこの driver が適用される条件。Resource.params の値がすべて一致した
	// ときだけ計上する。空なら常に適用する。
	When map[string]string `yaml:"when"`
	// PriceQuery は Price List API に渡すフィルタ。予約キー serviceCode と scope を除き、
	// キーは属性名、値は "{{param}}" テンプレートまたは定数。LLM はここに触れない（PRIN-3）。
	//
	// 値はスカラー（完全一致）か、{contains: "..."}（部分一致）で書く。
	// 部分一致は usagetype のようにリージョン接頭辞が付く属性で使う。
	PriceQuery map[string]FilterSpec `yaml:"price_query"`
	// QuantityFormula は数量の式。変数は Resource.params と Assumptions の
	// JSON フィールド名で参照する（ADR-0013）。
	QuantityFormula string `yaml:"quantity_formula"`

	quantity *formula.Expr
}

// Quantity はパース済みの数量式を返す。catalog のロード時にパースされている。
func (d Driver) Quantity() *formula.Expr { return d.quantity }

// ServiceCode は Price List API のサービスコードを返す。
func (d Driver) ServiceCode() string { return d.PriceQuery[keyServiceCode].Value }

// Global は region を指定せずに問い合わせる driver かどうかを返す。
// データ転送のようにリージョン別ではないサービスで true になる。
func (d Driver) Global() bool { return d.PriceQuery[keyScope].Value == scopeGlobal }

// Filters は price_query から予約キーを除いた、属性名とフィルタの対を返す。
func (d Driver) Filters() map[string]FilterSpec {
	out := make(map[string]FilterSpec, len(d.PriceQuery))
	for k, v := range d.PriceQuery {
		if k == keyServiceCode || k == keyScope {
			continue
		}
		out[k] = v
	}
	return out
}

// AppliesTo は When の条件が Resource.params を満たすかを返す。
func (d Driver) AppliesTo(params map[string]any) bool {
	for k, want := range d.When {
		got, ok := params[k]
		if !ok {
			return false
		}
		if s, isStr := got.(string); !isStr || s != want {
			return false
		}
	}
	return true
}

// FilterSpec は price_query の値 1 件。
type FilterSpec struct {
	Match pricing.Match
	Value string
}

// UnmarshalYAML はスカラーと {contains: "..."} の両方を受け付ける。
func (f *FilterSpec) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		f.Match = pricing.MatchEquals
		return node.Decode(&f.Value)
	case yaml.MappingNode:
		var m map[string]string
		if err := node.Decode(&m); err != nil {
			return err
		}
		if len(m) != 1 {
			return fmt.Errorf("フィルタは {contains: \"...\"} の形で 1 つだけ書く")
		}
		for k, v := range m {
			if k != string(pricing.MatchContains) {
				return fmt.Errorf("未対応のフィルタ条件です: %q（使えるのは contains）", k)
			}
			f.Match = pricing.MatchContains
			f.Value = v
		}
		return nil
	default:
		return fmt.Errorf("フィルタの値が不正です")
	}
}

// Service は 1 サービス分の定義。1 ファイル 1 サービス。
type Service struct {
	Service string           `yaml:"service"`
	Display string           `yaml:"display"`
	Icon    string           `yaml:"icon"`
	Doc     string           `yaml:"doc"`
	Params  map[string]Param `yaml:"params"`
	Drivers []Driver         `yaml:"drivers"`
}

// Catalog はサービス定義の集合。
type Catalog struct {
	services map[string]Service
}

// Builtin はリポジトリに同梱された catalog を読み込む。
func Builtin() (*Catalog, error) {
	sub, err := fs.Sub(builtinFS, "data")
	if err != nil {
		return nil, err
	}
	return Load(sub)
}

// Load は指定の FS 直下にある *.yaml を読み込む。
func Load(fsys fs.FS) (*Catalog, error) {
	entries, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	slices.Sort(entries)

	c := &Catalog{services: make(map[string]Service, len(entries))}
	for _, name := range entries {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("catalog %s の読み込みに失敗しました: %w", name, err)
		}
		var svc Service
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true)
		if err := dec.Decode(&svc); err != nil {
			return nil, fmt.Errorf("catalog %s のパースに失敗しました: %w", name, err)
		}
		if err := svc.compile(name); err != nil {
			return nil, err
		}
		if _, dup := c.services[svc.Service]; dup {
			return nil, fmt.Errorf("catalog %s: サービス名が重複しています: %q", name, svc.Service)
		}
		c.services[svc.Service] = svc
	}
	if len(c.services) == 0 {
		return nil, fmt.Errorf("catalog に 1 件も定義がありません")
	}
	return c, nil
}

// compile は定義を検証し、quantity_formula をパースして保持する。
func (s *Service) compile(file string) error {
	if s.Service == "" {
		return fmt.Errorf("catalog %s: service は必須です", file)
	}
	if want := s.Service + ".yaml"; path.Base(file) != want {
		return fmt.Errorf("catalog %s: ファイル名は %q である必要があります", file, want)
	}
	if s.Display == "" {
		return fmt.Errorf("catalog %s: display は必須です", file)
	}
	for name, p := range s.Params {
		switch p.Type {
		case ir.ParamString, ir.ParamInt, ir.ParamFloat, ir.ParamBool:
		default:
			return fmt.Errorf("catalog %s: params.%s.type が不正です: %q", file, name, p.Type)
		}
		if len(p.Enum) > 0 && p.Type != ir.ParamString {
			return fmt.Errorf("catalog %s: params.%s に enum を使えるのは string 型だけです", file, name)
		}
	}
	seen := make(map[string]bool, len(s.Drivers))
	for i := range s.Drivers {
		d := &s.Drivers[i]
		if d.ID == "" {
			return fmt.Errorf("catalog %s: driver の id は必須です", file)
		}
		if seen[d.ID] {
			return fmt.Errorf("catalog %s: driver の id が重複しています: %q", file, d.ID)
		}
		seen[d.ID] = true
		if err := s.compileDriver(file, d); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) compileDriver(file string, d *Driver) error {
	where := fmt.Sprintf("catalog %s: driver %q", file, d.ID)
	if d.Unit == "" {
		return fmt.Errorf("%s: unit は必須です", where)
	}
	if d.PriceQuery[keyServiceCode].Value == "" {
		return fmt.Errorf("%s: price_query.serviceCode は必須です", where)
	}
	switch d.PriceQuery[keyScope].Value {
	case "", scopeRegional, scopeGlobal:
	default:
		return fmt.Errorf("%s: price_query.scope は %q か %q です: %q",
			where, scopeRegional, scopeGlobal, d.PriceQuery[keyScope].Value)
	}
	for attr, spec := range d.Filters() {
		for _, m := range templatePattern.FindAllStringSubmatch(spec.Value, -1) {
			name := m[1]
			if _, ok := s.Params[name]; ok || builtinVars[name] {
				continue
			}
			return fmt.Errorf("%s: price_query.%s が未定義の変数を参照しています: %q", where, attr, name)
		}
	}
	for name := range d.When {
		if _, ok := s.Params[name]; !ok {
			return fmt.Errorf("%s: when が未定義のパラメータを参照しています: %q", where, name)
		}
	}
	if d.QuantityFormula == "" {
		return fmt.Errorf("%s: quantity_formula は必須です", where)
	}
	expr, err := formula.Parse(d.QuantityFormula)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	known := make(map[string]bool, len(s.Params)+8)
	for name := range s.Params {
		known[name] = true
	}
	for _, name := range ir.AssumptionVarNames() {
		known[name] = true
	}
	for _, name := range expr.Vars() {
		if !known[name] {
			return fmt.Errorf("%s: quantity_formula が未定義の変数を参照しています: %q", where, name)
		}
	}
	d.quantity = expr
	return nil
}

// Get はサービス定義を返す。
func (c *Catalog) Get(service string) (Service, bool) {
	s, ok := c.services[service]
	return s, ok
}

// ServiceNames は定義済みサービス名を昇順で返す。
func (c *Catalog) ServiceNames() []string {
	return slices.Sorted(maps.Keys(c.services))
}

// Services は定義済みサービスをサービス名の昇順で返す。
func (c *Catalog) Services() []Service {
	out := make([]Service, 0, len(c.services))
	for _, name := range c.ServiceNames() {
		out = append(out, c.services[name])
	}
	return out
}

// ServiceSpec は ir.Schema を実装する。IR のバリデーションに使う。
func (c *Catalog) ServiceSpec(service string) (ir.ServiceSpec, bool) {
	s, ok := c.services[service]
	if !ok {
		return ir.ServiceSpec{}, false
	}
	spec := ir.ServiceSpec{Params: make(map[string]ir.ParamSpec, len(s.Params))}
	for name, p := range s.Params {
		spec.Params[name] = ir.ParamSpec{
			Type:       p.Type,
			Required:   p.Required,
			HasDefault: p.Default != nil,
			Enum:       p.Enum,
		}
	}
	return spec, true
}

// Icon は図に当てるアイコン URL を返す。
func (c *Catalog) Icon(service string) (string, bool) {
	s, ok := c.services[service]
	if !ok || s.Icon == "" {
		return "", false
	}
	return s.Icon, true
}

// Display は図のラベルに使う表示名を返す。
func (c *Catalog) Display(service string) (string, bool) {
	s, ok := c.services[service]
	if !ok {
		return "", false
	}
	return s.Display, true
}

var _ ir.Schema = (*Catalog)(nil)

// ResolveParams は catalog の既定値で params を補完した写しを返す。
// 元の map は変更しない。
func (s Service) ResolveParams(params map[string]any) map[string]any {
	out := make(map[string]any, len(s.Params)+len(params))
	for name, p := range s.Params {
		if p.Default != nil {
			out[name] = p.Default
		}
	}
	for k, v := range params {
		out[k] = v
	}
	return out
}
