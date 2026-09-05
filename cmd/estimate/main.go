// Command estimate は IR JSON から見積もり Excel と構成図を生成する。
//
// 中身は estimate Flow（internal/estimateflow）で、LLM は経由しない。
//
//	go run ./cmd/estimate -in examples/ir/web-3tier.json -out-dir out
//	go run ./cmd/estimate -in examples/ir/web-3tier.json -formats svg,png,drawio,xlsx
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

	"github.com/firebase/genkit/go/genkit"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/diagram"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/estimateflow"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/pricing/awsmcp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	in := flag.String("in", "", "IR JSON ファイルのパス（必須）")
	outDir := flag.String("out-dir", "out", "成果物の出力先ディレクトリ")
	formats := flag.String("formats", "ir,svg,drawio,xlsx",
		"生成する形式（カンマ区切り: ir / svg / png / drawio / xlsx）")
	mcpCommand := flag.String("mcp-command", "uvx", "AWS Pricing MCP Server を起動するコマンド")
	mcpArgs := flag.String("mcp-args", "awslabs.aws-pricing-mcp-server@latest", "起動コマンドの引数（空白区切り）")
	timeout := flag.Duration("timeout", 5*time.Minute, "処理全体のタイムアウト")
	flag.Parse()

	if *in == "" {
		flag.Usage()
		return errors.New("-in は必須です")
	}
	arch, err := ir.LoadFile(*in)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	src := awsmcp.New(awsmcp.Options{Command: *mcpCommand, Args: strings.Fields(*mcpArgs)})
	defer src.Close()

	g := genkit.Init(ctx)
	flow := estimateflow.Define(g, estimateflow.Deps{
		Prices: src,
		PNG:    diagram.PNGOptions{IconLoader: diagram.HTTPIconLoader(nil)},
	})

	res, err := flow.Run(ctx, &estimateflow.Request{
		Architecture: arch,
		Formats:      parseFormats(*formats),
		BaseName:     strings.TrimSuffix(filepath.Base(*in), filepath.Ext(*in)),
	})
	if err != nil {
		return err
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	for _, a := range res.Artifacts {
		path := filepath.Join(*outDir, a.Filename)
		if err := os.WriteFile(path, a.Content, 0o644); err != nil {
			return fmt.Errorf("%s を書き出せませんでした: %w", path, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", path, len(a.Content))
	}
	fmt.Fprintf(os.Stderr, "明細 %d 行", len(res.Lines))
	if res.FailedLines > 0 {
		fmt.Fprintf(os.Stderr, "（うち %d 行は単価を取得できず未計上）", res.FailedLines)
		for _, l := range res.Lines {
			if l.Error != "" {
				fmt.Fprintf(os.Stderr, "\n  警告: %s/%s: %s", l.ResourceID, l.DriverID, l.Error)
			}
		}
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

func parseFormats(s string) []estimateflow.Format {
	var out []estimateflow.Format
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, estimateflow.Format(f))
		}
	}
	return out
}
