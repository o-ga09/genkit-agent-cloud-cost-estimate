---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」
informed: 見積もりを利用する営業・PM
---

# MVP のサービス対応範囲を基盤 5 種 + サーバーレス 4 種の計 9 サービスとする

## Context and Problem Statement

見積もり対象のサービスは catalog YAML に手書きする必要があり（[ADR-0005](0005-cost-model-catalog-yaml.md)）、
サービス数がそのまま初期実装コストと検証コストになる。引き継ぎ資料は「20〜30 サービスで大半の構成をカバーできる」としつつ、
実装順としては 5 サービスから始めることを推奨していた。MVP でどこまで対応するか。

## Decision Drivers

* catalog の `price_query` は 1 サービスごとに人手のレビューが必要で、サービス数に比例して検証コストが増える
* 実際に相談される構成にサーバーレス構成が多く、5 サービスだけでは提案できる構成が限られる
* IR → 図 → Excel の縦串を先に通し、その後で横に広げたい
* 対応していないサービスは LLM が提案しないため、カバー範囲がそのままプロダクトの表現力になる

## Considered Options

* 基盤 5 種（EC2 / ALB / RDS / S3 / データ転送）のみ
* 基盤 5 種 + サーバーレス 4 種（Lambda / ECS / API Gateway / DynamoDB）の計 9 サービス
* 20〜30 サービスを一気に用意する

## Decision Outcome

採用した選択肢: 「基盤 5 種 + サーバーレス 4 種の計 9 サービス」。
基盤 5 種だけでは実際に相談される構成の多くを表現できず、一方 20〜30 サービスは縦串が通る前に横に広がるリスクがあるため、
その中間として実用最小の範囲を採る。

MVP の対応サービス:

| 区分 | サービス |
|---|---|
| 基盤 | Amazon EC2（EBS 含む） |
| 基盤 | Elastic Load Balancing（ALB） |
| 基盤 | Amazon RDS |
| 基盤 | Amazon S3 |
| 基盤 | データ転送（主要経路のみ。[ADR-0011](0011-aws-first-and-limited-data-transfer-model.md)） |
| サーバーレス | AWS Lambda |
| サーバーレス | Amazon ECS（Fargate） |
| サーバーレス | Amazon API Gateway |
| サーバーレス | Amazon DynamoDB |

実装順は基盤 5 種を先に完成させ、縦串（IR → 図 → Excel）が通ってからサーバーレス 4 種を追加する。

catalog に定義のないサービスは `list_supported_services` に現れず、LLM も提案しない。
利用者が範囲外のサービスを求めた場合は、対応していない旨を明示して伝える。

### Consequences

* Good, because 3 層 Web 構成とサーバーレス構成の双方を提案・見積もりできる
* Good, because 9 サービスであれば `price_query` の初回レビューを現実的な工数で完了できる
* Neutral, because 対応範囲外のサービスを含む構成は見積もれず、利用者に明示的に断ることになる
* Bad, because 基盤 5 種のみの場合と比べて catalog 記述とフィルタ検証の工数が約 2 倍になる
* Bad, because Fargate / Lambda / DynamoDB は課金ディメンションが多く（vCPU・メモリ・リクエスト・GB秒・RCU/WCU など）、catalog の `drivers` 定義が基盤サービスより複雑になる

### Confirmation

* MVP リリース時点で 9 サービス分の catalog YAML が存在し、各 `price_query` のレビュー記録があること
* 各サービスについて、単価取得から Excel の行生成までの結合テストが通ること
* 範囲外サービスを含む IR がバリデーションで弾かれ、利用者に理由が伝わることをテストで確認する

## More Information

MVP 後の拡張候補: CloudFront、NAT Gateway（[ADR-0011](0011-aws-first-and-limited-data-transfer-model.md) の見直しとセット）、ElastiCache、SQS/SNS、CloudWatch。
拡張は catalog YAML の追加のみで完結する設計とする（[ADR-0005](0005-cost-model-catalog-yaml.md)）。
