package webapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/estimateflow"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/intake"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/webapi"
)

// fakeAgent は intake.Agent の代わりに使うフェイク。preview API（LLM）を
// 経由せずに HTTP 層だけをテストするための道具。
type fakeAgent struct {
	turn *intake.Turn
	err  error

	startedWith  string
	saidWith     string
	answeredWith map[string]intake.Answer
}

func (f *fakeAgent) Start(_ context.Context, sessionID, prompt string) (*intake.Turn, error) {
	f.startedWith = prompt
	if f.err != nil {
		return nil, f.err
	}
	t := *f.turn
	t.SessionID = sessionID
	return &t, nil
}

func (f *fakeAgent) Say(_ context.Context, sessionID, message string) (*intake.Turn, error) {
	f.saidWith = message
	if f.err != nil {
		return nil, f.err
	}
	t := *f.turn
	t.SessionID = sessionID
	return &t, nil
}

func (f *fakeAgent) Answer(_ context.Context, sessionID string, answers map[string]intake.Answer) (*intake.Turn, error) {
	f.answeredWith = answers
	if f.err != nil {
		return nil, f.err
	}
	t := *f.turn
	t.SessionID = sessionID
	return &t, nil
}

// fakeEstimator は estimateflow.Run の代わりに使うフェイク。
// AWS Pricing MCP Server や実際の図の生成に依存せずにテストする。
type fakeEstimator struct {
	resp    *estimateflow.Response
	err     error
	lastReq *estimateflow.Request
}

func (f *fakeEstimator) Run(_ context.Context, req *estimateflow.Request) (*estimateflow.Response, error) {
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func sampleArch() *ir.Architecture {
	return &ir.Architecture{
		Provider: ir.ProviderAWS,
		Region:   "ap-northeast-1",
		Assumptions: ir.Assumptions{
			HoursPerDay: 24, DaysPerMonth: 30, RequestsPerMonth: 1_000_000, FxRate: 150,
		},
		Resources: []ir.Resource{
			{ID: "web", Service: "ec2", Params: map[string]any{
				"instanceType": "t3.medium", "count": float64(2),
			}},
		},
	}
}

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Builtin()
	if err != nil {
		t.Fatalf("catalog.Builtin() error = %v", err)
	}
	return cat
}

func newTestServer(t *testing.T, agent *fakeAgent, est *fakeEstimator) (*webapi.Server, *httptest.Server) {
	t.Helper()
	srv := &webapi.Server{
		Agent:     agent,
		Estimator: est,
		Catalog:   testCatalog(t),
		Store:     webapi.NewMemoryArchitectureStore(),
		NewID:     idSequence(),
		Now:       func() time.Time { return time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) },
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return srv, ts
}

func idSequence() func() string {
	n := 0
	return func() string {
		n++
		return "id-" + itoa(n)
	}
}

func itoa(n int) string {
	return string(rune('0' + n))
}

