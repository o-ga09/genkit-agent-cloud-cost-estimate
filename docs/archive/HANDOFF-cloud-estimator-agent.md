# 引き継ぎ資料: クラウドコスト見積もり＆構成図作成エージェント

> このドキュメントは Claude Code に渡す前提の設計メモです。
> 実装を始める前に「設計の核」と「絶対に守る原則」だけは必ず読んでください。
> 作成日: 2026-09-04

---

## 1. 何を作るのか

AWS（将来的にGCPも）の構成をチャットで相談しながら決め、

- **コスト見積もりExcel**（数式で可変。再実行なしにパラメータを変えられる）
- **構成図の画像**（SVG/PNG）

を出力するエージェント。実装言語・フレームワークは **Genkit Go**（要件として確定）。

## 2. 解決したい課題（元の困りごと）

| # | 課題 | 本質的な原因 |
|---|---|---|
| 1 | AWS Cost MCPのセットアップが面倒。AWS Profileが無いと正確なコストが取れない | 認証前提のツールに依存している |
| 2 | drawioで矢印がうまく繋がらない、綺麗に描けない | 座標を人間（またはLLM）が手で置いている |
| 3 | 枠やアイコンを揃えたいが揃わない | 同上 |
| 4 | 時間がかかる | 上記の手作業の積み重ね |
| 5 | AIにMCPを使わせて出させても、検証に時間がかかる | LLMが金額とフィルタ条件を直接出しているので、全行を検算する羽目になる |

## 3. 設計の核（最重要）

**課題はすべて「LLMに数字と座標を出させている」ことに起因する。** そこを分離する。

```
LLM が出すもの   → 構成のIR（どのサービスを、いくつ、どう繋ぐか）+ 前提条件
Go が決定的にやる → 単価の取得、金額の計算、図のレイアウト、Excel生成
```

LLMには**金額を一切出させない**。出すのは「ALB×1、EC2 t3.medium×2を24h/日、RDS db.r6g.large Multi-AZ、S3 500GB」といった数量と前提だけ。単価はコードがAPI経由で引き、合計はExcelの計算式が出す。

これにより:

- 検証は「前提が妥当か」だけになる。電卓検算が不要（課題5への回答）
- 座標を誰も書かないので、矢印と枠は原理的に必ず揃う（課題2・3への回答）

## 4. IR（Intermediate Representation）

図とExcelの両方が派生する中心データ。**ここを最初に確定させること。** 決まれば残りは独立に並行実装できる。

```go
type Architecture struct {
    Provider    string       `json:"provider"`    // "aws" | "gcp"
    Region      string       `json:"region"`      // "ap-northeast-1"
    Assumptions Assumptions  `json:"assumptions"` // Excelの入力セルになる
    Resources   []Resource   `json:"resources"`
    Edges       []Edge       `json:"edges"`
}

type Resource struct {
    ID      string         `json:"id"`      // "web-alb"（一意）
    Service string         `json:"service"` // catalogのenumに制約する
    Label   string         `json:"label"`   // 図に出す表示名
    Parent  string         `json:"parent"`  // "vpc-main" → 図の入れ子になる。空可
    Params  map[string]any `json:"params"`  // {"instanceType":"t3.medium","count":2}
}

type Edge struct {
    From  string `json:"from"`
    To    string `json:"to"`
    Label string `json:"label"`
}

type Assumptions struct {
    HoursPerDay   float64 `json:"hoursPerDay"`
    DaysPerMonth  float64 `json:"daysPerMonth"`
    RequestsPerMo float64 `json:"requestsPerMonth"`
    FxRate        float64 `json:"fxRate"` // USD/JPY
    // 必要に応じて追加
}
```

```
                    ┌→ D2 → SVG/PNG（構成図）
LLM → Architecture ─┼→ drawio XML（手直ししたい人向け）
       (JSONで保存)  └→ excelize → Excel（見積もり）
```

IRをJSONで保存しておくことが重要。**再レンダリングも再見積もりもLLMを通さずにできる**ため、「毎回このツールで出力するのは面倒」という要求への回答になる。図とExcelが同じデータから出るので、両者の内容がズレることもない。

## 5. 構成図: D2 を使う

