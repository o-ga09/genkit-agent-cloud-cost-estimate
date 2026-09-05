# Architecture Decision Records

このディレクトリはプロジェクトの決定事項を [MADR 4.0.0](https://adr.github.io/madr/) 形式で記録する。

* 新しい ADR は [template/adr-template.md](template/adr-template.md) をコピーして作成する
* ファイル名は `NNNN-短いタイトル-ハイフン区切り.md`（英小文字）
* 番号は採番済みの最大値 +1。欠番は作らない
* 一度 `accepted` になった ADR は書き換えず、覆すときは新しい ADR を起こして旧 ADR の `status` を `superseded by [ADR-NNNN](...)` に更新する

## 一覧

| # | タイトル | Status |
|---|---|---|
| [0001](0001-llm-outputs-only-quantities-and-assumptions.md) | LLM には数量と前提のみを出力させ、金額・座標・価格フィルタは決定的コードが担う | accepted |
| [0002](0002-architecture-ir-as-json-source-of-truth.md) | 構成の中間表現 `Architecture`（IR）を JSON で永続化し、全成果物の唯一の正本とする | accepted |
| [0003](0003-d2-for-diagram-rendering.md) | 構成図のレンダリングに D2 を Go ライブラリとして採用する | accepted |
| [0004](0004-aws-pricing-mcp-server-behind-pricesource.md) | 単価取得に AWS Pricing MCP Server を用い、`PriceSource` インターフェースの背後に隠す | accepted |
| [0005](0005-cost-model-catalog-yaml.md) | コストモデルと価格クエリを catalog YAML に宣言的に定義する | accepted |
| [0006](0006-excelize-with-formula-cells.md) | 見積もり Excel を excelize で 3 シート構成として生成し、金額セルはすべて数式にする | accepted |
| [0007](0007-genkit-go-with-preview-api-isolation.md) | 実装フレームワークに Genkit Go を採用し、preview API への依存を対話層だけに閉じる | accepted |
| [0008](0008-mvp-service-scope.md) | MVP のサービス対応範囲を基盤 5 種 + サーバーレス 4 種の計 9 サービスとする | accepted |
| [0009](0009-on-demand-pricing-only.md) | 料金モデルはオンデマンドのみを扱い、割引は Assumptions の割引率セルで表現する | accepted |
| [0010](0010-web-service-as-delivery-form.md) | 配布形態を Web サービスとする | accepted |
| [0011](0011-aws-first-and-limited-data-transfer-model.md) | AWS を先行実装し、データ転送コストは主要経路のみモデル化する | accepted |
| [0012](0012-agpl-license-with-commercial-saas.md) | ライセンスを AGPL-3.0 とし、OSS 公開・セルフホスト・自社有料サービスを両立させる | accepted |
| [0013](0013-self-written-formula-engine.md) | `quantity_formula` の式エンジンを自前で実装し、変数名を IR の JSON フィールド名に揃える | accepted |
| [0014](0014-resvg-wasm-for-png.md) | SVG → PNG の変換に resvg を WASM（wazero）で埋め込む | accepted |
| [0015](0015-intake-custom-turn-loop.md) | intake agent は自前の対話ループを持ち、preview API の利用を interrupt ツールだけに絞る | accepted |

## 関連ドキュメント

* [../PRD.md](../PRD.md) — プロダクト要求仕様
* [../requirements.md](../requirements.md) — 要件定義
