# genkit-agent-cloud-cost-estimate

AWS の構成をチャットで相談しながら決め、**コスト見積もり Excel** と **構成図** を出力するエージェント。
Genkit Go で実装する。

> **ステータス: 実装中（M6 まで完了）**
> チャットで構成を相談し、確認のうえで構成図（SVG / PNG / drawio）と
> 見積もり Excel を生成できる。MVP の 9 サービスに対応済み。残りは Web サービス化。

## これは何か

提案・設計の初期フェーズで繰り返し発生する「AWS 構成のコスト見積もりと構成図作成」を自動化する。

出力するもの:

| 成果物 | 形式 | 特徴 |
|---|---|---|
| 見積もり | Excel（3 シート） | 金額セルがすべて**数式**。ツールを再実行せずに前提を変えて試算できる |
| 構成図 | SVG / PNG | レイアウトエンジンが座標を計算するため、矢印・枠・アイコンが必ず揃う |
| 構成図（編集用） | drawio XML | 座標が揃った状態で渡るので、受け取った側が手で直せる |
| 構成データ | JSON（IR） | 全成果物の正本。再生成に LLM を通さない |

## 動かす

```sh
# IR JSON から構成図を生成する（.svg / .png / .drawio）
go run ./cmd/render -in examples/ir/web-3tier.json -out out/web-3tier.svg
go run ./cmd/render -in examples/ir/web-3tier.json -out out/web-3tier.png
go run ./cmd/render -in examples/ir/web-3tier.json -out out/web-3tier.drawio

# 生成される D2 ソースを確認する（-out 省略時は標準出力）
go run ./cmd/render -in examples/ir/web-3tier.json

# IR JSON から成果物一式（IR / SVG / drawio / Excel）を生成する
go run ./cmd/estimate -in examples/ir/web-3tier.json -out-dir out

# 形式を選ぶ（ir / svg / png / drawio / xlsx）
go run ./cmd/estimate -in examples/ir/web-3tier.json -formats svg,png,xlsx

# チャットで構成を相談してから生成する（要 GEMINI_API_KEY）
GEMINI_API_KEY=... go run ./cmd/chat -session my-estimate
```

LLM は経由しない。IR JSON さえあれば図と見積もりが出る。

PNG は実行環境のフォントを使う（[ADR-0014](docs/adr/0014-resvg-wasm-for-png.md)）。
macOS / Linux の一般的なパスを探すが、見つからない場合は `PNGOptions.FontData` で渡す。

`cmd/estimate` は `estimate` Flow を実行する。`GENKIT_ENV=dev` を付けると
Genkit の Reflection API が立ち上がり、Dev UI から Flow を確認できる。

AWS Pricing MCP Server（`uvx` 経由）に接続するため `pricing:*` 権限を持つ
AWS 認証情報が必要。単価取得の疎通だけを確認する場合:

```sh
AWS_PRICING_MCP_E2E=1 go test ./internal/cost/ -run E2E -v
```

## 進捗

| # | マイルストーン | 状態 |
|---|---|---|
| M0 | IR スキーマ確定 | 完了 |
| M1 | IR + D2 レンダリング | 完了（SVG / PNG / drawio XML） |
| M2 | catalog + PriceSource | 完了（基盤 5 サービスの drivers と MCP 経由の単価取得） |
| M3 | Excel 生成 | 完了（3 シート・金額セルは全て数式） |
| M4 | estimate Flow | 完了（`estimate` Flow に結合。LLM は通らない） |
| M5 | intake agent + 選択 UI | 完了（tool interrupt による選択肢での問い返し） |
| M6 | サーバーレス 4 サービス追加 | 完了（Lambda / ECS(Fargate) / API Gateway / DynamoDB） |

## パッケージ構成

| パス | 役割 |
|---|---|
| `internal/ir` | IR の型定義・JSON 入出力・バリデーション |
| `internal/catalog` | サービス定義 YAML の読み込み。`Resource.service` の値域と `params` のスキーマを供給する |
| `internal/diagram` | IR → D2 ソース → SVG / PNG / drawio XML |
| `internal/formula` | `quantity_formula` の式エンジン（評価と Excel 数式化） |
| `internal/pricing` | `PriceSource` 抽象と、AWS Pricing MCP Server 実装（`awsmcp`） |
| `internal/cost` | IR + catalog + 単価 → 見積もり明細 |
| `internal/workbook` | 見積もり明細 → Excel（3 シート・数式） |
| `internal/estimateflow` | 上記を結合した Genkit の `estimate` Flow。preview API に依存しない |
| `internal/intake` | 構成をヒアリングする対話エージェント。preview API を使うのはここだけ |
| `cmd/render` | IR JSON から図を出す CLI |
| `cmd/estimate` | IR JSON から成果物一式を出す CLI（`estimate` Flow を実行する） |
| `cmd/chat` | チャットで構成を相談し、確認後に成果物を生成する CLI |
| `examples/ir` | 手書きの IR サンプル（3 層 Web / サーバーレス API） |