[D2](https://d2lang.com/) は図を書くための言語。採用理由:

- **Goで書かれていてライブラリとしてimportできる**（`oss.terrastruct.com/d2/d2lib`）。外部バイナリもNode.jsもCGoも不要で、Genkit Goと同じバイナリに同居できる
- ELK等のレイアウトエンジンで**座標を自動計算**する
- `icon:` でAWS/GCPの公式アイコンを当てられる

生成するD2ソースのイメージ:

```
vpc: VPC {
  alb: ALB { icon: https://icons.../ALB.svg }
  ec2: EC2 x2
  rds: RDS (Multi-AZ)
}
user -> vpc.alb -> vpc.ec2 -> vpc.rds
```

`Resource.Parent` をD2のコンテナのネストに写像すれば、VPC枠・AZ枠は自動で囲まれる。「枠を揃える」作業そのものが消える。

```go
func RenderD2(ctx context.Context, arch *Architecture) ([]byte, error) {
    src := buildD2Source(arch) // IR → D2ソース文字列
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

- PNGが必要なら SVG→PNG は resvg か headless Chrome
- **drawio XMLも同じIRから出力できるようにしておく**と、後から手で直したい人にも渡せる（座標はコードが計算するので綺麗に揃った状態で渡せる）

## 6. 価格取得: AWS Pricing MCP Server

### 事実関係（調査済み・重要）

- `awslabs/mcp` の **AWS Pricing MCP Server**（`awslabs.aws-pricing-mcp-server`）が該当のサーバー。Price List API を叩くので、**これから作る構成の見積もりに使える**
- Cost Analysis MCP Server は、価格系ツールについてはPricing MCPへ移行するよう非推奨扱いになっている
- Cost Explorer / Billing 系のMCPは「実際に使った請求の分析」であり、今回の用途ではない
- **認証は必要**。IAMロール/ユーザーに `pricing:*` 権限が要る。`AWS_PROFILE` 環境変数を読み、未指定なら `default` プロファイルにフォールバックする
- アクセスするのは一般公開の価格情報のみでユーザー固有データは取らない。API呼び出しは無料

→ **運用方針**: `pricing:*` だけを持つ読み取り専用IAMユーザーを1つ用意してツール側に埋める。請求データへのアクセス権が不要なので、これなら共有ツールとして配布できる。課題1（Profile設定が面倒）はこれで実用上ほぼ解消する。

### 重要な注意（課題5に直結）

AWS自身が明記している通り、**AIアシスタントが常に正しくフィルタを構成する保証はなく、最安の選択肢を確実に特定できる保証もない**。これがまさに「検証に時間がかかる」の原因なので、設計で潰す。

### 対策: LLMにMCPツールを直接握らせない

Genkit Go には `plugins/mcp`（MCPクライアント）があるので接続はできるが、**LLMのツールとして公開しない**。catalog YAMLに書いた確定的なフィルタ条件を**Goコードが組み立てて**MCP経由で問い合わせる。

```
LLM      → 「EC2 t3.medium ×2」というIRを出すだけ
Goコード → catalogのフィルタ定義 + IR.Params → PriceQuery → MCP → 単価
Excel    → 単価 × 数量 の数式
```

フィルタ定義のレビューは最初の1回で済み、以降はLLM由来のフィルタ構成ミスが起きない。

### 抽象化

```go
type PriceSource interface {
    Unit(ctx context.Context, q PriceQuery) (Price, error)
}

type PriceQuery struct {
    Service    string            // "AmazonEC2"
    Region     string
    Attributes map[string]string // instanceType, tenancy, operatingSystem...
}

type Price struct {
    Amount    float64
    Currency  string
    Unit      string // "Hrs", "GB-Mo"
    SKU       string // Excelに載せて検証可能にする
    FetchedAt time.Time
}
```

MCPを第一実装にしつつインターフェースを切っておく。`pricing:*` すら渡せない配布先が出てきたら、**認証不要の Price List Bulk API**（`https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonEC2/current/{region}/index.json` という静的JSON。認証不要だがEC2で数百MB級なので要キャッシュ・正規化）実装に差し替えられる。**最初はMCP実装だけで着手してよい。**

GCPは `Cloud Billing Catalog API`（`services.skus.list`）がAPIキーだけで叩けるので、同じ `PriceSource` 抽象に乗せる。

## 7. コストカタログ（YAML）

単価が引けても**コストモデル**（EC2なら 時間×台数 + EBS GB月 + データ転送）は自前で持つ必要がある。対応サービスを絞ってYAMLで手書きする。20〜30サービスで大半の構成をカバーできる。

```yaml
# catalog/ec2.yaml
service: ec2
display: Amazon EC2
icon: https://icons.../EC2.svg
params:                       # LLMがIRに入れてよいパラメータの定義
  instanceType: {type: string, required: true}
  count:        {type: int, default: 1}
drivers:
  - id: instance_hours
    unit: hour
    price_query:              # ← このフィルタはコードが使う。LLMは触らない
      serviceCode: AmazonEC2
      instanceType: "{{instanceType}}"
      tenancy: Shared
      operatingSystem: Linux
      preInstalledSw: NA
      capacitystatus: Used
    quantity_formula: "count * hours_per_day * days_per_month"
  - id: ebs
    unit: GB-Mo
    price_query:
      serviceCode: AmazonEC2
      volumeApiName: gp3
    quantity_formula: "count * ebs_gb"
```

**このcatalogが同時にLLMの選択肢リストになる**のが重要。`list_supported_services` ツールでcatalogの内容を返してやれば、価格を引けないサービスをLLMが勝手に提案する事故が防げる。

## 8. Excel出力（要求の肝）

`github.com/xuri/excelize/v2` で **3シート構成**。金額セルは全部数式にする。

| シート | 内容 |
|---|---|
| `Assumptions` | 稼働時間・リクエスト数・ストレージGB・為替・成長率などの**入力セル**（色分けして編集可能と明示） |
| `Prices` | 引いてきた単価。**SKU ID・取得日・リージョンを併記** |
| `Estimate` | `=Prices!C5 * Assumptions!$B$3 * 730` のような数式 |

```go
f.SetCellFormula("Estimate", "D5", "=Prices!C5*Assumptions!$B$3*730")
f.SetCellFormula("Estimate", "D20", "=SUM(D5:D19)")
```

- これで**再実行なしに使い回せる**（要求「毎回このツールで出力するのは面倒」への直接の回答）。「ユーザー数が3倍になったら?」は Assumptions のセルを書き換えるだけ
- 月次推移シートや3年TCOシートも、同じ入力セルを参照する数式で追加できる
- **SKU IDと取得日を載せるのは検証時間対策**。怪しい行だけAWSの料金ページと突き合わせられる

## 9. Genkit Go での構成

### バージョンと前提

- 最新は **Genkit Go v1.13.1**（2026-09-03公開）。モジュールは `github.com/firebase/genkit/go`
- リポジトリは `genkit-ai/genkit` 配下に移行中（将来は `genkit-ai/genkit-go`）。現状ソース・Issue・リリースは `genkit-ai/genkit/go` を見る
- **Agents API と exp系ツールAPIは preview。マイナーリリースでも破壊的変更あり**と明記されている

### 分割方針（preview依存を局所化する）

```
[Agent: intake]        ← preview API。ヒアリングと構成提案の対話のみ
   ↓ Architecture (IR)
[Flow: estimate]       ← 安定API。決定的処理のみ
   ├ PriceSource（MCP経由）
   ├ RenderD2 / RenderDrawio
   └ BuildWorkbook
```

**価値の中心である見積もりロジックは安定APIの上に乗せる。** preview依存は対話部分だけに閉じ込める。破壊的変更が来ても被害が限定される。

### 選択UI = tool interrupt

要求の「チャットや選択UIで入力」はこれで実現する。

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
            return "", tool.Interrupt(in) // フロントが選択UIを描画
        }
        return ans.Selected, nil
    },
)
```

`genkit.WithExperimental()` で exp サーフェスを有効化すること。exp系は `genkit/exp`（コンストラクタ）と `ai/exp`, `ai/exp/tool`（型・ヘルパー）に分かれている。

### 構成の確定は型で強制

```go
arch, err := genkit.GenerateData[Architecture](ctx, g,
    ai.WithModelName("googleai/gemini-flash-latest"),
    ai.WithTools(listServicesTool, askTool),
    ai.WithPrompt(...),
)
```

### その他使えるもの

- `plugins/middleware` の `Retry` / `Fallback` — モデル呼び出しの信頼性確保
- `core/status` のエラーセンチネル — `errors.Is` でリトライ要否を判定
- Agents のセッションストア + スナップショット — 会話をまたいだ見積もりの再開に使える

## 10. 実装順（この順で）

1. **IR型定義 + D2レンダリング**。LLMなしで、手書きJSONから図が出るところまで
2. **catalog YAML を5サービス分**（EC2/ALB/RDS/S3/データ転送あたり）+ `PriceSource` のMCP実装
3. **excelize でワークブック生成**（3シート・数式込み）
4. ここで初めて **Genkit の flow を被せる**
5. 最後に **intake agent と interrupt UI**

**1〜3が動けば、LLM抜きでも「JSONを書けば図とExcelが出る」ツールとして既に有用。** そこにLLMを足す形にすることで、検証で困ったときにLLMを切り離して切り分けられる。逆順でやらないこと。

## 11. 未決事項 / 最初に決めること

- [ ] **IRスキーマの確定**（最優先。ここが決まると図・Excel・catalogが並行実装できる）
- [ ] **カバーするサービス範囲**: EC2/ALB/RDS/S3中心か、ECS/Lambda等のサーバーレスも入れるか
- [ ] リザーブド/Savings Plans/スポットを扱うか（オンデマンドのみに割り切るか）
- [ ] データ転送コストをどこまで真面目にモデル化するか（誤差要因になりやすい）
- [ ] 出力の配布形態: CLIか、Webサービスか
- [ ] GCP対応を最初から入れるか、AWS先行か

## 12. 原則（迷ったらここに戻る）

1. **LLMに金額を出させない。** 数量と前提だけ出させる
2. **LLMに座標を出させない。** レイアウトエンジンに任せる
3. **LLMにMCPのフィルタを組ませない。** catalogの定義をコードが使う
4. **Excelの金額セルは必ず数式。** 定数を焼き込まない
5. **IRはJSONで保存する。** 再生成にLLMを通さない
6. **preview APIへの依存は対話層だけに閉じる**
