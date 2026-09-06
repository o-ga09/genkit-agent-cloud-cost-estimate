---
status: "accepted"
date: 2026-09-06
decision-makers: プロダクトオーナー, 開発担当
consulted: docs/requirements.md
informed: 見積もりを利用する営業・PM
---

# Web API のルーティングを Echo v5 に、リクエスト検証を go-validator v10 に、対話セッションストアを DynamoDB 対応にする

## Context and Problem Statement

`internal/webapi`（ADR-0010）はこれまで標準ライブラリの `net/http.ServeMux` と手書きの
`if req.Message == ""` のような ad hoc なバリデーションで実装していた。プロダクトオーナーから、
API 実装を Echo v5（`github.com/labstack/echo/v5`）+ go-validator v10
（`github.com/go-playground/validator/v10`）に変更し、`internal/intake.SessionStore`
（対話セッションの保存先）を DynamoDB に対応させる指示があった。対応する GitHub Issue は
現時点で存在しないが、[ADR-0020](0020-aws-serverless-hosting.md) でホスティング先を AWS の
サーバーレス構成に決めたことと合わせ、本番運用を見据えた変更として一気に対応する。

## Decision Drivers

* プロダクトオーナーからの直接の指示（ユーザープロンプトがプロジェクトの既定方針に優先する）
* [ADR-0020](0020-aws-serverless-hosting.md) で Lambda + API Gateway 上で動かす前提になったため、
  ルーティングとバリデーションを本番相当のライブラリに揃えておきたい
* 既存のテスト（`internal/webapi/*_test.go`）は `httptest.NewServer(srv.Routes())` を使う
  ブラックボックステストのため、`Routes()` が `http.Handler` を返す限り実装の中身を変えても
  互換性を保てる

## Considered Options

* 現状維持（`net/http.ServeMux` + ad hoc バリデーション）
* Echo v5 + go-validator v10 に変更する
* gin や chi など他のフレームワークに変更する

## Decision Outcome

採用した選択肢: 「Echo v5 + go-validator v10 に変更する」。

* ルーティングは `*echo.Echo`（`http.Handler` を実装）に置き換えるが、CORS ミドルウェア
  （`withCORS`）は net/http レベルの薄いラッパーのまま維持する。Echo 本体を包む形にすることで、
  既存の CORS 挙動・テスト（`cors_test.go`）に手を入れずに済む
* リクエスト DTO（`startSessionRequest` 等）に `validate:"required"` / `validate:"min=1"` の
  struct tag を付け、`bindAndValidate` ヘルパー（`c.Bind` → `validator.Struct`）で検証する。
  従来の `if req.Message == ""` 相当の挙動（400 を返す）は tag ベースの検証に置き換わるが、
  レスポンスの形（`{"error": "..."}`）とステータスコードは変えない
* `internal/intake.SessionStore` に `DynamoStore`（`internal/intake/dynamostore.go`）を追加する。
  テーブルはパーティションキー `id`（S）のみを持ち、セッション本体は `MemoryStore` / `FileStore`
  と同じ JSON エンコーディングを `data` 属性 1 つに丸ごと詰める（DynamoDB 側のスキーマを
  セッションの内部構造に追従させない）。`cmd/server` に `-session-store=memory|dynamodb` と
  `-dynamodb-session-table` を追加し、切り替え可能にする
* `internal/webapi.ArchitectureStore`（保存済み構成のパーマリンク用ストア）は対象外。
  今回の指示は「セッションストア」（対話の状態）に限定される。ArchitectureStore の
  DynamoDB 対応が必要になった場合は別途決定する

### Consequences

* Good, because Echo のルーティングと go-validator の struct tag により、リクエスト検証が
  宣言的になり ad hoc な if 文が減る
* Good, because `Routes()` が `http.Handler` を返す契約を維持したため、既存のハンドラテストは
  無修正で通る（ブラックボックステストの効果）
* Good, because DynamoDB セッションストアは `SessionStore` インターフェースの実装追加のみで、
  `intake.Agent` 側の変更が要らない
* Neutral, because Echo v5 はまだ新しく（2026年時点でリリースされたばかり）、v4 系に比べて
  エコシステム（ミドルウェア等）が薄い。今回は独自の CORS ミドルウェアを使うため実害はない
* Bad, because 依存が増える（`labstack/echo/v5`, `go-playground/validator/v10`,
  `aws/aws-sdk-go-v2/service/dynamodb` 系一式）

## More Information

DynamoDB のテーブル作成・IAM 権限付与などのインフラ構築は本 ADR の対象外
（[ADR-0020](0020-aws-serverless-hosting.md) 参照）。
