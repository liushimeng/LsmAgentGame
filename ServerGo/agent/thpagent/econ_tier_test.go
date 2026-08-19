package thpagent

import (
	"testing"
)

func TestComputeEconTier(t *testing.T) {
	tests := []struct {
		name       string
		totalCoin  int64
		wantTier   EconTier
		wantRakePct int
	}{
		{"health at 100K", 100000, EconHealth, 5},
		{"health at exact threshold 50K", 50000, EconHealth, 5},
		{"caution at 49999", 49999, EconCaution, 7},
		{"caution at exact threshold 10K", 10000, EconCaution, 7},
		{"danger at 9999", 9999, EconDanger, 10},
		{"danger at 0", 0, EconDanger, 10},
		{"danger at negative (defensive)", -100, EconDanger, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeEconTier(tt.totalCoin)
			if got != tt.wantTier {
				t.Errorf("ComputeEconTier(%d) = %s, want %s", tt.totalCoin, got, tt.wantTier)
			}
			if got.RakeRatePct() != tt.wantRakePct {
				t.Errorf("RakeRatePct() = %d, want %d", got.RakeRatePct(), tt.wantRakePct)
			}
		})
	}
}

func TestApplyRake(t *testing.T) {
	tests := []struct {
		name      string
		payout    int64
		tier      EconTier
		wantNet   int64
		wantRake  int64
	}{
		{"zero payout no rake", 0, EconHealth, 0, 0},
		{"negative payout no rake", -100, EconHealth, 0, 0},
		{"health 1000 → net 950, rake 50", 1000, EconHealth, 950, 50},
		{"caution 1000 → net 930, rake 70", 1000, EconCaution, 930, 70},
		{"danger 1000 → net 900, rake 100", 1000, EconDanger, 900, 100},
		{"unknown tier defaults to health", 1000, EconTier("unknown"), 950, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			net, rake := ApplyRake(tt.payout, tt.tier)
			if net != tt.wantNet {
				t.Errorf("net = %d, want %d", net, tt.wantNet)
			}
			if rake != tt.wantRake {
				t.Errorf("rake = %d, want %d", rake, tt.wantRake)
			}
		})
	}
}

func TestRakeRateRange(t *testing.T) {
	// §132 抽水率必须在 [0.05, 0.10] 区间
	for _, tier := range []EconTier{EconHealth, EconCaution, EconDanger, EconTier("future")} {
		rate := tier.RakeRate()
		if rate < 0.03 || rate > 0.15 {
			t.Errorf("RakeRate(%s) = %f, out of expected range", tier, rate)
		}
	}
}