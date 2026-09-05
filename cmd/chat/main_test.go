package main

import (
	"reflect"
	"testing"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/intake"
)

func TestParseSelection(t *testing.T) {
	single := intake.Choice{Question: "稼働時間は?", Options: []string{"24 時間", "業務時間のみ"}}
	multi := intake.Choice{Question: "必要なもの", Options: []string{"ALB", "RDS", "S3"}, Multi: true}

	tests := []struct {
		name    string
		input   string
		choice  intake.Choice
		want    []string
		wantErr bool
	}{
		{"番号で選ぶ", "1", single, []string{"24 時間"}, false},
		{"前後の空白を許す", " 2 ", single, []string{"業務時間のみ"}, false},
		{"複数選択", "1,3", multi, []string{"ALB", "S3"}, false},
		{"範囲外", "3", single, nil, true},
		{"数値でない", "はい", single, nil, true},
		{"空", "", single, nil, true},
		{"単一選択に複数", "1,2", single, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSelection(tt.input, tt.choice)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーになりませんでした: %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
