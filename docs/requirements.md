# 要件定義: クラウドコスト見積もり＆構成図作成エージェント

| 項目 | 内容 |
|---|---|
| ステータス | Draft |
| 最終更新 | 2026-09-04 |
| 関連ドキュメント | [PRD.md](PRD.md) / [ADR 一覧](adr/README.md) |

このドキュメントは要件の正本である。「なぜ作るか」は [PRD.md](PRD.md)、「どう作るか」の決定は [ADR](adr/README.md) を参照する。

要件 ID の接頭辞:

* `FR-` 機能要件
* `NFR-` 非機能要件
* `CON-` 制約
* `PRIN-` 設計原則

各要件の「根拠」欄は、その要件を導いた ADR または引き継ぎ資料の記述を指す。

---

## 1. 設計原則

実装で迷ったときはここに戻る。すべて [ADR-0001](adr/0001-llm-outputs-only-quantities-and-assumptions.md) 系列の帰結である。

| ID | 原則 | 根拠 |
|---|---|---|
| PRIN-1 | **LLM に金額を出させない。** 数量と前提だけを出させる | [ADR-0001](adr/0001-llm-outputs-only-quantities-and-assumptions.md) |
| PRIN-2 | **LLM に座標を出させない。** レイアウトエンジンに任せる | [ADR-0001](adr/0001-llm-outputs-only-quantities-and-assumptions.md) / [ADR-0003](adr/0003-d2-for-diagram-rendering.md) |
| PRIN-3 | **LLM に MCP のフィルタを組ませない。** catalog の定義をコードが使う | [ADR-0004](adr/0004-aws-pricing-mcp-server-behind-pricesource.md) / [ADR-0005](adr/0005-cost-model-catalog-yaml.md) |
| PRIN-4 | **Excel の金額セルは必ず数式。** 定数を焼き込まない | [ADR-0006](adr/0006-excelize-with-formula-cells.md) |
| PRIN-5 | **IR は JSON で保存する。** 再生成に LLM を通さない | [ADR-0002](adr/0002-architecture-ir-as-json-source-of-truth.md) |
| PRIN-6 | **preview API への依存は対話層だけに閉じる** | [ADR-0007](adr/0007-genkit-go-with-preview-api-isolation.md) |

## 2. 用語

| 用語 | 定義 |
|---|---|
| IR | Intermediate Representation。構成を表す `Architecture` 構造体。全成果物の唯一の正本 |
| catalog | サービスごとのコストモデルと価格クエリを定義した YAML |
| driver | catalog 内のコスト要素の単位。EC2 なら「インスタンス時間」「EBS GB 月」など |
| PriceSource | 単価取得の抽象インターフェース |
| Assumptions | 見積もりの前提条件。IR のフィールドであり、Excel の入力セルでもある |
| intake agent | 利用者からヒアリングして IR を組み立てる対話エージェント |
| estimate Flow | IR を入力に、図と Excel を決定的に生成する処理 |

---

## 3. 機能要件: 中間表現（IR）

| ID | 要件 | 受け入れ条件 | 根拠 |
|---|---|---|---|
| FR-IR-1 | 構成を `Architecture` 構造体として表現する。フィールドは `provider` / `region` / `assumptions` / `resources` / `edges` | 型定義が存在し、JSON にシリアライズできる | [ADR-0002](adr/0002-architecture-ir-as-json-source-of-truth.md) |
| FR-IR-2 | `Resource` は `id` / `service` / `label` / `parent` / `params` を持つ。`id` は構成内で一意 | 重複 `id` を含む IR がバリデーションで弾かれる | [ADR-0002](adr/0002-architecture-ir-as-json-source-of-truth.md) |
| FR-IR-3 | `Resource.service` の値域は catalog が定義するサービスに制約される | catalog にないサービスを含む IR がバリデーションで弾かれ、理由が利用者に伝わる | [ADR-0005](adr/0005-cost-model-catalog-yaml.md) |
| FR-IR-4 | `Resource.params` のキーと型は catalog の `params` 定義でバリデーションされる | 必須パラメータ欠落・型不一致がバリデーションで弾かれる | [ADR-0005](adr/0005-cost-model-catalog-yaml.md) |
| FR-IR-5 | `Resource.parent` により入れ子構造（VPC / AZ など）を表現できる。空を許容する | 入れ子を含む IR から、枠が入れ子になった図が生成される | [ADR-0003](adr/0003-d2-for-diagram-rendering.md) |
| FR-IR-6 | `Edge` は `from` / `to` / `label` を持ち、リソース間の接続を表す | 存在しない `id` を参照する `Edge` がバリデーションで弾かれる | [ADR-0002](adr/0002-architecture-ir-as-json-source-of-truth.md) |
| FR-IR-7 | `Assumptions` は稼働時間・稼働日数・月間リクエスト数・為替レート・割引率を持ち、拡張可能である | Excel の Assumptions シートの入力セルと 1:1 で対応する | [ADR-0002](adr/0002-architecture-ir-as-json-source-of-truth.md) / [ADR-0009](adr/0009-on-demand-pricing-only.md) |
| FR-IR-8 | IR を JSON ファイルとして保存・読み込みできる | 保存した IR を読み込んで成果物を再生成できる | [ADR-0002](adr/0002-architecture-ir-as-json-source-of-truth.md) |
| FR-IR-9 | IR に金額フィールドと座標フィールドを持たせない | 型定義のレビューで確認する | PRIN-1 / PRIN-2 |

