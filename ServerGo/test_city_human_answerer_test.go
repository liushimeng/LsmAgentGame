// Package main — test_city_human_answerer_test.go: 批次85 §3.3 装配层单测
// (方案 §7.1 硬性断言 3/4/5;断言 1/2 在 game/virtual_city/room_agent_handle_test.go)。
//
// 覆盖:
//   - 共享桶生效:cityHumanAnswerer.ResidentAnswer 优先取游戏内实例
//     (room.AgentForSeat 命中)发问 —— 与月度决策同桶同钩子,且不再写缓存;
//   - 零回归:AgentForSeat 返回 nil(人类座位/游戏内 Agent 未装配)时回退
//     既有缓存实例,行为与批次 84 逐字一致(含错误路径);
//   - §3.3.3:hub.Describe 对全部已注册 Agent 返回非空 MessageTypes /
//     Description(list_agents 展示面;镜像钉死各 Agent 的登记取值)。
package main

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/agent/vcplayer"
	"LsmAgentGame/citychrome"
	"LsmAgentGame/game/virtual_city"
	llmtypes "LsmAgentGame/llm/types"
)

// assemblyTextProvider 按固定文本应答的 fake LLM provider(计数调用)。
// 回复文本用于区分「游戏内实例」与「缓存实例」走的注册表。
type assemblyTextProvider struct {
	reply string
	calls int32
}

func (p *assemblyTextProvider) Chat(context.Context, string, llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	atomic.AddInt32(&p.calls, 1)
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: p.reply}},
	}, nil
}

