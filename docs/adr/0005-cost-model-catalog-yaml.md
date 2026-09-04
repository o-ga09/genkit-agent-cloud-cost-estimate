---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」
informed: catalog を保守する開発担当
---

# コストモデルと価格クエリを catalog YAML に宣言的に定義する

## Context and Problem Statement

単価が引けても、それだけでは見積もりにならない。EC2 なら「時間 × 台数 + EBS の GB 月 + データ転送」というように、
サービスごとのコストモデル（何を何倍するか）を自前で持つ必要がある。
またこのコストモデルは、Price List API に渡すフィルタ条件と対になっている。これをどこに、どう表現するか。

## Decision Drivers

* 価格フィルタを LLM に組ませない（[ADR-0001](0001-llm-outputs-only-quantities-and-assumptions.md) / [ADR-0004](0004-aws-pricing-mcp-server-behind-pricesource.md)）
* フィルタ条件のレビューをサービスごとに 1 回で終わらせたい
* 価格を引けないサービスを LLM が勝手に提案する事故を防ぎたい
* サービス追加を Go コードの改修ではなく定義ファイルの追加で行いたい

## Considered Options

* サービスごとのコストモデルと価格クエリを YAML（catalog）に宣言的に書く
* Go コードにサービスごとのハンドラを実装する
* コストモデルも LLM に生成させる

## Decision Outcome

採用した選択肢: 「サービスごとのコストモデルと価格クエリを YAML（catalog）に宣言的に書く」。
フィルタ定義が 1 箇所に集まってレビュー可能になり、さらに同じ catalog が LLM に提示する選択肢リストを兼ねられるため。

```yaml
# catalog/ec2.yaml
service: ec2
display: Amazon EC2
icon: https://icons.../EC2.svg
params:                       # LLM が IR に入れてよいパラメータの定義
  instanceType: {type: string, required: true}
  count:        {type: int, default: 1}
drivers:
  - id: instance_hours
    unit: hour
    price_query:              # ← このフィルタはコードが使う。LLM は触らない
      serviceCode: AmazonEC2
      instanceType: "{{instanceType}}"
      tenancy: Shared
      operatingSystem: Linux
      preInstalledSw: NA
      capacitystatus: Used
    quantity_formula: "count * hours_per_day * days_per_month"
  - id: ebs
    unit: GB-Mo
    price_query:
      serviceCode: AmazonEC2
      volumeApiName: gp3
    quantity_formula: "count * ebs_gb"
```

**この catalog が同時に LLM の選択肢リストになる。** `list_supported_services` ツールで catalog の内容を返すことで、
価格を引けないサービスを LLM が提案する事故を防ぐ。`params` は LLM が `Resource.Params` に入れてよいキーの定義でもあり、
`Resource.Service` の値域（enum）も catalog が定義する。

### Consequences

* Good, because フィルタ定義のレビューがサービスごとに 1 回で済み、以降は繰り返し検証しなくてよい
* Good, because catalog がそのまま LLM の選択肢リストになり、対応範囲と提案範囲が構造的に一致する
* Good, because サービス追加が YAML ファイルの追加で完結する
* Good, because `params` が IR のバリデーションスキーマを兼ねる
* Bad, because `quantity_formula` を評価する式エンジンを実装・保守する必要がある
* Bad, because 単純な掛け算に収まらない料金体系（段階課金など）は YAML だけでは表現しきれず、拡張が必要になる
* Bad, because catalog の記述漏れ・誤りは全見積もりに波及するため、初回レビューの品質が重要になる

### Confirmation

* catalog に定義のない `Resource.Service` を含む IR はバリデーションエラーとして弾く（テストで確認）
* `price_query` を組み立てるコードが catalog 以外の入力（LLM 出力）からフィルタキーを受け取らないことをコードレビューで確認する
* 各サービスの `price_query` について、AWS の料金ページと突き合わせた初回レビューの記録を残す

## More Information

MVP で用意する catalog は [ADR-0008](0008-mvp-service-scope.md) で定めた 9 サービス分。
`quantity_formula` が参照する変数は IR の `Assumptions`（[ADR-0002](0002-architecture-ir-as-json-source-of-truth.md)）と `Resource.Params` から解決する。
`quantity_formula` は Excel の数式にも写像される（[ADR-0006](0006-excelize-with-formula-cells.md)）。
