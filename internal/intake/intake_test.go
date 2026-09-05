package intake_test

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/middleware"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/intake"
)

const fakeModel = "fake/scripted"

// scriptedModel は API キー無しで対話ループを検証するためのフェイクモデル。
// リクエストの中身を見て、ツール呼び出し・要約テキスト・IR の JSON を返し分ける。
type scriptedModel struct {
	calls     int
	askedOnce bool
	archJSON  string
	// lastTools は最後のリクエストで渡されたツール名。
	lastTools []string
}

func (m *scriptedModel) fn(_ context.Context, req *ai.ModelRequest, _ ai.ModelStreamCallback) (*ai.ModelResponse, error) {
	m.calls++
	m.lastTools = nil
	for _, t := range req.Tools {
		m.lastTools = append(m.lastTools, t.Name)
	}

	// IR の組み立て要求（extract）は、直近のプロンプトで判別する。
	if strings.Contains(lastText(req), "構成を表す JSON") {
		return textResponse(m.archJSON), nil
	}
	// まだ一度も質問していなければ、選択肢で問い返す。
	if !m.askedOnce {
		m.askedOnce = true
		return &ai.ModelResponse{
			FinishReason: ai.FinishReasonStop,
			Message: ai.NewModelMessage(ai.NewToolRequestPart(&ai.ToolRequest{
				Name: "ask_user",
				Ref:  "q1",
				Input: map[string]any{
					"question": "想定する稼働時間は?",
					"options":  []any{"24 時間", "業務時間のみ"},
					"multi":    false,
				},
			})),
		}, nil
	}
	return textResponse("ALB 1 台、EC2 2 台、RDS 1 台の構成でよいか確認してほしい。"), nil
}

func textResponse(text string) *ai.ModelResponse {
	return &ai.ModelResponse{
		FinishReason: ai.FinishReasonStop,
		Message:      ai.NewModelTextMessage(text),
	}
}

func lastText(req *ai.ModelRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == ai.RoleUser {
			return req.Messages[i].Text()
		}
	}
	return ""
}

const validArchJSON = `{
  "provider": "aws",
  "region": "ap-northeast-1",
  "assumptions": {"hoursPerDay": 24, "daysPerMonth": 30, "requestsPerMonth": 1000000, "fxRate": 150, "discountRate": 0},
  "resources": [
    {"id": "vpc-main", "service": "vpc", "label": "VPC"},
    {"id": "web-alb", "service": "alb", "parent": "vpc-main", "params": {"count": 1, "processedGbPerMonth": 100}},
    {"id": "app", "service": "ec2", "parent": "vpc-main", "params": {"instanceType": "t3.medium", "count": 2, "ebsGb": 30}}
  ],
  "edges": [{"from": "web-alb", "to": "app", "label": "HTTP"}]
}`

// invalidArchJSON は catalog に無いサービスを含む（バリデーションで弾かれる）。
const invalidArchJSON = `{
  "provider": "aws",
  "region": "ap-northeast-1",
  "assumptions": {"hoursPerDay": 24, "daysPerMonth": 30, "requestsPerMonth": 1000000, "fxRate": 150, "discountRate": 0},
  "resources": [{"id": "search", "service": "opensearch", "params": {}}],
  "edges": []
}`

func newAgent(t *testing.T, model *scriptedModel, opts intake.Options) *intake.Agent {
	t.Helper()
	ctx := context.Background()
	g := genkit.Init(ctx, genkit.WithExperimental())
	genkit.DefineModel(g, fakeModel, &ai.ModelOptions{
		Supports: &ai.ModelSupports{Tools: true, Multiturn: true, ToolChoice: true},
	}, model.fn)

	opts.Model = fakeModel
	a, err := intake.New(g, opts)
	if err != nil {
		t.Fatalf("Agent を作れませんでした: %v", err)
	}
	return a
}

// FR-CHT-1 / FR-CHT-2: 不足情報を選択肢で問い返し、回答が処理に戻る。
func TestAgent_AsksWithChoicesAndResumes(t *testing.T) {
	model := &scriptedModel{archJSON: validArchJSON}
	store := intake.NewMemoryStore()
	agent := newAgent(t, model, intake.Options{Store: store})
	ctx := context.Background()

	turn, err := agent.Start(ctx, "s1", "小規模な Web システムの見積もりが欲しい")
	if err != nil {
		t.Fatalf("Start に失敗しました: %v", err)
	}
	if turn.Status != intake.StatusAsking {
		t.Fatalf("status = %q, want asking（%+v）", turn.Status, turn)
	}
	if len(turn.Questions) != 1 {
		t.Fatalf("質問が %d 件、want 1", len(turn.Questions))
	}
	q := turn.Questions[0]
	if q.Choice.Question != "想定する稼働時間は?" {
		t.Errorf("質問文 = %q", q.Choice.Question)
	}
	if len(q.Choice.Options) != 2 || q.Choice.Multi {
		t.Errorf("選択肢 = %+v", q.Choice)
	}
	if q.ID == "" {
		t.Error("質問 ID が空です")
	}

	// 回答を返すと構成案まで進む。
	turn, err = agent.Answer(ctx, "s1", map[string]intake.Answer{
		q.ID: {Selected: []string{"24 時間"}},
	})
	if err != nil {
		t.Fatalf("Answer に失敗しました: %v", err)
	}
	if turn.Status != intake.StatusProposed {
		t.Fatalf("status = %q, want proposed", turn.Status)
	}
	if turn.Architecture == nil {
		t.Fatal("構成案がありません")
	}
	if len(turn.Architecture.Resources) != 3 {
		t.Errorf("リソースが %d 件", len(turn.Architecture.Resources))
	}
	if turn.Message == "" {
		t.Error("利用者への説明文が空です")
	}
}