func (p *assemblyTextProvider) ChatStream(context.Context, string, llmtypes.LLMRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (p *assemblyTextProvider) ProviderType() string { return "fake-assembly" }

// assemblyRegistry 恒返回 assemblyTextProvider 的 fake 注册表。
type assemblyRegistry struct{ p llmtypes.LLMProvider }

func (r assemblyRegistry) Get(string) (llmtypes.LLMProvider, string, error) {
	return r.p, "fake-key", nil
}

func (r assemblyRegistry) GetThinkingEnabled(string) (bool, int) { return false, 0 }

// newAssemblyAnswererRoom 构造 10 bot 已开局房间 + 装配层 answerer。
// Start 先于 EnsureAgents:开局 wakeBots 时 r.agents 为空,不抢跑月度决策,
// 保证对话断言不受后台 LLM 调用干扰(令牌桶/统计计数确定性)。
func newAssemblyAnswererRoom(t *testing.T, roomID string, mgrReg virtual_city.LLMRegistry, answererReg vcplayer.LLMRegistry, ensureAgents bool) (*virtual_city.Manager, *virtual_city.VirtualCityRoom, *cityHumanAnswerer) {
	t.Helper()
	mgr := virtual_city.NewManager(virtual_city.Config{
		MonthMs: 3000, AgentEnabled: true, AgentDecisionTimeoutSec: 5,
	}, mgrReg)
	r := mgr.CreateRoom(roomID)
	botUsers := make(map[int]string, 10)
	botModels := make(map[int]string, 10)
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = "asm-bot-" + string(rune('0'+seat))
		botModels[seat] = "AsmModel" + string(rune('A'+seat))
	}
	r.RegisterBotSeats(botUsers, botModels)
	if err := r.Start(nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if ensureAgents {
		mgr.EnsureAgents(r)
	}
	a := &cityHumanAnswerer{
		mgr:             mgr,
		registry:        answererReg,
		rateInterval:    30 * time.Second,
		maxActions:      3,
		decisionTimeout: 5 * time.Second,
		agents:          map[cityHumanAgentKey]*vcplayer.Agent{},
	}
	return mgr, r, a
}

// TestCityHumanAnswerer_SharedInGameInstanceFirst 批次85 §3.3.2 + §7.1-3:
// AgentForSeat 命中 ⇒ 用游戏内实例发问(共享令牌桶 + BindLLMCallHook,
// 对话消耗计入房间 LLM 统计),且不再写 (房间,座位) 缓存。
func TestCityHumanAnswerer_SharedInGameInstanceFirst(t *testing.T) {
	inGame := &assemblyTextProvider{reply: "in-game"}
	cache := &assemblyTextProvider{reply: "cached"}
	_, r, a := newAssemblyAnswererRoom(t, "room-asm-shared",
		assemblyRegistry{p: inGame}, assemblyRegistry{p: cache}, true)
	_ = r

	reply, err := a.ResidentAnswer(context.Background(), "room-asm-shared", "最近怎么样?")
	if err != nil {
		t.Fatalf("ResidentAnswer: %v", err)
	}
	if reply != "in-game" {
		t.Fatalf("reply = %q, want in-game(应命中游戏内实例共享令牌桶)", reply)
	}
	if got := atomic.LoadInt32(&inGame.calls); got != 1 {
		t.Fatalf("游戏内注册表 provider 调用 = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&cache.calls); got != 0 {
		t.Fatalf("缓存注册表 provider 调用 = %d, want 0(命中游戏内实例不得走缓存)", got)
	}
	if len(a.agents) != 0 {
		t.Fatalf("缓存写入 %d 条 —— 命中游戏内实例时不得新建缓存实例", len(a.agents))
	}
}

// TestCityHumanAnswerer_FallbackCacheWhenNoInGameAgent 批次85 §3.3.2 + §7.1-4
// (零回归):AgentForSeat 返回 nil(游戏内 Agent 未装配)时回退既有缓存实例,
// 行为与批次 84 逐字一致 —— 同 (房间,座位) 缓存复用(令牌桶跨调用生效),
// 错误路径(未知房间/无可受访存活居民)不变。
func TestCityHumanAnswerer_FallbackCacheWhenNoInGameAgent(t *testing.T) {
	cache := &assemblyTextProvider{reply: "cached"}
	mgr, r, a := newAssemblyAnswererRoom(t, "room-asm-fallback",
		assemblyRegistry{p: &assemblyTextProvider{reply: "in-game"}}, assemblyRegistry{p: cache}, false)

	// 游戏内实例未装配 ⇒ AgentForSeat 恒 nil。
	if got := r.AgentForSeat(0); got != nil {
		t.Fatalf("AgentForSeat(0) = %p, want nil", got)
	}

	reply, err := a.ResidentAnswer(context.Background(), "room-asm-fallback", "最近怎么样?")
	if err != nil {
		t.Fatalf("ResidentAnswer: %v", err)
	}
	if reply != "cached" {
		t.Fatalf("reply = %q, want cached(应走批次84 缓存实例)", reply)
	}
	if got := atomic.LoadInt32(&cache.calls); got != 1 {
		t.Fatalf("缓存注册表 provider 调用 = %d, want 1", got)
	}
	if len(a.agents) != 1 {
		t.Fatalf("缓存条目 = %d, want 1(批次84 按 (房间,座位) 缓存口径)", len(a.agents))
	}

	// 同键复用缓存实例(令牌桶跨调用生效,不重复新建)。
	if _, err := a.ResidentAnswer(context.Background(), "room-asm-fallback", "再说一句"); err != nil {
		t.Fatalf("ResidentAnswer #2: %v", err)
	}
	if got := atomic.LoadInt32(&cache.calls); got != 2 {
		t.Fatalf("缓存注册表 provider 调用 = %d, want 2", got)
	}
	if len(a.agents) != 1 {
		t.Fatalf("缓存条目 = %d, want 1(同键不得重复新建)", len(a.agents))
	}

	// 零回归错误路径:未知房间。
	if _, err := a.ResidentAnswer(context.Background(), "room-asm-missing", "hi"); err == nil {
		t.Fatal("未知房间未报错(批次84 错误路径回归)")
	}
	// 零回归错误路径:无可受访存活居民(全部出局)。
	r.MuLock()
	for i := range r.World.Players {
		if p := r.World.Players[i]; p != nil {
			p.Alive = false
		}
	}
	r.MuUnlock()
	if _, err := a.ResidentAnswer(context.Background(), "room-asm-fallback", "hi"); err == nil {
		t.Fatal("全员出局后未报错(批次84 错误路径回归)")
	}
	_ = mgr
}

// citySystemBusMetaForDescribe 是 City-System 房间级注册的**约定元信息**
// (批次85 §3.3.3 迁移清单;MessageTypes 实测自 citysystem.bus_handler.go
// handleBusMessage 的 `msg.Type != BusTypeMessage("message")` 单一分支)。
// 生产接线在 ServerGo/agent/citysystem/bus_handler.go(该包按分工归 City-System
// 线,本线不动)—— 待其 RegisterRoom → RegisterRoomWith 后,本取值即为权威。
const citySystemBusMetaDesc = "城市系统主 Agent:接收其它 Agent 的上行来信(message 类型;入队 + 前端来信卡片 + 回执确认,不代答),下一轮系统对话注入上下文。"

// cityWebGLShowBusMetaDesc 是 City-WebGL-Show 房间级注册的能力描述 ——
// 与 ws/game_service_virtual_city_system.go 的 RegisterRoomWith 登记**逐字一致**
// (迁移清单 §3.3.3;MessageTypes 实测自该 handler 的 switch msg.Type 分支)。
const cityWebGLShowBusMetaDesc = "渲染显示 Agent:把结构化/自然语言显示请求翻译为 DisplayCommand 显示原语(run_display 缺省零延迟直通,message 走对话式翻译并逐条下发前端,回中文摘要)。"

// TestBusDescribe_RegisteredAgentsCarryMetadata 批次85 §7.1-5:list_agents /
// hub.Describe 对全部已注册 Agent 返回非空 Description 与 MessageTypes。
//
// 类级两条与 main.go 装配逐字相同的 RegisterWith 调用(批次84 常量);
// 房间级两条按 §3.3.3 迁移清单的元信息登记(City-WebGL-Show 与生产 ws/ 登记
// 逐字一致;City-System 为约定取值,生产接线待 City-System 线迁移后由其单测
// 钉死 —— 本测试钉死的是「取值 + Describe 契约」)。
func TestBusDescribe_RegisteredAgentsCarryMetadata(t *testing.T) {
	hub := citybus.New()
	defer hub.Close()

	if err := hub.RegisterWith(string(agentroot.AgentClassCityChrome),
		citychrome.BusMessageTypes, citychrome.BusCapabilityDesc,
		citychrome.HandleBusMessage); err != nil {
		t.Fatalf("register City-Chrome: %v", err)
	}
	if err := hub.RegisterWith(string(agentroot.AgentClassCityHuman),
		vcplayer.BusMessageTypes, vcplayer.BusCapabilityDesc,
		vcplayer.HandleBusMessage); err != nil {
		t.Fatalf("register City-Human: %v", err)
	}
	const roomID = "room-bus-describe"
	noop := func(context.Context, citybus.Message) (any, error) { return nil, nil }
	if err := hub.RegisterRoomWith(roomID, string(agentroot.AgentClassCityWebGLShow),
		[]string{"", "run_display", "message"}, cityWebGLShowBusMetaDesc, noop); err != nil {
		t.Fatalf("register City-WebGL-Show: %v", err)
	}
	if err := hub.RegisterRoomWith(roomID, string(agentroot.AgentClassCitySystem),
		[]string{"message"}, citySystemBusMetaDesc, noop); err != nil {
		t.Fatalf("register City-System: %v", err)
	}

	descs := hub.Describe(roomID)
	if len(descs) != 4 {
		t.Fatalf("Describe 条目 = %d, want 4: %+v", len(descs), descs)
	}
	for _, d := range descs {
		if d.Description == "" {
			t.Fatalf("agent %s(%s) Description 为空(list_agents 显示「未登记描述」回归)", d.ClassName, d.Scope)
		}
		if len(d.MessageTypes) == 0 {
			t.Fatalf("agent %s(%s) MessageTypes 为空", d.ClassName, d.Scope)
		}
	}
}
