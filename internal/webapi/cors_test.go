package webapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/webapi"
)

// フロントエンドをオブジェクトストレージ + CDN（S3 / R2 等）から配信する場合、
// API サーバーとは別オリジンになるため CORS が必要になる。

func TestCORS_DefaultIsDisabled(t *testing.T) {
	_, ts := newTestServer(t, &fakeAgent{}, &fakeEstimator{})

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	req.Header.Set("Origin", "https://frontend.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty (CORS disabled by default)", got)
	}
}

func TestCORS_AllowsConfiguredOrigin(t *testing.T) {
	srv := &webapi.Server{
		Agent:          &fakeAgent{},
		Estimator:      &fakeEstimator{},
		Catalog:        testCatalog(t),
		Store:          webapi.NewMemoryArchitectureStore(),
		AllowedOrigins: []string{"https://frontend.example.com"},
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	// プリフライト（OPTIONS）
	preflight, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/sessions", nil)
	preflight.Header.Set("Origin", "https://frontend.example.com")
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://frontend.example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Access-Control-Allow-Methods が空")
	}

	// 実リクエストにもヘッダが付く
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	req.Header.Set("Origin", "https://frontend.example.com")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if got := resp2.Header.Get("Access-Control-Allow-Origin"); got != "https://frontend.example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestCORS_RejectsUnlistedOrigin(t *testing.T) {
	srv := &webapi.Server{
		Agent:          &fakeAgent{},
		Estimator:      &fakeEstimator{},
		Catalog:        testCatalog(t),
		Store:          webapi.NewMemoryArchitectureStore(),
		AllowedOrigins: []string{"https://frontend.example.com"},
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for unlisted origin", got)
	}
}

func TestCORS_WildcardAllowsAnyOrigin(t *testing.T) {
	srv := &webapi.Server{
		Agent:          &fakeAgent{},
		Estimator:      &fakeEstimator{},
		Catalog:        testCatalog(t),
		Store:          webapi.NewMemoryArchitectureStore(),
		AllowedOrigins: []string{"*"},
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	req.Header.Set("Origin", "https://anything.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
}

func TestHealthz(t *testing.T) {
	_, ts := newTestServer(t, &fakeAgent{}, &fakeEstimator{})
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
