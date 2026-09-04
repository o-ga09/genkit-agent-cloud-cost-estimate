---
status: "accepted"
date: 2026-09-04
decision-makers: プロダクトオーナー, 開発担当
consulted: 引き継ぎ資料「クラウドコスト見積もり＆構成図作成エージェント」
informed: 構成図を受け取るレビュアー
---

# 構成図のレンダリングに D2 を Go ライブラリとして採用する

## Context and Problem Statement

これまで drawio で構成図を手描きしていたが、矢印がうまく繋がらない、枠やアイコンが揃わない、時間がかかる、という問題があった。
原因は座標を人間または LLM が手で置いていることにある（[ADR-0001](0001-llm-outputs-only-quantities-and-assumptions.md)）。
IR から図を自動生成するにあたり、どのレンダリング手段を採るか。

## Decision Drivers

* 座標計算を人にも LLM にも一切させない（レイアウトエンジンに任せる）
* VPC 枠・AZ 枠のような入れ子構造を表現でき、枠が自動で囲まれること
* AWS/GCP の公式アイコンを当てられること
* Genkit Go と同じ単一バイナリに同居でき、外部バイナリ・Node.js・CGo に依存しないこと（Web サービスとしてコンテナ配布するため）
* 手直ししたい利用者に drawio 形式でも渡せること

## Considered Options

* D2（`oss.terrastruct.com/d2/d2lib`）を Go ライブラリとして import する
* Graphviz（DOT）を外部バイナリまたは CGo バインディングで呼ぶ
* Mermaid を Node.js ランタイム経由でレンダリングする
* drawio XML を直接生成し、座標も自前で計算する

## Decision Outcome

採用した選択肢: 「D2 を Go ライブラリとして import する」。
Go で書かれておりライブラリとして直接 import できるため外部プロセス依存がなく、ELK 等のレイアウトエンジンで座標を自動計算し、
`icon:` でクラウド公式アイコンを当てられるという 3 条件を同時に満たす唯一の選択肢であるため。

`Resource.Parent` を D2 のコンテナのネストに写像することで、VPC 枠・AZ 枠は自動で囲まれる。「枠を揃える」作業そのものが消える。

生成する D2 ソースのイメージ:

```
vpc: VPC {
  alb: ALB { icon: https://icons.../ALB.svg }
  ec2: EC2 x2
  rds: RDS (Multi-AZ)
}
user -> vpc.alb -> vpc.ec2 -> vpc.rds
```

```go
func RenderD2(ctx context.Context, arch *Architecture) ([]byte, error) {
    src := buildD2Source(arch) // IR → D2 ソース文字列
    diagram, _, err := d2lib.Compile(ctx, src, &d2lib.CompileOptions{
        LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
            return d2elklayout.DefaultLayout, nil
        },
    }, nil)
    if err != nil {
        return nil, err
    }
    return d2svg.Render(diagram, &d2svg.RenderOpts{ThemeID: &themeID})
}
```

補足方針:

* PNG が必要な場合、SVG → PNG の変換は resvg または headless Chrome で行う
* drawio XML も同じ IR から出力できるようにする。座標はコードが計算するため、手直ししたい利用者にも揃った状態で渡せる

### Consequences

* Good, because 矢印・枠・アイコンが原理的に必ず揃う（座標を誰も書かない）
* Good, because 外部バイナリも Node.js も CGo も不要で、単一 Go バイナリのコンテナに収まる
* Good, because `Resource.Parent` の入れ子がそのままコンテナの入れ子になり、写像が単純
* Neutral, because レイアウトの細部（ノードの並び順など）はエンジンに委ねられ、細かな見た目の指定はできない
* Bad, because D2 のバージョン更新でレイアウト結果が変わり、ゴールデンテストが壊れうる
* Bad, because PNG 出力には SVG → PNG 変換の追加依存（resvg / headless Chrome）が必要になる

### Confirmation

* LLM に渡すツール・IR のどこにも座標を指定する経路がないことをコードレビューで確認する
* 代表的な構成（VPC + AZ 入れ子 + 複数エッジ）の IR に対して、生成 SVG のゴールデンテストを置く
* drawio XML 出力が drawio で開けることを手動で確認する（リリース前チェック）

## More Information

* D2: https://d2lang.com/
* この決定は [ADR-0002](0002-architecture-ir-as-json-source-of-truth.md) の IR を入力として前提にしている。
* D2 のバージョン更新でゴールデンテストが継続的に壊れるようであれば、レイアウトエンジンの固定または代替手段を再検討する。
