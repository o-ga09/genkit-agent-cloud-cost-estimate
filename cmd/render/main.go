// Command render は IR JSON から構成図をレンダリングする。
//
// LLM を経由せず、手書き（または保存済み）の IR だけで成果物が出せることを
// 担保するための入口である（NFR-3 / PRIN-5）。
//
//	go run ./cmd/render -in examples/ir/web-3tier.json -out out/web-3tier.svg
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/catalog"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/diagram"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	in := flag.String("in", "", "IR JSON ファイルのパス（必須）")
	out := flag.String("out", "", "出力先。拡張子 .svg または .d2（既定: 標準出力に D2 ソース）")
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
	if err := arch.Validate(cat); err != nil {
		return err
	}

	switch ext := filepath.Ext(*out); ext {
	case "":
		_, err := os.Stdout.WriteString(diagram.BuildD2(arch, cat))
		return err
	case ".d2":
		return write(*out, []byte(diagram.BuildD2(arch, cat)))
	case ".svg":
		svg, err := diagram.RenderSVG(context.Background(), arch, cat)
		if err != nil {
			return err
		}
		return write(*out, svg)
	default:
		return fmt.Errorf("未対応の出力形式です: %q（.svg または .d2）", ext)
	}
}

func write(path string, b []byte) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "wrote", path)
	return nil
}
