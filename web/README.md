# web

チャット + 選択 UI のフロントエンド（React + TypeScript + Vite）。
バックエンドは `cmd/server`（`internal/webapi`）。ADR-0010 参照。

## 開発

```sh
# 別ターミナルでバックエンドを起動しておく（:8080）
GEMINI_API_KEY=... go run ../cmd/server

npm install
npm run dev   # :5173。/api は vite.config.ts の proxy で :8080 に転送される
```

## ビルド

```sh
npm run build   # dist/ に出力
```

`cmd/server` は既定で `web/dist` を静的配信する（`-static-dir` で変更可）。
本番相当の動作を確認するときは、ビルド後に `go run ./cmd/server` を実行する。

## 認証について

現時点で認証は未実装（`docs/requirements.md` の未決事項）。
セルフホストする場合はリバースプロキシ側でアクセス制御すること。
