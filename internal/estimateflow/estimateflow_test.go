package estimateflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/firebase/genkit/go/genkit"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/cost"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/estimateflow"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
)

var generatedAt = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

// unitSource は catalog が期待する単位の単価を返すフェイク（FR-PRC-1）。
type unitSource struct {
	units map[string]string
	fail  map[string]bool
}

func (s *unitSource) Unit(_ context.Context, q pricing.PriceQuery) (pricing.Price, error) {
	if s.fail[q.Key()] {
		return pricing.Price{}, errors.New("単価を引けません")
	}
	return pricing.Price{
		Amount: 0.05, Currency: "USD", Unit: s.units[q.Key()],
		SKU: "SKU-" + q.Service, FetchedAt: generatedAt,
	}, nil
}

func deps(t *testing.T, arch *ir.Architecture) estimateflow.Deps {
	t.Helper()
	cat, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	// driver ごとの unit を拾って、それを返すフェイクを作る。
	probe := &unitSource{units: map[string]string{}}
	est, err := cost.Build(context.Background(), arch, cat, probe)
	if err != nil {
		t.Fatal(err)
	}
	src := &unitSource{units: map[string]string{}, fail: map[string]bool{}}
	for _, it := range est.Items {
		src.units[it.Query.Key()] = it.Unit
	}
	return estimateflow.Deps{
		Catalog: cat,
		Prices:  src,
		Now:     func() time.Time { return generatedAt },
	}
}

func exampleIR(t *testing.T) *ir.Architecture {
	t.Helper()
	arch, err := ir.LoadFile("../../examples/ir/web-3tier.json")
	if err != nil {
		t.Fatal(err)
	}
	return arch
}

// NFR-3: LLM を経由せず、IR だけで図と Excel が生成できる。
func TestRun_ProducesArtifacts(t *testing.T) {
	arch := exampleIR(t)
	res, err := estimateflow.Run(context.Background(), deps(t, arch), &estimateflow.Request{
		Architecture: arch,
	})
	if err != nil {
		t.Fatalf("Flow の実行に失敗しました: %v", err)
	}
	if res.Region != "ap-northeast-1" {
		t.Errorf("region = %q", res.Region)
	}
	if res.FailedLines != 0 {
		t.Errorf("失敗した行が %d 件あります", res.FailedLines)
	}
	if len(res.Lines) == 0 {
		t.Fatal("明細が空です")
	}

	want := map[estimateflow.Format]string{
		estimateflow.FormatIR:     "estimate.json",
		estimateflow.FormatSVG:    "estimate.svg",
		estimateflow.FormatDrawio: "estimate.drawio",
		estimateflow.FormatXLSX:   "estimate.xlsx",
	}
	if len(res.Artifacts) != len(want) {
		t.Fatalf("成果物が %d 個、want %d", len(res.Artifacts), len(want))
	}
	for _, a := range res.Artifacts {
		filename, ok := want[a.Format]
		if !ok {
			t.Errorf("想定外の形式です: %q", a.Format)
			continue
		}
		if a.Filename != filename {
			t.Errorf("%s のファイル名 = %q, want %q", a.Format, a.Filename, filename)
		}
		if len(a.Content) == 0 {
			t.Errorf("%s の中身が空です", a.Format)
		}
		if a.ContentType == "" {
			t.Errorf("%s の ContentType が空です", a.Format)
		}
	}
}

// 出力した IR を読み直すと元に戻る（PRIN-5: 再生成に LLM を通さない）。
func TestRun_IRArtifactRoundTrips(t *testing.T) {
	arch := exampleIR(t)
	res, err := estimateflow.Run(context.Background(), deps(t, arch), &estimateflow.Request{
		Architecture: arch,
		Formats:      []estimateflow.Format{estimateflow.FormatIR},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ir.Decode(strings.NewReader(string(res.Artifacts[0].Content)))
	if err != nil {
		t.Fatalf("出力した IR を読み直せませんでした: %v", err)
	}
	if got.Region != arch.Region || len(got.Resources) != len(arch.Resources) {
		t.Errorf("読み直した IR が元と違います: %+v", got)
	}
}

// PRIN-4: Flow の出力に月額や合計を持たせない。金額は Excel の数式が出す。
func TestResponse_HasNoTotals(t *testing.T) {
	arch := exampleIR(t)
	res, err := estimateflow.Run(context.Background(), deps(t, arch), &estimateflow.Request{
		Architecture: arch,
		Formats:      []estimateflow.Format{estimateflow.FormatIR},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"total", "monthly", "amount", "cost"} {
		if strings.Contains(strings.ToLower(string(b)), banned) {
			t.Errorf("Flow の出力に %q を含むフィールドがあります（金額は Excel の数式が出す）", banned)
		}
	}
}

// FR-PRC-7: 単価を取得できない行があっても成果物は作る。
func TestRun_FailedLinesDoNotStopArtifacts(t *testing.T) {
	arch := exampleIR(t)
	d := deps(t, arch)
	src := d.Prices.(*unitSource)
	for key := range src.units {
		src.fail[key] = true
		break
	}
	res, err := estimateflow.Run(context.Background(), d, &estimateflow.Request{Architecture: arch})
	if err != nil {
		t.Fatalf("行の失敗で Flow が失敗しました: %v", err)
	}
	if res.FailedLines != 1 {
		t.Errorf("失敗した行 = %d, want 1", res.FailedLines)
	}
	if len(res.Artifacts) == 0 {
		t.Error("成果物が作られていません")
	}
}

func TestRun_Errors(t *testing.T) {
	arch := exampleIR(t)
	ctx := context.Background()

	if _, err := estimateflow.Run(ctx, deps(t, arch), &estimateflow.Request{}); err == nil {
		t.Error("architecture なしでエラーになりませんでした")
	}
	if _, err := estimateflow.Run(ctx, estimateflow.Deps{}, &estimateflow.Request{Architecture: arch}); err == nil {
		t.Error("単価の取得元なしでエラーになりませんでした")
	}
	_, err := estimateflow.Run(ctx, deps(t, arch), &estimateflow.Request{
		Architecture: arch, Formats: []estimateflow.Format{"pdf"},
	})
	if err == nil || !strings.Contains(err.Error(), "未対応の形式") {
		t.Errorf("err = %v, want 未対応の形式", err)
	}
}

// Genkit の Flow として登録して実行できる。
func TestDefine_RunsAsFlow(t *testing.T) {
	arch := exampleIR(t)
	ctx := context.Background()
	g := genkit.Init(ctx)
	flow := estimateflow.Define(g, deps(t, arch))

	res, err := flow.Run(ctx, &estimateflow.Request{
		Architecture: arch,
		Formats:      []estimateflow.Format{estimateflow.FormatSVG},
	})
	if err != nil {
		t.Fatalf("Flow の実行に失敗しました: %v", err)
	}
	if len(res.Artifacts) != 1 || res.Artifacts[0].Format != estimateflow.FormatSVG {
		t.Errorf("成果物 = %+v", res.Artifacts)
	}
}

// NFR-5: estimate Flow のパッケージが Genkit の preview（exp 系）API を import しない。
func TestNoPreviewAPIDependency(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go コマンドが無いためスキップします")
	}
	out, err := exec.Command("go", "list", "-deps",
		"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/estimateflow").Output()
	if err != nil {
		t.Fatalf("依存を取得できませんでした: %v", err)
	}
	for _, dep := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.Contains(dep, "genkit/go") && strings.Contains(dep, "/exp") {
			t.Errorf("preview API に依存しています: %s", dep)
		}
	}
}
