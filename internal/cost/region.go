package cost

import "fmt"

// regionLocations は Price List API の location 名。
// データ転送のようなグローバルサービスでは region ではなく location で絞る。
// 値は 2026-09-05 に AWSDataTransfer の fromLocation の実データと突き合わせている。
var regionLocations = map[string]string{
	"us-east-1":      "US East (N. Virginia)",
	"us-east-2":      "US East (Ohio)",
	"us-west-1":      "US West (N. California)",
	"us-west-2":      "US West (Oregon)",
	"ap-northeast-1": "Asia Pacific (Tokyo)",
	"ap-northeast-2": "Asia Pacific (Seoul)",
	"ap-northeast-3": "Asia Pacific (Osaka)",
	"ap-southeast-1": "Asia Pacific (Singapore)",
	"ap-southeast-2": "Asia Pacific (Sydney)",
	"ap-south-1":     "Asia Pacific (Mumbai)",
	"eu-west-1":      "EU (Ireland)",
	"eu-west-2":      "EU (London)",
	"eu-west-3":      "EU (Paris)",
	"eu-central-1":   "EU (Frankfurt)",
	"eu-north-1":     "EU (Stockholm)",
	"ca-central-1":   "Canada (Central)",
	"sa-east-1":      "South America (Sao Paulo)",
}

// RegionLocation はリージョンコードに対応する location 名を返す。
func RegionLocation(region string) (string, error) {
	loc, ok := regionLocations[region]
	if !ok {
		return "", fmt.Errorf("location 名が未登録のリージョンです: %q（internal/cost/region.go に追加してください）", region)
	}
	return loc, nil
}
