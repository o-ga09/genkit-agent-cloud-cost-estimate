package formula

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse は式をパースする。
func Parse(src string) (*Expr, error) {
	p := &parser{src: src}
	p.next()
	root, err := p.parseExpr()
	if err != nil {
		return nil, fmt.Errorf("式 %q のパースに失敗しました: %w", src, err)
	}
	if p.tok.kind != tokEOF {
		return nil, fmt.Errorf("式 %q のパースに失敗しました: 余分な入力 %q があります", src, p.tok.text)
	}
	return &Expr{src: src, root: root}, nil
}

// MustParse はパースに失敗したら panic する。catalog の定数など、
// パッケージ初期化時に決まっている式にだけ使う。
func MustParse(src string) *Expr {
	e, err := Parse(src)
	if err != nil {
		panic(err)
	}
	return e
}

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokNumber
	tokIdent
	tokOp // + - * / ( )
)

type token struct {
	kind tokenKind
	text string
	num  float64
	pos  int
}

type parser struct {
	src string
	pos int
	tok token
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("%d 文字目: %s", p.tok.pos+1, fmt.Sprintf(format, args...))
}

func (p *parser) next() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\n') {
		p.pos++
	}
	if p.pos >= len(p.src) {
		p.tok = token{kind: tokEOF, pos: p.pos}
		return
	}
	start := p.pos
	c := p.src[p.pos]
	switch {
	case strings.IndexByte("+-*/()", c) >= 0:
		p.pos++
		p.tok = token{kind: tokOp, text: string(c), pos: start}
	case c >= '0' && c <= '9' || c == '.':
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
			p.pos++
		}
		text := p.src[start:p.pos]
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			p.tok = token{kind: tokIdent, text: text, pos: start} // 数値として不正。後段でエラーにする
			return
		}
		p.tok = token{kind: tokNumber, text: text, num: f, pos: start}
	case isIdentStart(c):
		for p.pos < len(p.src) && isIdentPart(p.src[p.pos]) {
			p.pos++
		}
		p.tok = token{kind: tokIdent, text: p.src[start:p.pos], pos: start}
	default:
		p.pos++
		p.tok = token{kind: tokOp, text: string(c), pos: start} // 未知の記号。後段でエラーにする
	}
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}

func (p *parser) parseExpr() (node, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tokOp && (p.tok.text == "+" || p.tok.text == "-") {
		op := p.tok.text[0]
		p.next()
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseTerm() (node, error) {
	left, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tokOp && (p.tok.text == "*" || p.tok.text == "/") {
		op := p.tok.text[0]
		p.next()
		right, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseFactor() (node, error) {
	switch {
	case p.tok.kind == tokNumber:
		n := numberNode{value: p.tok.num}
		p.next()
		return n, nil
	case p.tok.kind == tokIdent:
		if !isIdentStart(p.tok.text[0]) {
			return nil, p.errorf("数値として解釈できません: %q", p.tok.text)
		}
		n := varNode{name: p.tok.text}
		p.next()
		return n, nil
	case p.tok.kind == tokOp && p.tok.text == "-":
		p.next()
		child, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		return unaryNode{op: '-', child: child}, nil
	case p.tok.kind == tokOp && p.tok.text == "(":
		p.next()
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.tok.kind != tokOp || p.tok.text != ")" {
			return nil, p.errorf("閉じカッコがありません")
		}
		p.next()
		return inner, nil
	case p.tok.kind == tokEOF:
		return nil, p.errorf("式が途中で終わっています")
	default:
		return nil, p.errorf("予期しない字句です: %q", p.tok.text)
	}
}
