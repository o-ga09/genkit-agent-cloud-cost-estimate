package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Decode は JSON を読み込んで Architecture を返す。
// 未知のフィールドはエラーにする（IR に金額・座標を紛れ込ませないため）。
// バリデーションは呼び出し側で Validate を使って行う。
func Decode(r io.Reader) (*Architecture, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var a Architecture
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("IR JSON の読み込みに失敗しました: %w", err)
	}
	if a.SchemaVersion == "" {
		a.SchemaVersion = SchemaVersion
	}
	return &a, nil
}

// Encode は Architecture を JSON として書き出す。
// 同じ IR からは常に同じバイト列になる（PRIN-5 / NFR-1）。
func Encode(w io.Writer, a *Architecture) error {
	out := *a
	if out.SchemaVersion == "" {
		out.SchemaVersion = SchemaVersion
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&out); err != nil {
		return fmt.Errorf("IR JSON の書き出しに失敗しました: %w", err)
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// LoadFile は IR JSON ファイルを読み込む。
func LoadFile(path string) (*Architecture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("IR ファイルを開けませんでした: %w", err)
	}
	defer f.Close()
	return Decode(f)
}

// SaveFile は IR を JSON ファイルとして保存する。
func SaveFile(path string, a *Architecture) error {
	var buf bytes.Buffer
	if err := Encode(&buf, a); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("IR ファイルを書き出せませんでした: %w", err)
	}
	return nil
}