## 4. 機能要件: 構成図生成

| ID | 要件 | 受け入れ条件 | 根拠 |
|---|---|---|---|
| FR-DIA-1 | IR から D2 ソースを生成し、SVG をレンダリングする | 手書き IR JSON から SVG が出力される（LLM 不使用） | [ADR-0003](adr/0003-d2-for-diagram-rendering.md) |
| FR-DIA-2 | 座標はレイアウトエンジン（ELK）が計算する。IR にもコードにも座標を書かない | ソースコードに座標のハードコードが存在しない | PRIN-2 |
| FR-DIA-3 | `Resource.parent` を D2 のコンテナのネストに写像する | VPC / AZ の枠が自動で囲まれた図が生成される | [ADR-0003](adr/0003-d2-for-diagram-rendering.md) |
| FR-DIA-4 | catalog に定義された `icon` を D2 の `icon:` に適用する | AWS 公式アイコンが図に表示される | [ADR-0003](adr/0003-d2-for-diagram-rendering.md) / [ADR-0005](adr/0005-cost-model-catalog-yaml.md) |
| FR-DIA-5 | SVG を PNG に変換して出力できる | 同一 IR から PNG が生成される | [ADR-0003](adr/0003-d2-for-diagram-rendering.md) |
| FR-DIA-6 | 同一 IR から drawio XML を出力できる。座標はコードが計算済みで、揃った状態で渡る | 生成した drawio XML が drawio で開け、図が崩れていない | [ADR-0003](adr/0003-d2-for-diagram-rendering.md) |
| FR-DIA-7 | 同一 IR からの再レンダリング結果は決定的である | 同じ IR を 2 回レンダリングした SVG がバイト一致する | PRIN-5 |

## 5. 機能要件: 単価取得

| ID | 要件 | 受け入れ条件 | 根拠 |
|---|---|---|---|
| FR-PRC-1 | 単価取得を `PriceSource` インターフェースで抽象化する。第一実装は AWS Pricing MCP Server 経由 | フェイク実装に差し替えて見積もりロジックをテストできる | [ADR-0004](adr/0004-aws-pricing-mcp-server-behind-pricesource.md) |
| FR-PRC-2 | `PriceQuery` は `service` / `region` / `attributes` からなり、catalog の `price_query` 定義と IR の `params` からコードが組み立てる | フィルタ値が LLM 出力から直接渡る経路が存在しない | PRIN-3 |
| FR-PRC-3 | MCP 接続を LLM のツールとして公開しない | Genkit のツール登録一覧に価格取得系ツールが含まれない | PRIN-3 |
| FR-PRC-4 | 取得した `Price` は金額・通貨・単位・SKU・取得日時を持つ | Excel の Prices シートに SKU と取得日時が出力される | [ADR-0004](adr/0004-aws-pricing-mcp-server-behind-pricesource.md) / [ADR-0006](adr/0006-excelize-with-formula-cells.md) |
| FR-PRC-5 | AWS 認証情報はサービス側が保持し、利用者に設定を要求しない | 利用者側の環境変数・プロファイル設定なしで見積もりが実行できる | [ADR-0004](adr/0004-aws-pricing-mcp-server-behind-pricesource.md) / [ADR-0010](adr/0010-web-service-as-delivery-form.md) |
| FR-PRC-6 | 参照する料金はオンデマンドのみとする | catalog の全 `price_query` が OnDemand を対象としている | [ADR-0009](adr/0009-on-demand-pricing-only.md) |
| FR-PRC-7 | 単価取得に失敗したサービスは、見積もり全体を失敗させず、該当行に取得失敗を明示する | 一部の単価取得が失敗しても他の行を含む Excel が生成される | — |

