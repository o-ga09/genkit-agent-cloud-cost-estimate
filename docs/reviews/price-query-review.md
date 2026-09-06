# catalog の price_query レビュー記録

| 項目 | 内容 |
|---|---|
| レビュー日 | 2026-09-05 |
| 対象 | `internal/catalog/data/*.yaml` の全 driver（MVP の 9 サービス） |
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
| lambda / requests | AWSLambda | productFamily=Serverless, group=AWS-Lambda-Requests | 3BE8DYKG4FYSZGDW | Request | 0.0000002 |
| lambda / duration_gb_seconds | AWSLambda | productFamily=Serverless, group=AWS-Lambda-Duration | FSYUV9NMNDEXRJ5H | Lambda-GB-Second | 0.0000166667（第 1 階層） |
| lambda / requests_arm | AWSLambda | productFamily=Serverless, group=AWS-Lambda-Requests-ARM | AA4Q79463N2JQHZA | Requests | 0.0000002 |
| lambda / duration_gb_seconds_arm | AWSLambda | productFamily=Serverless, group=AWS-Lambda-Duration-ARM | ZSE7CMBEBTMPH8ET | Lambda-GB-Second | 0.0000133334（第 1 階層） |
| ecs / vcpu_hours | AmazonECS | productFamily=Compute, usagetype **contains** Fargate-vCPU-Hours | KBQ3Q6DY9J327G8N | hours | 0.05056 |
| ecs / memory_gb_hours | AmazonECS | productFamily=Compute, usagetype **contains** Fargate-GB-Hours | JQEE6EF5FAF2AESH | hours | 0.00553 |
| ecs / vcpu_hours_arm | AmazonECS | productFamily=Compute, usagetype **contains** Fargate-ARM-vCPU-Hours | ZNX9M5D8R95VT2QQ | hours | 0.04045 |
| ecs / memory_gb_hours_arm | AmazonECS | productFamily=Compute, usagetype **contains** Fargate-ARM-GB-Hours | WXXGZM434J9JXCU5 | hours | 0.00442 |
| apigateway / http_api_requests | AmazonApiGateway | productFamily=API Calls, operation=ApiGatewayHttpApi | 5TU3JKA59EGYZ8XY | Requests | 0.0000012900（第 1 階層） |
| apigateway / rest_api_requests | AmazonApiGateway | productFamily=API Calls, operation=ApiGatewayRequest | MK9X5G6WSHJT7QGM | Requests | 0.0000042500（第 1 階層） |
| dynamodb / write_request_units | AmazonDynamoDB | productFamily=Amazon DynamoDB PayPerRequest Throughput, group=DDB-WriteUnits | HBK75W4NW4DYPFAC | WriteRequestUnits | 0.000000715 |
| dynamodb / read_request_units | AmazonDynamoDB | productFamily=Amazon DynamoDB PayPerRequest Throughput, group=DDB-ReadUnits | TX6BW4TWG2FZQMPQ | ReadRequestUnits | 0.0000001425 |
| dynamodb / storage_gb_month | AmazonDynamoDB | productFamily=Database Storage, volumeType=Amazon DynamoDB - Indexed DataStore | 2HPSAWPXJ2JJJ6XY | GB-Mo | 0.285（無料枠 25GB を超えた分） |

## 追加検証: NAT Gateway（2026-09-06 / ADR-0018）

MVP 後の拡張として追加した `nat_gateway.yaml` の 2 driver。AWS Price List API に
（`mcp__aws-pricing` 経由。MVP レビュー時と同じ AWS Price List API を参照する）
catalog のフィルタをそのまま投げ、1 SKU に一意に解決すること・単位が一致することを確認した。
検証リージョンは ap-northeast-1、レビュー時点の単価。

| service / driver | serviceCode | 主なフィルタ | SKU | 単位 | 単価 (USD) |
|---|---|---|---|---|---|
| nat_gateway / nat_gateway_hours | AmazonEC2 | productFamily=NAT Gateway, groupDescription=Hourly charge for NAT Gateways | CA23TN2NAN47KGCF | Hrs | 0.062 |
| nat_gateway / nat_gateway_gb_processed | AmazonEC2 | productFamily=NAT Gateway, groupDescription=Charge for per GB data processed by NatGateways | 3Z2F4ZNXEMZB88ED | GB | 0.062 |

