# PRD: クラウドコスト見積もり＆構成図作成エージェント

| 項目 | 内容 |
|---|---|
| ステータス | Draft |
| 最終更新 | 2026-09-04 |
| オーナー | プロダクトオーナー |
| 関連ドキュメント | [requirements.md](requirements.md) / [ADR 一覧](adr/README.md) |

このドキュメントは「何を、誰のために、なぜ作るか」を定める正本である。
「どう作るか」の技術的決定は [ADR](adr/README.md)、詳細な要件と受け入れ条件は [requirements.md](requirements.md) を参照する。

---

## 1. 背景

AWS 構成のコスト見積もりと構成図作成は、提案・設計の初期フェーズで繰り返し発生する作業である。
現状は AWS Cost MCP・drawio・手作業の組み合わせで行われており、以下の困りごとが報告されている。

| # | 課題 | 本質的な原因 |
|---|---|---|
| 1 | AWS Cost MCP のセットアップが面倒。AWS Profile が無いと正確なコストが取れない | 認証前提のツールに依存している |
| 2 | drawio で矢印がうまく繋がらない、綺麗に描けない | 座標を人間（または LLM）が手で置いている |
| 3 | 枠やアイコンを揃えたいが揃わない | 同上 |
| 4 | 時間がかかる | 上記の手作業の積み重ね |
| 5 | AI に MCP を使わせて出させても、検証に時間がかかる | LLM が金額とフィルタ条件を直接出しているので、全行を検算する羽目になる |

これらは個別の不具合ではなく、**LLM に数字と座標を出させている**という一つの構造的原因に帰着する。

## 2. プロダクトの定義

AWS の構成をチャットで相談しながら決め、次の 2 つを出力する Web サービス。

* **コスト見積もり Excel** — 金額セルがすべて数式で、再実行なしにパラメータを変えられる
* **構成図** — SVG / PNG。手直ししたい人向けに drawio XML も出力する

中核となる設計方針は責務の分離である。

| 担当 | 出力するもの |
|---|---|
| LLM | 構成の中間表現（どのサービスをいくつ、どう繋ぐか）+ 前提条件 |
| Go コード | 単価の取得、金額の計算、図のレイアウト、Excel 生成 |

LLM には金額を一切出させない。出させるのは「ALB×1、EC2 t3.medium×2 を 24h/日、RDS db.r6g.large Multi-AZ、S3 500GB」といった数量と前提だけである。
単価はコードが API 経由で引き、合計は Excel の計算式が出す。

→ [ADR-0001](adr/0001-llm-outputs-only-quantities-and-assumptions.md)

## 3. ターゲットユーザー

| ユーザー | 状況 | このプロダクトに求めるもの |
|---|---|---|
| 提案・設計を行うエンジニア | 顧客や社内向けに AWS 構成案とその概算費用を出す | 構成図と見積もりを短時間で、かつ自分で検証できる形で得たい |
| 見積もりをレビューする側 | 出てきた見積もりの妥当性を判断する | 数字の根拠を追える。全行を検算しなくて済む |
| 見積もりを使う営業・PM | 顧客との条件交渉で数字を動かす | 「ユーザー数が 3 倍になったら」をツールに戻らず自分で試したい |

## 4. 解決すること / 解決しないこと

### 解決すること

* 構成図の作図作業（座標・枠・アイコンの調整）をなくす
* 見積もり金額の全行検算をなくし、検証を「前提が妥当か」の確認だけにする
* 利用者側の AWS 認証情報セットアップを不要にする
* 一度作った見積もりを、ツールに戻らず前提の変更だけで再利用できるようにする

### 解決しないこと（MVP）

* 実際の請求データの分析（Cost Explorer の代替ではない）
* Reserved Instances / Savings Plans / スポットの正確な価格計算 → [ADR-0009](adr/0009-on-demand-pricing-only.md)
* GCP をはじめとする AWS 以外のプロバイダ → [ADR-0011](adr/0011-aws-first-and-limited-data-transfer-model.md)
* 対応 9 サービス + NAT Gateway（[ADR-0018](adr/0018-nat-gateway-cost-model.md)）以外の見積もり → [ADR-0008](adr/0008-mvp-service-scope.md)
* リージョン間転送・VPC エンドポイント・CloudFront 経由など、NAT Gateway 以外のデータ転送コスト → [ADR-0011](adr/0011-aws-first-and-limited-data-transfer-model.md)
* 構成図の細かなレイアウト調整（レイアウトエンジンに委ねる） → [ADR-0003](adr/0003-d2-for-diagram-rendering.md)

