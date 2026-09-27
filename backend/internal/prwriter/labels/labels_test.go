package labels

import (
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      []string
		want    Policy
		wantErr string
	}{
		{
			name: "skip recognised",
			in:   []string{"optiqor:skip"},
			want: Policy{Skip: true},
		},
		{
			name: "budget parsed",
			in:   []string{"optiqor:budget=$1500"},
			want: Policy{BudgetUSDSet: true, BudgetUSD: 1500},
		},
		{
			name: "budget without dollar sign",
			in:   []string{"optiqor:budget=750"},
			want: Policy{BudgetUSDSet: true, BudgetUSD: 750},
		},
		{
			name: "wait-for-prom days",
			in:   []string{"optiqor:wait-for-prom=7d"},
			want: Policy{WaitForProm: 7 * 24 * time.Hour},
		},
		{
			name: "wait-for-prom hours",
			in:   []string{"optiqor:wait-for-prom=12h"},
			want: Policy{WaitForProm: 12 * time.Hour},
		},
		{
			name: "non-optiqor labels ignored",
			in:   []string{"area/cost", "team/platform", "optiqor:skip"},
			want: Policy{Skip: true},
		},
		{
			name: "unknown optiqor label captured",
			in:   []string{"optiqor:typo"},
			want: Policy{UnknownLabels: []string{"optiqor:typo"}},
		},
		{
			name:    "budget not a number errors",
			in:      []string{"optiqor:budget=nope"},
			wantErr: "not a number",
		},
		{
			name:    "negative budget errors",
			in:      []string{"optiqor:budget=-50"},
			wantErr: "non-negative",
		},
		{
			name:    "bad duration errors",
			in:      []string{"optiqor:wait-for-prom=fortnight"},
			wantErr: "not a duration",
		},
		{
			name: "multiple labels compose",
			in:   []string{"optiqor:skip", "optiqor:budget=$200"},
			want: Policy{Skip: true, BudgetUSDSet: true, BudgetUSD: 200},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.Skip != tc.want.Skip {
				t.Errorf("Skip = %v, want %v", got.Skip, tc.want.Skip)
			}
			if got.BudgetUSDSet != tc.want.BudgetUSDSet || got.BudgetUSD != tc.want.BudgetUSD {
				t.Errorf("Budget = (%v,%v), want (%v,%v)", got.BudgetUSDSet, got.BudgetUSD, tc.want.BudgetUSDSet, tc.want.BudgetUSD)
			}
			if got.WaitForProm != tc.want.WaitForProm {
				t.Errorf("WaitForProm = %v, want %v", got.WaitForProm, tc.want.WaitForProm)
			}
			if len(got.UnknownLabels) != len(tc.want.UnknownLabels) {
				t.Errorf("UnknownLabels = %v, want %v", got.UnknownLabels, tc.want.UnknownLabels)
			}
		})
	}
}
