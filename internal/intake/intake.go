// Package intake は利用者から構成をヒアリングする対話層（M5）。
//
// Genkit の preview（exp 系）API を使うのはこのパッケージだけである。
// 見積もりの本体（internal/estimateflow）は安定 API の上にあり、preview に
// 破壊的変更が入っても被害はここに閉じる（PRIN-6 / ADR-0007）。
//
// このパッケージは成果物を作らない。作るのは「構成案」までで、
// 利用者が確認したうえで estimate Flow を呼ぶのは呼び出し側の責任（FR-CHT-5）。
package intake

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/firebase/genkit/go/ai"
	aix "github.com/firebase/genkit/go/ai/exp"
	aixtool "github.com/firebase/genkit/go/ai/exp/tool"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/middleware"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

// DefaultModel は既定で使うモデル。
const DefaultModel = "googleai/gemini-flash-latest"

// defaultRepairAttempts は、組み立てた IR がバリデーションに落ちたときに
// 理由を伝えて作り直させる回数。
const defaultRepairAttempts = 2

// Status は 1 ターンの結果の種類。
type Status string

const (
	// StatusAsking は利用者への質問が残っている状態。
	StatusAsking Status = "asking"
	// StatusProposed は構成案ができ、利用者の確認待ちの状態。
	StatusProposed Status = "proposed"
)

// Question は利用者に提示する 1 問。ID は回答を戻すときの対応付けに使う。
type Question struct {
	ID     string `json:"id"`
	Choice Choice `json:"choice"`
}

// Turn は 1 ターンの結果。
type Turn struct {
	SessionID string     `json:"sessionId"`
	Status    Status     `json:"status"`
	Message   string     `json:"message,omitempty"`
	Questions []Question `json:"questions,omitempty"`
	// Architecture は Status が proposed のときの構成案。
	// 利用者の確認を経てから estimate Flow に渡す（FR-CHT-5）。
	Architecture *ir.Architecture `json:"architecture,omitempty"`
}

// Options は Agent の設定。
type Options struct {
	// Catalog はサービス定義。nil なら同梱の catalog を読む。
	Catalog *catalog.Catalog
	// Model は使うモデル名。空なら DefaultModel。
	Model string
	// Store はセッションの保存先。nil ならプロセス内に持つ。
	Store SessionStore
	// RepairAttempts は IR がバリデーションに落ちたときの作り直し回数。
	RepairAttempts int
	// Retry はモデル呼び出しのリトライ設定。nil なら既定（3 回・指数バックオフ）。
	// リトライ要否は core/status のセンチネルで判定される（NFR-6）。
	Retry *middleware.Retry
	// FallbackModels は主モデルが落ちたときに順に試すモデル。空なら切り替えない。
	FallbackModels []ai.ModelRef
}

// Agent は構成をヒアリングする対話エージェント。
type Agent struct {
	g       *genkit.Genkit
	catalog *catalog.Catalog
	model   string
	store   SessionStore
	repairs int

	listTool ai.Tool
	askTool  *aix.InterruptibleTool[Choice, string, Answer]

	// middlewares は一時的なモデル障害でリクエストを落とさないための保険（NFR-6）。
	middlewares []ai.Middleware
}

// New は Agent を作り、ツールを Genkit に登録する。
// g は genkit.WithExperimental() を付けて初期化しておく必要がある。
func New(g *genkit.Genkit, opts Options) (*Agent, error) {
	cat := opts.Catalog
	if cat == nil {
		var err error
		if cat, err = catalog.Builtin(); err != nil {
			return nil, err
		}
	}
	a := &Agent{
		g:       g,
		catalog: cat,
		model:   opts.Model,
		store:   opts.Store,
		repairs: opts.RepairAttempts,
	}
	if a.model == "" {
		a.model = DefaultModel
	}
	if a.store == nil {
		a.store = NewMemoryStore()
	}
	if a.repairs == 0 {
		a.repairs = defaultRepairAttempts
	}
	a.listTool = defineListServicesTool(g, cat)
	a.askTool = defineAskUserTool(g)

	retry := opts.Retry
	if retry == nil {
		retry = &middleware.Retry{MaxRetries: 3}
	}
	a.middlewares = []ai.Middleware{retry}
	if len(opts.FallbackModels) > 0 {
		a.middlewares = append(a.middlewares, &middleware.Fallback{Models: opts.FallbackModels})
	}
	return a, nil
}

// Start は新しい対話を始める。
func (a *Agent) Start(ctx context.Context, sessionID, prompt string) (*Turn, error) {
	if sessionID == "" {
		return nil, errors.New("セッション ID は必須です")
	}
	sess := &Session{ID: sessionID}
	return a.generate(ctx, sess, ai.WithPrompt(prompt))
}