## 6. 機能要件: コストカタログ

| ID | 要件 | 受け入れ条件 | 根拠 |
|---|---|---|---|
| FR-CAT-1 | サービスごとの表示名・アイコン・パラメータ定義・drivers を YAML で定義する | catalog ファイルを追加するだけでサービスを追加できる | [ADR-0005](adr/0005-cost-model-catalog-yaml.md) |
| FR-CAT-2 | 各 driver は `id` / `unit` / `price_query` / `quantity_formula` を持つ | EC2 のインスタンス時間と EBS GB 月がそれぞれ driver として定義される | [ADR-0005](adr/0005-cost-model-catalog-yaml.md) |
| FR-CAT-3 | `quantity_formula` は IR の `Assumptions` と `Resource.params` の変数を参照できる | `count * hours_per_day * days_per_month` が評価できる | [ADR-0005](adr/0005-cost-model-catalog-yaml.md) |
| FR-CAT-4 | `list_supported_services` ツールが catalog の内容を LLM に返す | LLM が catalog にないサービスを提案しない | [ADR-0005](adr/0005-cost-model-catalog-yaml.md) |
| FR-CAT-5 | MVP で以下 9 サービスの catalog を用意する: EC2（EBS 含む）/ ALB / RDS / S3 / データ転送 / Lambda / ECS(Fargate) / API Gateway / DynamoDB | 9 ファイルが存在し、各 `price_query` のレビュー記録がある | [ADR-0008](adr/0008-mvp-service-scope.md) |
| FR-CAT-6 | データ転送 catalog はインターネット egress と AZ 間の 2 経路のみを定義する | 定義されている経路が 2 つであることをレビューで確認する | [ADR-0011](adr/0011-aws-first-and-limited-data-transfer-model.md) |

## 7. 機能要件: Excel 生成

| ID | 要件 | 受け入れ条件 | 根拠 |
|---|---|---|---|
| FR-XLS-1 | `Assumptions` / `Prices` / `Estimate` の 3 シート構成のワークブックを生成する | 3 シートが存在する | [ADR-0006](adr/0006-excelize-with-formula-cells.md) |
| FR-XLS-2 | `Assumptions` シートに稼働時間・リクエスト数・ストレージ GB・為替・割引率などの入力セルを置き、編集可能であることを色分けで示す | 入力セルが IR の `Assumptions` と対応し、書式で区別されている | [ADR-0006](adr/0006-excelize-with-formula-cells.md) |
| FR-XLS-3 | `Prices` シートに単価・SKU ID・取得日・リージョンを記載する | 各行に SKU と取得日が埋まっている | [ADR-0006](adr/0006-excelize-with-formula-cells.md) |
| FR-XLS-4 | `Estimate` シートの金額セルはすべて数式とし、定数を焼き込まない | 金額セルがすべて `=` 始まりであることをテストで検証する | PRIN-4 |
| FR-XLS-5 | `Estimate` の数式は `Prices` と `Assumptions` のセルを参照する | Assumptions の値を変更すると合計が変わる | [ADR-0006](adr/0006-excelize-with-formula-cells.md) |
| FR-XLS-6 | `Estimate` に月額合計と、円換算（為替レートセル参照）を出力する | 合計行が `SUM` 数式である | [ADR-0006](adr/0006-excelize-with-formula-cells.md) |
| FR-XLS-7 | `Estimate` に「未計上の項目」を明示する | NAT Gateway 処理料等が未計上であることが記載される | [ADR-0011](adr/0011-aws-first-and-limited-data-transfer-model.md) |
| FR-XLS-8 | 「オンデマンド単価に基づく試算であり、RI / Savings Plans による実際の割引額とは異なる」旨を記載する | 注記が生成物に含まれる | [ADR-0009](adr/0009-on-demand-pricing-only.md) |

## 8. 機能要件: 対話（intake agent）

