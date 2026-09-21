// Package wealth — engine_v212_test.go: v2.12 动态并发公式 + 自适应月窗边界单测
// (2026-09-21 §城市扩张v2.12)。
package wealth

import (
	"testing"

	agentroot "LsmAgentGame/agent"
)

// TestDefaultAgentConcurrencyFor v2.12 动态并发公式边界。
// 公式: target = maxSeats/2+1,clamp [4, 64, cap poolTotal];poolTotal≤0 回落默认。
func TestDefaultAgentConcurrencyFor(t *testing.T) {
	cases := []struct {
		maxSeats, poolTotal int
	}{
		{12, 0},   // 默认回落:池 fallback=8,target=7,7<8 → 7
		{200, 64}, // 200→MaxSeats=12,target=7,cap 64=7 → 7
		{0, 8},    // 0→MaxSeats=12,target=7,cap 8=7 → 7
		{12, 64},  // target=7,7<64 → 7
		{4, 8},    // target=3,<4 fallback → 4
		{12, 4},   // target=7,cap poolTotal=4 → 4
		{100, 2},  // 100→MaxSeats=12,target=7,cap poolTotal=2 → 2 → clamp 4 → 4
	}
	for _, c := range cases {
		got := DefaultAgentConcurrencyFor(c.maxSeats, c.poolTotal)
		// 重新计算期望(与实现同公式)
		max := c.maxSeats
		if max <= 0 || max > MaxSeats {
			max = MaxSeats
		}
		pool := c.poolTotal
		if pool <= 0 {
			pool = DefaultAgentConcurrency
		}
		target := max/2 + 1
		if target > pool {
			target = pool
		}
		if target > 64 {
			target = 64
		}
		if target < 4 {
			target = 4
		}
		if got != target {
			t.Errorf("DefaultAgentConcurrencyFor(%d,%d)=%d want %d",
				c.maxSeats, c.poolTotal, got, target)
		}
	}
}

// TestMonthWindowFor v2.12 月窗自适应公式边界。
// 公式: batches=ceil(maxSeats/llmConcurrency),totalMs=batches*4000+20000+4000,
// clamp [3000, 60000]。
func TestMonthWindowFor(t *testing.T) {
	cases := []struct {
		maxSeats, llmConcurrency int
	}{
		{12, 8},    // batches=2,totalMs=8000+20000+4000=32000
		{200, 64},  // 200→MaxSeats=12,llm=64,batches=1,totalMs=4000+20000+4000=28000
		{0, 0},     // fallback:maxSeats=12,llm=8 → 32000
		{12, 4},    // batches=3,totalMs=12000+20000+4000=36000
		{12, 1},    // batches=12,totalMs=48000+20000+4000=72000 → clamp 60000
		{12, 100},  // batches=1,totalMs=28000
	}
	for _, c := range cases {
		got := MonthWindowFor(c.maxSeats, c.llmConcurrency)
		if got < 3000 || got > 60000 {
			t.Errorf("MonthWindowFor(%d,%d)=%d out of [3000,60000]",
				c.maxSeats, c.llmConcurrency, got)
		}
		// 重新计算期望
		llm := c.llmConcurrency
		if llm <= 0 {
			llm = DefaultAgentConcurrency
		}
		max := c.maxSeats
		if max <= 0 {
			max = MaxSeats
		}
		batches := (max + llm - 1) / llm
		want := batches*4000 + 20000 + 4000
		if want < 3000 {
			want = 3000
		}
		if want > 60000 {
			want = 60000
		}
		if got != want {
			t.Errorf("MonthWindowFor(%d,%d)=%d want %d",
				c.maxSeats, c.llmConcurrency, got, want)
		}
	}
}

// TestAgentClassNamesRegistered v2.12 三个新 AgentClassName 已注册 (§130 grep 验证接线)。
// 注: 这是 wealth 包内单测;class_names.go 在 agent 包。
// 仅校验引用能解析(编译期会失败);完整注册验证见 agent 包测试。
func TestAgentClassNamesRegistered(t *testing.T) {
	// 编译期校验:class_names.go 中 AgentClassCityVoice 常量可解析 (§130 接线验证)
	_ = agentroot.AgentClassCityVoice
}