`productFamily=NAT Gateway` だけでは、クラシック版（時間課金・データ処理料）に加えて
Provisioned Bandwidth オプション版（時間課金・データ処理料・Gbps 課金）と
Regional NAT Gateway（新世代。時間課金・データ処理料）の 6 SKU が該当し一意に絞れない。
`groupDescription` の完全一致でクラシック版の 2 つだけに絞っている。

## レビューで確認した点と判断

* **フィルタは 1 SKU に絞れていること。** 複数該当した場合、コードは 1 件目を採らずエラーにする
  （`pricing.AmbiguousError`）。ALB と S3 リクエストは `productFamily` だけでは Outposts 版・
  Trust Store 版・Annotation Requests まで該当したため、`groupDescription` を足して一意にした。
* **`usagetype` は使わない。** `APN1-BoxUsage:t3.medium` のようにリージョン接頭辞が入るため、
  catalog をリージョン非依存に保てない。リージョン非依存の属性だけでフィルタしている。
* **データ転送はグローバルサービス。** `region` を渡すと 0 件になるため、`price_query.scope: global`
  として region を渡さず、`fromLocation` に location 名（`Asia Pacific (Tokyo)` など）を渡す。
  リージョンコードから location 名への対応表は `internal/cost/region.go` にある。
* **段階課金は「課金が始まる最初の階層」を採用する。** 単純に第 1 階層を採ると、
  DynamoDB のストレージのように第 1 階層が無料枠（$0）の SKU で費用が丸ごと消えてしまう。
  そこで単価が 0 でない最初の階層を採る。採用した階層の範囲は `Price.TierLowerBound`〜`TierUpperBound`
  に入り、Excel の「未計上の項目」に出る（FR-XLS-7）。無料枠は未計上（＝過大評価側）になる。
* **usagetype でしか絞れない場合は部分一致を使う。** Fargate の Linux/x86 は、ARM と Windows にしか
  `cpuArchitecture` / `operatingSystem` が入っておらず、完全一致では区別できない。
  usagetype にはリージョン接頭辞（`APN1-` など）が付くため、`{contains: Fargate-vCPU-Hours}` で絞る。
* **単位の食い違いを検出できる。** catalog の `unit` と取得した単価の単位が違う行は計上しない。
  実際、Lambda の ARM 版だけ単位が `Requests`（x86 は `Request`）で、この仕組みで気づいた。
* **オンデマンドのみを参照している。** MCP には `output_options.pricing_terms: ["OnDemand"]` を渡しており、
  Reserved は取得対象から除外される（FR-PRC-6 / ADR-0009）。

## 既知の未計上（この catalog では見積もらない）

* ALB の LCU のうち、新規接続数・アクティブ接続数・ルール評価数の次元（処理データ量の次元のみ計上）
* EBS の追加 IOPS / スループット、RDS のバックアップストレージと追加 IOPS
* S3 のライフサイクル移行リクエスト、データ取り出し料金
* インターネット egress の無料枠（月 100GB）
* リージョン間転送、VPC エンドポイント、CloudFront 経由の転送（[ADR-0011](../adr/0011-aws-first-and-limited-data-transfer-model.md)）
* NAT Gateway の Provisioned Bandwidth オプションと Regional NAT Gateway（新世代）（[ADR-0018](../adr/0018-nat-gateway-cost-model.md)）
* ライセンス込みの RDS エンジン（Oracle / SQL Server）
* Lambda の無料枠、プロビジョンドコンカレンシー、エフェメラルストレージの追加分
* Fargate のエフェメラルストレージ追加分と Windows タスク
* API Gateway のキャッシュ、WebSocket API、データ転送
* DynamoDB のプロビジョンドキャパシティ、バックアップ（PITR / オンデマンド）、グローバルテーブル、ストリーム
