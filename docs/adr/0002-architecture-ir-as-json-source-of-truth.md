---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」
informed: 見積もりを利用する営業・PM
---

# 構成の中間表現 `Architecture`（IR）を JSON で永続化し、全成果物の唯一の正本とする

## Context and Problem Statement

このツールは構成図（SVG/PNG/drawio）と見積もり Excel という複数の成果物を出す。
これらを個別に生成すると、図と Excel の内容がズレる。また「毎回このツールで出力するのは面倒」という要求があり、
一度作った見積もりを LLM を通さずに再生成・再見積もりできる必要がある。
すべての成果物が派生する中心データをどう置くか。

## Decision Drivers

* 図と Excel が同じ構成を表していることを構造的に保証したい
* 再レンダリング・再見積もりを LLM を通さずに実行したい（コスト・レイテンシ・非決定性の排除）
* 成果物のレビュー対象を 1 つのファイルに集約したい
* 図・Excel・catalog の実装を並行して進められるようにしたい

## Considered Options

* `Architecture` 構造体を JSON で永続化し、図・Excel・drawio をそこから派生させる
* 会話履歴（LLM のセッション）を正本とし、必要なときに再生成する
* Excel を正本とし、図は Excel のシートから起こす

## Decision Outcome

採用した選択肢: 「`Architecture` 構造体を JSON で永続化し、図・Excel・drawio をそこから派生させる」。
1 つの決定的なデータから全成果物が導出されるため、成果物間の不整合が原理的に起きず、再生成に LLM を必要としないため。

```go
type Architecture struct {
    Provider    string      `json:"provider"`    // "aws" | "gcp"
    Region      string      `json:"region"`      // "ap-northeast-1"
    Assumptions Assumptions `json:"assumptions"` // Excel の入力セルになる
    Resources   []Resource  `json:"resources"`
    Edges       []Edge      `json:"edges"`
}

type Resource struct {
    ID      string         `json:"id"`      // "web-alb"（一意）
    Service string         `json:"service"` // catalog の enum に制約する
    Label   string         `json:"label"`   // 図に出す表示名
    Parent  string         `json:"parent"`  // "vpc-main" → 図の入れ子になる。空可
    Params  map[string]any `json:"params"`  // {"instanceType":"t3.medium","count":2}
}

type Edge struct {
    From  string `json:"from"`
    To    string `json:"to"`
    Label string `json:"label"`
}

type Assumptions struct {
    HoursPerDay   float64 `json:"hoursPerDay"`
    DaysPerMonth  float64 `json:"daysPerMonth"`
    RequestsPerMo float64 `json:"requestsPerMonth"`
    FxRate        float64 `json:"fxRate"` // USD/JPY
}
```

派生関係:

```
                    ┌→ D2 → SVG/PNG（構成図）
LLM → Architecture ─┼→ drawio XML（手直ししたい人向け）
       (JSON で保存) └→ excelize → Excel（見積もり）
```

IR スキーマの確定を最優先タスクとする。確定すれば図・Excel・catalog は独立に並行実装できる。

### Consequences

* Good, because 図と Excel が同じデータから出るため、両者の内容がズレない
* Good, because 再レンダリングも再見積もりも LLM を通さずにできる
* Good, because レビュー対象が JSON 1 ファイルに集約され、差分も見やすい
* Good, because IR が確定した時点で図・Excel・catalog を並行実装できる
* Bad, because IR スキーマの変更が全下流（図・Excel・catalog・LLM の出力スキーマ）に波及する
* Bad, because 保存済み IR に対するスキーマバージョニングと移行の仕組みが別途必要になる

### Confirmation

* 図生成・Excel 生成・drawio 生成の各関数が、引数として `*Architecture` のみを取ることをコードレビューで確認する（他のソースを参照していないこと）
* 保存済み IR JSON から成果物を再生成するゴールデンテストを置く
* `Architecture` に金額・座標のフィールドが混入していないことを確認する（[ADR-0001](0001-llm-outputs-only-quantities-and-assumptions.md)）

## More Information

`Resource.Service` の値域は catalog（[ADR-0005](0005-cost-model-catalog-yaml.md)）が定義する enum に制約する。
`Resource.Parent` は D2 のコンテナのネストに写像する（[ADR-0003](0003-d2-for-diagram-rendering.md)）。
`Assumptions` は Excel の Assumptions シートの入力セルに 1:1 で対応する（[ADR-0006](0006-excelize-with-formula-cells.md)）。
