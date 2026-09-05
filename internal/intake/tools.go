package intake

import (
	"context"
	"fmt"
	"strings"

	"maps"
	"slices"

	"github.com/firebase/genkit/go/ai"
	aix "github.com/firebase/genkit/go/ai/exp"
	aixtool "github.com/firebase/genkit/go/ai/exp/tool"
	"github.com/firebase/genkit/go/genkit"
	genkitexp "github.com/firebase/genkit/go/genkit/exp"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
)

// LLM に渡すツールはこの 2 つだけ（FR-CHT-4）。
// 価格取得系のツールは渡さない。フィルタを LLM に組ませないため（PRIN-3）。
const (
	ToolListServices = "list_supported_services"
	ToolAskUser      = "ask_user"
)

// Choice は利用者に提示する選択肢（FR-CHT-2）。
type Choice struct {
	Question string   `json:"question" jsonschema_description:"利用者に尋ねる質問"`
	Options  []string `json:"options" jsonschema_description:"選ばせる選択肢"`
	Multi    bool     `json:"multi" jsonschema_description:"複数選択を許すかどうか"`
}

// Answer は利用者の回答。
type Answer struct {
	Selected []string `json:"selected" jsonschema_description:"利用者が選んだ選択肢"`
}

// Text は回答をモデルに戻すための文字列にする。
func (a Answer) Text() string { return strings.Join(a.Selected, ", ") }

// serviceInfo は list_supported_services が返すサービス 1 件。
// catalog の内容をそのまま渡すことで、価格を引けないサービスを
// LLM が提案する事故を防ぐ（FR-CAT-4 / ADR-0005）。
type serviceInfo struct {
	Service string      `json:"service"`
	Display string      `json:"display"`
	Doc     string      `json:"doc,omitempty"`
	Params  []paramInfo `json:"params,omitempty"`
}

type paramInfo struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required,omitempty"`
	Default  any      `json:"default,omitempty"`
	Enum     []string `json:"enum,omitempty"`
	Doc      string   `json:"doc,omitempty"`
}

// defineListServicesTool は catalog の内容を返すツールを登録する。
func defineListServicesTool(g *genkit.Genkit, cat *catalog.Catalog) ai.Tool {
	return genkit.DefineTool(g, ToolListServices,
		"見積もりに使えるサービスと、そのパラメータの一覧を返す。構成に入れてよいのはここにあるサービスだけ。",
		func(_ *ai.ToolContext, _ struct{}) ([]serviceInfo, error) {
			return describeCatalog(cat), nil
		})
}

func describeCatalog(cat *catalog.Catalog) []serviceInfo {
	services := cat.Services()
	out := make([]serviceInfo, 0, len(services))
	for _, s := range services {
		info := serviceInfo{Service: s.Service, Display: s.Display, Doc: s.Doc}
		for _, name := range sortedParamNames(s) {
			p := s.Params[name]
			info.Params = append(info.Params, paramInfo{
				Name:     name,
				Type:     string(p.Type),
				Required: p.Required,
				Default:  p.Default,
				Enum:     p.Enum,
				Doc:      p.Doc,
			})
		}
		out = append(out, info)
	}
	return out
}

// defineAskUserTool は選択 UI で問い返すツールを登録する（FR-CHT-1）。
//
// 回答がまだ無いときは tool.Interrupt で処理を止める。中断された質問は
// 呼び出し側（フロント）に渡り、回答を持って再開する。
func defineAskUserTool(g *genkit.Genkit) *aix.InterruptibleTool[Choice, string, Answer] {
	return genkitexp.DefineInterruptibleTool(g, ToolAskUser,
		"構成を決めるのに必要な情報を、選択肢の形で利用者に尋ねる。自由記述ではなく必ず選択肢を出す。",
		func(_ context.Context, in Choice, ans *Answer) (string, error) {
			if ans == nil {
				return "", aixtool.Interrupt(in)
			}
			if len(ans.Selected) == 0 {
				return "", fmt.Errorf("回答が空です")
			}
			return ans.Text(), nil
		})
}

func sortedParamNames(s catalog.Service) []string {
	return slices.Sorted(maps.Keys(s.Params))
}