func doJSON(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(raw)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func TestHandleStartSession(t *testing.T) {
	agent := &fakeAgent{turn: &intake.Turn{
		Status: intake.StatusAsking,
		Questions: []intake.Question{
			{ID: "q1", Choice: intake.Choice{Question: "稼働時間は?", Options: []string{"24時間", "業務時間"}}},
		},
	}}
	_, ts := newTestServer(t, agent, &fakeEstimator{})

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/sessions", map[string]string{"message": "3層 Web を作りたい"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeJSON[intake.Turn](t, resp)
	if got.SessionID == "" {
		t.Error("sessionId が空")
	}
	if got.Status != intake.StatusAsking {
		t.Errorf("status = %q, want asking", got.Status)
	}
	if len(got.Questions) != 1 || got.Questions[0].Choice.Question != "稼働時間は?" {
		t.Errorf("questions = %+v", got.Questions)
	}
	if agent.startedWith != "3層 Web を作りたい" {
		t.Errorf("Agent.Start への入力 = %q", agent.startedWith)
	}
}

func TestHandleStartSession_EmptyMessageIsRejected(t *testing.T) {
	_, ts := newTestServer(t, &fakeAgent{turn: &intake.Turn{}}, &fakeEstimator{})
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/sessions", map[string]string{"message": ""})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleSay(t *testing.T) {
	agent := &fakeAgent{turn: &intake.Turn{Status: intake.StatusProposed, Architecture: sampleArch()}}
	_, ts := newTestServer(t, agent, &fakeEstimator{})

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/sessions/sess-1/messages", map[string]string{"message": "RDS も足して"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeJSON[intake.Turn](t, resp)
	if got.SessionID != "sess-1" {
		t.Errorf("sessionId = %q, want sess-1", got.SessionID)
	}
	if agent.saidWith != "RDS も足して" {
		t.Errorf("Agent.Say への入力 = %q", agent.saidWith)
	}
}

func TestHandleSay_UnknownSession(t *testing.T) {
	agent := &fakeAgent{err: intake.ErrSessionNotFound}
	_, ts := newTestServer(t, agent, &fakeEstimator{})
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/sessions/nope/messages", map[string]string{"message": "hi"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleAnswer(t *testing.T) {
	agent := &fakeAgent{turn: &intake.Turn{Status: intake.StatusProposed, Architecture: sampleArch()}}
	_, ts := newTestServer(t, agent, &fakeEstimator{})

	body := map[string]any{"answers": map[string]intake.Answer{
		"q1": {Selected: []string{"24時間"}},
	}}
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/sessions/sess-1/answers", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeJSON[intake.Turn](t, resp)
	if got.Status != intake.StatusProposed {
		t.Errorf("status = %q, want proposed", got.Status)
	}
	if agent.answeredWith["q1"].Selected[0] != "24時間" {
		t.Errorf("Agent.Answer への入力 = %+v", agent.answeredWith)
	}
}

func TestHandleAnswer_NoAnswers(t *testing.T) {
	_, ts := newTestServer(t, &fakeAgent{turn: &intake.Turn{}}, &fakeEstimator{})
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/sessions/sess-1/answers", map[string]any{"answers": map[string]intake.Answer{}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleSaveArchitecture(t *testing.T) {
	_, ts := newTestServer(t, &fakeAgent{}, &fakeEstimator{})

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/architectures", map[string]any{"architecture": sampleArch()})
	if resp.StatusCode != http.StatusCreated {
		body := decodeJSON[map[string]any](t, resp)
		t.Fatalf("status = %d, want 201, body=%v", resp.StatusCode, body)
	}
	got := decodeJSON[struct {
		ID           string           `json:"id"`
		Architecture *ir.Architecture `json:"architecture"`
	}](t, resp)
	if got.ID == "" {
		t.Fatal("id が空")
	}
	if got.Architecture.Region != "ap-northeast-1" {
		t.Errorf("Region = %q", got.Architecture.Region)
	}

	// 保存された構成は GET で読み戻せる（FR-WEB-3: パーマリンク）。
	getResp := doJSON(t, http.MethodGet, ts.URL+"/api/architectures/"+got.ID, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
}

func TestHandleSaveArchitecture_InvalidIsRejected(t *testing.T) {
	_, ts := newTestServer(t, &fakeAgent{}, &fakeEstimator{})

	invalid := sampleArch()
	invalid.Resources[0].Service = "no-such-service"
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/architectures", map[string]any{"architecture": invalid})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleLoadArchitecture_NotFound(t *testing.T) {
	_, ts := newTestServer(t, &fakeAgent{}, &fakeEstimator{})
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/architectures/unknown", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleEstimate(t *testing.T) {
	est := &fakeEstimator{resp: &estimateflow.Response{
		Region: "ap-northeast-1",
		Lines:  []estimateflow.Line{{ResourceID: "web", Service: "ec2", DriverID: "instance-hours", Unit: "hour", Quantity: 1440}},
		Artifacts: []estimateflow.Artifact{
			{Format: estimateflow.FormatSVG, Filename: "estimate.svg", ContentType: "image/svg+xml", Content: []byte("<svg/>")},
			{Format: estimateflow.FormatXLSX, Filename: "estimate.xlsx", ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Content: []byte("PK")},
		},
	}}
	srv, ts := newTestServer(t, &fakeAgent{}, est)

	rec := &webapi.ArchitectureRecord{ID: "arch-1", Architecture: sampleArch()}
	if err := srv.Store.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/architectures/arch-1/estimate", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Region    string `json:"region"`
		Lines     []estimateflow.Line
		Artifacts []struct {
			Format      string `json:"format"`
			Filename    string `json:"filename"`
			ContentType string `json:"contentType"`
			DownloadURL string `json:"downloadUrl"`
		} `json:"artifacts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Region != "ap-northeast-1" {
		t.Errorf("region = %q", out.Region)
	}
	if len(out.Artifacts) != 2 {
		t.Fatalf("artifacts = %+v", out.Artifacts)
	}
	if !strings.Contains(out.Artifacts[0].DownloadURL, "/api/architectures/arch-1/artifacts/svg") {
		t.Errorf("downloadUrl = %q", out.Artifacts[0].DownloadURL)
	}
	if est.lastReq.Architecture.Region != "ap-northeast-1" {
		t.Errorf("Estimator に渡された architecture = %+v", est.lastReq.Architecture)
	}
}

func TestHandleEstimate_UnknownArchitecture(t *testing.T) {
	_, ts := newTestServer(t, &fakeAgent{}, &fakeEstimator{})
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/architectures/unknown/estimate", map[string]any{})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleDownloadArtifact(t *testing.T) {
	est := &fakeEstimator{resp: &estimateflow.Response{
		Artifacts: []estimateflow.Artifact{
			{Format: estimateflow.FormatSVG, Filename: "estimate.svg", ContentType: "image/svg+xml", Content: []byte("<svg>ok</svg>")},
		},
	}}
	srv, ts := newTestServer(t, &fakeAgent{}, est)
	if err := srv.Store.Save(context.Background(), &webapi.ArchitectureRecord{ID: "arch-1", Architecture: sampleArch()}); err != nil {
		t.Fatal(err)
	}

	resp := doJSON(t, http.MethodGet, ts.URL+"/api/architectures/arch-1/artifacts/svg", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "estimate.svg") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	if buf.String() != "<svg>ok</svg>" {
		t.Errorf("body = %q", buf.String())
	}
	// 単一フォーマットだけを estimateflow に依頼している（大きな成果物を毎回全部作らない）。
	if len(est.lastReq.Formats) != 1 || est.lastReq.Formats[0] != estimateflow.FormatSVG {
		t.Errorf("Formats = %+v", est.lastReq.Formats)
	}
}

func TestHandleDownloadArtifact_UnsupportedFormat(t *testing.T) {
	srv, ts := newTestServer(t, &fakeAgent{}, &fakeEstimator{})
	if err := srv.Store.Save(context.Background(), &webapi.ArchitectureRecord{ID: "arch-1", Architecture: sampleArch()}); err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/architectures/arch-1/artifacts/pdf", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleDownloadArtifact_UnknownArchitecture(t *testing.T) {
	_, ts := newTestServer(t, &fakeAgent{}, &fakeEstimator{})
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/architectures/unknown/artifacts/svg", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
