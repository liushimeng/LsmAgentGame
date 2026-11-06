// Package citysystem — dispatch_scope13_test.go: query_city_state 13 维度
// scope 全集(批次85 E2E 缺口 F1)单测。
//
// 覆盖:scope 枚举恰 19 个(6 个既有 + 13 个维度名,断言枚举内容)/
// 13 个维度名逐个可查(实质内容 + 观察闭环,无「子系统未启用」空答复)/
// 注入场景后对应维度 scope 含「【本维度进行中】」段与该场景名 /
// 既有 6 个 scope 零回归(关键指标仍在)/ 7 个英文旧键别名继续可达。
package citysystem

import (
	"context"
	"strings"
	"testing"

	"LsmAgentGame/game/virtual_city"
)

// wantLegacyScopes 是 6 个既有 scope(语义零回归)。
var wantLegacyScopes = []string{"overview", "market", "labor", "treasury", "election", "all"}

// wantDimensionScopes 是 13 个维度名(与 virtual_city.ScenarioGroup* 取值
// 逐字一致;测试独立列出防映射表自证)。
var wantDimensionScopes = []string{
	"市场与经济", "政策与治理", "社会与舆情", "自然灾害", "未知灾难", "科技与新文明",
	"公共卫生", "基础设施", "交通物流", "文化教育", "环境生态", "公共安全", "民生日常",
}

// dimensionWantMarkers 各维度 scope 的实质内容锚点(真实指标或如实的
// 「暂无」声明;§29.11 无数据明说,绝不编造)。
//
// ⚠ 口径:未月结的房间 Society 为空(settlement ④.6B 才 ComputeSociety),
// 社会结构类指标(财富基尼/圈层)只在月结后可得 —— 锚点只钉**恒在**的
// 真实指标,不钉月结依赖项。
var dimensionWantMarkers = map[string][]string{
	"市场与经济":   {"市场与经济(M", "CPI 同比", "股指", "黄金", "八区房价指数"},
	"政策与治理":   {"政策与治理(M", "国库", "政策利率", "市长选举"},
	"社会与舆情":   {"社会与舆情(M", "满意度偏置", "本维度活动场景"},
	"自然灾害":     {"自然灾害(M", "食品类商品价格指数", "本维度活动场景"},
	"未知灾难":     {"未知灾难(M", "暂无该维度专属指标", "本维度活动场景"},
	"科技与新文明": {"科技与新文明(M", "暂无该维度专属指标", "本维度活动场景"},
	"公共卫生":     {"公共卫生(M", "医疗质量", "教育质量"},
	"基础设施":     {"基础设施(M", "产业链节点", "层级价格指数", "产业集群"},
	"交通物流":     {"交通物流(M", "交通类商品价格指数", "物流依托产业链"},
	"文化教育":     {"文化教育(M", "教育质量", "公共教育累积投入"},
	"环境生态":     {"环境生态(M", "食品类商品价格指数", "本维度活动场景"},
	"公共安全":     {"公共安全(M", "累计罚款", "居民现金损失类场景"},
	"民生日常":     {"民生日常(M", "CPI 同比", "价格指数"},
}

// TestQueryCityState_ScopeEnum19 scope 枚举恰 19 个 = 6 个既有 + 13 个维度名,
// 且 13 个维度名与 virtual_city.ScenarioGroupOrder() 全集双向一致
// (模型可直接把 list_scenarios 维度索引里的组名当 scope 传)。
func TestQueryCityState_ScopeEnum19(t *testing.T) {
	if len(queryCityStateScopes) != 19 {
		t.Fatalf("scope count = %d, want 19: %v", len(queryCityStateScopes), queryCityStateScopes)
	}
	got := map[string]bool{}
	for _, s := range queryCityStateScopes {
		if got[s] {
			t.Fatalf("duplicate scope %q", s)
		}
		got[s] = true
	}
	for _, w := range append(append([]string(nil), wantLegacyScopes...), wantDimensionScopes...) {
		if !got[w] {
			t.Errorf("scope enum missing %q", w)
		}
	}
	if len(got) != len(wantLegacyScopes)+len(wantDimensionScopes) {
		t.Errorf("scope enum = %v, want exactly 6 legacy + 13 dimension names", queryCityStateScopes)
	}
	// 13 个维度名与上游分组表全集一致(双向无遗漏)。
	groups := map[string]bool{}
	for _, g := range virtual_city.ScenarioGroupOrder() {
		groups[g] = true
	}
	for _, d := range wantDimensionScopes {
		if !groups[d] {
			t.Errorf("dimension scope %q not in ScenarioGroupOrder()", d)
		}
	}
	if len(groups) != len(wantDimensionScopes) {
		t.Errorf("ScenarioGroupOrder() = %v, want %d groups",
			virtual_city.ScenarioGroupOrder(), len(wantDimensionScopes))
	}
}