// Say は利用者の自由入力を対話に足す。
func (a *Agent) Say(ctx context.Context, sessionID, message string) (*Turn, error) {
	sess, err := a.store.Load(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return a.generate(ctx, sess, ai.WithPrompt(message))
}

// Answer は選択 UI で得た回答を返して対話を再開する（FR-CHT-1）。
// answers のキーは Turn.Questions の ID。
func (a *Agent) Answer(ctx context.Context, sessionID string, answers map[string]Answer) (*Turn, error) {
	sess, err := a.store.Load(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	pending := sess.pending()
	if len(pending) == 0 {
		return nil, errors.New("回答を待っている質問がありません")
	}

	restarts := make([]*ai.Part, 0, len(pending))
	for _, part := range pending {
		id := questionID(part)
		answer, ok := answers[id]
		if !ok {
			return nil, fmt.Errorf("質問 %q への回答がありません", id)
		}
		restart, err := a.askTool.Resume(part, answer)
		if err != nil {
			return nil, fmt.Errorf("対話を再開できませんでした: %w", err)
		}
		restarts = append(restarts, restart)
	}
	return a.generate(ctx, sess, ai.WithToolRestarts(restarts...))
}

// generate は 1 ターン分のモデル呼び出しを行い、結果を Turn にする。
func (a *Agent) generate(ctx context.Context, sess *Session, extra ...ai.GenerateOption) (*Turn, error) {
	opts := []ai.GenerateOption{
		ai.WithModelName(a.model),
		ai.WithSystem(systemPrompt),
		ai.WithTools(a.listTool, a.askTool),
		ai.WithUse(a.middlewares...),
	}
	if len(sess.Messages) > 0 {
		opts = append(opts, ai.WithMessages(sess.Messages...))
	}
	opts = append(opts, extra...)

	resp, err := genkit.Generate(ctx, a.g, opts...)
	if err != nil {
		return nil, fmt.Errorf("モデルの呼び出しに失敗しました: %w", err)
	}
	sess.Messages = resp.History()
	if err := a.store.Save(ctx, sess); err != nil {
		return nil, err
	}

	turn := &Turn{SessionID: sess.ID, Message: strings.TrimSpace(resp.Text())}
	if interrupts := resp.Interrupts(); len(interrupts) > 0 {
		turn.Status = StatusAsking
		for _, part := range interrupts {
			choice, ok := aixtool.InterruptAs[Choice](part)
			if !ok {
				continue
			}
			turn.Questions = append(turn.Questions, Question{ID: questionID(part), Choice: choice})
		}
		if len(turn.Questions) == 0 {
			return nil, errors.New("中断されましたが、利用者に尋ねる質問がありませんでした")
		}
		return turn, nil
	}

	arch, err := a.extract(ctx, sess)
	if err != nil {
		return nil, err
	}
	turn.Status = StatusProposed
	turn.Architecture = arch
	return turn, nil
}

// extract は会話から IR を組み立てる（FR-CHT-3）。
// catalog のバリデーションに落ちた場合は、理由を伝えて作り直させる。
func (a *Agent) extract(ctx context.Context, sess *Session) (*ir.Architecture, error) {
	instruction := extractPrompt
	var lastErr error
	for attempt := 0; attempt <= a.repairs; attempt++ {
		arch, _, err := genkit.GenerateData[ir.Architecture](ctx, a.g,
			ai.WithModelName(a.model),
			ai.WithMessages(sess.Messages...),
			ai.WithPrompt(instruction),
			ai.WithUse(a.middlewares...),
		)
		if err != nil {
			return nil, fmt.Errorf("構成の組み立てに失敗しました: %w", err)
		}
		if arch.SchemaVersion == "" {
			arch.SchemaVersion = ir.SchemaVersion
		}
		if err := arch.Validate(a.catalog); err != nil {
			lastErr = err
			instruction = extractPrompt + "\n\n直前の出力は次の理由で受け付けられなかった。修正してもう一度出力する。\n" + err.Error()
			continue
		}
		return arch, nil
	}
	return nil, fmt.Errorf("構成を %d 回作り直しても妥当になりませんでした: %w", a.repairs+1, lastErr)
}

// questionID は中断された質問の識別子。ツール呼び出しの ref をそのまま使う。
func questionID(part *ai.Part) string {
	if part == nil || part.ToolRequest == nil {
		return ""
	}
	if part.ToolRequest.Ref != "" {
		return part.ToolRequest.Ref
	}
	return part.ToolRequest.Name
}
