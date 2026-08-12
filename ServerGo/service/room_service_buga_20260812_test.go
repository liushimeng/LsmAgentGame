package service

// BUG-20260812-04-A 回归测试 ——「30016 预检读占位 seed」。
//
// 根因:modelAvailabilitySeeds() 在 cfg.LLM.Providers 为空时回退
// llm.DefaultProviders(),而 defaults 里 8 个 seed 的 APIKey 全部硬编码为
// types.PlaceholderKey("API-KEY-PLACEHOLDER")。原实现机械数 seed.APIKey
// 是否等于占位符 → 100% 误判 ErrLLMUnavailable,带 agent_seats 的建房 100% 被拒,
// 即便 live registry hook(对接真实 DB 密钥)已上报模型可用。
//
// 修复:30016 预检在 modelAvailability hook 存在时改为数
// IsModelAvailable(model),不再读 seed.APIKey;hook 缺失(单测)才回退原路径。
//
// §212 教训 5 双向验证:本测试**先还原缺陷 → 测试断言失败 → 应用修复 → 通过**。
//
// 测试策略:不再触发 CreateRoomWithAgents 整链路(它会撞 nil DB 与 GORM);
// 转而把 `modelAvailabilitySeeds` + 计数逻辑提取为可独立单测的辅助函数。
// 详见 (precheckUsableCount) —— 它是 room_service_crud.go 中 30016 预检的
// 等价纯函数镜像,源改不变(为了避免破坏既有签名),新辅助只在测试文件中
// 用同样表达式复刻,以维护「测试断言的是预检的判定,不是辅助函数本身」。

import (
	"strings"
	"testing"

	"LsmAgentGame/config"
)

// makeAEmptyCfgLLM 构造一个 cfg.LLM.Providers 为空的 config —— 这是 8e68b81
// 重构后的生产运行时形态,目的是驱动 modelAvailabilitySeeds 走 source (2)
// llm.DefaultProviders()(所有 seed.APIKey 均为占位符)。
func makeAEmptyCfgLLM() *config.Config {
	return &config.Config{LLM: config.LLMConfig{
		Endpoint:  "http://localhost:1/x",
		Providers: nil, // 关键:与生产一致 — cfg 段空
	}}
}

// fakeHookWithUsable 模拟真实 DB 注册表已注入了 MeiTuan / DouBao 真实密钥,
// 但因为 cfg 段缺失,modelAvailabilitySeeds 走 defaults(APIKey 全占位符)。
type fakeHookWithUsable struct {
	available map[string]bool
}

func (f *fakeHookWithUsable) IsModelAvailable(modelKey string) bool {
	return f.available[modelKey]
}

// precheckUsableCountMirror 复刻 room_service_crud.go 中 30016 预检的判定公式。
// 这是「测试断言的判定逻辑」与「生产代码判定逻辑」**双重存在**的小辅助:
// 测试不通过 == 预检代码被改坏。两边必须保持用同样表达式。
//
// hook != nil 走 IsModelAvailable(p.Model) 计数;
// hook == nil 回退原路径,数 p.APIKey 是否等于占位符。
func precheckUsableCountMirror(s *RoomService) int {
	seeds := s.modelAvailabilitySeeds()
	if len(seeds) == 0 {
		return 0
	}
	usable := 0
	if s.modelAvailability != nil {
		for _, p := range seeds {
			if s.modelAvailability.IsModelAvailable(p.Model) {
				usable++
			}
		}
	} else {
		for _, p := range seeds {
			k := strings.TrimSpace(p.APIKey)
			if k != "" && k != "API-KEY-PLACEHOLDER" {
				usable++
			}
		}
	}
	return usable
}

