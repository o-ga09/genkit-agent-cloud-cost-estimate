---
status: "accepted"
date: 2026-09-05
decision-makers: プロダクトオーナー, 開発担当
consulted: [ADR-0005](0005-cost-model-catalog-yaml.md), [ADR-0006](0006-excelize-with-formula-cells.md)
informed: catalog を保守する開発担当
---

# `quantity_formula` の式エンジンを自前の小さなパーサとして実装し、変数名を IR の JSON フィールド名に揃える

## Context and Problem Statement

catalog の `quantity_formula`（例: `count * hoursPerDay * daysPerMonth`）は 2 通りに使われる。

1. Go 側で数量を評価する（見積もりの内部計算・検証）
2. **Excel の数式に写像する**（[ADR-0006](0006-excelize-with-formula-cells.md)。`Estimate` シートの金額セルは
   `=Prices!C5*Assumptions!$B$3*730` のように、変数が入力セルへの参照に置き換わった数式でなければならない）

2 が本質的な制約である。式を「評価するだけ」の仕組みでは足りず、**式の構造を保ったまま
変数をセル参照に差し替えて出力できる**必要がある。式エンジンをどう用意するか。

あわせて、式が参照する変数の命名も決める必要がある。[ADR-0005](0005-cost-model-catalog-yaml.md) の
例は `count * hours_per_day * days_per_month`（snake_case）だが、IR の `Resource.params` のキーは
`instanceType` / `ebsGb` のように camelCase であり、2 つの命名規則が 1 つの式に混在してしまう。

## Decision Drivers

* 式を評価するだけでなく、AST を保ったまま Excel 数式にレンダリングできること
* catalog を書く人が読める式であること（四則演算とカッコで足りる）
* 未定義変数・ゼロ除算・構文エラーを catalog のロード時またはテストで検出できること
* 依存を増やさないこと（[NFR-4](../requirements.md#10-非機能要件) 可搬性）

## Considered Options

* 四則演算だけの小さなパーサを自前で実装し、AST を評価と Excel 出力の両方に使う
* `expr-lang/expr` などの汎用式エンジンを使い、Excel 数式は別途文字列置換で作る
* `quantity_formula` をやめ、driver ごとに Go のハンドラを書く

## Decision Outcome

採用した選択肢: 「四則演算だけの小さなパーサを自前で実装し、AST を評価と Excel 出力の両方に使う」。

汎用式エンジンは評価には便利だが、Excel 数式への写像には結局 AST へのアクセスが要る。
必要な文法は `+ - * / ( )` と単項マイナス、数値、識別子だけで、これは 200 行程度で書ける。
1 つの AST から「Go での評価」と「Excel 数式の生成」の両方を出すほうが、2 つの経路が食い違う余地がない。

**変数名は IR の JSON フィールド名に揃える。** `Resource.params` のキー（`count` / `ebsGb` / `storageGb` …）と
`Assumptions` の JSON フィールド名（`hoursPerDay` / `daysPerMonth` / `requestsPerMonth` / `fxRate` /
`discountRate`、および `extra` のキー）をそのまま式の変数として使う。
[ADR-0005](0005-cost-model-catalog-yaml.md) の例にある snake_case は採らない。

```yaml
quantity_formula: "count * hoursPerDay * daysPerMonth"
```

サポートする文法はこれだけとする。

```
expr   := term (('+' | '-') term)*
term   := factor (('*' | '/') factor)*
factor := '-' factor | number | ident | '(' expr ')'
```

条件分岐は式に持ち込まない。「Multi-AZ なら別の単価」のような分岐は、
driver の適用条件（`when`）と catalog のパラメータ値で表現する。

### Consequences

* Good, because 同じ AST から評価と Excel 数式を出すため、Go の計算と Excel の計算が食い違わない
* Good, because 変数の命名規則が 1 つになり、catalog を書くときに IR の JSON をそのまま見ればよい
* Good, because 外部依存が増えず、単一バイナリの可搬性を保てる
* Neutral, because 文法が四則演算に限られるため、段階課金や条件分岐は式では表現できない（catalog 側で表現する）
* Bad, because パーサとその保守は自前の責任になる（テストで担保する）
* Bad, because [ADR-0005](0005-cost-model-catalog-yaml.md) に載っている snake_case の例は、この ADR により無効になる

### Confirmation

* catalog の全 `quantity_formula` がロード時にパースされ、構文エラーで catalog のロードが失敗することをテストで確認する
* 式が参照する変数が catalog の `params` または `Assumptions` のフィールドに存在することをテストで確認する
* 同じ式から生成した Go の評価結果と Excel 数式が、同じ入力に対して同じ値になることを確認する（Excel 生成は M3）

## More Information

* 式の変数解決は `Resource.params` を優先し、無ければ `Assumptions` を見る（同名のキーは params が勝つ）
* `Assumptions.extra` のキーも変数として参照できる。Excel では追加の入力セルになる
