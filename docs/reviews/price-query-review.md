# catalog の price_query レビュー記録

| 項目 | 内容 |
|---|---|
| レビュー日 | 2026-09-05 |
| 対象 | `internal/catalog/data/*.yaml` の全 driver（基盤 5 サービス） |
| 検証リージョン | ap-northeast-1 |
| 方法 | AWS Price List API（AWS Pricing MCP Server 経由）に catalog のフィルタをそのまま投げ、**1 SKU に一意に解決すること**と、返る単位が catalog の `unit` と一致することを確認した |

[FR-CAT-5](../requirements.md#6-機能要件-コストカタログ) の「各 `price_query` のレビュー記録」にあたる。
catalog を変更したときは、このドキュメントを更新すること。

再実行:

```sh
AWS_PRICING_MCP_E2E=1 go test ./internal/cost/ -run E2E -v
```

## 結果

すべての driver が 1 SKU に解決し、単位も一致した。単価はレビュー時点（2026-09-05）の ap-northeast-1 の値。

| service / driver | serviceCode | 主なフィルタ | SKU | 単位 | 単価 (USD) |
|---|---|---|---|---|---|
| ec2 / instance_hours | AmazonEC2 | productFamily=Compute Instance, instanceType, tenancy=Shared, operatingSystem=Linux, preInstalledSw=NA, capacitystatus=Used | 77PTRYZ5MAUP8HU6 (t3.medium) | Hrs | 0.0544 |
| ec2 / ebs_gb_month | AmazonEC2 | productFamily=Storage, volumeApiName=gp3 | C8Y3GJZBQTH8T5JV | GB-Mo | 0.096 |
| alb / alb_hours | AWSELB | productFamily=Load Balancer-Application, groupDescription=LoadBalancer hourly usage by Application Load Balancer | 98ZU8QNDMR4AS8FJ | Hrs | 0.0243 |
| alb / alb_lcu_hours | AWSELB | productFamily=Load Balancer-Application, groupDescription=Used Application Load Balancer capacity units-hr | F2UQT6CZGM7BTWS8 | LCU-Hrs | 0.008 |
| rds / instance_hours | AmazonRDS | productFamily=Database Instance, instanceType, databaseEngine, deploymentOption | CHNNA42S59D3JB99 (db.r6g.large / PostgreSQL / Multi-AZ) | Hrs | 0.54 |
| rds / storage_gb_month | AmazonRDS | productFamily=Database Storage, databaseEngine, deploymentOption, volumeType=General Purpose-GP3 | V3SXBAA72P93JFMB | GB-Mo | 0.276 |
| s3 / storage_gb_month | AmazonS3 | productFamily=Storage, volumeType=Standard | CCBFTMENTDWZHXHX | GB-Mo | 0.025（段階課金の第 1 階層） |
| s3 / put_requests | AmazonS3 | productFamily=API Request, group=S3-API-Tier1, groupDescription=PUT/COPY/POST or LIST requests | XAKHUSC46NWC9PPM | Requests | 0.0000047 |
| s3 / get_requests | AmazonS3 | productFamily=API Request, group=S3-API-Tier2, groupDescription=GET and all other requests | 9C7873KU3ZQBD6BJ | Requests | 0.00000037 |
| data_transfer / internet_egress | AWSDataTransfer | transferType=AWS Outbound, fromLocation=Asia Pacific (Tokyo) | 9ESU2G5WSY6FMZR3 | GB | 0.114（段階課金の第 1 階層） |
| data_transfer / inter_az | AWSDataTransfer | transferType=IntraRegion, fromLocation=Asia Pacific (Tokyo) | RVH645383RKU285J | GB | 0.01 |

## レビューで確認した点と判断

* **フィルタは 1 SKU に絞れていること。** 複数該当した場合、コードは 1 件目を採らずエラーにする
  （`pricing.AmbiguousError`）。ALB と S3 リクエストは `productFamily` だけでは Outposts 版・
  Trust Store 版・Annotation Requests まで該当したため、`groupDescription` を足して一意にした。
* **`usagetype` は使わない。** `APN1-BoxUsage:t3.medium` のようにリージョン接頭辞が入るため、
  catalog をリージョン非依存に保てない。リージョン非依存の属性だけでフィルタしている。
* **データ転送はグローバルサービス。** `region` を渡すと 0 件になるため、`price_query.scope: global`
  として region を渡さず、`fromLocation` に location 名（`Asia Pacific (Tokyo)` など）を渡す。
  リージョンコードから location 名への対応表は `internal/cost/region.go` にある。
* **段階課金は第 1 階層のみを採用する。** S3 ストレージ（〜50TB/月）とインターネット egress（〜10TB/月）が
  該当する。取得した `Price.Tiered` が true になり、上限は `TierUpperBound` に入る。
  上位階層は未計上のため、大容量では過大評価になる（Excel の「未計上の項目」に出す: FR-XLS-7）。
* **オンデマンドのみを参照している。** MCP には `output_options.pricing_terms: ["OnDemand"]` を渡しており、
  Reserved は取得対象から除外される（FR-PRC-6 / ADR-0009）。

## 既知の未計上（この catalog では見積もらない）

* ALB の LCU のうち、新規接続数・アクティブ接続数・ルール評価数の次元（処理データ量の次元のみ計上）
* EBS の追加 IOPS / スループット、RDS のバックアップストレージと追加 IOPS
* S3 のライフサイクル移行リクエスト、データ取り出し料金
* インターネット egress の無料枠（月 100GB）
* NAT Gateway 処理料、リージョン間転送、VPC エンドポイント、CloudFront 経由の転送（[ADR-0011](../adr/0011-aws-first-and-limited-data-transfer-model.md)）
* ライセンス込みの RDS エンジン（Oracle / SQL Server）
