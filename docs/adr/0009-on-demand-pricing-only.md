---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」
informed: 見積もりを利用する営業・PM
---

# 料金モデルはオンデマンドのみを扱い、割引は Assumptions の割引率セルで表現する

## Context and Problem Statement

AWS の料金にはオンデマンドのほかに Reserved Instances、Savings Plans、スポットインスタンスがある。
Price List API はオンデマンドと RI/SP を `termAttributes`（1yr/3yr、No/Partial/All Upfront）で区別して返すが、
スポット価格は変動するため Price List API では取得できず、別 API・別モデルが必要になる。
MVP でどこまで扱うか。

## Decision Drivers

* 購入オプションを増やすと catalog の `price_query` と Excel の列が増え、検証範囲が広がる
* 検証時間の短縮がこのプロジェクトの主目的であり、検証対象を増やす判断は慎重であるべき
* 実務上「RI を使えば何割引」という概算は係数で足りることが多い
* スポットは価格変動するため、そもそも固定的な見積もりに載せられない

## Considered Options

* オンデマンドのみを扱い、割引は Assumptions の入力セルで表現する
* オンデマンド + Reserved Instances / Savings Plans を扱う
* オンデマンド + スポットも扱う

## Decision Outcome

採用した選択肢: 「オンデマンドのみを扱い、割引は Assumptions の入力セルで表現する」。
Price List API の OnDemand 項のみを参照することで catalog のフィルタが単純になり検証範囲が狭まる一方、
Excel の Assumptions シートに「割引率」の入力セルを置けば、RI/SP 相当の概算は利用者が係数で表現できるため。

* `price_query` は OnDemand の単価のみを対象とする
* Excel の `Assumptions` シートに全体またはサービス区分ごとの「割引率」入力セルを設ける
* `Estimate` シートの金額数式はこの割引率セルを参照する
* Excel には「オンデマンド単価に基づく試算であり、RI/Savings Plans による実際の割引額とは異なる」旨を明記する

### Consequences

* Good, because catalog の `price_query` に `termType` 系の分岐が入らず、フィルタ定義が単純に保たれる
* Good, because 検証対象がオンデマンド単価だけになり、初回レビューの範囲が狭い
* Good, because 割引率セルにより、RI/SP 相当の概算はツール再実行なしで試せる
* Neutral, because 割引率は利用者の入力であり、その妥当性の責任は利用者側にある
* Bad, because 実際の RI/SP 価格に基づいた正確な見積もりは出せない
* Bad, because スポット前提のコスト最適化提案には使えない

### Confirmation

* catalog の全 `price_query` が OnDemand を対象としていることをレビューで確認する
* Excel の `Assumptions` シートに割引率セルが存在し、`Estimate` の数式がそれを参照していることをテストで検証する
* 生成物に「オンデマンド前提」の注記が入っていることを確認する

## More Information

RI/SP の正式対応が必要になった場合は、`PriceQuery` に `termAttributes` を追加し、
catalog の `drivers` に購入オプション別の定義を持たせる拡張になる（[ADR-0004](0004-aws-pricing-mcp-server-behind-pricesource.md) / [ADR-0005](0005-cost-model-catalog-yaml.md)）。
スポットは Price List API の対象外であり、対応する場合は `PriceSource` の別実装が必要になる。
