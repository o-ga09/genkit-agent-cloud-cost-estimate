// Package formula は catalog の quantity_formula を扱う小さな式エンジン（ADR-0013）。
//
// 1 つの AST を 2 通りに使う。
//   - Eval: Go で数量を評価する
//   - Excel: 変数をセル参照に差し替えて Excel の数式を組み立てる
//
// 文法は四則演算・単項マイナス・カッコ・数値・識別子だけ。条件分岐は持たない。
//
//	expr   := term (('+' | '-') term)*
//	term   := factor (('*' | '/') factor)*
//	factor := '-' factor | number | ident | '(' expr ')'
package formula

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Expr はパース済みの式。
type Expr struct {
	src  string
	root node
}

// String はパース元の文字列を返す。
func (e *Expr) String() string { return e.src }

// Vars は式が参照する変数名を昇順で返す。
func (e *Expr) Vars() []string {
	set := map[string]struct{}{}
	collectVars(e.root, set)
	return slices.Sorted(maps.Keys(set))
}

// Eval は変数に値を割り当てて式を評価する。
// 未定義の変数とゼロ除算はエラーにする（黙って 0 として扱わない）。
func (e *Expr) Eval(vars map[string]float64) (float64, error) {
	v, err := eval(e.root, vars)
	if err != nil {
		return 0, fmt.Errorf("式 %q の評価に失敗しました: %w", e.src, err)
	}
	return v, nil
}

// Ref は変数名をセル参照（例: "Assumptions!$B$3"）に解決する。
type Ref func(name string) (string, error)

// Excel は式を Excel の数式に変換する。先頭の "=" は付けない。
// 変数の解決に失敗した場合はエラーを返す（定数を焼き込まない: PRIN-4）。
func (e *Expr) Excel(ref Ref) (string, error) {
	var b strings.Builder
	if err := render(e.root, ref, &b, 0); err != nil {
		return "", fmt.Errorf("式 %q の Excel 数式化に失敗しました: %w", e.src, err)
	}
	return b.String(), nil
}

// ---- AST ----

type node interface{ isNode() }

type numberNode struct{ value float64 }

type varNode struct{ name string }

type unaryNode struct {
	op    byte // '-'
	child node
}

type binaryNode struct {
	op          byte // '+' '-' '*' '/'
	left, right node
}

func (numberNode) isNode() {}
func (varNode) isNode()    {}
func (unaryNode) isNode()  {}
func (binaryNode) isNode() {}

func collectVars(n node, out map[string]struct{}) {
	switch t := n.(type) {
	case varNode:
		out[t.name] = struct{}{}
	case unaryNode:
		collectVars(t.child, out)
	case binaryNode:
		collectVars(t.left, out)
		collectVars(t.right, out)
	}
}

func eval(n node, vars map[string]float64) (float64, error) {
	switch t := n.(type) {
	case numberNode:
		return t.value, nil
	case varNode:
		v, ok := vars[t.name]
		if !ok {
			return 0, fmt.Errorf("変数 %q に値がありません", t.name)
		}
		return v, nil
	case unaryNode:
		v, err := eval(t.child, vars)
		if err != nil {
			return 0, err
		}
		return -v, nil
	case binaryNode:
		l, err := eval(t.left, vars)
		if err != nil {
			return 0, err
		}
		r, err := eval(t.right, vars)
		if err != nil {
			return 0, err
		}
		switch t.op {
		case '+':
			return l + r, nil
		case '-':
			return l - r, nil
		case '*':
			return l * r, nil
		case '/':
			if r == 0 {
				return 0, fmt.Errorf("0 で除算しました")
			}
			return l / r, nil
		}
	}
	return 0, fmt.Errorf("不正なノードです: %T", n)
}

// precedence は演算子の優先順位。カッコを最小限にするために使う。
func precedence(n node) int {
	switch t := n.(type) {
	case binaryNode:
		switch t.op {
		case '+', '-':
			return 1
		case '*', '/':
			return 2
		}
	case unaryNode:
		return 3
	}
	return 4 // 数値・変数
}

func render(n node, ref Ref, b *strings.Builder, parent int) error {
	switch t := n.(type) {
	case numberNode:
		b.WriteString(strconv.FormatFloat(t.value, 'g', -1, 64))
		return nil
	case varNode:
		cell, err := ref(t.name)
		if err != nil {
			return err
		}
		b.WriteString(cell)
		return nil
	case unaryNode:
		b.WriteByte('-')
		return render(t.child, ref, b, precedence(t))
	case binaryNode:
		prec := precedence(t)
		wrap := prec < parent
		if wrap {
			b.WriteByte('(')
		}
		if err := render(t.left, ref, b, prec); err != nil {
			return err
		}
		b.WriteByte(t.op)
		// 右辺は同順位でも結合方向の都合でカッコが要る（a-(b-c) など）。
		if err := render(t.right, ref, b, prec+1); err != nil {
			return err
		}
		if wrap {
			b.WriteByte(')')
		}
		return nil
	}
	return fmt.Errorf("不正なノードです: %T", n)
}
