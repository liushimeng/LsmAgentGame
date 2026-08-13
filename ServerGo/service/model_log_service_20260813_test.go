// Package service — model_log_service_20260813_test.go
//
// §20260813-02 U1/U2 回归测试:
//   - nil-DB 路径(WinRateTrends / PropEconomyStats → ErrInternal)
//   - buildWinTrends / buildPropEconomy 纯转换层端到端断言
//     (§20260811-08 教训 (5):转换结果必须断言非空,不能只测「转换没报错」)。

package service

import (
	"context"
	"testing"

	"LsmAgentGame/errcode"
)

func TestModelLogService_NilDB_20260813(t *testing.T) {
	s := NewModelLogService(nil)
	ctx := context.Background()

	t.Run("WinRateTrends nil DB", func(t *testing.T) {
		_, err := s.WinRateTrends(ctx)
		assertErrCode(t, err, errcode.ErrInternal)
	})

	t.Run("PropEconomyStats nil DB", func(t *testing.T) {
		_, err := s.PropEconomyStats(ctx)
		assertErrCode(t, err, errcode.ErrInternal)
	})
}

func TestBuildWinTrends_Basic(t *testing.T) {
	days := []winTrendDayRow{
		{ProviderID: "p1", Day: "2026-08-10", Games: 4, Wins: 3},
		{ProviderID: "p1", Day: "2026-08-11", Games: 2, Wins: 1},
	}
	roles := []winTrendSliceRow{
		{ProviderID: "p1", SliceKey: "werewolf", Games: 3, Wins: 2},
		{ProviderID: "p1", SliceKey: "seer", Games: 2, Wins: 1},
	}
	seats := []winTrendSliceRow{
		{ProviderID: "p1", SliceKey: "7", Games: 2, Wins: 1},
		{ProviderID: "p1", SliceKey: "0", Games: 3, Wins: 2},
	}
	out := buildWinTrends(days, roles, seats, map[string]string{"p1": "豆包 2.0"})

	m := out["p1"]
	if m == nil {
		t.Fatal("p1 must exist in result (转换产物非空,§20260811-08 教训 5)")
	}
	if m.AgentName != "豆包 2.0" {
		t.Errorf("AgentName = %q, want JOIN 显示名", m.AgentName)
	}
	if m.Games != 6 || m.Wins != 4 {
		t.Errorf("Games/Wins = %d/%d, want 6/4(按日行汇总)", m.Games, m.Wins)
	}
	// 4/6 = 66.666...% → 66.7
	if m.WinRate != 66.7 {
		t.Errorf("WinRate = %v, want 66.7", m.WinRate)
	}
	if len(m.Trend) != 2 || m.Trend[0].Day != "2026-08-10" || m.Trend[1].WinRate != 50 {
		t.Errorf("Trend = %+v, want 2 天升序且 day2 胜率 50", m.Trend)
	}
	if len(m.ByRole) != 2 || m.ByRole[0].Key != "werewolf" {
		t.Errorf("ByRole = %+v, want 局数降序(werewolf 先)", m.ByRole)
	}
	if len(m.BySeat) != 2 || m.BySeat[0].Key != "0" || m.BySeat[1].Key != "7" {
		t.Errorf("BySeat = %+v, want 座位升序", m.BySeat)
	}
	if m.SampleOK {
		t.Error("6 局 < SelfPortraitMinGames(8),SampleOK 应为 false")
	}
}

