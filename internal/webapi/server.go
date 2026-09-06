package webapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v5"

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

// validate はリクエスト DTO の struct tag（`validate:"..."`）を検証する共通インスタンス。
// go-playground/validator は goroutine セーフで、構造体タグをリフレクションで
// キャッシュするためインスタンスは使い回す。
var validate = validator.New()

// Server は Web サービスの HTTP 層（ADR-0010）。ルーティングと Bind/Validate は
// Echo（github.com/labstack/echo/v5）+ go-validator（v10）を使う。
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
	e := echo.New()
	e.GET("/healthz", handleHealthz)
	e.POST("/api/sessions", s.handleStartSession)
	e.POST("/api/sessions/:id/messages", s.handleSay)
	e.POST("/api/sessions/:id/answers", s.handleAnswer)
	e.POST("/api/architectures", s.handleSaveArchitecture)
	e.GET("/api/architectures/:id", s.handleLoadArchitecture)
	e.POST("/api/architectures/:id/estimate", s.handleEstimate)
	e.GET("/api/architectures/:id/artifacts/:format", s.handleDownloadArtifact)
	return s.withCORS(e)
}

func handleHealthz(c *echo.Context) error {
	return c.String(http.StatusOK, "ok")
}

// withCORS はフロントエンドが別オリジン（S3 / R2 + CDN 等）から叩けるように
// CORS ヘッダを付ける。AllowedOrigins が空なら何もしない（既定は同一オリジンのみ）。
//
// CORS はアクセス制御ではなくブラウザ側の読み取り許可の仕組みでしかない
// （FR-WEB-5 の認証の代わりにはならない）。認証は別途必要になる。
//
// Echo の *echo.Echo は http.Handler（ServeHTTP）を実装するため、ルーター本体を
// 素の net/http ミドルウェアで包める。
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

// bindAndValidate はリクエストボディを Echo の Bind で構造体にデコードし、
// go-validator（構造体タグ `validate:"..."`）で必須項目などを検証する。
// ボディが空（GET や省略可能な JSON ボディ）の場合は Bind をスキップし、
// ゼロ値のまま検証だけ行う。
func bindAndValidate(c *echo.Context, v any) error {
	if c.Request().ContentLength != 0 {
		if err := c.Bind(v); err != nil {
			return fmt.Errorf("リクエストボディの JSON を読み込めませんでした: %w", err)
		}
	}
	if err := validate.Struct(v); err != nil {
		return err
	}
	return nil
}

func writeJSON(c *echo.Context, status int, v any) error {
	return c.JSON(status, v)
}

func writeError(c *echo.Context, status int, err error) error {
	return c.JSON(status, map[string]string{"error": err.Error()})
}
