# web

チャット + 選択 UI のフロントエンド（React + TypeScript + Vite）。
バックエンドは `cmd/server`（`internal/webapi`）。ADR-0010 / [ADR-0016](../docs/adr/0016-react-frontend-with-no-mvp-auth.md) 参照。

**本番はオブジェクトストレージ + CDN（S3 / CloudFront、Cloudflare R2 + Cloudflare 等）からの配信を想定する。**
`cmd/server` は API 専用で、既定では静的ファイルを配信しない。

## 開発

```sh
# 別ターミナルで API サーバーを起動しておく（:8080）
GEMINI_API_KEY=... go run ../cmd/server

npm install
npm run dev   # :5173。/api は vite.config.ts の proxy で :8080 に転送される
```

ローカル開発では `VITE_API_BASE_URL` は未設定のままでよい（相対パス + vite proxy で届く）。

## ビルドとオブジェクトストレージへのデプロイ

API サーバーと別オリジンになるため、ビルド時に API のベース URL を埋め込む。

```sh
# .env.production を作り、公開する API の URL を書く
echo "VITE_API_BASE_URL=https://api.example.com" > .env.production

npm run build   # dist/ に出力
```

`dist/` の中身をそのままオブジェクトストレージにアップロードする（例）:

```sh
# S3 + CloudFront
aws s3 sync dist/ s3://your-bucket/ --delete

# Cloudflare R2（S3 互換 API）
aws s3 sync dist/ s3://your-bucket/ --endpoint-url https://<account-id>.r2.cloudflarestorage.com --delete
```

`index.html` へのフォールバック（SPA のクライアントサイドルーティング）は使っていない
（画面遷移は `?id=` クエリパラメータだけで、パスを増やさない設計になっている）ため、
バケット / CDN 側の 404 ハンドリング設定は不要。

## API サーバー側の CORS 設定

フロントエンドと API が別オリジンになるので、`cmd/server` 側でフロントエンドの
オリジンを許可する必要がある。

```sh
GEMINI_API_KEY=... go run ../cmd/server -allowed-origins https://app.example.com
```

`-allowed-origins` を空のままにすると CORS ヘッダを一切付けない
（同一オリジン配信、または `-static-dir` を使う簡易モードのときの既定）。

## 簡易モード（別オリジン配信をしない場合）

CDN を用意せず、`cmd/server` 自体にビルド成果物を配信させることもできる。

```sh
npm run build
go run ../cmd/server -static-dir web/dist
```

## 認証について

現時点で認証は未実装（`docs/requirements.md` の未決事項）。
セルフホストする場合はリバースプロキシ側でアクセス制御すること。
