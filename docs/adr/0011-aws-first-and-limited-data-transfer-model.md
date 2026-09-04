---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」
informed: 見積もりを利用する営業・PM
---

# AWS を先行実装し、データ転送コストは主要経路のみモデル化する

## Context and Problem Statement

将来的に GCP にも対応したいという要求がある一方、MVP をどこで切るかを決める必要がある。
またデータ転送コストは経路が多岐にわたり（インターネット egress、AZ 間、リージョン間、NAT Gateway 処理料、
VPC エンドポイント、CloudFront 経由など）、真面目にモデル化するほど catalog が重くなる。
一方でモデル化しなければ見積もりの誤差要因になる。どこまで扱うか。

## Decision Drivers

* MVP の規模を抑え、縦串（IR → 図 → Excel）を早く通したい
* `PriceSource` 抽象と IR の `provider` フィールドは将来の GCP 対応のために用意されている
* データ転送は誤差要因になりやすく、モデル化の粒度と catalog の複雑さがトレードオフになる
* 未計上のコストがあること自体は、見積もり上で明示すれば許容できる

## Considered Options

* AWS 先行 / データ転送は主要経路のみ
* AWS 先行 / データ転送を詳細にモデル化（NAT Gateway 処理料・VPC エンドポイント・CloudFront 経由など経路別）
* AWS / GCP 両対応を最初から実装

## Decision Outcome

採用した選択肢: 「AWS 先行 / データ転送は主要経路のみ」。
GCP 同時対応は MVP を約 2 倍の規模にし、データ転送の詳細モデル化は catalog の記述量を大きく増やす。
いずれも縦串を通す前に着手すべき投資ではないため。

**プロバイダ**:

* MVP は AWS のみ実装する
* IR の `provider` フィールドと `PriceSource` インターフェースは最初から用意し、GCP 実装を後から差し込める形にしておく
* GCP は `Cloud Billing Catalog API`（`services.skus.list`）が API キーだけで叩けるため、同じ `PriceSource` 抽象に乗せる

**データ転送**:

* MVP でモデル化するのは次の 2 経路のみとする
  * インターネットへの egress（データ転送 OUT）
  * AZ 間のデータ転送
* それ以外の経路（NAT Gateway 処理料、リージョン間転送、VPC エンドポイント、CloudFront 経由など）は計上しない
* Excel の `Estimate` シートに「未計上の項目」を明示する行またはセクションを設け、何が含まれていないかを利用者が把握できるようにする

### Consequences

* Good, because MVP の実装範囲が抑えられ、縦串を早く通せる
* Good, because 抽象だけ先に用意することで、GCP 対応時に既存コードの構造変更が不要になる
* Good, because 未計上項目を明示することで、見積もりの精度に関する誤解を防げる
* Neutral, because 抽象の妥当性は GCP 実装まで検証されないため、実装時に見直しが入る可能性がある
* Bad, because NAT Gateway を含む構成では実際のコストが見積もりを上回る（未計上分だけ過小評価になる）
* Bad, because GCP 案件には MVP 時点では使えない

### Confirmation

* データ転送関連の catalog に定義されている経路が上記 2 経路であることをレビューで確認する
* 生成 Excel に「未計上の項目」の記載が含まれることをテストで検証する
* AWS 固有のロジックが `PriceSource` の AWS 実装と catalog 内に閉じており、IR や図・Excel 生成側に漏れていないことをコードレビューで確認する

## More Information

NAT Gateway は「未計上だと過小評価が大きい」典型であるため、MVP 後の拡張優先度を高くする
（[ADR-0008](0008-mvp-service-scope.md) の拡張候補と合わせて再検討する）。
GCP 対応の着手時期は未決とし、`docs/requirements.md` の未決事項に記載する。
