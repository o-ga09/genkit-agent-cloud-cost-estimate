---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」
informed: ツールを利用するチーム, 運用担当
---

# 配布形態を Web サービスとする

## Context and Problem Statement

要求に「チャットや選択 UI で入力」がある。これは tool interrupt で実現するが（[ADR-0007](0007-genkit-go-with-preview-api-isolation.md)）、
interrupt から返ってくる選択肢を描画するフロントエンドが必要になる。
また [ADR-0004](0004-aws-pricing-mcp-server-behind-pricesource.md) では `pricing:*` 権限を持つ IAM ユーザーをサービス側に持たせる方針を採った。
これをどう配布するか。

## Decision Drivers

* 選択 UI をリッチに描画したい（チャット + 選択肢の提示）
* 利用者側に AWS 認証情報を持たせない（[ADR-0004](0004-aws-pricing-mcp-server-behind-pricesource.md)）
* 利用者にセットアップを要求しない（元の困りごと「セットアップが面倒」への対応）
* IR JSON を保存し、後から再見積もり・再レンダリングできること（[ADR-0002](0002-architecture-ir-as-json-source-of-truth.md)）

## Considered Options

* Web サービス
* CLI（単一 Go バイナリ）
* CLI を正本とし、対話は Genkit Dev UI で代替する

## Decision Outcome

採用した選択肢: 「Web サービス」。
IAM クレデンシャルをサーバー側に集約したまま、利用者にセットアップを一切要求せず、
tool interrupt の選択 UI をブラウザで描画できるのはこの形態のみであるため。

構成:

* サーバー: Genkit Go アプリケーション（単一バイナリ + MCP サーバーを同梱したコンテナ）
* フロントエンド: チャット + 選択 UI。interrupt のペイロード（`Choice`）を受けて選択肢を描画し、`Answer` を返す
* 成果物（SVG/PNG/drawio XML/Excel）はサーバーで生成し、ダウンロードで提供する
* IR JSON をサーバー側に保存し、パーマリンクから再レンダリング・再見積もりできるようにする
* AWS クレデンシャルはサーバー側にのみ存在し、クライアントには渡さない

### Consequences

* Good, because 利用者は URL を開くだけで使え、AWS Profile も実行環境の準備も不要になる
* Good, because IAM クレデンシャルが単一の管理下に集約され、ローテーションも一箇所で済む
* Good, because 選択 UI を要求どおりのリッチな形で提供できる
* Good, because IR JSON をサーバーに保存でき、パーマリンク共有と再生成が自然に実現する
* Bad, because フロントエンド実装・ホスティング・認証（誰が使えるか）の実装が MVP に含まれ、開発量が増える
* Bad, because サーバーの運用責任（可用性・秘密情報の管理・コスト）が発生する
* Bad, because 利用者の構成情報がサーバーに保存されるため、アクセス制御と保持ポリシーの検討が必要になる

### Confirmation

* AWS クレデンシャルがクライアントに配信されるコードパスがないことをコードレビューとネットワークトレースで確認する
* 認証されていないアクセスで見積もり実行と保存済み IR の閲覧ができないことをテストで検証する
* `estimate` Flow は HTTP 層から独立して呼び出せる状態を維持する（[ADR-0007](0007-genkit-go-with-preview-api-isolation.md)）

## More Information

`estimate` Flow を HTTP から独立させておくことで、後から CLI を薄いラッパーとして追加できる。
認証方式・ホスティング先・IR の保持期間は未決事項として `docs/requirements.md` に記載する。
