---
status: "accepted"
date: 2026-09-05
decision-makers: プロダクトオーナー, 開発担当
consulted: [ADR-0007](0007-genkit-go-with-preview-api-isolation.md)
informed: 対話 UI を実装するフロント担当
---

# intake agent は自前の対話ループを持ち、preview API の利用を interrupt ツールだけに絞る

## Context and Problem Statement

[ADR-0007](0007-genkit-go-with-preview-api-isolation.md) のとおり、対話層は Genkit の preview API を使う。
Genkit Go には対話の枠組みとして 2 つの選択肢がある。

* `genkit/exp.DefineAgent` — セッション管理・スナップショット・HTTP 提供まで面倒を見る Agents API
* `genkit.Generate` のループを自分で回し、preview からは `DefineInterruptibleTool` だけを使う

[CON-2](../requirements.md#11-制約) のとおり preview API はマイナーリリースでも壊れうる。
どちらに乗るかで、破壊的変更を受ける面積が変わる。

## Decision Drivers

* preview API への依存面積を最小にしたい（壊れたときの修正範囲を小さくする）
* IR がバリデーションに落ちたとき、理由を伝えて作り直させるループを自分で制御したい
* API キーが無い状態でも、フェイクモデルで対話ループをテストできること
* 中断した対話を再開できること（[FR-CHT-6](../requirements.md#8-機能要件-対話intake-agent)）

## Considered Options

* 自前の対話ループ + 自前のセッションストア（preview は `DefineInterruptibleTool` のみ）
* `exp.DefineAgent` に乗り、セッションストアとスナップショットも Agents API に任せる

## Decision Outcome

採用した選択肢: 「自前の対話ループ + 自前のセッションストア」。

preview から使うのは `DefineInterruptibleTool` と `tool.Interrupt` / `Resume` だけになり、
破壊的変更の影響が `internal/intake` の中でも数十行に収まる。
また、IR の組み立て（`GenerateData[Architecture]`）とバリデーション失敗時の作り直しは
安定 API の上で自分で回すため、対話の枠組みが変わっても壊れない。

セッションは会話履歴（`[]*ai.Message`）そのものを保存する。未回答の質問は履歴の最後の
メッセージにある interrupt から復元できるので、別に持たない。保存先は `SessionStore`
インターフェースにし、プロセス内（`MemoryStore`）とファイル（`FileStore`）を用意する。

### Consequences

* Good, because preview API に触れる範囲が最小になり、破壊的変更の修正が局所で済む
* Good, because フェイクモデルを差し込むだけで対話ループ全体をテストできる（API キー不要）
* Good, because IR のバリデーション失敗を理由付きで作り直させるループを自分で制御できる
* Good, because セッションの中身が会話履歴だけなので、保存形式が単純で移行もしやすい
* Neutral, because Agents API が持つスナップショットの分岐・中断制御は使わない（現時点で要求がない）
* Bad, because 対話の並行実行・ストリーミングなど、Agents API なら得られる機能は自前で足す必要がある
* Bad, because 将来 Agents API に載せ替える場合は `Agent` の中身を書き直すことになる（外の API は変えずに済む）

### Confirmation

* `internal/intake` 以外のパッケージが `genkit/go/**/exp` を import していないことをテストで確認する（NFR-5）
* フェイクモデルで、質問 → 回答 → 構成案までの 1 往復をテストで通す
* 別の Agent インスタンスから同じセッション ID で再開できることをテストで確認する（FR-CHT-6）

## More Information

* 使用するモデルは `googleai/gemini-flash-latest` を既定とする（`Options.Model` で差し替え可能）
* モデル呼び出しは `plugins/middleware` の Retry を通す。リトライ要否は `core/status` の
  センチネルで判定される（[NFR-6](../requirements.md#10-非機能要件)）
