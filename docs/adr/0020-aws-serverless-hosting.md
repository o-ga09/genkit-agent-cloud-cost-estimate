---
status: "accepted"
date: 2026-09-06
decision-makers: プロダクトオーナー
consulted: docs/requirements.md, docs/adr/0016-react-frontend-with-no-mvp-auth.md
informed: 見積もりを利用する営業・PM
---

# ホスティング先を AWS（サーバーレス: API Gateway + Lambda + DynamoDB + S3 + CloudFront + Cognito）に決定する

## Context and Problem Statement

`docs/requirements.md` §12 未決事項は「ホスティング先（Cloud Run / ECS / その他）」を
決着済み一覧から外し、未決のまま残していた。プロダクトオーナーから、ホスティング先を AWS とし、
API Gateway + Lambda + DynamoDB + S3 + CloudFront + Cognito の構成でコストを見積もる指示があった。
LLM（Gemini API）はホスティング先の変更対象外とする（引き続き `googleai/gemini-flash-latest` を
Google 側で呼び出す。ADR-0007 のとおり preview API 依存は `internal/intake` に局所化されている）。

## Decision Drivers

* プロダクトオーナーからの直接の指示
* [ADR-0011](0011-aws-first-and-limited-data-transfer-model.md) が「AWS 先行」を既に決めており、
  単価取得も AWS Pricing MCP Server 経由（[ADR-0004](0004-aws-pricing-mcp-server-behind-pricesource.md)）
  であるため、ホスティングも AWS に揃えるのが自然
* [ADR-0017](0017-frontend-served-from-object-storage-cdn.md) で「フロントエンドはオブジェクト
  ストレージ + CDN 配信」と既に決めており、S3 + CloudFront はその実装そのものになる
* `cmd/server`（`internal/webapi`）は API 専用プロセスであり、常駐サーバーを前提にしていない
  （リクエスト単位でステートレスに動く）ため Lambda + API Gateway と相性がよい

## Considered Options

* コンテナ常駐（ECS Fargate / Cloud Run）
* サーバーレス（API Gateway + Lambda + DynamoDB + S3 + CloudFront + Cognito）

## Decision Outcome

採用した選択肢: 「サーバーレス（API Gateway + Lambda + DynamoDB + S3 + CloudFront + Cognito）」。

| コンポーネント | 役割 |
|---|---|
| Amazon API Gateway | `cmd/server`（`internal/webapi.Server.Routes()`）への HTTP エントリーポイント |
| AWS Lambda | API ハンドラの実行環境（Echo v5 のルーティングごと 1 関数、または関数分割は実装時に決める） |
| Amazon DynamoDB | 対話セッションストア（[ADR-0019](0019-echo-v5-validator-v10-dynamodb-session-store.md)）。保存済み構成（`ArchitectureStore`）への適用は別途判断 |
| Amazon S3 | React フロントエンドのビルド成果物配信元（[ADR-0017](0017-frontend-served-from-object-storage-cdn.md)） |
| Amazon CloudFront | S3 の手前の CDN（ADR-0017 で決定済みの構成の具体化） |
| Amazon Cognito | Web サービスの認証（`docs/requirements.md` 未決事項「Web サービスの認証方式」FR-WEB-5 の
  実装候補。本 ADR ではコスト試算のためコンポーネント一覧に含めるが、認証方式そのものの決定は
  引き続き未決とする） |

### コスト試算

前提（本アプリを社内向け MVP 規模で自己ホストするケースを想定。すべて `ap-northeast-1`、
オンデマンド料金。取得日 2026-09-06、`mcp__aws-pricing` 経由）:

| 前提項目 | MVP 想定 | 成長時想定（10 倍） |
|---|---|---|
| Cognito MAU | 100 | 1,000 |
| API Gateway / Lambda 呼び出し数（月間） | 10,000 | 100,000 |
| Lambda 平均実行時間 / メモリ | 800ms / 512MB | 同左 |
| DynamoDB 書き込み・読み込みリクエストユニット（月間） | 各 5,000 | 各 50,000 |
| S3 保存量 | 1GB | 5GB |
| S3 GET / PUT リクエスト（月間） | 20,000 / 500 | 200,000 / 5,000 |
| CloudFront リクエスト数（月間） | 50,000 | 500,000 |
| CloudFront データ転送量（月間） | 5GB | 50GB |

月額試算（USD、単価は下表）:

| サービス | 単価根拠 | MVP 想定 | 成長時想定 |
|---|---|---|---|
| API Gateway（HTTP API） | $1.29 / 百万リクエスト | $0.01 | $0.13 |
| Lambda（リクエスト + GB秒） | $0.20/百万リクエスト、$0.0000166667/GB秒（Tier-1） | $0.07 | $0.69 |
| DynamoDB（オンデマンド） | 書込 $0.715/百万、読込 $0.1425/百万、保存 25GB まで無料 | $0.00 | $0.04 |
| S3 | 保存 $0.025/GB、GET $0.37/百万、PUT $4.70/百万 | $0.03 | $0.22 |
| CloudFront | リクエスト $0.012/千（HTTPS）、Japan→Internet 転送 $0.114/GB（先頭 10TB 帯） | $0.63 | $6.30 |
| Cognito（Lite tier） | $0.0055/MAU（0〜9万 MAU 帯） | $0.55 | $5.50 |
| **合計** | | **約 $1.30/月** | **約 $12.88/月** |

* LLM（Gemini API）はこの試算に含めない。Google 側の従量課金であり AWS のホスティング費用とは別建て
* 最小構成のため月額固定費はほぼゼロ（サーバー常駐コストがない）。コストはトラフィックに比例して線形に近い増え方をする
* CloudFront のデータ転送と Cognito の MAU 課金が、この規模ではコストの大半を占める

### Consequences

* Good, because 低トラフィックの MVP 段階ではホスティング費用がほぼゼロに近い（月額 $1〜2 程度）
* Good, because ADR-0011 / ADR-0017 が既に前提としていた「AWS + オブジェクトストレージ + CDN」と
  一貫する
* Good, because サーバー常駐（ECS Fargate 等）と違い、アイドル時の固定費がかからない
* Neutral, because Cognito はコスト試算のために含めたが、FR-WEB-5「Web サービスの認証方式」自体の
  決定はまだ行っていない。Cognito を採用するかどうかは別途判断する
* Bad, because Lambda のコールドスタートや API Gateway のペイロードサイズ制限など、サーバーレス
  特有の制約が乗る（既存の `net/http` ベースの実装からの移行コストは [ADR-0019](0019-echo-v5-validator-v10-dynamodb-session-store.md) で対応）

## More Information

`docs/requirements.md` §12 未決事項の「ホスティング先」を本 ADR により決着済みとする。
「Web サービスの認証方式」（FR-WEB-5）は引き続き未決事項として残る。