// FR-CHT-4: LLM に渡すツールは list_supported_services と ask_user だけ。
func TestAgent_ToolsAreLimited(t *testing.T) {
	model := &scriptedModel{archJSON: validArchJSON}
	agent := newAgent(t, model, intake.Options{})
	if _, err := agent.Start(context.Background(), "s1", "相談したい"); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"list_supported_services": true, "ask_user": true}
	if len(model.lastTools) != len(want) {
		t.Fatalf("渡したツール = %v, want %v", model.lastTools, want)
	}
	for _, name := range model.lastTools {
		if !want[name] {
			t.Errorf("想定外のツールが渡っています: %q", name)
		}
	}
}

// FR-CHT-6: セッションストアにより、別の Agent インスタンスからでも再開できる。
func TestAgent_ResumesFromStore(t *testing.T) {
	store := intake.NewMemoryStore()
	ctx := context.Background()

	first := newAgent(t, &scriptedModel{archJSON: validArchJSON}, intake.Options{Store: store})
	turn, err := first.Start(ctx, "s1", "見積もりが欲しい")
	if err != nil {
		t.Fatal(err)
	}
	if turn.Status != intake.StatusAsking {
		t.Fatalf("status = %q", turn.Status)
	}

	// 別プロセス相当の Agent を作り直し、同じセッション ID で再開する。
	resumed := newAgent(t, &scriptedModel{archJSON: validArchJSON, askedOnce: true}, intake.Options{Store: store})
	turn2, err := resumed.Answer(ctx, "s1", map[string]intake.Answer{
		turn.Questions[0].ID: {Selected: []string{"24 時間"}},
	})
	if err != nil {
		t.Fatalf("再開に失敗しました: %v", err)
	}
	if turn2.Status != intake.StatusProposed {
		t.Errorf("status = %q, want proposed", turn2.Status)
	}
}

// catalog に無いサービスを出したら、理由を伝えて作り直させる（FR-IR-3 / FR-CHT-3）。
func TestAgent_RepairsInvalidArchitecture(t *testing.T) {
	ctx := context.Background()
	g := genkit.Init(ctx, genkit.WithExperimental())
	model := &repairingModel{
		scriptedModel: scriptedModel{archJSON: invalidArchJSON, askedOnce: true},
		fixed:         validArchJSON,
	}
	genkit.DefineModel(g, fakeModel, &ai.ModelOptions{
		Supports: &ai.ModelSupports{Tools: true, Multiturn: true, ToolChoice: true},
	}, model.fn)
	a, err := intake.New(g, intake.Options{Model: fakeModel})
	if err != nil {
		t.Fatal(err)
	}

	turn, err := a.Start(ctx, "s1", "見積もりが欲しい")
	if err != nil {
		t.Fatalf("作り直しに失敗しました: %v", err)
	}
	if turn.Status != intake.StatusProposed || turn.Architecture == nil {
		t.Fatalf("turn = %+v", turn)
	}
	if model.repairPrompts == 0 {
		t.Error("バリデーションの理由がモデルに伝わっていません")
	}
	if model.extractCalls != 2 {
		t.Errorf("組み立ての試行が %d 回、want 2", model.extractCalls)
	}
}

// repairingModel は 1 回目に不正な IR を返し、理由を伝えられたら正しい IR を返す。
type repairingModel struct {
	scriptedModel
	fixed         string
	extractCalls  int
	repairPrompts int
}

func (m *repairingModel) fn(ctx context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
	if strings.Contains(lastText(req), "構成を表す JSON") {
		m.extractCalls++
		if strings.Contains(lastText(req), "受け付けられなかった") {
			m.repairPrompts++
			return textResponse(m.fixed), nil
		}
		return textResponse(invalidArchJSON), nil
	}
	return m.scriptedModel.fn(ctx, req, cb)
}

