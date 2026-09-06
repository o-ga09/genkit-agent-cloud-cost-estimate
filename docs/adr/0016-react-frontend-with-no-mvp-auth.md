---
status: "accepted"
date: 2026-09-06
decision-makers: プロダクトオーナー, 開発担当
informed: ツールを利用するチーム
---

# フロントエンドは React、認証は MVP では未実装とする

## Context and Problem Statement

[ADR-0010](0010-web-service-as-delivery-form.md) で配布形態を Web サービスと決めたが、
フロントエンドの実装手段（フレームワークの要否）と、FR-WEB-5（未認証アクセスの遮断）の
実装方式は未決のままだった（`docs/requirements.md` §12 未決事項）。
`internal/webapi`（HTTP 層）と `web/`（フロントエンド）を実装するにあたり、この 2 点を決める必要がある。

## Decision Drivers

* tool interrupt の選択肢（`Choice`）と会話履歴を状態として持つ UI を、素の DOM 操作より
  宣言的に書きたい
* NFR-4（可搬性: 実行に外部バイナリ・Node.js を必要としない）を壊さないこと
* 認証方式（社内 SSO / メールドメイン制限 / 招待制）はまだ選定できていないが、
  Web UI 自体の実装は先に進めたい

## Considered Options

* フロントエンド: 素の HTML/CSS/JS（ビルド不要） / React + TypeScript + Vite
* 認証: MVP では未実装（セルフホスト時はリバースプロキシに委ねる） / 共有トークン認証 / Basic 認証

## Decision Outcome

採用した選択肢: 「React + TypeScript + Vite」と「認証は MVP では未実装」。

**フロントエンド**: React を採用した。チャット + 選択 UI + 構成案の確認画面は
状態遷移（`intro` → `asking`/`proposed` → `result`）が明確な SPA で、コンポーネント単位の
宣言的な UI が素の DOM 操作より保守しやすい。ビルド成果物（`web/dist`）は静的ファイルであり、
`cmd/server` はそれを配信するだけなので、**実行時**に Node.js は不要（NFR-4 は壊れない。
Node.js が要るのはフロントエンドの**ビルド時**のみ）。

**認証**: MVP では実装しない。認証方式そのものが未決事項であり、方式を選ばずに
Web UI と API を先に動かせる状態を優先した。`internal/webapi.Server` は
`net/http.Handler` を返すだけなので、認証方式が決まり次第、`Routes()` の手前に
ミドルウェアとして差し込める。セルフホストする場合は、決まるまでの間はリバースプロキシ側
（Cloud Run の IAP、nginx Basic 認証など）でアクセス制御することを前提とする。

### Consequences

* Good, because チャット UI の状態管理が素の DOM 操作より見通しよく書ける
* Good, because `internal/webapi` は `net/http` 標準ライブラリだけに依存し、
  認証ミドルウェアを後から差し込める形になっている
* Bad, because フロントエンドのビルドに Node.js とその依存関係が要る（実行時ではなくビルド時）
* Bad, because FR-WEB-5（未認証アクセスの遮断）は本 ADR の時点では未達成。
  そのまま公開インターネットに晒すと、見積もり実行と保存済み IR の閲覧が誰でもできてしまう

### Confirmation

* `cmd/server` が Node.js の実行バイナリを呼び出していないことをコードレビューで確認する
* 認証方式が決まったら、この ADR の status を更新するか、新しい ADR を起こして FR-WEB-5 を実装する

## More Information

認証方式・ホスティング先は `docs/requirements.md` の未決事項として残る。
本番公開前には、認証未実装であることを利用者に明示するか、リバースプロキシでの
アクセス制御を必須にすること。
