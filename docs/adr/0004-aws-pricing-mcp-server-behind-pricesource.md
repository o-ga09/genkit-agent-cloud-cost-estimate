---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」, AWS Pricing MCP Server ドキュメント
informed: ツールを配布する運用担当
---

# 単価取得に AWS Pricing MCP Server を用い、`PriceSource` インターフェースの背後に隠す

## Context and Problem Statement

見積もりには AWS の単価が必要である。単価の取得手段として何を使うか、そしてそれを LLM にどう扱わせるか。

調査で判明している事実:

* `awslabs/mcp` の **AWS Pricing MCP Server**（`awslabs.aws-pricing-mcp-server`）が該当のサーバーで、Price List API を叩くため、これから作る構成の見積もりに使える
* Cost Analysis MCP Server は、価格系ツールについては Pricing MCP への移行を促す非推奨扱いになっている
* Cost Explorer / Billing 系の MCP は「実際に使った請求の分析」であり、今回の用途ではない
* 認証は必要で、IAM ロール/ユーザーに `pricing:*` 権限が要る。`AWS_PROFILE` 環境変数を読み、未指定なら `default` プロファイルにフォールバックする
* アクセスするのは一般公開の価格情報のみでユーザー固有データは取らない。API 呼び出しは無料
* AWS 自身が「AI アシスタントが常に正しくフィルタを構成する保証はなく、最安の選択肢を確実に特定できる保証もない」と明記している

元の困りごとに「AWS Cost MCP のセットアップが面倒。AWS Profile が無いと正確なコストが取れない」が含まれている。

## Decision Drivers

* 利用者ごとの AWS Profile 設定を不要にしたい（配布の障壁を下げる）
* 請求データへのアクセス権を要求せず、共有ツールとして配布できること
* LLM 由来のフィルタ構成ミスを構造的に排除したい
* 将来 `pricing:*` すら渡せない配布先や GCP 対応が出てきたときに差し替えられること

## Considered Options

* AWS Pricing MCP Server を Go コードから呼び、`PriceSource` インターフェースの背後に隠す
* AWS Pricing MCP Server を LLM のツールとして公開する
* Price List Bulk API（認証不要の静的 JSON）を直接叩く
* AWS SDK for Go の Pricing API クライアントを直接叩く

## Decision Outcome

採用した選択肢: 「AWS Pricing MCP Server を Go コードから呼び、`PriceSource` インターフェースの背後に隠す」。
MCP を第一実装としつつ抽象を切ることで、配布要件の変化や GCP 対応に対して実装差し替えで応じられるため。

**LLM に MCP ツールを直接握らせない。** Genkit Go には `plugins/mcp`（MCP クライアント）があるので接続はできるが、LLM のツールとしては公開しない。
catalog YAML に書いた確定的なフィルタ条件を Go コードが組み立てて MCP 経由で問い合わせる。

```
LLM      → 「EC2 t3.medium ×2」という IR を出すだけ
Go コード → catalog のフィルタ定義 + IR.Params → PriceQuery → MCP → 単価
Excel    → 単価 × 数量 の数式
```

抽象:

```go
type PriceSource interface {
    Unit(ctx context.Context, q PriceQuery) (Price, error)
}

type PriceQuery struct {
    Service    string            // "AmazonEC2"
    Region     string
    Attributes map[string]string // instanceType, tenancy, operatingSystem...
}

type Price struct {
    Amount    float64
    Currency  string
    Unit      string // "Hrs", "GB-Mo"
    SKU       string // Excel に載せて検証可能にする
    FetchedAt time.Time
}
```

**運用方針**: `pricing:*` だけを持つ読み取り専用 IAM ユーザーを 1 つ用意し、サービス側に持たせる。
請求データへのアクセス権が不要なため、共有ツールとして配布できる。利用者側の Profile 設定は不要になる。

### Consequences

* Good, because 利用者が AWS Profile を設定する必要がなくなり、セットアップの手間がほぼ解消する
* Good, because フィルタ定義のレビューは catalog に対して最初の 1 回で済み、以降 LLM 由来のフィルタ構成ミスが起きない
* Good, because Price List API 呼び出しは無料で、コスト増を伴わない
* Good, because `PriceSource` 抽象により、Bulk API 実装や GCP の Cloud Billing Catalog API 実装を後から追加できる
* Bad, because サービス側で IAM クレデンシャルを保管・ローテーションする運用責任が発生する
* Bad, because MCP サーバープロセスへの依存が実行環境に加わる（コンテナへの同梱が必要）
* Bad, because catalog にフィルタを書いていないサービスは見積もれない（[ADR-0008](0008-mvp-service-scope.md) のスコープ制約と表裏一体）

### Confirmation

* Genkit のツール登録一覧に価格取得系のツールが含まれていないことをコードレビューで確認する
* `PriceSource` のフェイク実装でユニットテストを回し、MCP 実装への依存が `PriceSource` の実装パッケージ内に閉じていることを確認する
* 取得した単価には必ず SKU と取得日時を伴わせ、Excel の Prices シートに出力する（[ADR-0006](0006-excelize-with-formula-cells.md)）

## More Information

代替実装の候補:

* **Price List Bulk API**: `https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonEC2/current/{region}/index.json` という静的 JSON。認証不要だが EC2 で数百 MB 級のため、キャッシュと正規化が必要。`pricing:*` すら渡せない配布先が出てきた場合に実装する。
* **GCP**: `Cloud Billing Catalog API`（`services.skus.list`）は API キーだけで叩けるため、同じ `PriceSource` 抽象に乗せる（[ADR-0011](0011-aws-first-and-limited-data-transfer-model.md)）。

最初は MCP 実装だけで着手してよい。
