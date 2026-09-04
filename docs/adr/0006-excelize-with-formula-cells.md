---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」
informed: 見積もりを利用する営業・PM
---

# 見積もり Excel を excelize で 3 シート構成として生成し、金額セルはすべて数式にする

## Context and Problem Statement

「毎回このツールで出力するのは面倒」という要求がある。前提条件（ユーザー数、稼働時間、為替など）を変えるたびに
ツールを再実行し、LLM を通し直すのは実用に耐えない。また「検証に時間がかかる」という課題に対して、
出てきた数字の根拠を追える形で提示する必要がある。Excel の生成方法とシート構成をどう決めるか。

## Decision Drivers

* ツールを再実行せずに前提を変更できること（「ユーザー数が 3 倍になったら?」に即答できる）
* 金額の根拠（どの SKU の単価をいつ取得したか）を追跡できること
* 月次推移や 3 年 TCO のような派生シートを後から足せること
* Go の単一バイナリで生成でき、Excel/LibreOffice がインストールされた環境を必要としないこと

## Considered Options

* `github.com/xuri/excelize/v2` で 3 シート構成のワークブックを生成し、金額セルはすべて数式にする
* 計算済みの数値を焼き込んだ Excel を出力する
* CSV を出力し、Excel 化は利用者に任せる

## Decision Outcome

採用した選択肢: 「excelize で 3 シート構成のワークブックを生成し、金額セルはすべて数式にする」。
数式にしておけば Assumptions シートの入力セルを書き換えるだけで再計算されるため、再実行なしに使い回せる（要求への直接の回答）ため。

| シート | 内容 |
|---|---|
| `Assumptions` | 稼働時間・リクエスト数・ストレージ GB・為替・成長率などの**入力セル**（色分けして編集可能と明示） |
| `Prices` | 引いてきた単価。**SKU ID・取得日・リージョンを併記** |
| `Estimate` | `=Prices!C5 * Assumptions!$B$3 * 730` のような数式 |

```go
f.SetCellFormula("Estimate", "D5", "=Prices!C5*Assumptions!$B$3*730")
f.SetCellFormula("Estimate", "D20", "=SUM(D5:D19)")
```

**金額セルに定数を焼き込まない。** これは本プロジェクトの原則の一つとする。

**SKU ID と取得日を載せるのは検証時間対策である。** 全行を検算する代わりに、怪しい行だけを AWS の料金ページと突き合わせられる。

### Consequences

* Good, because 前提を変えるだけで再計算でき、ツールの再実行も LLM 呼び出しも不要になる
* Good, because SKU ID と取得日により、疑わしい行だけをピンポイントで検証できる
* Good, because 月次推移シートや 3 年 TCO シートを、同じ入力セルを参照する数式として後から追加できる
* Good, because excelize は純 Go で、Excel がインストールされていない環境でも生成できる
* Neutral, because 単価は取得時点のスナップショットであり、価格改定には再取得が必要（取得日を明示することで利用者が判断できる）
* Bad, because 数式の参照ずれ（行挿入・シート名変更）が壊れやすく、生成ロジックのテストが必要になる
* Bad, because Excel の数式エンジンに依存するため、Google スプレッドシート等での再現性は保証しない

### Confirmation

* 生成した Excel の `Estimate` シートの金額セルがすべて `=` 始まりであることをテストで検証する（定数の焼き込み禁止）
* `Prices` シートの各行に SKU・取得日・リージョンが埋まっていることをテストで検証する
* `Assumptions` の入力セルを変更した際に `Estimate` の合計が変わることを、生成物を実際に開いて確認する（リリース前チェック）

## More Information

`Assumptions` シートの入力セルは IR の `Assumptions` 構造体に 1:1 で対応する（[ADR-0002](0002-architecture-ir-as-json-source-of-truth.md)）。
`Estimate` シートの数式は catalog の `quantity_formula` から生成する（[ADR-0005](0005-cost-model-catalog-yaml.md)）。
オンデマンド以外の購入オプションは扱わないが、`Assumptions` に「割引率」の入力セルを置いて利用者が係数で表現できるようにする（[ADR-0009](0009-on-demand-pricing-only.md)）。
