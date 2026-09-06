// Package workbook は見積もり明細から Excel ワークブックを生成する（ADR-0006）。
//
// 金額セルには定数を焼き込まない。すべて Prices シートの単価と Assumptions シートの
// 入力セルを参照する数式にする（PRIN-4 / FR-XLS-4）。
// これにより、ツールを再実行せずに Assumptions を書き換えるだけで試算し直せる。
package workbook

// シート名。数式から参照するので定数にする。
const (
	SheetAssumptions = "Assumptions"
	SheetPrices      = "Prices"
	SheetEstimate    = "Estimate"
)

// notAccounted は「未計上の項目」（FR-XLS-7 / ADR-0011）。
// catalog がモデル化していないコストを見積もりの利用者に明示する。
//
// NAT Gateway の時間課金とデータ処理料は ADR-0018 で catalog（nat_gateway）に
// 追加したため、ここには含めない。Provisioned Bandwidth オプションと
// Regional NAT Gateway（新世代）は引き続き未計上（catalog の doc 参照）。
var notAccounted = []string{
	"リージョン間のデータ転送、VPC エンドポイント経由の転送、CloudFront 経由の転送",
	"インターネット向け送信の無料枠（月 100GB）",
	"ALB の LCU のうち、新規接続数・アクティブ接続数・ルール評価数の次元",
	"EBS の追加 IOPS / スループット、RDS のバックアップストレージと追加 IOPS",
	"S3 のライフサイクル移行リクエストとデータ取り出し料金",
	"サポートプラン、監視・ログなどの周辺サービス",
}

// onDemandNote はオンデマンド前提であることの注記（FR-XLS-8 / ADR-0009）。
const onDemandNote = "この試算はオンデマンド単価に基づく。" +
	"Reserved Instances / Savings Plans を適用した場合の実際の割引額とは異なる。" +
	"割引の見込みは Assumptions の discountRate に入れて表現する。"
