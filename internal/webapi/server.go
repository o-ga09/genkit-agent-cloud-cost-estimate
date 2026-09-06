package webapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/estimateflow"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/intake"
)

// IntakeAgent は intake.Agent が満たすインターフェース。
// テストでは preview API（LLM）を経由しないフェイクに差し替える。
type IntakeAgent interface {
	Start(ctx context.Context, sessionID, prompt string) (*intake.Turn, error)
	Say(ctx context.Context, sessionID, message string) (*intake.Turn, error)
	Answer(ctx context.Context, sessionID string, answers map[string]intake.Answer) (*intake.Turn, error)
}

// Estimator は estimateflow.Run を抽象化したインターフェース。
// テストでは AWS Pricing MCP Server への接続を避けたフェイクに差し替える。
type Estimator interface {
	Run(ctx context.Context, req *estimateflow.Request) (*estimateflow.Response, error)
}

// FlowEstimator は estimateflow.Run を Estimator として公開するアダプタ。
type FlowEstimator struct {
	Deps estimateflow.Deps
}

// Run は Estimator を実装する。
func (f FlowEstimator) Run(ctx context.Context, req *estimateflow.Request) (*estimateflow.Response, error) {
	return estimateflow.Run(ctx, f.Deps, req)
}

// Server は Web サービスの HTTP 層（ADR-0010）。
//
// 認証は現時点で実装していない（FR-WEB-5 は未対応。requirements.md の
// 未決事項「Web サービスの認証方式」が決まり次第、Routes() の手前に
// ミドルウェアとして差し込む）。セルフホスト時はリバースプロキシ側で
// アクセス制御することを前提とする。
type Server struct {
	// Agent は構成をヒアリングする対話エージェント。必須。
	Agent IntakeAgent
	// Estimator は IR から成果物一式を作る。必須。LLM を経由しない（NFR-3）。
	Estimator Estimator
	// Catalog は POST /api/architectures で受け取った構成の検証に使う。必須。
	Catalog *catalog.Catalog
	// Store は構成の永続化先。必須。パーマリンク（FR-WEB-3）を支える。
	Store ArchitectureStore
	// NewID は ID 採番方法。nil なら暗号乱数由来の 16 文字 hex。
	NewID func() string
	// Now は時刻取得方法。nil なら time.Now。
	Now func() time.Time
	// AllowedOrigins は CORS で許可するオリジン一覧。
	//
	// フロントエンドをオブジェクトストレージ + CDN（S3 / R2 等）から配信する場合、
	// API サーバーとは別オリジンになるため設定が要る。空なら CORS ヘッダを付けない
	// （同一オリジン配信のみを想定した既定動作）。"*" を含めると全オリジンを許可する。
	AllowedOrigins []string
}

func (s *Server) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	return randomID()
}

func randomID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand の失敗は環境異常。時刻ベースにフォールバックする。
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Routes は API のハンドラを組み立てる。
//
// フロントエンドと別オリジンで動かす前提（オブジェクトストレージ + CDN 配信）のため、
// 静的ファイルはここでは配信しない。API 専用のハンドラだけを持つ。
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /api/sessions", s.handleStartSession)
	mux.HandleFunc("POST /api/sessions/{id}/messages", s.handleSay)
	mux.HandleFunc("POST /api/sessions/{id}/answers", s.handleAnswer)
	mux.HandleFunc("POST /api/architectures", s.handleSaveArchitecture)
	mux.HandleFunc("GET /api/architectures/{id}", s.handleLoadArchitecture)
	mux.HandleFunc("POST /api/architectures/{id}/estimate", s.handleEstimate)
	mux.HandleFunc("GET /api/architectures/{id}/artifacts/{format}", s.handleDownloadArtifact)
	return s.withCORS(mux)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// withCORS はフロントエンドが別オリジン（S3 / R2 + CDN 等）から叩けるように
// CORS ヘッダを付ける。AllowedOrigins が空なら何もしない（既定は同一オリジンのみ）。
//
// CORS はアクセス制御ではなくブラウザ側の読み取り許可の仕組みでしかない
// （FR-WEB-5 の認証の代わりにはならない）。認証は別途必要になる。
func (s *Server) withCORS(h http.Handler) http.Handler {
	if len(s.AllowedOrigins) == 0 {
		return h
	}
	allowAll := slices.Contains(s.AllowedOrigins, "*")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowAll || slices.Contains(s.AllowedOrigins, origin)) {
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("リクエストボディの JSON を読み込めませんでした: %w", err)
	}
	return nil
}
