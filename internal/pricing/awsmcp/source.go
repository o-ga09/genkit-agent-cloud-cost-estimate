package awsmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
)

// toolName は単価取得に使う MCP ツール。LLM には公開しない（FR-PRC-3）。
const toolName = "get_pricing"

// Options は MCP サーバーの起動方法。
type Options struct {
	// Command は MCP サーバーを起動するコマンド。既定は uvx。
	Command string
	// Args はコマンドの引数。既定は awslabs.aws-pricing-mcp-server@latest。
	Args []string
	// Env はサーバープロセスに渡す環境変数（"KEY=VALUE" 形式）。
	// AWS の認証情報はサービス側が保持し、利用者には要求しない（FR-PRC-5）。
	Env []string
	// Now は取得日時の取得方法。テストで固定するために差し替える。
	Now func() time.Time
}

func (o Options) command() (string, []string) {
	if o.Command != "" {
		return o.Command, o.Args
	}
	return "uvx", []string{"awslabs.aws-pricing-mcp-server@latest"}
}

// Source は AWS Pricing MCP Server 経由の PriceSource。
// 最初の問い合わせでサーバープロセスを起動し、以降はセッションを使い回す。
type Source struct {
	opts Options

	mu      sync.Mutex
	session *mcp.ClientSession
}

// New は Source を作る。サーバーの起動は最初の問い合わせまで遅延する。
func New(opts Options) *Source {
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Source{opts: opts}
}

// Close は MCP サーバーとの接続を閉じる。
func (s *Source) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return nil
	}
	err := s.session.Close()
	s.session = nil
	return err
}

// Unit は PriceSource を実装する。
func (s *Source) Unit(ctx context.Context, q pricing.PriceQuery) (pricing.Price, error) {
	session, err := s.connect(ctx)
	if err != nil {
		return pricing.Price{}, err
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: buildArguments(q),
	})
	if err != nil {
		return pricing.Price{}, fmt.Errorf("AWS Pricing MCP の呼び出しに失敗しました: %w", err)
	}
	raw, err := resultJSON(res)
	if err != nil {
		return pricing.Price{}, err
	}
	return parsePrice(raw, q, s.opts.Now())
}

func (s *Source) connect(ctx context.Context) (*mcp.ClientSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		return s.session, nil
	}
	name, args := s.opts.command()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), s.opts.Env...)
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "genkit-agent-cloud-cost-estimate",
		Version: "0.1.0",
	}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, fmt.Errorf("AWS Pricing MCP Server (%s) に接続できませんでした: %w", name, err)
	}
	s.session = session
	return session, nil
}

// buildArguments は get_pricing に渡す引数を組み立てる。
// フィルタは catalog 由来の値だけで構成され、LLM の出力は通らない（PRIN-3）。
func buildArguments(q pricing.PriceQuery) map[string]any {
	filters := make([]map[string]any, 0, len(q.Attributes))
	for _, field := range slices.Sorted(maps.Keys(q.Attributes)) {
		filters = append(filters, map[string]any{
			"Field": field,
			"Type":  "EQUALS",
			"Value": q.Attributes[field],
		})
	}
	args := map[string]any{
		"service_code": q.Service,
		"max_results":  10,
		"output_options": map[string]any{
			// オンデマンドのみを扱う（ADR-0009 / FR-PRC-6）。
			"pricing_terms": []string{"OnDemand"},
		},
	}
	if len(filters) > 0 {
		args["filters"] = filters
	}
	// グローバルサービス（データ転送など）は region を渡さない。
	if q.Region != "" {
		args["region"] = q.Region
	}
	return args
}

// resultJSON はツールの戻り値から JSON 本文を取り出す。
func resultJSON(res *mcp.CallToolResult) ([]byte, error) {
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err == nil && len(b) > 2 {
			return b, nil
		}
	}
	var texts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("AWS Pricing MCP の応答が空でした")
	}
	if res.IsError {
		return nil, fmt.Errorf("AWS Pricing MCP がエラーを返しました: %s", strings.Join(texts, " "))
	}
	return []byte(strings.Join(texts, "")), nil
}

var _ pricing.PriceSource = (*Source)(nil)