| ID | 要件 | 受け入れ条件 | 根拠 |
|---|---|---|---|
| FR-CHT-1 | 利用者の説明を受けて、不足情報を選択 UI で問い返す | tool interrupt により選択肢がフロントに渡り、回答が処理に戻る | [ADR-0007](adr/0007-genkit-go-with-preview-api-isolation.md) |
| FR-CHT-2 | 選択肢は質問文・選択肢配列・複数選択可否を持つ | `Choice` 型として定義される | [ADR-0007](adr/0007-genkit-go-with-preview-api-isolation.md) |
| FR-CHT-3 | LLM の出力を `Architecture` 型として受け取り、型で構成を強制する | `GenerateData[Architecture]` を使用する | [ADR-0007](adr/0007-genkit-go-with-preview-api-isolation.md) |
| FR-CHT-4 | LLM に渡すツールは `list_supported_services` と `ask_user` に限る | ツール登録一覧のレビューで確認する | PRIN-1 / PRIN-3 |
| FR-CHT-5 | 提示された構成と前提条件を、生成前に利用者が確認・修正できる | 確定操作を経てはじめて成果物が生成される | [PRD 5.1](PRD.md#51-主要フロー) |
| FR-CHT-6 | 会話をまたいで見積もりを再開できる | セッションストアにより中断した対話を再開できる | [ADR-0007](adr/0007-genkit-go-with-preview-api-isolation.md) |

## 9. 機能要件: Web サービス

| ID | 要件 | 受け入れ条件 | 根拠 |
|---|---|---|---|
| FR-WEB-1 | ブラウザからチャットと選択 UI で構成を相談できる | 利用者側のインストール作業なしで利用開始できる | [ADR-0010](adr/0010-web-service-as-delivery-form.md) |
| FR-WEB-2 | 生成した SVG / PNG / drawio XML / Excel をダウンロードできる | 4 形式が取得できる | [ADR-0010](adr/0010-web-service-as-delivery-form.md) |
| FR-WEB-3 | IR JSON をサーバー側に保存し、パーマリンクから再レンダリング・再見積もりできる | 保存済み IR の URL を開くと成果物を再生成できる（LLM 不使用） | PRIN-5 / [ADR-0010](adr/0010-web-service-as-delivery-form.md) |
| FR-WEB-4 | AWS クレデンシャルをクライアントに配信しない | ネットワークトレースで確認する | [ADR-0010](adr/0010-web-service-as-delivery-form.md) |
| FR-WEB-5 | 認証されていないアクセスから、見積もり実行と保存済み IR の閲覧を遮断する | 未認証アクセスが拒否されることをテストで検証する | [ADR-0010](adr/0010-web-service-as-delivery-form.md) |

---

## 10. 非機能要件

| ID | 要件 | 受け入れ条件 | 根拠 |
|---|---|---|---|
| NFR-1 | **決定性**: 同一 IR から生成される図と Excel は毎回同一である | 2 回生成して SVG がバイト一致し、Excel の数式が一致する（単価はスタブ固定） | PRIN-5 |
| NFR-2 | **検証可能性**: 金額の根拠（SKU・取得日・リージョン）が成果物上で追跡できる | Prices シートから AWS 料金ページへの突き合わせが行える | [ADR-0006](adr/0006-excelize-with-formula-cells.md) |
| NFR-3 | **LLM 独立性**: LLM を経由せずに、IR JSON だけで図と Excel を生成できる | `estimate` Flow が対話層を通さず単独で実行できる | PRIN-5 / [ADR-0007](adr/0007-genkit-go-with-preview-api-isolation.md) |
| NFR-4 | **可搬性**: 実行に外部バイナリ・Node.js・CGo を必要としない（PNG 変換と MCP サーバーを除く） | 単一 Go バイナリを含むコンテナで動作する | [ADR-0003](adr/0003-d2-for-diagram-rendering.md) |
| NFR-5 | **preview 依存の局所化**: `estimate` Flow のパッケージが Genkit の `exp` 系パッケージを import しない | 静的チェックで検証する | PRIN-6 |
| NFR-6 | **信頼性**: モデル呼び出しは Retry / Fallback ミドルウェアを通す。リトライ要否は `core/status` のセンチネルで判定する | 一時的なモデル障害でリクエストが即座に失敗しない | [ADR-0007](adr/0007-genkit-go-with-preview-api-isolation.md) |
| NFR-7 | **セキュリティ**: サービスが保持する IAM 権限は `pricing:*` の読み取りのみ。請求データへのアクセス権を持たない | IAM ポリシーのレビューで確認する | [ADR-0004](adr/0004-aws-pricing-mcp-server-behind-pricesource.md) |
| NFR-8 | **拡張性**: サービス追加が catalog YAML の追加のみで完結する | Go コードの改修なしにサービスを 1 つ追加できる | [ADR-0005](adr/0005-cost-model-catalog-yaml.md) |
| NFR-9 | **依存バージョンの固定**: Genkit Go と D2 のバージョンを go.mod で固定する | 更新時はリグレッションテストを必須とする | [ADR-0003](adr/0003-d2-for-diagram-rendering.md) / [ADR-0007](adr/0007-genkit-go-with-preview-api-isolation.md) |

---

## 11. 制約

| ID | 制約 | 根拠 |
|---|---|---|
| CON-1 | 実装言語・フレームワークは Genkit Go（`github.com/firebase/genkit/go`）とする。要件として確定 | 引き継ぎ資料 §9 |
| CON-2 | Genkit Go の Agents API と exp 系ツール API は preview であり、マイナーリリースでも破壊的変更がありうる | [ADR-0007](adr/0007-genkit-go-with-preview-api-isolation.md) |
| CON-3 | Genkit Go のリポジトリは `genkit-ai/genkit` 配下に移行中（将来は `genkit-ai/genkit-go`）。ソース・Issue・リリースは `genkit-ai/genkit` の `go` を参照する | 引き継ぎ資料 §9 |
| CON-4 | AWS Pricing MCP Server の利用には IAM の `pricing:*` 権限が必要。API 呼び出し自体は無料 | [ADR-0004](adr/0004-aws-pricing-mcp-server-behind-pricesource.md) |
| CON-5 | スポットインスタンス価格は Price List API では取得できない | [ADR-0009](adr/0009-on-demand-pricing-only.md) |
| CON-6 | Price List Bulk API は認証不要だが EC2 で数百 MB 級。代替実装に採る場合はキャッシュと正規化が必須 | [ADR-0004](adr/0004-aws-pricing-mcp-server-behind-pricesource.md) |
| CON-7 | Excel の数式は Excel の数式エンジンを前提とする。他の表計算ソフトでの再現性は保証しない | [ADR-0006](adr/0006-excelize-with-formula-cells.md) |

---

## 12. 未決事項

引き継ぎ資料の未決事項のうち、以下は本ドキュメント作成時点で決着済みである。

| 項目 | 決定 | ADR |
|---|---|---|
| カバーするサービス範囲 | 基盤 5 種 + サーバーレス 4 種の計 9 サービス | [ADR-0008](adr/0008-mvp-service-scope.md) |
| リザーブド / Savings Plans / スポット | オンデマンドのみ。割引は Assumptions の割引率セルで表現 | [ADR-0009](adr/0009-on-demand-pricing-only.md) |
| 出力の配布形態 | Web サービス | [ADR-0010](adr/0010-web-service-as-delivery-form.md) |
| GCP 対応 | AWS 先行。抽象のみ用意 | [ADR-0011](adr/0011-aws-first-and-limited-data-transfer-model.md) |
| データ転送のモデル化 | インターネット egress と AZ 間の 2 経路のみ | [ADR-0011](adr/0011-aws-first-and-limited-data-transfer-model.md) |

未決のまま残っている項目:

* [ ] **IR スキーマの確定**（最優先。確定すれば図・Excel・catalog を並行実装できる）
  * `Assumptions` に追加すべきフィールド（成長率、月次推移の期間など）
  * IR のスキーマバージョニング方式と、保存済み IR の移行方針
* [ ] **Web サービスの認証方式**（誰が使えるか。社内 SSO / メールドメイン制限 / 招待制）
* [ ] **ホスティング先**（Cloud Run / ECS / その他）
* [ ] **保存した IR の保持期間とアクセス制御**（作成者のみか、リンクを知る人全員か）
* [ ] **使用する LLM モデルの確定**（引き継ぎ資料の例は `googleai/gemini-flash-latest`）
* [ ] **PNG 変換の実装手段**（resvg / headless Chrome）
* [ ] **`quantity_formula` の式エンジン**（既存ライブラリを使うか自前実装か）
* [ ] **アイコンの取得元と再配布可否**（AWS 公式アイコンの利用条件の確認）
* [ ] **GCP 対応の着手時期**
* [ ] **NAT Gateway 対応の着手時期**（未計上による過小評価が大きい典型のため優先度は高い）

---

## 13. スコープ外（MVP）

* 実際の請求データの分析（Cost Explorer / Billing 系の代替ではない）
* Reserved Instances / Savings Plans / スポットの正確な価格計算
* AWS 以外のクラウドプロバイダ
* 対応 9 サービス以外の見積もり
* NAT Gateway 処理料・リージョン間転送・VPC エンドポイント・CloudFront 経由のデータ転送コスト
* 構成図の細かなレイアウト調整機能（座標指定 UI 等）
* 月次推移シート・3 年 TCO シート（同じ入力セルを参照する数式として後から追加可能）