func TestBuildWinTrends_NameFallbackAndTrimming(t *testing.T) {
	// 31 天趋势 → 裁剪到最近 30 天(丢最旧一天)。Day 仅作排序标识,
	// buildWinTrends 信任 SQL ORDER BY day ASC 的输入序(与 RadarStats 同范式)。
	days := make([]winTrendDayRow, 0, 31)
	for i := 0; i < 31; i++ {
		days = append(days, winTrendDayRow{
			ProviderID: "pX", Day: "day-" + itoa2(i), Games: 1, Wins: 0,
		})
	}
	roles := make([]winTrendSliceRow, 0, 10)
	for i := 0; i < 10; i++ {
		roles = append(roles, winTrendSliceRow{
			ProviderID: "pX", SliceKey: "role" + itoa2(i), Games: int64(10 - i), Wins: 1,
		})
	}
	out := buildWinTrends(days, roles, nil, nil) // names 缺失 → 回退 provider_id
	m := out["pX"]
	if m == nil {
		t.Fatal("pX must exist")
	}
	if m.AgentName != "pX" {
		t.Errorf("AgentName = %q, want provider_id 回退", m.AgentName)
	}
	if len(m.Trend) != winTrendMaxDays {
		t.Errorf("Trend = %d 天, want 裁剪到 %d", len(m.Trend), winTrendMaxDays)
	}
	if m.Trend[0].Day == days[0].Day {
		t.Error("最旧一天应被裁剪掉")
	}
	if len(m.ByRole) != winTrendMaxRoles {
		t.Errorf("ByRole = %d 行, want 裁剪到 %d", len(m.ByRole), winTrendMaxRoles)
	}
	if m.ByRole[0].Games != 10 {
		t.Errorf("ByRole[0].Games = %d, want 10(局数降序首位)", m.ByRole[0].Games)
	}
}

// itoa2 把 0..99 格式化为两位十进制字符串(避免测试引入 strconv 依赖歧义)。
func itoa2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func TestBuildPropEconomy_Basic(t *testing.T) {
	rows := []propEconomyRow{
		{
			PropID: "id1", PropKey: "markdown_bomb", NameZh: "紧急公告",
			Price: 120, BaseHitRate: 30,
			Uses: 10, Hits: 4,
			TotalSpent: 1200, PotReturn: 600, SystemAbsorb: 360, TargetCompens: 240,
		},
		{
			PropID: "id2", PropKey: "long_swear", NameZh: "长篇废话",
			Price: 90, BaseHitRate: 25,
			Uses: 5, Hits: 1,
			TotalSpent: 450, PotReturn: 225, SystemAbsorb: 135, TargetCompens: 90,
		},
	}
	out := buildPropEconomy(rows)
	if out == nil || len(out.Entries) != 2 {
		t.Fatalf("Entries = %+v, want 2 行(转换产物非空)", out)
	}
	// 命中率:4/10 = 40%
	if out.Entries[0].HitRate != 40 {
		t.Errorf("entry0.HitRate = %v, want 40", out.Entries[0].HitRate)
	}
	s := out.Summary
	if s.TotalUses != 15 || s.TotalHits != 5 {
		t.Errorf("Summary uses/hits = %d/%d, want 15/5", s.TotalUses, s.TotalHits)
	}
	// 5/15 = 33.333...% → 33.3
	if s.OverallHitRate != 33.3 {
		t.Errorf("OverallHitRate = %v, want 33.3", s.OverallHitRate)
	}
	if s.TotalSpent != 1650 || s.TotalPotReturn != 825 ||
		s.TotalSystemAbsorb != 495 || s.TotalTargetCompens != 330 {
		t.Errorf("Summary 金币四流向错误: %+v", s)
	}
}

func TestBuildPropEconomy_EmptyAndZeroUses(t *testing.T) {
	out := buildPropEconomy(nil)
	if out == nil || out.Entries == nil || len(out.Entries) != 0 {
		t.Fatalf("空输入应返回非 nil wrapper + 空 Entries, got %+v", out)
	}
	if out.Summary.OverallHitRate != 0 {
		t.Errorf("空输入 OverallHitRate = %v, want 0(除零保护)", out.Summary.OverallHitRate)
	}
	// Uses=0 的行被过滤
	out = buildPropEconomy([]propEconomyRow{{PropID: "x", Uses: 0}})
	if len(out.Entries) != 0 {
		t.Errorf("Uses=0 行应被过滤, got %+v", out.Entries)
	}
}
