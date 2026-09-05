# catalog

サービス 1 種類につき 1 ファイル。ファイル名は `<service>.yaml` にする。
サービスの追加は、このディレクトリに YAML を足すだけで完結する（NFR-8）。

```yaml
service: ec2                     # 必須。Resource.service の値。ファイル名と一致させる
display: Amazon EC2              # 必須。図のラベルのフォールバックに使う
icon: https://.../Amazon-EC2.svg # 任意。D2 の icon: に渡る
doc: 説明                        # 任意
params:                          # LLM が Resource.params に入れてよいキーの定義
  instanceType:
    type: string                 # string | int | float | bool
    required: true
    doc: インスタンスタイプ
  count:
    type: int
    default: 1                   # 既定値があれば required でも省略できる
  storageClass:
    type: string
    enum: [Standard, Standard-IA]  # enum は string 型のみ
drivers:                         # コスト要素。1 driver が Excel の 1 行になる
  - id: instance_hours
    unit: Hrs                    # 必須。取得した単価の単位と一致しない行は計上しない
    when: {path: internet_egress}  # 任意。params がこの値のときだけ計上する
    price_query:                 # このフィルタはコードが使う。LLM には渡さない（PRIN-3）
      serviceCode: AmazonEC2     # 必須（予約キー）
      scope: regional            # 任意（予約キー）。global なら region を渡さない
      instanceType: "{{instanceType}}"   # 以降は Price List API の属性名
    quantity_formula: count * hoursPerDay * daysPerMonth
```

## price_query の書き方

* `serviceCode` と `scope` だけが予約キー。それ以外は Price List API の属性名として渡る
* 値は定数か `{{name}}` テンプレート。`name` には params のキーか、組み込み変数
  `region`（`ap-northeast-1`）/ `regionLocation`（`Asia Pacific (Tokyo)`）が使える
* 既定は完全一致。部分一致が要るときは `usagetype: {contains: Fargate-vCPU-Hours}` と書く
  （完全一致で絞れる属性が無い Fargate だけで使っている）
* **フィルタは 1 SKU に絞れるまで書く。** 複数該当したときコードはエラーにする（1 件目を黙って採らない）
* `usagetype` はリージョン接頭辞（`APN1-` など）が付くので使わない
* 追加・変更したら実データで検証し、[../../../docs/reviews/price-query-review.md](../../../docs/reviews/price-query-review.md) を更新する

```sh
AWS_PRICING_MCP_E2E=1 go test ./internal/cost/ -run E2E -v
```

## quantity_formula の書き方

四則演算・カッコ・単項マイナスだけ（[ADR-0013](../../../docs/adr/0013-self-written-formula-engine.md)）。
変数は params のキーと `Assumptions` の JSON フィールド名
（`hoursPerDay` / `daysPerMonth` / `requestsPerMonth` / `fxRate` / `discountRate`）で参照する。
未定義の変数を参照する式は catalog のロード時にエラーになる。

## 現状

MVP の 9 サービス（ec2 / alb / rds / s3 / data_transfer / lambda / ecs / apigateway / dynamodb）の
drivers が入っている。

`vpc` と `az` は課金要素を持たないグルーピング用の定義で、図の入れ子（FR-IR-5 / FR-DIA-3）に使う。
drivers を持たないため見積もりの明細には出ない。

catalog は次の 4 つの役割を担う。

- `Resource.service` の値域（enum）
- `Resource.params` のバリデーションスキーマ
- 図に当てるアイコンと表示名
- コストモデル（drivers の `price_query` と `quantity_formula`）

## アイコンについて

暫定で D2 が公開している icons.terrastruct.com の URL を参照している（再配布はせず参照のみ）。
AWS 公式アセットの利用条件の確認は未決のまま（requirements.md §12）。