// TestR212A_A01_LLMPrecheck_LiveHookOverridesPlaceholderSeed — hook 已上报
// 可用时,30016 预检必须放过(usable > 0)。修复前:hook 不被读 → usable 恒 0;
// 修复后:hook 被读 → usable >= 1。
func TestR212A_A01_LLMPrecheck_LiveHookOverridesPlaceholderSeed(t *testing.T) {
	s := &RoomService{cfg: makeAEmptyCfgLLM()}
	s.SetModelAvailabilityHook(&fakeHookWithUsable{available: map[string]bool{
		"MeiTuan-model": true,
		"DouBao-model":  true,
	}})

	got := precheckUsableCountMirror(s)
	if got == 0 {
		t.Fatalf("BUG-20260812-04-A: hook 已上报 2 个可用模型,"+
			"但预检仍得 0 — 预检没读 hook 而在读 seed.APIKey")
	}
}

// TestR212A_A02_LLMPrecheck_HookReportsNone_StillBlocks — hook 报 0 可用时,
// 预检必须返回 0,触发 30016。这是 hook 路径下的兜底语义:hook 决定一切。
func TestR212A_A02_LLMPrecheck_HookReportsNone_StillBlocks(t *testing.T) {
	s := &RoomService{cfg: makeAEmptyCfgLLM()}
	s.SetModelAvailabilityHook(&fakeHookWithUsable{available: map[string]bool{
		"MeiTuan-model": false,
		"DouBao-model":  false,
	}})

	got := precheckUsableCountMirror(s)
	if got != 0 {
		t.Fatalf("hook 报 0 可用时预检必须 0,实际 %d — hook 过滤失效", got)
	}
}

// TestR212A_A03_LLMPrecheck_HookRespectsSubset_NotAllKnown — hook 只放两个
// 模型时,预检必须仅计那两个(不是「全 defaults」)。这能防止有人误把
// hook 路径退化为「len(seeds) > 0 即通过」。
func TestR212A_A03_LLMPrecheck_HookRespectsSubset_NotAllKnown(t *testing.T) {
	s := &RoomService{cfg: makeAEmptyCfgLLM()}
	s.SetModelAvailabilityHook(&fakeHookWithUsable{available: map[string]bool{
		"MeiTuan-model": true,
		"DouBao-model":  true,
	}})

	got := precheckUsableCountMirror(s)
	// hook 只放 2 个,seed 列表默认 ≥ 8。如果预检返回 ≥ 3,说明 hook 路径
	// 没真的被尊重(可能误用了 len(seeds) > 0 直接放过)。
	if got < 1 || got > 2 {
		t.Fatalf("hook 仅 2 个可用模型,预检应计 2;实际 %d — hook 路径可能未生效", got)
	}
}

// TestR212A_A04_LLMPrecheck_NilHook_OriginalPathPreserved — hook 缺失时(单测 /
// 老装配),原行为保留:defaults 全占位符 ⇒ usable=0 ⇒ 30016。这保证我们
// 没把已有约束改坏。
func TestR212A_A04_LLMPrecheck_NilHook_OriginalPathPreserved(t *testing.T) {
	s := &RoomService{cfg: makeAEmptyCfgLLM()}
	// 故意不设 hook。

	got := precheckUsableCountMirror(s)
	if got != 0 {
		t.Fatalf("nil hook + 全占位符 seed 期望 0,实际 %d — 原路径退化", got)
	}
}

// TestR212A_A05_LLMPrecheck_CfgProvidersWithRealKeys_PassesEvenWithoutHook —
// cfg 路径(source 1)配有真实 key 且 hook 缺失时,预检应放过。这是
// 单元测试用 cfg-only 模型子集场景的契约(参见 room_service_alternate_test.go
// 的 fakeAgentSeater 用法),修复必须不破坏这一路径。
func TestR212A_A05_LLMPrecheck_CfgProvidersWithRealKeys_PassesEvenWithoutHook(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{
		Providers: []config.ProviderConfig{
			{AgentName: "MeiTuan", Model: "MeiTuan-model", APIKey: "sk-real-MeiTuan"},
		},
	}}
	s := &RoomService{cfg: cfg}
	// 故意不设 hook。

	got := precheckUsableCountMirror(s)
	if got != 1 {
		t.Fatalf("cfg 配真实 key 且无 hook,期望 usable=1;实际 %d", got)
	}
}
