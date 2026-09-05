// Command chat は構成をチャットで相談し、確認のうえで成果物を生成する。
//
//	GEMINI_API_KEY=... go run ./cmd/chat -session my-estimate
//
// 対話は intake agent（preview API）、成果物の生成は estimate Flow（安定 API）が担う。
// 構成案は利用者が確認してからでないと成果物にならない（FR-CHT-5）。
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/diagram"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/estimateflow"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/intake"
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
	session := flag.String("session", "", "セッション ID（同じ ID で再開できる）")
	sessionDir := flag.String("session-dir", ".sessions", "セッションの保存先")
	outDir := flag.String("out-dir", "out", "成果物の出力先")
	model := flag.String("model", intake.DefaultModel, "使用するモデル")
	formats := flag.String("formats", "ir,svg,drawio,xlsx", "生成する形式")
	mcpCommand := flag.String("mcp-command", "uvx", "AWS Pricing MCP Server を起動するコマンド")
	mcpArgs := flag.String("mcp-args", "awslabs.aws-pricing-mcp-server@latest", "起動コマンドの引数")
	flag.Parse()

	if *session == "" {
		*session = "session-" + time.Now().UTC().Format("20060102-150405")
	}
	ctx := context.Background()

	g := genkit.Init(ctx,
		genkit.WithPlugins(&googlegenai.GoogleAI{}),
		genkit.WithExperimental(),
	)
	store, err := intake.NewFileStore(*sessionDir)
	if err != nil {
		return err
	}
	agent, err := intake.New(g, intake.Options{Model: *model, Store: store})
	if err != nil {
		return err
	}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	fmt.Printf("セッション: %s\n作りたいシステムを説明してください（空行で終了）。\n> ", *session)
	if !in.Scan() {
		return nil
	}
	turn, err := agent.Start(ctx, *session, in.Text())
	if err != nil {
		return err
	}

	for {
		switch turn.Status {
		case intake.StatusAsking:
			answers, err := ask(in, turn)
			if err != nil {
				return err
			}
			if turn, err = agent.Answer(ctx, *session, answers); err != nil {
				return err
			}

		case intake.StatusProposed:
			printProposal(turn)
			fmt.Print("この内容で成果物を作りますか? [y/N/追記したいこと] > ")
			if !in.Scan() {
				return nil
			}
			reply := strings.TrimSpace(in.Text())
			switch strings.ToLower(reply) {
			case "y", "yes":
				return generate(ctx, g, turn.Architecture, *outDir, *formats, *mcpCommand, *mcpArgs)
			case "", "n", "no":
				fmt.Println("中断した。同じ -session で再開できる。")
				return nil
			default:
				// 追記があれば対話を続ける。
				if turn, err = agent.Say(ctx, *session, reply); err != nil {
					return err
				}
			}

		default:
			return fmt.Errorf("未知の状態です: %q", turn.Status)
		}
	}
}

// ask は選択 UI の代わりに、番号で選ばせる。
func ask(in *bufio.Scanner, turn *intake.Turn) (map[string]intake.Answer, error) {
	if turn.Message != "" {
		fmt.Println(turn.Message)
	}
	answers := make(map[string]intake.Answer, len(turn.Questions))
	for _, q := range turn.Questions {
		fmt.Println()
		fmt.Println(q.Choice.Question)
		for i, opt := range q.Choice.Options {
			fmt.Printf("  %d) %s\n", i+1, opt)
		}
		if q.Choice.Multi {
			fmt.Print("番号をカンマ区切りで > ")
		} else {
			fmt.Print("番号 > ")
		}
		if !in.Scan() {
			return nil, errors.New("入力が終了しました")
		}
		selected, err := parseSelection(in.Text(), q.Choice)
		if err != nil {
			return nil, err
		}
		answers[q.ID] = intake.Answer{Selected: selected}
	}
	return answers, nil
}

// parseSelection は "1" や "1,3" を選択肢の文字列に変換する。
func parseSelection(input string, choice intake.Choice) ([]string, error) {
	var out []string
	for _, field := range strings.Split(input, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil || n < 1 || n > len(choice.Options) {
			return nil, fmt.Errorf("選択肢の番号ではありません: %q", field)
		}
		out = append(out, choice.Options[n-1])
	}
	if len(out) == 0 {
		return nil, errors.New("選択されていません")
	}
	if !choice.Multi && len(out) > 1 {
		return nil, errors.New("この質問は 1 つだけ選べます")
	}
	return out, nil
}

func printProposal(turn *intake.Turn) {
	fmt.Println()
	if turn.Message != "" {
		fmt.Println(turn.Message)
	}
	arch := turn.Architecture
	fmt.Printf("\n--- 構成案（%s / %s）---\n", arch.Provider, arch.Region)
	for _, r := range arch.Resources {
		label := r.Label
		if label == "" {
			label = r.Service
		}
		fmt.Printf("  %-14s %-14s %s", r.ID, r.Service, label)
		if len(r.Params) > 0 {
			fmt.Printf("  %v", r.Params)
		}
		fmt.Println()
	}
	a := arch.Assumptions
	fmt.Printf("--- 前提条件 ---\n  稼働 %g 時間/日 × %g 日/月、月間リクエスト %g、為替 %g 円、割引率 %g\n",
		a.HoursPerDay, a.DaysPerMonth, a.RequestsPerMonth, a.FxRate, a.DiscountRate)
	fmt.Println("金額はここには出ない。単価の取得と計算はこのあと成果物の生成で行う。")
}

func generate(ctx context.Context, g *genkit.Genkit, arch *ir.Architecture, outDir, formats, mcpCommand, mcpArgs string) error {
	src := awsmcp.New(awsmcp.Options{Command: mcpCommand, Args: strings.Fields(mcpArgs)})
	defer src.Close()

	flow := estimateflow.Define(g, estimateflow.Deps{
		Prices: src,
		PNG:    diagram.PNGOptions{IconLoader: diagram.HTTPIconLoader(nil)},
	})
	res, err := flow.Run(ctx, &estimateflow.Request{
		Architecture: arch,
		Formats:      parseFormats(formats),
		BaseName:     "estimate",
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, a := range res.Artifacts {
		path := filepath.Join(outDir, a.Filename)
		if err := os.WriteFile(path, a.Content, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%d bytes)\n", path, len(a.Content))
	}
	if res.FailedLines > 0 {
		fmt.Printf("警告: %d 行は単価を取得できず未計上\n", res.FailedLines)
	}
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
