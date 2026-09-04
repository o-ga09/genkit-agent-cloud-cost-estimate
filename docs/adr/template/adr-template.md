---
# これらのメタデータ要素は任意です。不要なものは削除して構いません。
status: "proposed | rejected | accepted | deprecated | superseded by [ADR-0123](0123-example.md)"
date: YYYY-MM-DD # この決定が最後に更新された日
decision-makers: # この決定に関わった全員
consulted: # 意見を求めた相手（通常はその分野の専門家）。双方向のやり取りがある人
informed: # 進捗を共有する相手。一方向の伝達で足りる人
---

# {解決した課題と採用した解決策を表す短いタイトル}

## Context and Problem Statement

{2〜3文、または明快な問いの形で、コンテキストと課題を記述する。
課題の性質を説明する記事へのリンクや、チケットへのリンクを添えてもよい。}

<!-- この見出しと以下の3つの見出し（Decision Drivers / Considered Options / Pros and Cons of the Options）は任意です。 -->

## Decision Drivers

* {決定要因 1、例: ある力、直面している懸念、…}
* {決定要因 2、例: ある力、直面している懸念、…}
* …

## Considered Options

* {選択肢 1 のタイトル}
* {選択肢 2 のタイトル}
* {選択肢 3 のタイトル}
* …

## Decision Outcome

採用した選択肢: 「{選択肢 1 のタイトル}」。理由は {採用理由。例: 唯一の要件を満たす選択肢である／決定要因に照らして最良である／…}。

<!-- この見出しと以下の見出し（Confirmation）は任意です。 -->

### Consequences

* Good, because {良い結果、例: ある品質特性の改善、…}
* Bad, because {悪い結果、例: ある品質特性の悪化、…}
* …

### Confirmation

{この ADR の遵守と正しい実装をどう確認するかを記述する。
例: 設計/コードレビュー、ArchUnit テスト、静的解析ルールなど。
自動化されたチェックが望ましい。テストや検証を担当する個人・チームに触れてもよい。}

<!-- この見出しと以下の見出しは任意です。 -->

## Pros and Cons of the Options

### {選択肢 1 のタイトル}

<!-- この選択肢の説明は任意。より詳しい情報へのポインタでもよい。 -->

{例 | 説明 | 詳細情報へのポインタ | …}

* Good, because {論拠 a}
* Good, because {論拠 b}
* Neutral, because {論拠 c}
* Bad, because {論拠 d}
* …

### {選択肢 2 のタイトル}

{例 | 説明 | 詳細情報へのポインタ | …}

* Good, because {論拠 a}
* Good, because {論拠 b}
* Neutral, because {論拠 c}
* Bad, because {論拠 d}
* …

## More Information

{他の証拠・確信度、チームの合意状況、決定がどう・いつ再検討され、どんな条件で見直されるかなどを記述する。
関連する決定や関連リソースへのリンクを置いてもよい。}