// TestQueryCityState_13DimensionScopes_Answer F1 核心:13 个维度名逐个调用
// ⇒ 成功返回实质内容(含观察闭环回执),且不含「子系统未启用」空答复
// (修前 6 个维度无 scope 可调,模型只能误答「进行中场景:无」)。
func TestQueryCityState_13DimensionScopes_Answer(t *testing.T) {
	f := newExtFixture(t)
	for _, dim := range wantDimensionScopes {
		text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryCityState,
			map[string]any{"scope": dim})
		if isErr {
			t.Fatalf("dimension scope %s must be queryable (F1), got error: %s", dim, text)
		}
		if strings.Contains(text, "子系统未启用") {
			t.Errorf("dimension scope %s must not answer 子系统未启用: %q", dim, text)
		}
		marker := strings.Contains(text, "【本维度进行中】") ||
			strings.Contains(text, "【相关世界事件】") ||
			strings.Contains(text, "当前该维度无进行中场景、无相关事件")
		if !marker {
			t.Errorf("dimension scope %s missing observation loop: %q", dim, text)
		}
		for _, want := range dimensionWantMarkers[dim] {
			if !strings.Contains(text, want) {
				t.Errorf("dimension scope %s missing marker %q: %q", dim, want, text)
			}
		}
	}
}

// TestQueryCityState_DimensionScope_ObservationLoop 注入场景后,对应维度
// scope 结果含「【本维度进行中】」段与该场景名(观察闭环 13 维度逐个;
// 活动场景上限 ≤8/房,故每维度独立房间注入)。
func TestQueryCityState_DimensionScope_ObservationLoop(t *testing.T) {
	for _, dim := range wantDimensionScopes {
		t.Run(dim, func(t *testing.T) {
			f := newExtFixture(t)
			spec := firstKindOfGroup(t, dim)
			if _, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolInjectScenario,
				map[string]any{"kind": spec.Kind, "magnitude": 3, "duration_months": 6}); isErr {
				t.Fatalf("inject %s must succeed", spec.Kind)
			}
			text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryCityState,
				map[string]any{"scope": dim})
			if isErr {
				t.Fatalf("scope %s: unexpected error: %s", dim, text)
			}
			if !strings.Contains(text, "【本维度进行中】") {
				t.Errorf("scope %s missing 【本维度进行中】 section: %q", dim, text)
			}
			if !strings.Contains(text, spec.Name) {
				t.Errorf("scope %s missing scenario name %q: %q", dim, spec.Name, text)
			}
			if !strings.Contains(text, "L3") || !strings.Contains(text, "注入于 M") {
				t.Errorf("scope %s scenario line must carry magnitude/applied month: %q", dim, text)
			}
			if strings.Contains(text, "子系统未启用") {
				t.Errorf("scope %s must not answer 子系统未启用: %q", dim, text)
			}
		})
	}
}

// TestQueryCityState_LegacyScopes_ZeroRegression 既有 6 个 scope 语义零回归:
// 关键指标仍在(F1 只扩维度 scope,不动既有口径)。
func TestQueryCityState_LegacyScopes_ZeroRegression(t *testing.T) {
	f := newExtFixture(t)
	cases := []struct {
		scope string
		want  []string
	}{
		{"overview", []string{"城市状态", "人口", "CPI 同比", "股指", "活动场景"}},
		{"market", []string{"CPI 同比", "股指", "黄金", "国库"}},
		{"labor", []string{"失业率", "工资"}},
		{"treasury", []string{"国库", "政策利率"}},
		{"election", []string{"市长选举", "满意度偏置"}},
		{"all", []string{"城市状态", "基础设施", "公共卫生", "民生日常"}},
	}
	for _, tc := range cases {
		text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryCityState,
			map[string]any{"scope": tc.scope})
		if isErr {
			t.Fatalf("scope %s: unexpected error: %s", tc.scope, text)
		}
		for _, want := range tc.want {
			if !strings.Contains(text, want) {
				t.Errorf("scope %s regressed, missing %q: %q", tc.scope, want, text)
			}
		}
	}
}

// TestQueryCityState_ScopeAliases 批次84 的 7 个英文维度键别名继续可达
// (向后兼容零回归,dispatch_dimension_test.go 等既有调用不受影响),
// 且命中**维度**口径(非全局聚合)。
func TestQueryCityState_ScopeAliases(t *testing.T) {
	f := newExtFixture(t)
	cases := []struct{ scope, want string }{
		{"infrastructure", "产业链节点"},
		{"public_health", "医疗质量"},
		{"environment", "食品类商品价格指数"},
		{"security", "累计罚款"},
		{"transport", "交通类商品价格指数"},
		{"culture", "教育质量"},
		{"daily", "CPI 同比"},
	}
	for _, tc := range cases {
		text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryCityState,
			map[string]any{"scope": tc.scope})
		if isErr {
			t.Fatalf("alias %s must still work (zero regression), got error: %s", tc.scope, text)
		}
		if !strings.Contains(text, tc.want) {
			t.Errorf("alias %s missing %q: %q", tc.scope, tc.want, text)
		}
		if strings.Contains(text, "【全局进行中】") {
			t.Errorf("alias %s must hit dimension view, not global: %q", tc.scope, text)
		}
	}
}