## 5. ユーザー体験

### 5.1 主要フロー

```
1. 利用者が Web サービスを開き、作りたいシステムをチャットで説明する
2. エージェントが不足情報を選択 UI で問い返す（想定ユーザー数、可用性要件、稼働時間 …）
3. エージェントが構成案（IR）を提示する。利用者は前提条件を確認・修正する
4. 確定すると、構成図（SVG/PNG/drawio）と見積もり Excel が生成される
5. 利用者は Excel の Assumptions シートを書き換えて、条件違いの試算を自分で行う
6. IR は保存され、パーマリンクからいつでも再レンダリング・再見積もりできる
```

ステップ 5 と 6 が「毎回このツールで出力するのは面倒」への回答である。
ステップ 5 はツールに戻らずに済み、ステップ 6 は LLM を通さずに済む。

### 5.2 成果物

| 成果物 | 形式 | 用途 |
|---|---|---|
| 構成図 | SVG / PNG | 提案資料への貼り付け |
| 構成図（編集用） | drawio XML | 受け取った側が手で直したいとき。座標はコードが計算済みなので揃った状態で渡る |
| 見積もり | Excel（3 シート） | 費用の提示と条件変更の試算 |
| 構成データ | JSON（IR） | 再生成・差分レビュー・共有 |

### 5.3 Excel の構成

| シート | 内容 |
|---|---|
| `Assumptions` | 稼働時間・リクエスト数・ストレージ GB・為替・割引率などの**入力セル**（色分けして編集可能と明示） |
| `Prices` | 引いてきた単価。**SKU ID・取得日・リージョンを併記** |
| `Estimate` | `=Prices!C5 * Assumptions!$B$3 * 730` のような数式。合計と「未計上の項目」も記載 |

金額セルに定数を焼き込まない。SKU ID と取得日を載せるのは検証時間対策であり、
全行を検算する代わりに怪しい行だけを AWS の料金ページと突き合わせられるようにするためである。

→ [ADR-0006](adr/0006-excelize-with-formula-cells.md)

## 6. MVP のスコープ

| 軸 | MVP の範囲 | 根拠 |
|---|---|---|
| クラウド | AWS のみ | [ADR-0011](adr/0011-aws-first-and-limited-data-transfer-model.md) |
| サービス | EC2 / ALB / RDS / S3 / データ転送 / Lambda / ECS(Fargate) / API Gateway / DynamoDB の 9 種 | [ADR-0008](adr/0008-mvp-service-scope.md) |
| 料金モデル | オンデマンドのみ（割引は Assumptions の割引率セルで表現） | [ADR-0009](adr/0009-on-demand-pricing-only.md) |
| データ転送 | インターネット egress と AZ 間の 2 経路のみ | [ADR-0011](adr/0011-aws-first-and-limited-data-transfer-model.md) |
| 配布形態 | Web サービス | [ADR-0010](adr/0010-web-service-as-delivery-form.md) |

MVP 後の拡張: NAT Gateway（時間課金 + データ処理料）を catalog に追加した（[ADR-0018](adr/0018-nat-gateway-cost-model.md)）。

## 7. 成功指標

| 指標 | 現状 | 目標 |
|---|---|---|
| 構成図 1 枚の作成時間 | 手作業で数十分〜 | 作図作業ゼロ（レイアウト調整の工数が発生しない） |
| 見積もりの検証時間 | 全行を電卓で検算 | 「前提が妥当か」の確認のみ。金額の検算をしない |
| 条件変更の試算 | ツールを再実行 | Excel の入力セル書き換えのみ（ツール再実行ゼロ） |
| 利用開始までのセットアップ | AWS Profile の設定が必要 | URL を開くだけ（利用者側の設定ゼロ） |
| 図と Excel の内容不一致 | 手作業由来で発生しうる | 構造的にゼロ（同一 IR から生成） |

## 8. 主要な設計判断

詳細は各 ADR を参照。

