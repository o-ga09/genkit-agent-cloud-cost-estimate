// Command server は Web サービスとして配布する（ADR-0010）。
//
// ブラウザからチャット + 選択 UI で構成を相談し（FR-WEB-1）、
// 構成図 SVG / PNG / drawio XML / Excel をダウンロードできる（FR-WEB-2）。
// 保存した IR JSON のパーマリンクから再レンダリング・再見積もりできる（FR-WEB-3）。
// AWS クレデンシャルはサーバー側にのみ存在し、クライアントには渡さない（FR-WEB-4）。
//
// 認証（FR-WEB-5）は requirements.md の未決事項であり、このコマンド単体では
// 実装していない。セルフホストする場合はリバースプロキシ側でアクセス制御すること。
//
//	GEMINI_API_KEY=... go run ./cmd/server -addr :8080
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
	staticDir := flag.String("static-dir", "web/dist", "フロントエンド（React）のビルド成果物のディレクトリ")
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
		Catalog: cat,
		Store:   store,
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Routes())
	mux.Handle("/", spaHandler(*staticDir))

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("listening on %s (static: %s, data: %s)", *addr, *staticDir, *dataDir)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// spaHandler は React SPA の静的ファイルを配信する。
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