## 設計の核

このプロジェクトの課題は例外なく「**LLM に数字と座標を出させていること**」に起因する。そこを分離する。

```
LLM が出すもの   → 構成の IR（どのサービスを、いくつ、どう繋ぐか）+ 前提条件
Go が決定的にやる → 単価の取得、金額の計算、図のレイアウト、Excel 生成
```

LLM には**金額を一切出させない**。出させるのは「ALB×1、EC2 t3.medium×2 を 24h/日、RDS db.r6g.large Multi-AZ、S3 500GB」といった数量と前提だけ。
単価はコードが API 経由で引き、合計は Excel の計算式が出す。

これにより:

- 検証は「前提が妥当か」だけになる。金額の電卓検算が不要になる
- 座標を誰も書かないので、矢印と枠は原理的に必ず揃う

```
                    ┌→ D2 → SVG/PNG（構成図）
LLM → Architecture ─┼→ drawio XML（手直ししたい人向け）
       (JSON で保存) └→ excelize → Excel（見積もり）
```

## 設計原則

迷ったらここに戻る。

1. **LLM に金額を出させない。** 数量と前提だけ出させる
2. **LLM に座標を出させない。** レイアウトエンジンに任せる
3. **LLM に MCP のフィルタを組ませない。** catalog の定義をコードが使う
4. **Excel の金額セルは必ず数式。** 定数を焼き込まない
5. **IR は JSON で保存する。** 再生成に LLM を通さない
6. **preview API への依存は対話層だけに閉じる**

## ドキュメント

| ドキュメント | 内容 |
|---|---|
| [docs/PRD.md](docs/PRD.md) | 何を、誰のために、なぜ作るか。スコープとマイルストーン |
| [docs/requirements.md](docs/requirements.md) | 要件定義。機能要件・非機能要件・制約・受け入れ条件 |
| [docs/adr/](docs/adr/README.md) | 決定事項（[MADR 4.0.0](https://adr.github.io/madr/) 形式） |
| [docs/archive/](docs/archive/) | 上記の元になった引き継ぎ資料。参考用で、正本ではない |

## 技術スタック

| 領域 | 採用 | 決定 |
|---|---|---|
| フレームワーク | Genkit Go | [ADR-0007](docs/adr/0007-genkit-go-with-preview-api-isolation.md) |
| 構成図 | [D2](https://d2lang.com/)（Go ライブラリとして import） | [ADR-0003](docs/adr/0003-d2-for-diagram-rendering.md) |
| Excel | [excelize](https://github.com/xuri/excelize) | [ADR-0006](docs/adr/0006-excelize-with-formula-cells.md) |
| 単価取得 | AWS Pricing MCP Server（`PriceSource` 抽象の背後） | [ADR-0004](docs/adr/0004-aws-pricing-mcp-server-behind-pricesource.md) |

## MVP のスコープ

| 軸 | 範囲 |
|---|---|
| クラウド | AWS のみ（GCP は抽象のみ用意） |
| サービス | EC2 / ALB / RDS / S3 / データ転送 / Lambda / ECS(Fargate) / API Gateway / DynamoDB の 9 種 |
| 料金モデル | オンデマンドのみ（割引は Assumptions の割引率セルで表現） |
| データ転送 | インターネット egress と AZ 間の 2 経路のみ |
| 配布形態 | Web サービス |

詳細は [docs/PRD.md](docs/PRD.md) を参照。

## セルフホストと有料サービスについて

本ソフトウェアは **AGPL-3.0** で公開している。セルフホストは自由に行える。
セルフホストする場合は、`pricing:*` 権限を持つ読み取り専用の IAM ユーザーを自前で用意する必要がある
（請求データへのアクセス権は不要）。

将来的にマネージドな有料サービスとしての提供を予定しているが、本リポジトリのコードは AGPL-3.0 のまま公開を続ける。

→ [ADR-0012](docs/adr/0012-agpl-license-with-commercial-saas.md)

## ライセンス

[AGPL-3.0](LICENSE)
