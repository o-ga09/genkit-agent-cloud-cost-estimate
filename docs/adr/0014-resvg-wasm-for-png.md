---
status: "accepted"
date: 2026-09-05
decision-makers: プロダクトオーナー, 開発担当
consulted: [ADR-0003](0003-d2-for-diagram-rendering.md)
informed: 構成図を受け取るレビュアー
---

# SVG → PNG の変換に resvg を WASM（wazero）で埋め込む

## Context and Problem Statement

構成図は SVG で生成しているが、提案資料に貼るには PNG も必要である（[FR-DIA-5](../requirements.md#4-機能要件-構成図生成)）。
[ADR-0003](0003-d2-for-diagram-rendering.md) では「resvg または headless Chrome」として保留していた。
[NFR-4](../requirements.md#10-非機能要件) は「外部バイナリ・Node.js・CGo を必要としない」ことを求めている。

## Decision Drivers

* 単一 Go バイナリのコンテナに収めたい（外部バイナリ・Node.js・CGo を増やさない）
* 日本語を含むラベルが描けること
* AWS アイコン（外部 URL の SVG）が描けること
* レンダリング結果が SVG と一致すること（同じ D2 のレイアウト結果を使う）

## Considered Options

* resvg を WASM にしたもの（`github.com/kanrichan/resvg-go`、wazero で実行）を埋め込む
* headless Chrome / Playwright で SVG を開いてスクリーンショットを撮る
* `srwiley/oksvg` などの純 Go の SVG ラスタライザを使う
* PNG を提供せず、SVG だけを出す

## Decision Outcome

採用した選択肢: 「resvg を WASM で埋め込む」。
wazero は純 Go の WASM ランタイムで CGo も外部プロセスも要らず、resvg 本体は実績のある
レンダラなので、NFR-4 を満たしたまま実用的な品質の PNG が得られるため。

ただしそのままでは 2 つ問題があり、変換前に SVG を加工して解決している。

1. **フォント。** d2 は `@font-face` に woff を base64 で埋め込み、`d2-<hash>-font-regular` という
   独自のファミリ名を CSS で当てる。ラスタライザはこの woff を使えず、結果としてテキストが
   1 文字も描かれない。→ 変換時に独自ファミリ名を、実際に読み込むフォントのファミリ名に置換する。
   フォントは実行環境から読み込む（既定の探索先は macOS / Linux の一般的なパス。
   コンテナには Noto Sans CJK を入れる想定）。フォント名はフォントファイルの name テーブルから読む。
2. **アイコン。** ラスタライザはネットワークを見にいかないため、外部 URL のアイコンは描かれない。
   → `IconLoader` で取得して data URI として埋め込んでから変換する。取得に失敗したアイコンは
   その 1 つが描かれないだけで、図全体は生成する。

### Consequences

* Good, because 外部バイナリ・Node.js・CGo なしで PNG が出せる（NFR-4 を維持）
* Good, because SVG と同じ D2 のレイアウト結果を使うため、PNG と SVG の内容がずれない
* Good, because 日本語ラベルと AWS アイコンが描ける（実際の構成図で確認済み）
* Neutral, because 変換のたびに WASM ランタイムを起動するため、SVG 生成より数秒遅い
* Bad, because 実行環境にフォントが必要になる。フォントが無い環境では PNG だけが失敗する
* Bad, because d2 が出力する SVG の構造（`@font-face` の使い方）に依存した加工が入る。
  d2 の更新で壊れうるので、PNG 生成のテストで検知する

### Confirmation

* 生成した PNG に文字とアイコンが描かれていることを目視で確認する（リリース前チェック）
* フォントが見つからない環境では、原因の分かるエラーを返すことをテストで確認する
* 同じ IR から 2 回生成した PNG がバイト一致することをテストで確認する（NFR-1）

## More Information

* resvg: https://github.com/linebender/resvg
* wazero: https://wazero.io/
* 将来 PNG の品質や速度が問題になった場合は、headless Chrome を別サービスとして切り出す案を再検討する。
