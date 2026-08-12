// Package werewolf — econ_tier_test.go: EconTier 单元测试（v4 §13.2 → v5 5 档）。
//
// 覆盖：
//  1. ComputeEconTier 5 档阈值切档正确（含 v5 新增 Boom / Critical）
//  2. EconBoom/EconHealth/EconCaution/EconDanger/EconCritical 配置正确
//  3. EconTierSpec 比例合计 + 100%（v3 默认行为保持）
//  4. EconTierAbsorbPct / EconTierPotPct 快速 helper
//  5. 负数输入 → EconCritical（最严档防刷道具）
//  6. EconTierDisplayName 本地化显示（含 Boom/Critical）
//  7. ConfigureEconTier 自定义阈值生效
package werewolf

import "testing"

func TestComputeEconTier_Boom(t *testing.T) {
	if got := ComputeEconTier(100000); got != EconBoom {
		t.Fatalf("100000 should be Boom, got %s", got)
	}
	if got := ComputeEconTier(999999); got != EconBoom {
		t.Fatalf("999999 should be Boom, got %s", got)
	}
}

func TestComputeEconTier_Health(t *testing.T) {
	if got := ComputeEconTier(50000); got != EconHealth {
		t.Fatalf("50000 should be Health, got %s", got)
	}
	if got := ComputeEconTier(99999); got != EconHealth {
		t.Fatalf("99999 should be Health, got %s", got)
	}
}

func TestComputeEconTier_Caution(t *testing.T) {
	if got := ComputeEconTier(49999); got != EconCaution {
		t.Fatalf("49999 should be Caution, got %s", got)
	}
	if got := ComputeEconTier(10000); got != EconCaution {
		t.Fatalf("10000 (boundary) should be Caution, got %s", got)
	}
	if got := ComputeEconTier(25000); got != EconCaution {
		t.Fatalf("25000 should be Caution, got %s", got)
	}
}

func TestComputeEconTier_Danger(t *testing.T) {
	if got := ComputeEconTier(9999); got != EconDanger {
		t.Fatalf("9999 should be Danger, got %s", got)
	}
	if got := ComputeEconTier(5000); got != EconDanger {
		t.Fatalf("5000 (boundary) should be Danger, got %s", got)
	}
}

func TestComputeEconTier_Critical(t *testing.T) {
	if got := ComputeEconTier(4999); got != EconCritical {
		t.Fatalf("4999 should be Critical, got %s", got)
	}
	if got := ComputeEconTier(0); got != EconCritical {
		t.Fatalf("0 should be Critical, got %s", got)
	}
}

func TestComputeEconTier_Negative(t *testing.T) {
	if got := ComputeEconTier(-100); got != EconCritical {
		t.Fatalf("negative should be Critical (defensive), got %s", got)
	}
}

func TestEconTierSpec_SumIs100(t *testing.T) {
	// 每档的 AbsorbPct + PotReturnPct + 隐式 TargetCompensPct = 100
	// target compens 是余数(price - potReturn - systemAbsorb)
	for _, tier := range []EconTier{EconBoom, EconHealth, EconCaution, EconDanger, EconCritical} {
		spec := GetEconTierSpec(tier)
		if spec.SystemAbsorbPct < 0 || spec.SystemAbsorbPct > 100 {
			t.Fatalf("tier %s: AbsorbPct out of range: %d", tier, spec.SystemAbsorbPct)
		}
		if spec.PotReturnPct < 0 || spec.PotReturnPct > 100 {
			t.Fatalf("tier %s: PotReturnPct out of range: %d", tier, spec.PotReturnPct)
		}
		if spec.SystemAbsorbPct+spec.PotReturnPct >= 100 {
			t.Fatalf("tier %s: Absorb+Pot must leave room for target compens, got %d+%d",
				tier, spec.SystemAbsorbPct, spec.PotReturnPct)
		}
	}
}

func TestEconTierSpec_V3DefaultIsHealth(t *testing.T) {
	// v3 默认 30% 销毁 / 50% 彩池 → EconHealth 必须是这个值（向后兼容）
	spec := GetEconTierSpec(EconHealth)
	if spec.SystemAbsorbPct != 30 {
		t.Fatalf("EconHealth AbsorbPct must be 30 (v3 default), got %d", spec.SystemAbsorbPct)
	}
	if spec.PotReturnPct != 50 {
		t.Fatalf("EconHealth PotReturnPct must be 50 (v3 default), got %d", spec.PotReturnPct)
	}
}