// 何度作り直しても妥当にならなければ、理由の分かるエラーになる。
func TestAgent_GivesUpAfterRepairs(t *testing.T) {
	ctx := context.Background()
	g := genkit.Init(ctx, genkit.WithExperimental())
	model := &scriptedModel{archJSON: invalidArchJSON, askedOnce: true}
	genkit.DefineModel(g, fakeModel, &ai.ModelOptions{
		Supports: &ai.ModelSupports{Tools: true, Multiturn: true, ToolChoice: true},
	}, model.fn)
	a, err := intake.New(g, intake.Options{Model: fakeModel, RepairAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Start(ctx, "s1", "見積もりが欲しい"); err == nil {
		t.Fatal("不正な構成がエラーになりませんでした")
	} else if !strings.Contains(err.Error(), "catalog に定義がないサービス") {
		t.Errorf("err = %v, want catalog の理由", err)
	}
}

func TestAnswer_Errors(t *testing.T) {
	ctx := context.Background()
	store := intake.NewMemoryStore()
	agent := newAgent(t, &scriptedModel{archJSON: validArchJSON}, intake.Options{Store: store})

	if _, err := agent.Answer(ctx, "missing", nil); err == nil {
		t.Error("存在しないセッションがエラーになりませんでした")
	}
	turn, err := agent.Start(ctx, "s1", "相談")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Answer(ctx, "s1", map[string]intake.Answer{"other": {Selected: []string{"x"}}}); err == nil {
		t.Error("回答の無い質問がエラーになりませんでした")
	}
	_ = turn
}

// FR-CAT-4: list_supported_services が catalog の内容をモデルに返す。
// モデルがツールを呼ぶと、次のリクエストにツールの応答が入る。
func TestListServicesTool_ReturnsCatalog(t *testing.T) {
	ctx := context.Background()
	g := genkit.Init(ctx, genkit.WithExperimental())
	model := &listingModel{}
	genkit.DefineModel(g, fakeModel, &ai.ModelOptions{
		Supports: &ai.ModelSupports{Tools: true, Multiturn: true, ToolChoice: true},
	}, model.fn)
	a, err := intake.New(g, intake.Options{Model: fakeModel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Start(ctx, "s1", "何が見積もれる?"); err != nil {
		t.Fatalf("Start に失敗しました: %v", err)
	}
	for _, want := range []string{"ec2", "Amazon EC2", "instanceType", "data_transfer", "internet_egress"} {
		if !strings.Contains(model.toolResult, want) {
			t.Errorf("ツールの応答に %q が含まれていません: %s", want, model.toolResult)
		}
	}
}

// listingModel は list_supported_services を 1 度呼び、その応答を記録する。
type listingModel struct {
	called     bool
	toolResult string
}

func (m *listingModel) fn(_ context.Context, req *ai.ModelRequest, _ ai.ModelStreamCallback) (*ai.ModelResponse, error) {
	if !m.called {
		m.called = true
		return &ai.ModelResponse{
			FinishReason: ai.FinishReasonStop,
			Message: ai.NewModelMessage(ai.NewToolRequestPart(&ai.ToolRequest{
				Name: "list_supported_services", Ref: "l1", Input: map[string]any{},
			})),
		}, nil
	}
	for _, msg := range req.Messages {
		for _, p := range msg.Content {
			if p.IsToolResponse() {
				raw, _ := json.Marshal(p.ToolResponse.Output)
				m.toolResult = string(raw)
			}
		}
	}
	if strings.Contains(lastText(req), "構成を表す JSON") {
		return textResponse(validArchJSON), nil
	}
	return textResponse("対応しているのは EC2 / ALB / RDS / S3 / データ転送。"), nil
}

// PRIN-6 / NFR-5 の裏返し: preview API に依存してよいのは対話層だけ。
// intake は exp を使うが、estimateflow を巻き込まないことを確認する。
func TestIntakeDoesNotDependOnEstimateFlow(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go コマンドが無いためスキップします")
	}
	out, err := exec.Command("go", "list", "-deps",
		"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/intake").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "internal/estimateflow") {
		t.Error("対話層が estimate Flow に依存しています（成果物の生成は確認後に呼び出し側が行う）")
	}
}

// NFR-6: 一時的なモデル障害でリクエストが即座に失敗しない。
func TestAgent_RetriesTransientModelFailure(t *testing.T) {
	ctx := context.Background()
	g := genkit.Init(ctx, genkit.WithExperimental())
	model := &flakyModel{failures: 2, inner: scriptedModel{archJSON: validArchJSON}}
	genkit.DefineModel(g, fakeModel, &ai.ModelOptions{
		Supports: &ai.ModelSupports{Tools: true, Multiturn: true, ToolChoice: true},
	}, model.fn)

	a, err := intake.New(g, intake.Options{
		Model: fakeModel,
		Retry: &middleware.Retry{MaxRetries: 3, InitialDelayMs: 1, MaxDelayMs: 5, NoJitter: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := a.Start(ctx, "s1", "見積もりが欲しい")
	if err != nil {
		t.Fatalf("リトライされずに失敗しました: %v", err)
	}
	if turn.Status != intake.StatusAsking {
		t.Errorf("status = %q, want asking", turn.Status)
	}
	if model.attempts <= model.failures {
		t.Errorf("試行回数 = %d, want %d より多い", model.attempts, model.failures)
	}
}

// flakyModel は最初の failures 回だけ失敗する。
type flakyModel struct {
	failures int
	attempts int
	inner    scriptedModel
}

func (m *flakyModel) fn(ctx context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
	m.attempts++
	if m.attempts <= m.failures {
		return nil, errors.New("一時的にモデルへ接続できません")
	}
	return m.inner.fn(ctx, req, cb)
}
