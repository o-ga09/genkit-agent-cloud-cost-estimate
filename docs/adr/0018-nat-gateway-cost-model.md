---
status: "accepted"
date: 2026-09-06
decision-makers: プロダクトオーナー, 開発担当
consulted: docs/reviews/price-query-review.md
informed: 見積もりを利用する営業・PM
---

# NAT Gateway をコストカタログに追加する

## Context and Problem Statement

[ADR-0008](0008-mvp-service-scope.md) は MVP のサービス対応範囲を基盤 5 種 + サーバーレス 4 種の
計 9 サービスに絞り、NAT Gateway は「MVP 後の拡張候補」として明示的に残した。
[ADR-0011](0011-aws-first-and-limited-data-transfer-model.md) も、データ転送のモデル化を
インターネット egress と AZ 間の 2 経路に絞る一方、「NAT Gateway は未計上だと過小評価が大きい
典型であるため、MVP 後の拡張優先度を高くする」と明記していた。

MVP 一式（M0〜M7）が完了したため、次の拡張として NAT Gateway のコストモデル化に着手する。

## Decision Drivers

* NAT Gateway は多くの 3 層 Web 構成に登場し、未計上のままだと見積もりの過小評価が大きい
  （ADR-0011 の指摘どおり）
* ADR-0011 の「データ転送は 2 経路のみ」という決定自体は変更しない。NAT Gateway の課金は
  `data_transfer` の経路ではなく、AWS の Price List 上も別サービス（`AmazonEC2` の
  `NAT Gateway` productFamily）に属するため、独立した catalog サービスとして追加する

## Considered Options

* `data_transfer.yaml` の `path` に `nat_gateway` を追加する（データ転送の 3 経路目として扱う）
* 独立した catalog サービス `nat_gateway` を追加する（ALB と同じ「時間課金 + 使用量課金」の 2 driver 構成）

## Decision Outcome

採用した選択肢: 「独立した catalog サービス `nat_gateway` を追加する」。
NAT Gateway は AWS の Price List 上も `AmazonEC2` の `productFamily=NAT Gateway` という
独立した課金体系であり、時間課金（Hrs）とデータ処理料（GB）の 2 軸を持つ点は
ALB の「時間課金 + LCU 課金」と同じ形。`data_transfer.yaml` の `path`（`internet_egress` /
`inter_az`）に無理に押し込むより、既存の ALB パターンを踏襲するほうが catalog の一貫性が保てる。
[FR-CAT-6](../requirements.md#6-機能要件-コストカタログ) が定める「データ転送は 2 経路のみ」は
そのまま維持される（NAT Gateway は `data_transfer` の経路を増やしていない）。

catalog（`internal/catalog/data/nat_gateway.yaml`）:

* `params`: `count`（既定 1 台。通常は可用性のため AZ ごとに 1）、`gbProcessedPerMonth`（必須）
* `drivers`:
  * `nat_gateway_hours`: `serviceCode=AmazonEC2`, `productFamily=NAT Gateway`,
    `groupDescription=Hourly charge for NAT Gateways`。数量は `count * hoursPerDay * daysPerMonth`
  * `nat_gateway_gb_processed`: 同じ `productFamily` で
    `groupDescription=Charge for per GB data processed by NatGateways`。数量は `gbProcessedPerMonth`

Price List API には NAT Gateway 関連で 6 種類の `groupDescription` があり
（クラシック版の時間課金・データ処理料、Provisioned Bandwidth オプション版の時間課金・データ処理料・
Gbps 課金、Regional NAT Gateway という新世代リソースの時間課金・データ処理料）、
`productFamily` だけでは一意に絞れない。上記の完全一致する `groupDescription` で
クラシック版の 2 つだけを対象にし、残り 4 種（Provisioned Bandwidth・Regional）は対象外とする。

### Consequences

* Good, because NAT Gateway を含む構成の見積もりが実コストに近づく
  （ADR-0011 が指摘した過小評価の主要因が 1 つ解消する）
* Good, because ALB と同じ「時間課金 + 使用量課金」パターンを再利用でき、catalog の設計が増えない
* Good, because `data_transfer.yaml` の 2 経路という決定（FR-CAT-6 / ADR-0011）に手を入れずに済む
* Neutral, because 既存の `docs/reviews/price-query-review.md` の「既知の未計上」リストから
  NAT Gateway を外し、新しい driver の SKU 検証結果を追記する必要がある
* Bad, because Provisioned Bandwidth オプションと Regional NAT Gateway（新世代）は
  引き続き未計上のまま。これらを使う構成では依然として過小評価になる

### Confirmation

* `internal/catalog/data/nat_gateway.yaml` の 2 driver が AWS Price List API で
  1 SKU に一意に解決し、単位が catalog の `unit` と一致することを確認した
  （`mcp__aws-pricing` 経由。ap-northeast-1、2026-09-06 時点）:
  * `nat_gateway_hours`: SKU `CA23TN2NAN47KGCF`、単位 `Hrs`、$0.062/時間
  * `nat_gateway_gb_processed`: SKU `3Z2F4ZNXEMZB88ED`、単位 `GB`、$0.062/GB
  詳細は `docs/reviews/price-query-review.md` に転記する
* `internal/cost/cost_test.go` の `TestBuild_NATGateway` で quantity_formula とフィルタを検証する
* `internal/workbook/layout.go` の「未計上の項目」から NAT Gateway の記載を外したことを
  `internal/workbook/workbook_test.go` で確認する

## More Information

Provisioned Bandwidth オプションと Regional NAT Gateway（新世代）への対応は、
利用実績を見て優先度を判断する（`docs/requirements.md` の未決事項）。
