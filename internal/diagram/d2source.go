// Package diagram は IR から構成図を生成する。
//
// 座標は一切ここで決めない。ノードの入れ子と接続だけを D2 のソースに写像し、
// 配置はレイアウトエンジン（ELK）が計算する（ADR-0003 / PRIN-2）。
package diagram

import (
	"fmt"
	"strings"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

// Style は図の見た目に必要な情報を供給する。catalog が実装する（ADR-0005）。
type Style interface {
	// Icon はサービスのアイコン URL を返す。
	Icon(service string) (string, bool)
	// Display はサービスの表示名を返す。Resource.Label が空のときに使う。
	Display(service string) (string, bool)
}

// direction は図全体の流れ。構成図は左から右に読むほうが資料に貼りやすいので
// 既定を right にする。これは並びの向きであって座標ではない（PRIN-2）。
const direction = "right"

// BuildD2 は IR から D2 ソースを生成する。
// 出力は IR の記述順にのみ依存し、同じ IR からは常に同じ文字列になる。
func BuildD2(arch *ir.Architecture, style Style) string {
	var b strings.Builder
	fmt.Fprintf(&b, "direction: %s\n\n", direction)
	for _, r := range arch.Children("") {
		writeResource(&b, arch, style, r, 0)
	}
	for _, e := range arch.Edges {
		from, okFrom := nodePath(arch, e.From)
		to, okTo := nodePath(arch, e.To)
		if !okFrom || !okTo {
			// 存在しない ID の参照は Validate が弾く。ここでは黙って捨てる。
			continue
		}
		if e.Label == "" {
			fmt.Fprintf(&b, "%s -> %s\n", from, to)
			continue
		}
		fmt.Fprintf(&b, "%s -> %s: %s\n", from, to, quote(e.Label))
	}
	return b.String()
}

func writeResource(b *strings.Builder, arch *ir.Architecture, style Style, r *ir.Resource, depth int) {
	indent := strings.Repeat("  ", depth)
	children := arch.Children(r.ID)
	icon, hasIcon := iconOf(style, r.Service)

	fmt.Fprintf(b, "%s%s: %s", indent, r.ID, quote(label(style, r)))
	if !hasIcon && len(children) == 0 {
		b.WriteString("\n")
		return
	}
	b.WriteString(" {\n")
	if hasIcon {
		fmt.Fprintf(b, "%s  icon: %s\n", indent, icon)
	}
	for _, c := range children {
		writeResource(b, arch, style, c, depth+1)
	}
	fmt.Fprintf(b, "%s}\n", indent)
}

// label は図に出す表示名を決める。Resource.Label > catalog の表示名 > サービス名。
func label(style Style, r *ir.Resource) string {
	if r.Label != "" {
		return r.Label
	}
	if style != nil {
		if d, ok := style.Display(r.Service); ok && d != "" {
			return d
		}
	}
	return r.Service
}

func iconOf(style Style, service string) (string, bool) {
	if style == nil {
		return "", false
	}
	return style.Icon(service)
}

// nodePath は D2 のノードキー（祖先を "." で連結したパス）を返す。
func nodePath(arch *ir.Architecture, id string) (string, bool) {
	r, ok := arch.ResourceByID(id)
	if !ok {
		return "", false
	}
	parts := []string{r.ID}
	seen := map[string]bool{r.ID: true}
	for r.Parent != "" {
		p, ok := arch.ResourceByID(r.Parent)
		if !ok || seen[p.ID] {
			break
		}
		seen[p.ID] = true
		parts = append([]string{p.ID}, parts...)
		r = p
	}
	return strings.Join(parts, "."), true
}

// quote は D2 のラベルとして安全な二重引用符付き文字列にする。
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ")
	return `"` + r.Replace(s) + `"`
}
