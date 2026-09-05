package ir

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
)

// idPattern は Resource.ID に許す文字。図のノードキーにそのまま使うため、
// D2 の予約文字（ドット・空白・記号）を含めない。
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// Issue は 1 件のバリデーション違反。Path は IR 内の位置を指す。
type Issue struct {
	Path    string
	Message string
}

func (i Issue) String() string { return i.Path + ": " + i.Message }

// ValidationError は違反の一覧。利用者に理由を伝えるため、全件を保持する。
type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string {
	msgs := make([]string, 0, len(e.Issues))
	for _, is := range e.Issues {
		msgs = append(msgs, is.String())
	}
	return fmt.Sprintf("IR のバリデーションに失敗しました (%d 件): %s",
		len(e.Issues), strings.Join(msgs, "; "))
}

// Validate は IR の整合性を検証する。schema には catalog を渡す。
// schema が nil の場合、サービス名とパラメータの検証はスキップする。
func (a *Architecture) Validate(schema Schema) error {
	v := &validator{arch: a, schema: schema}
	v.checkMeta()
	v.checkAssumptions()
	v.checkResources()
	v.checkEdges()
	if len(v.issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: v.issues}
}

type validator struct {
	arch   *Architecture
	schema Schema
	issues []Issue
}

func (v *validator) add(path, format string, args ...any) {
	v.issues = append(v.issues, Issue{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) checkMeta() {
	if v.arch.SchemaVersion != "" && v.arch.SchemaVersion != SchemaVersion {
		v.add("schemaVersion", "未対応のスキーマバージョンです（対応: %q, 指定: %q）",
			SchemaVersion, v.arch.SchemaVersion)
	}
	switch v.arch.Provider {
	case ProviderAWS, ProviderGCP:
	case "":
		v.add("provider", "必須です（%q または %q）", ProviderAWS, ProviderGCP)
	default:
		v.add("provider", "未対応のプロバイダです: %q（対応: %q, %q）",
			v.arch.Provider, ProviderAWS, ProviderGCP)
	}
	if strings.TrimSpace(v.arch.Region) == "" {
		v.add("region", "必須です（例: ap-northeast-1）")
	}
	if len(v.arch.Resources) == 0 {
		v.add("resources", "1 つ以上のリソースが必要です")
	}
}

func (v *validator) checkAssumptions() {
	as := v.arch.Assumptions
	if as.HoursPerDay < 0 || as.HoursPerDay > 24 {
		v.add("assumptions.hoursPerDay", "0〜24 の範囲で指定してください: %v", as.HoursPerDay)
	}
	if as.DaysPerMonth < 0 || as.DaysPerMonth > 31 {
		v.add("assumptions.daysPerMonth", "0〜31 の範囲で指定してください: %v", as.DaysPerMonth)
	}
	if as.RequestsPerMonth < 0 {
		v.add("assumptions.requestsPerMonth", "0 以上で指定してください: %v", as.RequestsPerMonth)
	}
	if as.FxRate <= 0 {
		v.add("assumptions.fxRate", "0 より大きい値が必要です（円換算に使います）: %v", as.FxRate)
	}
	if as.DiscountRate < 0 || as.DiscountRate >= 1 {
		v.add("assumptions.discountRate", "0 以上 1 未満で指定してください: %v", as.DiscountRate)
	}
}

func (v *validator) checkResources() {
	seen := make(map[string]int, len(v.arch.Resources))
	for i, r := range v.arch.Resources {
		path := fmt.Sprintf("resources[%d]", i)
		switch {
		case r.ID == "":
			v.add(path+".id", "必須です")
		case !idPattern.MatchString(r.ID):
			v.add(path+".id", "英数字で始まり、英数字・ハイフン・アンダースコアのみ使えます: %q", r.ID)
		}
		if r.ID != "" {
			if first, dup := seen[r.ID]; dup {
				v.add(path+".id", "id が重複しています: %q（resources[%d] と同じ）", r.ID, first)
			} else {
				seen[r.ID] = i
			}
		}
		v.checkService(path, r)
	}
	v.checkParents(seen)
}

func (v *validator) checkService(path string, r Resource) {
	if r.Service == "" {
		v.add(path+".service", "必須です")
		return
	}
	if v.schema == nil {
		return
	}
	spec, ok := v.schema.ServiceSpec(r.Service)
	if !ok {
		names := v.schema.ServiceNames()
		slices.Sort(names)
		v.add(path+".service", "catalog に定義がないサービスです: %q（対応: %s）",
			r.Service, strings.Join(names, ", "))
		return
	}
	v.checkParams(path, r, spec)
}

func (v *validator) checkParams(path string, r Resource, spec ServiceSpec) {
	for name, ps := range spec.Params {
		val, ok := r.Params[name]
		if !ok {
			if ps.Required && !ps.HasDefault {
				v.add(fmt.Sprintf("%s.params.%s", path, name),
					"%q に必須のパラメータです", r.Service)
			}
			continue
		}
		v.checkParamValue(fmt.Sprintf("%s.params.%s", path, name), ps, val)
	}
	names := make([]string, 0, len(r.Params))
	for name := range r.Params {
		if _, defined := spec.Params[name]; !defined {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		v.add(fmt.Sprintf("%s.params.%s", path, name),
			"%q の catalog に定義がないパラメータです", r.Service)
	}
}

func (v *validator) checkParamValue(path string, ps ParamSpec, val any) {
	switch ps.Type {
	case ParamString:
		s, ok := val.(string)
		if !ok {
			v.add(path, "文字列である必要があります: %v (%T)", val, val)
			return
		}
		if len(ps.Enum) > 0 && !slices.Contains(ps.Enum, s) {
			v.add(path, "次のいずれかである必要があります: %s（指定: %q）",
				strings.Join(ps.Enum, ", "), s)
		}
	case ParamInt:
		f, ok := toFloat(val)
		if !ok {
			v.add(path, "整数である必要があります: %v (%T)", val, val)
			return
		}
		if f != math.Trunc(f) {
			v.add(path, "整数である必要があります: %v", val)
		}
	case ParamFloat:
		if _, ok := toFloat(val); !ok {
			v.add(path, "数値である必要があります: %v (%T)", val, val)
		}
	case ParamBool:
		if _, ok := val.(bool); !ok {
			v.add(path, "真偽値である必要があります: %v (%T)", val, val)
		}
	default:
		v.add(path, "catalog のパラメータ型が不正です: %q", ps.Type)
	}
}

// checkParents は parent 参照の存在と循環を検証する。
func (v *validator) checkParents(index map[string]int) {
	for i, r := range v.arch.Resources {
		if r.Parent == "" {
			continue
		}
		path := fmt.Sprintf("resources[%d].parent", i)
		if r.Parent == r.ID {
			v.add(path, "自分自身を親にできません: %q", r.ID)
			continue
		}
		if _, ok := index[r.Parent]; !ok {
			v.add(path, "存在しないリソースを参照しています: %q", r.Parent)
			continue
		}
		if v.hasParentCycle(r) {
			v.add(path, "親子関係が循環しています: %q", r.ID)
		}
	}
}

func (v *validator) hasParentCycle(start Resource) bool {
	visited := map[string]bool{start.ID: true}
	cur := start
	for cur.Parent != "" {
		next, ok := v.arch.ResourceByID(cur.Parent)
		if !ok {
			return false
		}
		if visited[next.ID] {
			return true
		}
		visited[next.ID] = true
		cur = *next
	}
	return false
}

func (v *validator) checkEdges() {
	for i, e := range v.arch.Edges {
		path := fmt.Sprintf("edges[%d]", i)
		if _, ok := v.arch.ResourceByID(e.From); !ok {
			v.add(path+".from", "存在しないリソースを参照しています: %q", e.From)
		}
		if _, ok := v.arch.ResourceByID(e.To); !ok {
			v.add(path+".to", "存在しないリソースを参照しています: %q", e.To)
		}
		if e.From != "" && e.From == e.To {
			v.add(path, "from と to が同じリソースです: %q", e.From)
		}
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
