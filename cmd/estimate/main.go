// Command estimate は IR JSON から見積もり Excel を生成する。
//
// LLM は経由しない。単価は catalog の price_query をコードが組み立てて引く。
//
//	go run ./cmd/estimate -in examples/ir/web-3tier.json -out out/estimate.xlsx
//
// 既定では AWS Pricing MCP Server（uvx 経由）に接続する。
// pricing:* 権限を持つ AWS 認証情報が必要（FR-PRC-5 のとおり、本来はサービス側が持つ）。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/cost"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing/awsmcp"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/workbook"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	in := flag.String("in", "", "IR JSON ファイルのパス（必須）")
	out := flag.String("out", "out/estimate.xlsx", "出力する Excel のパス")
	mcpCommand := flag.String("mcp-command", "uvx", "AWS Pricing MCP Server を起動するコマンド")
	mcpArgs := flag.String("mcp-args", "awslabs.aws-pricing-mcp-server@latest", "起動コマンドの引数（空白区切り）")
	timeout := flag.Duration("timeout", 5*time.Minute, "単価取得全体のタイムアウト")
	flag.Parse()

	if *in == "" {
		flag.Usage()
		return errors.New("-in は必須です")
	}

	arch, err := ir.LoadFile(*in)
	if err != nil {
		return err
	}
	cat, err := catalog.Builtin()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	src := awsmcp.New(awsmcp.Options{Command: *mcpCommand, Args: strings.Fields(*mcpArgs)})
	defer src.Close()

	est, err := cost.Build(ctx, arch, cat, pricing.NewCache(src))
	if err != nil {
		return err
	}
	if dir := filepath.Dir(*out); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := workbook.Save(*out, est, workbook.Options{}); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "wrote %s（%d 行）\n", *out, len(est.Items))
	for _, it := range est.Failed() {
		fmt.Fprintf(os.Stderr, "  警告: %s/%s の単価を取得できませんでした: %v\n",
			it.ResourceID, it.DriverID, it.Err)
	}
	return nil
}
