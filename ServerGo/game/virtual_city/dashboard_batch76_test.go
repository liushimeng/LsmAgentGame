// Package virtualcity — dashboard_batch76_test.go: 批次76 数据源登记册
// 扩容守护(22 → 45 条,新增「时政军事」分组)+ 副本语义回归。
package virtual_city

import "testing"

// TestDashboardDataSourcesRegistry45 登记册条数与分组齐备(§八 清单 2026-10-22
// 实测版;条数/分组漂移即失败,防后续增删无感)。
func TestDashboardDataSourcesRegistry45(t *testing.T) {
	srcs := DashboardDataSources()
	if len(srcs) != 45 {
		t.Fatalf("data sources = %d, want 45 (批次76)", len(srcs))
	}
	groups := map[string]int{}
	for _, s := range srcs {
		if s.Category == "" || s.Name == "" || s.Purpose == "" {
			t.Errorf("incomplete entry: %+v", s)
		}
		if len(s.URL) < 8 || s.URL[:8] != "https://" {
			t.Errorf("entry %s: URL not https: %q", s.Name, s.URL)
		}
		groups[s.Category]++
	}
	want := map[string]int{
		"综合财经": 5, "A股市场": 4, "港股市场": 1, "美股市场": 1, "贵金属大宗": 7,
		"期货市场": 6, "加密货币": 3, "宏观数据": 4, "时政军事": 8, "数据接口": 6,
	}
	for g, n := range want {
		if groups[g] != n {
			t.Errorf("group %q = %d entries, want %d", g, groups[g], n)
		}
	}
}

// TestDashboardDataSourcesCopyIsolation 副本语义:调用方排序/改字段不得污染包级登记册。
func TestDashboardDataSourcesCopyIsolation(t *testing.T) {
	srcs := DashboardDataSources()
	if len(srcs) == 0 {
		t.Fatal("empty registry")
	}
	srcs[0].Name = "POLLUTED"
	srcs = append(srcs[:0], srcs[1:]...)
	again := DashboardDataSources()
	if len(again) != 45 || again[0].Name == "POLLUTED" {
		t.Errorf("registry polluted via returned slice: len=%d first=%q", len(again), again[0].Name)
	}
}
