/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * API サーバーのベース URL（例: https://api.example.com）。
   * フロントエンドをオブジェクトストレージ + CDN（S3 / R2 等）から配信する場合に設定する。
   * 未設定なら相対パス（同一オリジン配信・ローカル開発時の vite proxy を想定）。
   */
  readonly VITE_API_BASE_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
