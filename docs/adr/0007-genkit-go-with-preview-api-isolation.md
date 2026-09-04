---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」, Genkit Go リリースノート
informed: 開発担当
---

# 実装フレームワークに Genkit Go を採用し、preview API への依存を対話層だけに閉じる

## Context and Problem Statement

実装言語・フレームワークは **Genkit Go** を使うことが要件として確定している。
一方で調査により以下が判明している。

* 最新は **Genkit Go v1.13.1**（2026-09-03 公開）。モジュールは `github.com/firebase/genkit/go`
* リポジトリは `genkit-ai/genkit` 配下に移行中（将来は `genkit-ai/genkit-go`）。現状ソース・Issue・リリースは `genkit-ai/genkit/go` を見る
* **Agents API と exp 系ツール API は preview で、マイナーリリースでも破壊的変更があると明記されている**

要求である「チャットや選択 UI で入力」は preview の tool interrupt 機能に依存する。
このフレームワーク上で、破壊的変更の影響をどう抑えるか。

## Decision Drivers

* preview API の破壊的変更が、プロダクトの価値の中心（見積もりロジック）を巻き込まないこと
* LLM を切り離しても中核機能が動き、障害の切り分けができること（[ADR-0001](0001-llm-outputs-only-quantities-and-assumptions.md)）
* 「選択 UI で入力」という要求を満たすこと
* 出力される構成が型として保証されること

## Considered Options

* 対話層（Agent + interrupt）と決定的処理層（Flow）を分離し、preview API を対話層だけに閉じる
* Agents API を全体に使い、見積もりロジックもエージェントのツールとして実装する
* preview API を使わず、安定 API だけでフォームベースの入力に割り切る

## Decision Outcome

採用した選択肢: 「対話層と決定的処理層を分離し、preview API を対話層だけに閉じる」。
価値の中心である見積もりロジックを安定 API の上に乗せることで、破壊的変更が来ても被害が対話部分に限定されるため。

```
[Agent: intake]        ← preview API。ヒアリングと構成提案の対話のみ
   ↓ Architecture (IR)
[Flow: estimate]       ← 安定 API。決定的処理のみ
   ├ PriceSource（MCP 経由）
   ├ RenderD2 / RenderDrawio
   └ BuildWorkbook
```

**選択 UI は tool interrupt で実現する。**

```go
type Choice struct {
    Question string   `json:"question"`
    Options  []string `json:"options"`
    Multi    bool     `json:"multi"`
}

askTool := genkitx.DefineInterruptibleTool(g, "ask_user",
    "構成を決めるのに必要な選択肢をユーザーに提示する",
    func(ctx context.Context, in Choice, ans *Answer) (string, error) {
        if ans == nil {
            return "", tool.Interrupt(in) // フロントが選択 UI を描画
        }
        return ans.Selected, nil
    },
)
```

`genkit.WithExperimental()` で exp サーフェスを有効化する。exp 系は `genkit/exp`（コンストラクタ）と `ai/exp`, `ai/exp/tool`（型・ヘルパー）に分かれている。

**構成の確定は型で強制する。**

```go
arch, err := genkit.GenerateData[Architecture](ctx, g,
    ai.WithModelName("googleai/gemini-flash-latest"),
    ai.WithTools(listServicesTool, askTool),
    ai.WithPrompt(...),
)
```

あわせて次を利用する。

* `plugins/middleware` の `Retry` / `Fallback` — モデル呼び出しの信頼性確保
* `core/status` のエラーセンチネル — `errors.Is` でリトライ要否を判定
* Agents のセッションストア + スナップショット — 会話をまたいだ見積もりの再開

### Consequences

* Good, because preview API の破壊的変更の影響が対話層に限定され、見積もりロジックは無傷で済む
* Good, because `estimate` Flow は IR JSON さえあれば単独で実行でき、LLM 障害時の切り分けができる
* Good, because `GenerateData[Architecture]` により LLM 出力が IR 型に適合することが保証される
* Neutral, because Genkit Go はリポジトリ移行中であり、依存パスの変更に追従する必要がある
* Bad, because preview API 依存のため、Genkit Go のマイナーバージョン更新時に対話層の改修が発生しうる
* Bad, because 層の分離により、対話中に即座に概算を返すような密結合な体験は作りにくくなる

### Confirmation

* `estimate` Flow のパッケージが `exp` 系パッケージを import していないことを静的チェックする
* IR JSON を直接入力として `estimate` Flow を実行する E2E テストを置く（対話層を通さない経路の動作保証）
* Genkit Go のバージョンを go.mod で固定し、更新時は対話層のリグレッションテストを必須にする

## More Information

* Genkit Go: `github.com/firebase/genkit/go`（v1.13.1, 2026-09-03）
* ソース・Issue・リリース: `genkit-ai/genkit` の `go` ディレクトリ
* 選択 UI を描画するフロントエンドは Web サービスとして実装する（[ADR-0010](0010-web-service-as-delivery-form.md)）
