---
status: "accepted"
date: 2026-09-06
decision-makers: プロダクトオーナー, 開発担当
informed: ツールを利用するチーム
---

# フロントエンドはオブジェクトストレージ + CDN から配信し、cmd/server は API 専用とする

## Context and Problem Statement

[ADR-0016](0016-react-frontend-with-no-mvp-auth.md) では、`cmd/server` が
`web/dist`（React のビルド成果物）を静的配信する構成で実装した。
その後、フロントエンドは S3 / Cloudflare R2 のようなオブジェクトストレージ + CDN から
配信する想定であることが明確になった。この場合、フロントエンドと API サーバーは
別オリジンになる。`internal/webapi` と `web/` をこの構成に合わせる必要がある。

## Decision Drivers

* 静的アセットの配信をオブジェクトストレージ + CDN に任せ、`cmd/server`（Go）を
  API だけに専念させたい（スケーリング・キャッシュ・コストの観点で有利）
* フロントエンドと API が別オリジンになるため、ブラウザの CORS 制約を満たす必要がある
* NFR-4（可搬性）・ADR-0016 の「実行時に Node.js を要求しない」という前提は変えない

## Considered Options

* `cmd/server` が `web/dist` を配信し続ける（同一オリジン。ADR-0016 のまま）
* フロントエンドをオブジェクトストレージ + CDN から配信し、`cmd/server` は API 専用にする（CORS 対応）

## Decision Outcome

採用した選択肢: 「フロントエンドをオブジェクトストレージ + CDN から配信し、
`cmd/server` は API 専用にする」。

* `internal/webapi.Server` に `AllowedOrigins []string` を追加した。空なら CORS ヘッダを
  一切付けない（同一オリジン配信時の既定動作を壊さない）。値を設定すると、該当オリジンの
  プリフライト（OPTIONS）と実リクエストに `Access-Control-Allow-*` を付ける
* `GET /healthz` を追加した。ロードバランサ / CDN のオリジンヘルスチェック用
* `cmd/server` の `-static-dir` は既定を空にした。空なら静的ファイルを一切配信しない
  （フロントエンドは別途 S3 / R2 + CDN にデプロイする前提）。値を指定すれば
  ADR-0016 のときと同じ「`cmd/server` 自身が配信する」簡易モードとしても動く
* フロントエンドはビルド時に `VITE_API_BASE_URL` で API サーバーの絶対 URL を埋め込む
  （`web/src/api.ts`）。ダウンロードリンクなど、サーバーが返す相対パスもこれで解決する。
  未設定なら相対パス（ローカル開発の vite proxy、または簡易モードでの同一オリジン配信に対応）

ADR-0016 の「React を採用する」「認証は MVP では未実装」という決定自体は変わらない。
本 ADR はその配信トポロジだけを更新する。

### Consequences

* Good, because 静的アセットの配信を CDN に任せられる（キャッシュ・スケーリング・コスト）
* Good, because `cmd/server` は API 専用のステートレスな Go バイナリのままでいられる
  （Cloud Run 等へのデプロイが素直になる）
* Good, because 同一オリジン配信（簡易モード）も引き続きサポートしており、
  すぐに CDN を用意できない場合の後退経路がある
* Bad, because CORS はブラウザの読み取り制御に過ぎず、アクセス制御ではない。
  FR-WEB-5（未認証アクセスの遮断）は依然として未達成のままである
* Bad, because デプロイ手順が「フロントエンドのビルド + アップロード」と
  「API サーバーの起動」の 2 系統に分かれ、CI/CD がやや複雑になる

### Confirmation

* `AllowedOrigins` が空のときに `Access-Control-Allow-Origin` が一切付かないことをテストで確認する
  （`internal/webapi/cors_test.go`）
* 許可したオリジンからのプリフライト・実リクエストの双方にヘッダが付くことをテストで確認する
* `web/src/api.ts` の `apiUrl` が `VITE_API_BASE_URL` を正しく反映することをビルドして確認する

## More Information

認証方式が決まった場合、`AllowedOrigins` の設定と認証ミドルウェアは独立に効く
（CORS は「誰が呼べるか」ではなく「ブラウザがレスポンスを読めるか」の制御でしかない）ため、
FR-WEB-5 の実装時に本 ADR の内容を見直す必要はない想定。