| # | 決定 |
|---|---|
| [0001](adr/0001-llm-outputs-only-quantities-and-assumptions.md) | LLM には数量と前提のみを出力させ、金額・座標・価格フィルタは決定的コードが担う |
| [0002](adr/0002-architecture-ir-as-json-source-of-truth.md) | 構成の中間表現（IR）を JSON で永続化し、全成果物の唯一の正本とする |
| [0003](adr/0003-d2-for-diagram-rendering.md) | 構成図のレンダリングに D2 を Go ライブラリとして採用する |
| [0004](adr/0004-aws-pricing-mcp-server-behind-pricesource.md) | 単価取得に AWS Pricing MCP Server を用い、`PriceSource` の背後に隠す |
| [0005](adr/0005-cost-model-catalog-yaml.md) | コストモデルと価格クエリを catalog YAML に宣言的に定義する |
| [0006](adr/0006-excelize-with-formula-cells.md) | Excel を excelize で 3 シート構成として生成し、金額セルはすべて数式にする |
| [0007](adr/0007-genkit-go-with-preview-api-isolation.md) | Genkit Go を採用し、preview API への依存を対話層だけに閉じる |
| [0008](adr/0008-mvp-service-scope.md) | MVP のサービス対応範囲を 9 サービスとする |
| [0009](adr/0009-on-demand-pricing-only.md) | 料金モデルはオンデマンドのみを扱う |
| [0010](adr/0010-web-service-as-delivery-form.md) | 配布形態を Web サービスとする |
| [0011](adr/0011-aws-first-and-limited-data-transfer-model.md) | AWS を先行実装し、データ転送は主要経路のみモデル化する |

## 9. 開発マイルストーン

引き継ぎ資料の実装順を踏襲する。**逆順でやらないこと。**
M1〜M3 が動けば、LLM 抜きでも「JSON を書けば図と Excel が出る」ツールとして既に有用である。
そこに LLM を足す形にすることで、検証で困ったときに LLM を切り離して切り分けられる。

| # | マイルストーン | 完了条件 |
|---|---|---|
| M0 | IR スキーマ確定 | `Architecture` 型が確定し、以降の作業を並行実行できる |
| M1 | IR + D2 レンダリング | LLM なしで、手書き JSON から構成図（SVG）が出る |
| M2 | catalog + PriceSource | 基盤 5 サービスの catalog と MCP 経由の単価取得が動く |
| M3 | Excel 生成 | 3 シート・数式込みのワークブックが手書き JSON から出る |
| M4 | estimate Flow | M1〜M3 を Genkit の Flow として結合する |
| M5 | intake agent + 選択 UI | チャットと tool interrupt による構成ヒアリングが動く |
| M6 | サーバーレス 4 サービス追加 | Lambda / ECS / API Gateway / DynamoDB の catalog を追加する |
| M7 | Web サービス化 | ブラウザからチャット + 選択 UI で相談し、成果物をダウンロードできる（FR-WEB-1〜4）。認証（FR-WEB-5）は未決事項のため対象外 |

## 10. リスク

| リスク | 影響 | 緩和策 |
|---|---|---|
| Genkit Go の preview API に破壊的変更が入る | 対話層が動かなくなる | preview 依存を対話層だけに閉じる。バージョンを固定する（[ADR-0007](adr/0007-genkit-go-with-preview-api-isolation.md)） |
| catalog の `price_query` が誤っている | 全見積もりが誤る | 初回レビューを必須にする。SKU を Excel に出して事後検証できるようにする |
| 未計上のデータ転送コストで過小評価になる | 見積もりの信頼を損なう | Excel に「未計上の項目」を明示する（[ADR-0011](adr/0011-aws-first-and-limited-data-transfer-model.md)） |
| IAM クレデンシャルの管理 | セキュリティインシデント | `pricing:*` のみの読み取り専用ユーザーに限定する（[ADR-0004](adr/0004-aws-pricing-mcp-server-behind-pricesource.md)） |
| D2 のバージョン更新でレイアウトが変わる | ゴールデンテストが壊れる | バージョン固定。レイアウト差分は目視確認のうえ更新する |
| 対応 9 サービス外の相談が多い | 使えない場面が増える | catalog 追加で拡張できる設計。利用実績から追加優先度を決める |

## 11. 未決事項

[requirements.md の「未決事項」](requirements.md#12-未決事項) を参照。
