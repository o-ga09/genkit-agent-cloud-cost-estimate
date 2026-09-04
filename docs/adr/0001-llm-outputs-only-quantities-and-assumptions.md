---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」
informed: 見積もりを利用する営業・PM
---

# LLM には数量と前提のみを出力させ、金額・座標・価格フィルタは決定的コードが担う

## Context and Problem Statement

既存の運用では、LLM に MCP ツールを直接握らせ、金額・drawio の座標・Price List API のフィルタ条件までを出力させていた。その結果、

* 出てきた金額を全行電卓で検算する必要があり、検証に時間がかかる
* drawio の矢印が繋がらない・枠やアイコンが揃わない
* LLM が構成したフィルタが正しい保証がなく、最安の選択肢を特定できる保証もない（AWS 自身が Pricing MCP Server について明記している）

という問題が生じていた。LLM の出力責務をどこで切るべきか。

## Decision Drivers

* 見積もり結果の検証にかかる時間を「前提が妥当か」の確認だけに縮めたい
* 図の矢印・枠・アイコンが原理的に揃うようにしたい
* 同じ入力から何度実行しても同じ結果が出る（決定的である）こと
* LLM を切り離しても中核機能が動き、障害の切り分けができること

## Considered Options

* LLM は数量と前提のみを出力し、金額・レイアウト・価格フィルタは Go コードが決定的に処理する
* LLM に MCP ツールを公開し、価格取得から集計まで任せる（現状の延長）
* LLM に出力させたあと、別の LLM に検算・レビューさせる

## Decision Outcome

採用した選択肢: 「LLM は数量と前提のみを出力し、金額・レイアウト・価格フィルタは Go コードが決定的に処理する」。
本プロジェクトの課題は例外なく「LLM に数字と座標を出させていること」に起因しており、責務境界を引き直すことが唯一の根本対策であるため。

具体的な境界:

| 担当 | 出力するもの |
|---|---|
| LLM | 構成の IR（どのサービスをいくつ、どう繋ぐか）+ 前提条件 |
| Go コード | 単価の取得、金額の計算、図のレイアウト、Excel 生成 |

LLM が出してよいのは「ALB×1、EC2 t3.medium×2 を 24h/日、RDS db.r6g.large Multi-AZ、S3 500GB」といった数量と前提だけであり、金額は一切出させない。単価はコードが API 経由で引き、合計は Excel の計算式が出す。

### Consequences

* Good, because 検証が「前提が妥当か」の確認だけになり、金額の電卓検算が不要になる
* Good, because 座標を誰も書かないので、矢印と枠は原理的に必ず揃う
* Good, because LLM を切り離しても IR を手書きすれば図と Excel が出るため、障害の切り分けができる
* Good, because 同じ IR からは常に同じ図・同じ Excel が出る
* Bad, because LLM が扱える表現力が catalog に定義した範囲に制限される
* Bad, because 新しいサービスへの対応が LLM のプロンプト調整ではなく catalog 実装作業になる

### Confirmation

* LLM に渡すツール定義に、価格取得・金額計算・座標指定のツールが含まれていないことをコードレビューで確認する
* `Architecture` 型（IR）に金額フィールドと座標フィールドを持たせない。型定義がこの原則の強制装置になる
* 同一 IR を 2 回レンダリングして、生成された SVG と Excel がバイト一致することをテストする（価格取得はスタブ）

## More Information

この決定から派生する具体的な適用が [ADR-0002](0002-architecture-ir-as-json-source-of-truth.md)（IR を正本とする）、
[ADR-0003](0003-d2-for-diagram-rendering.md)（レイアウトエンジンに座標を任せる）、
[ADR-0005](0005-cost-model-catalog-yaml.md)（価格フィルタを catalog に固定する）、
[ADR-0006](0006-excelize-with-formula-cells.md)（金額セルを数式にする）である。
迷ったときはこの ADR に戻る。
