package wwplayer

import (
	"strings"
	"testing"
)

// §20260812-04 U4 回归测试 —— 长期记忆按角色选取。
//
// 缺陷:记忆按 model_key 存储、不按角色隔离,注入时一律全量。同一模型坐预言家
// 学到的教训,坐狼人时照样被全量注入(既浪费 token 又干扰决策)。

const roleScopedMemoryMD = `# Agent 长期记忆

## 战绩与趋势
共 12 局,胜率 58%。

## 我的失误与教训
通用:发言不要复读,注意票型变化。

### 作为预言家
第 3 局首验查了自己人,浪费一晚。

### 作为狼人
悍跳节奏太急,第一天就被识破。

### 作为女巫
首夜盲救导致解药浪费。

## 其他模型特点分析
DouBao 偏激进。

## 决策策略迭代
优先归票,不做无谓自爆。
`

// U4-01: 预言家只应看到通用段 + 预言家子段。
func TestMemoryRole_U4_01_SeerGetsOwnSubsectionOnly(t *testing.T) {
	got := SelectMemoryForRole(roleScopedMemoryMD, "seer")

	if !strings.Contains(got, "首验查了自己人") {
		t.Fatalf("预言家应保留自己的教训子段\n%s", got)
	}
	if !strings.Contains(got, "发言不要复读") {
		t.Fatalf("跨角色通用教训必须保留\n%s", got)
	}
	if strings.Contains(got, "悍跳节奏太急") {
		t.Fatalf("预言家不应看到狼人子段\n%s", got)
	}
	if strings.Contains(got, "首夜盲救") {
		t.Fatalf("预言家不应看到女巫子段\n%s", got)
	}
	// 其它二级段必须完整保留(角色过滤只作用于 ### 子段)。
	for _, want := range []string{"## 战绩与趋势", "## 其他模型特点分析", "## 决策策略迭代", "优先归票"} {
		if !strings.Contains(got, want) {
			t.Fatalf("非角色段 %q 被误删\n%s", want, got)
		}
	}
}

// U4-02: 狼人只应看到自己的子段。
func TestMemoryRole_U4_02_WerewolfGetsOwnSubsection(t *testing.T) {
	got := SelectMemoryForRole(roleScopedMemoryMD, "werewolf")
	if !strings.Contains(got, "悍跳节奏太急") {
		t.Fatalf("狼人应保留自己的教训\n%s", got)
	}
	if strings.Contains(got, "首验查了自己人") {
		t.Fatalf("狼人不应看到预言家子段\n%s", got)
	}
}

// U4-03: 旧格式（无 ### 子段）必须原样返回 —— 向后兼容硬约束。
func TestMemoryRole_U4_03_LegacyFormatUnchanged(t *testing.T) {
	legacy := "# 记忆\n\n## 战绩与趋势\n10 局 6 胜。\n\n## 我的失误与教训\n别复读。\n"
	if got := SelectMemoryForRole(legacy, "seer"); got != strings.TrimSpace(legacy) {
		t.Fatalf("旧格式记忆不应被改写\nwant:\n%s\ngot:\n%s", legacy, got)
	}
}

// U4-04: 未知/空角色不做过滤（保守：宁可多注入也不误删）。
func TestMemoryRole_U4_04_UnknownRoleNoFilter(t *testing.T) {
	for _, role := range []string{"", "unknown_role", "magician"} {
		got := SelectMemoryForRole(roleScopedMemoryMD, role)
		if !strings.Contains(got, "悍跳节奏太急") || !strings.Contains(got, "首验查了自己人") {
			t.Fatalf("role=%q 不应触发角色过滤", role)
		}
	}
}

// U4-05: 角色裁剪确实降低了注入体积 —— 这是本项优化的收益本体。
func TestMemoryRole_U4_05_ReducesInjectedSize(t *testing.T) {
	full := InjectBlockWithBudget(roleScopedMemoryMD, 0)
	scoped := InjectBlockForRole(roleScopedMemoryMD, "seer", 0)

	if len([]rune(scoped)) >= len([]rune(full)) {
		t.Fatalf("按角色裁剪后应更短:full=%d runes, scoped=%d runes",
			len([]rune(full)), len([]rune(scoped)))
	}
	// 但头尾包装必须一致（两条路径同一实现点）。
	for _, want := range []string{"【你的长期记忆（跨局积累）】", "本局信息以上方实时状态为准"} {
		if !strings.Contains(scoped, want) {
			t.Fatalf("角色感知路径缺少包装 %q", want)
		}
	}
}

// U4-06: 难度档位预算生效（difficulty.MemoryInjectRunes 此前是死配置）。
func TestMemoryRole_U4_06_BudgetIsHonoured(t *testing.T) {
	long := strings.Repeat("忆", 5000)
	small := InjectBlockForRole(long, "seer", 200)
	big := InjectBlockForRole(long, "seer", 3000)

	if len([]rune(small)) >= len([]rune(big)) {
		t.Fatalf("maxRunes 未生效:small=%d, big=%d", len([]rune(small)), len([]rune(big)))
	}
}

// U4-07: 空记忆不注入。
func TestMemoryRole_U4_07_EmptyNoInject(t *testing.T) {
	for _, in := range []string{"", "   \n\t "} {
		if got := InjectBlockForRole(in, "seer", 0); got != "" {
			t.Fatalf("空记忆不应注入,got %q", got)
		}
	}
}
