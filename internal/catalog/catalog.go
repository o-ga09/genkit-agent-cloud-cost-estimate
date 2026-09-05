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
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

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

// Driver はコスト要素 1 件。price_query と quantity_formula は M2 以降で使う。
type Driver struct {
	ID              string            `yaml:"id"`
	Unit            string            `yaml:"unit"`
	PriceQuery      map[string]string `yaml:"price_query"`
	QuantityFormula string            `yaml:"quantity_formula"`
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
		if err := svc.validate(name); err != nil {
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

func (s Service) validate(file string) error {
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
	for _, d := range s.Drivers {
		if d.ID == "" {
			return fmt.Errorf("catalog %s: driver の id は必須です", file)
		}
		if seen[d.ID] {
			return fmt.Errorf("catalog %s: driver の id が重複しています: %q", file, d.ID)
		}
		seen[d.ID] = true
	}
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