func TestEconTierSpec_V5TiersHaveExpectedRates(t *testing.T) {
	// v5 §16.3 表：Boom 20/60, Health 30/50, Caution 40/40, Danger 45/30, Critical 60/20
	cases := []struct {
		tier       EconTier
		absorb     int
		potReturn  int
	}{
		{EconBoom, 20, 60},
		{EconHealth, 30, 50},
		{EconCaution, 40, 40},
		{EconDanger, 45, 30},
		{EconCritical, 60, 20},
	}
	for _, c := range cases {
		spec := GetEconTierSpec(c.tier)
		if spec.SystemAbsorbPct != c.absorb || spec.PotReturnPct != c.potReturn {
			t.Fatalf("tier %s: expected %d/%d, got %d/%d",
				c.tier, c.absorb, c.potReturn, spec.SystemAbsorbPct, spec.PotReturnPct)
		}
	}
}

func TestGetEconTierSpec_UnknownFallback(t *testing.T) {
	spec := GetEconTierSpec(EconTier("unknown-tier"))
	if spec.SystemAbsorbPct != 30 || spec.PotReturnPct != 50 {
		t.Fatalf("unknown tier should fallback to Health (30/50), got %d/%d",
			spec.SystemAbsorbPct, spec.PotReturnPct)
	}
}

func TestEconTierAbsorbPct_QuickHelper(t *testing.T) {
	if EconTierAbsorbPct(EconBoom) != 20 {
		t.Fatalf("Boom absorb should be 20")
	}
	if EconTierAbsorbPct(EconHealth) != 30 {
		t.Fatalf("Health absorb should be 30")
	}
	if EconTierAbsorbPct(EconCaution) != 40 {
		t.Fatalf("Caution absorb should be 40")
	}
	if EconTierAbsorbPct(EconDanger) != 45 {
		t.Fatalf("Danger absorb should be 45 (v5 微调)")
	}
	if EconTierAbsorbPct(EconCritical) != 60 {
		t.Fatalf("Critical absorb should be 60")
	}
}

func TestEconTierPotPct_QuickHelper(t *testing.T) {
	if EconTierPotPct(EconBoom) != 60 {
		t.Fatalf("Boom pot should be 60")
	}
	if EconTierPotPct(EconHealth) != 50 {
		t.Fatalf("Health pot should be 50")
	}
	if EconTierPotPct(EconCaution) != 40 {
		t.Fatalf("Caution pot should be 40")
	}
	if EconTierPotPct(EconDanger) != 30 {
		t.Fatalf("Danger pot should be 30")
	}
	if EconTierPotPct(EconCritical) != 20 {
		t.Fatalf("Critical pot should be 20")
	}
}

func TestEconTierDisplayName(t *testing.T) {
	if got := EconTierDisplayName(EconBoom); got != "🟣 Boom（暴富）" {
		t.Fatalf("Boom display wrong: %s", got)
	}
	if got := EconTierDisplayName(EconHealth); got != "🟢 Health（健康）" {
		t.Fatalf("Health display wrong: %s", got)
	}
	if got := EconTierDisplayName(EconCaution); got != "🟡 Caution（警戒）" {
		t.Fatalf("Caution display wrong: %s", got)
	}
	if got := EconTierDisplayName(EconDanger); got != "🟠 Danger（危险）" {
		t.Fatalf("Danger display wrong: %s", got)
	}
	if got := EconTierDisplayName(EconCritical); got != "🔴 Critical（危急）" {
		t.Fatalf("Critical display wrong: %s", got)
	}
}

func TestConfigureEconTier_Override(t *testing.T) {
	// 备份并恢复默认阈值(避免影响其他测试)。
	orig := EconTierThresholds
	defer func() { EconTierThresholds = orig }()

	// 自定义：Boom 200K, Caution 80K, Danger 20K, Critical 8K
	ConfigureEconTier(200000, 80000, 20000, 8000)

	if got := ComputeEconTier(200000); got != EconBoom {
		t.Fatalf("after Configure(200K/80K/20K/8K), 200000 should be Boom, got %s", got)
	}
	if got := ComputeEconTier(199999); got != EconHealth {
		t.Fatalf("199999 should be Health (below custom Boom), got %s", got)
	}
	if got := ComputeEconTier(7999); got != EconCritical {
		t.Fatalf("7999 should be Critical (below custom 8K), got %s", got)
	}
}

func TestConfigureEconTier_PanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("ConfigureEconTier should panic on invalid (non-monotonic) thresholds")
		}
	}()
	// Boom=10K 应小于 Caution=20K,违反单调性 → panic
	ConfigureEconTier(10000, 20000, 5000, 1000)
}