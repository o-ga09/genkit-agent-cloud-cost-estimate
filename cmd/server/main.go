// Command server は Web サービスの API を提供する（ADR-0010）。
//
// フロントエンド（web/）はオブジェクトストレージ + CDN（S3 / R2 等）から
// 別オリジンで配信する想定で、このコマンドは API 専用である
// （-static-dir を指定すればビルド成果物を配信する簡易モードも使える）。
//
// ブラウザからチャット + 選択 UI で構成を相談し（FR-WEB-1）、
// 構成図 SVG / PNG / drawio XML / Excel をダウンロードできる（FR-WEB-2）。
// 保存した IR JSON のパーマリンクから再レンダリング・再見積もりできる（FR-WEB-3）。
// AWS クレデンシャルはサーバー側にのみ存在し、クライアントには渡さない（FR-WEB-4）。
//
// 認証（FR-WEB-5）は requirements.md の未決事項であり、このコマンド単体では
// 実装していない。セルフホストする場合はリバースプロキシ側でアクセス制御すること。
//
//	# API サーバーのみ（フロントエンドは S3 / R2 + CDN から別途配信する）
//	GEMINI_API_KEY=... go run ./cmd/server -addr :8080 -allowed-origins https://app.example.com
//
//	# 簡易モード（ビルド成果物をこのプロセスからも配信する）
//	GEMINI_API_KEY=... go run ./cmd/server -static-dir web/dist
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/diagram"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/estimateflow"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/intake"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing/awsmcp"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/webapi"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", ":8080", "リッスンアドレス")
	staticDir := flag.String("static-dir", "",
		"フロントエンドのビルド成果物のディレクトリ。空なら配信しない（S3/R2 + CDN 等、別オリジンでの配信を想定）")
	allowedOrigins := flag.String("allowed-origins", "",
		"CORS で許可するオリジン（カンマ区切り）。フロントエンドを別オリジン（S3/R2 + CDN 等）で"+
			"配信する場合はそのオリジンを指定する。\"*\" で全オリジン許可。空なら CORS ヘッダを付けない")
	dataDir := flag.String("data-dir", ".data/architectures", "保存済み構成 JSON の保存先（FR-WEB-3）")
	model := flag.String("model", intake.DefaultModel, "使用するモデル")
	mcpCommand := flag.String("mcp-command", "uvx", "AWS Pricing MCP Server を起動するコマンド")
	mcpArgs := flag.String("mcp-args", "awslabs.aws-pricing-mcp-server@latest", "起動コマンドの引数（空白区切り）")
	flag.Parse()

	ctx := context.Background()

	g := genkit.Init(ctx,
		genkit.WithPlugins(&googlegenai.GoogleAI{}),
		genkit.WithExperimental(),
	)

	agent, err := intake.New(g, intake.Options{Model: *model})
	if err != nil {
		return fmt.Errorf("intake agent を初期化できませんでした: %w", err)
	}

	cat, err := catalog.Builtin()
	if err != nil {
		return err
	}

	// AWS 認証情報はこのプロセス（サーバー側）だけが持つ。クライアントには渡さない（FR-WEB-4）。
	src := awsmcp.New(awsmcp.Options{Command: *mcpCommand, Args: strings.Fields(*mcpArgs)})
	defer src.Close()

	store, err := webapi.NewFileArchitectureStore(*dataDir)
	if err != nil {
		return err
	}

	srv := &webapi.Server{
		Agent: agent,
		Estimator: webapi.FlowEstimator{Deps: estimateflow.Deps{
			Catalog: cat,
			Prices:  src,
			PNG:     diagram.PNGOptions{IconLoader: diagram.HTTPIconLoader(nil)},
		}},
		Catalog:        cat,
		Store:          store,
		AllowedOrigins: parseOrigins(*allowedOrigins),
	}

	apiHandler := srv.Routes()
	handler := apiHandler
	if *staticDir != "" {
		mux := http.NewServeMux()
		mux.Handle("/api/", apiHandler)
		mux.Handle("/healthz", apiHandler)
		mux.Handle("/", spaHandler(*staticDir))
		handler = mux
	}

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("listening on %s (static: %q, allowed-origins: %v, data: %s)",
		*addr, *staticDir, srv.AllowedOrigins, *dataDir)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func parseOrigins(s string) []string {
	var out []string
	for o := range strings.SplitSeq(s, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
}

// spaHandler は React SPA の静的ファイルを配信する（簡易モード用）。
// 存在しないパス（クライアントサイドルーティング用）は index.html にフォールバックする。
func spaHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean(r.URL.Path)
		path := filepath.Join(dir, clean)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
