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
drivers:                         # コスト要素。M2 で追加する
  - id: instance_hours
    unit: hour
    price_query:                 # このフィルタはコードが使う。LLM には渡さない（PRIN-3）
      serviceCode: AmazonEC2
      instanceType: "{{instanceType}}"
    quantity_formula: "count * hours_per_day * days_per_month"
```

## 現状

`drivers` はまだどのファイルにも入っていない。`price_query` は AWS の料金ページと
突き合わせた初回レビューが必須のため（FR-CAT-5 / PRD のリスク欄）、単価取得を扱う
M2 で追加する。現時点の catalog は次の 3 つの役割を担っている。

- `Resource.service` の値域（enum）
- `Resource.params` のバリデーションスキーマ
- 図に当てるアイコンと表示名

`vpc` と `az` は課金要素を持たないグルーピング用の定義で、図の入れ子（FR-IR-5 / FR-DIA-3）に使う。

## アイコンについて

暫定で D2 が公開している icons.terrastruct.com の URL を参照している（再配布はせず参照のみ）。
AWS 公式アセットの利用条件の確認は未決のまま（requirements.md §12）。
