// Package virtual_city — room_agent_handle_test.go: 批次85 §3.3.1 座位 Agent
// 只读句柄访问器单测(方案 §7.1 硬性断言)。
//
// 覆盖:
//   - AgentForSeat 契约:nil 房间/越界/未启动/非居民 ⇒ nil;命中 ⇒ 非 nil
//     且两次调用同一指针(只读句柄,不新建实例);
//   - AgentForSeat 只读:调用前后房间/世界状态逐字段相等(既有快照对比:
//     World.StateHash + transcript 快照 + 房间字段摘要 + r.agents 指针表);
//   - 共享桶生效:游戏内实例(与月度决策同指针 ⇒ 同令牌桶 + 同
//     BindLLMCallHook)的对话问答 LLM 调用计入房间 LLM 统计
//     (noteSeatLLMCall → llmStats.seatCalls),且令牌桶容量语义(2 枚,
//     第 3 次节流)在共享实例上生效。
package virtual_city

import (
	"context"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"LsmAgentGame/agent/vcplayer"
	"LsmAgentGame/agent/vctypes"
	llmtypes "LsmAgentGame/llm/types"
)

// handleTextProvider 按固定文本应答的 fake LLM provider(计数调用)。
type handleTextProvider struct {
	reply string
	calls int32
}

func (p *handleTextProvider) Chat(context.Context, string, llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	atomic.AddInt32(&p.calls, 1)
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: p.reply}},
	}, nil
}

func (p *handleTextProvider) ChatStream(context.Context, string, llmtypes.LLMRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (p *handleTextProvider) ProviderType() string { return "fake-handle" }

// handleTextRegistry 恒返回 handleTextProvider 的 fake 注册表。
type handleTextRegistry struct{ p llmtypes.LLMProvider }

func (r handleTextRegistry) Get(string) (llmtypes.LLMProvider, string, error) {
	return r.p, "fake-key", nil
}

func (r handleTextRegistry) GetThinkingEnabled(string) (bool, int) { return false, 0 }

// newHandleTestRoom 构造带 bot 座位的房间(不含 Start;EnsureAgents 由调用方
// 按需触发,避免开局 wakeBots 抢跑月度决策干扰断言)。
func newHandleTestRoom(t *testing.T, roomID string, botSeats map[int]string, models map[int]string) (*Manager, *VirtualCityRoom) {
	t.Helper()
	m := NewManager(Config{MonthMs: 3000, AgentEnabled: true, AgentDecisionTimeoutSec: 5},
		handleTextRegistry{p: &handleTextProvider{reply: "ok"}})
	r := m.CreateRoom(roomID)
	r.RegisterBotSeats(botSeats, models)
	return m, r
}

// TestAgentHandle_AgentForSeatContract 批次85 §7.1:AgentForSeat 契约 ——
// 未启动(World nil)/越界/非居民 ⇒ nil 且不 panic;命中 ⇒ 非 nil 且两次
// 调用同一指针(只读句柄,不新建实例)。
func TestAgentHandle_AgentForSeatContract(t *testing.T) {
	var nilRoom *VirtualCityRoom
	if got := nilRoom.AgentForSeat(0); got != nil {
		t.Fatalf("nil room: got %p, want nil", got)
	}

	botUsers := make(map[int]string, 10)
	botModels := make(map[int]string, 10)
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = "bot-h" + string(rune('0'+seat))
		botModels[seat] = "ModelH" + string(rune('A'+seat))
	}

	// ── 房间 U:未启动(World nil)——即使 Agent 已装配(hydrate 形态)也必须
	// 返回 nil 且不 panic(§3.3.1:未开局不放行句柄)。
	mu, ru := newHandleTestRoom(t, "room-agent-handle-u", botUsers, botModels)
	for seat := 0; seat < MaxSeats; seat++ {
		if got := ru.AgentForSeat(seat); got != nil {
			t.Fatalf("unstarted seat %d before EnsureAgents: got %p, want nil", seat, got)
		}
	}
	mu.EnsureAgents(ru)
	if got := ru.AgentForSeat(1); got != nil {
		t.Fatalf("unstarted (World nil) seat 1 with installed agent: got %p, want nil(未启动必须返回 nil 且不 panic)", got)
	}

	// ── 房间 S:已开局 —— 命中/同指针/非居民/空位/越界。
	ms, rs := newHandleTestRoom(t, "room-agent-handle-s", botUsers, botModels)
	humanSeat, _, e := rs.JoinGame("human-uid-handle", "human")
	if e != nil {
		t.Fatalf("JoinGame: %v", e)
	}
	if err := rs.Start(nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	// 已开局但 Agent 未装配 ⇒ nil。
	if got := rs.AgentForSeat(1); got != nil {
		t.Fatalf("started seat 1 before EnsureAgents: got %p, want nil", got)
	}
	ms.EnsureAgents(rs)
	ag1 := rs.AgentForSeat(1)
	if ag1 == nil {
		t.Fatal("bot seat 1: AgentForSeat = nil, want in-game agent")
	}
	// 同一指针(两次调用不新建、不换实例)。
	if ag2 := rs.AgentForSeat(1); ag2 != ag1 {
		t.Fatal("两次 AgentForSeat(1) 返回不同实例(只读句柄不得新建 Agent)")
	}
	// 与 r.agents 内既有实例同指针(月度决策共用)。
	rs.mu.Lock()
	inMap := rs.agents[1]
	rs.mu.Unlock()
	if inMap != ag1 {
		t.Fatal("AgentForSeat 返回值与 r.agents[1] 不是同一实例(令牌桶不共享)")
	}

	// 非居民(人类座位)/空位/越界 ⇒ nil。
	if got := rs.AgentForSeat(humanSeat); got != nil {
		t.Fatalf("human seat %d: got %p, want nil(非居民无 City-Human Agent)", humanSeat, got)
	}
	if got := rs.AgentForSeat(11); got != nil {
		t.Fatalf("empty seat 11: got %p, want nil", got)
	}
	if got := rs.AgentForSeat(-1); got != nil {
		t.Fatalf("seat -1: got %p, want nil", got)
	}
	if got := rs.AgentForSeat(MaxSeats); got != nil {
		t.Fatalf("seat %d: got %p, want nil", MaxSeats, got)
	}
}

// roomHandleDigest 房间 + 世界状态快照(只读比对用;StateHash 覆盖世界账目,
// transcript/座位字段覆盖房间面;agents 指针表覆盖生命周期不变)。
type roomHandleDigest struct {
	WorldHash     string
	WorldMonth    int
	Status        string
	Phase         string
	Paused        bool
	MonthMs       int
	ResidentCount int
	Seats         [MaxSeats]string
	SeatModelKeys [MaxSeats]string
	BotSeats      [MaxSeats]bool
	Nicknames     [MaxSeats]string
	Transcripts   [MaxSeats]BotTranscript
	AgentSeats    []int
	AgentPtrs     map[int]*vcplayer.Agent
}

// roomHandleDigestOf 在锁内采集一次状态摘要(World nil 安全 —— 未开局房间)。
func roomHandleDigestOf(r *VirtualCityRoom) roomHandleDigest {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := roomHandleDigest{
		WorldHash:     r.World.StateHash(),
		Status:        r.Status,
		Phase:         r.Phase,
		Paused:        r.Paused,
		MonthMs:       r.MonthMs,
		ResidentCount: r.ResidentCount,
		Seats:         r.Seats,
		SeatModelKeys: r.SeatModelKeys,
		BotSeats:      r.BotSeats,
		Nicknames:     r.Nicknames,
		Transcripts:   r.Transcripts,
		AgentPtrs:     make(map[int]*vcplayer.Agent, len(r.agents)),
	}
	for s, a := range r.agents {
		d.AgentSeats = append(d.AgentSeats, s)
		d.AgentPtrs[s] = a
	}
	sort.Ints(d.AgentSeats)
	if r.World != nil {
		d.WorldMonth = r.World.Month
	}
	return d
}

// TestAgentHandle_AgentForSeatReadOnly 批次85 §7.1:AgentForSeat 只读 ——
// 调用前后房间/世界状态逐字段相等(既有快照对比:StateHash + transcript +
// 房间字段 + agents 指针表),r.agents 生命周期不变。覆盖未开局(World nil)
// 与已开局两种状态。
func TestAgentHandle_AgentForSeatReadOnly(t *testing.T) {
	botUsers := make(map[int]string, 10)
	botModels := make(map[int]string, 10)
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = "bot-r" + string(rune('0'+seat))
		botModels[seat] = "ModelR" + string(rune('A'+seat))
	}
	m, r := newHandleTestRoom(t, "room-agent-handle-readonly", botUsers, botModels)
	// Start 先于 EnsureAgents:开局 wakeBots 时 r.agents 为空,世界已建立;
	// 随后装配游戏内 Agent,快照即覆盖 World + agents 全状态。
	if err := r.Start(nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	m.EnsureAgents(r)

	before := roomHandleDigestOf(r)
	// 覆盖命中/空位/越界/nil 房间全部分支。
	var nilRoom *VirtualCityRoom
	_ = nilRoom.AgentForSeat(0)
	for i := 0; i < 3; i++ {
		_ = r.AgentForSeat(1)
		_ = r.AgentForSeat(2)
		_ = r.AgentForSeat(0)
		_ = r.AgentForSeat(-1)
		_ = r.AgentForSeat(MaxSeats)
	}
	after := roomHandleDigestOf(r)

	if !reflect.DeepEqual(before, after) {
		t.Fatalf("AgentForSeat 改变了房间/世界状态(必须只读):\nbefore=%+v\nafter=%+v", before, after)
	}

	// 未开局房间(World nil):同样只读、nil 安全。
	m2, r2 := newHandleTestRoom(t, "room-agent-handle-readonly2",
		map[int]string{1: "bot-u1"}, map[int]string{1: "ModelU1"})
	m2.EnsureAgents(r2)
	before2 := roomHandleDigestOf(r2)
	_ = r2.AgentForSeat(1)
	_ = r2.AgentForSeat(5)
	after2 := roomHandleDigestOf(r2)
	if !reflect.DeepEqual(before2, after2) {
		t.Fatalf("未开局房间:AgentForSeat 改变了状态:\nbefore=%+v\nafter=%+v", before2, after2)
	}
}

// TestAgentHandle_SharedInstanceFeedsRoomLLMStats 批次85 §7.1(共享桶生效):
// 游戏内实例与月度决策共享同一令牌桶与 BindLLMCallHook —— 对话问答的 LLM
// 调用计入房间 LLM 统计(llmStats.seatCalls),且令牌桶容量语义(容量 2,
// 第 3 次调用节流)在共享实例上生效。装配层路径(main.go cityHumanAnswerer)
// 命中 AgentForSeat 后调用的就是本实例的 AnswerQuestion,故本断言即
// 「ask_agent → City-Human 对话消耗计入房间 LLM 统计」的机制证明。
func TestAgentHandle_SharedInstanceFeedsRoomLLMStats(t *testing.T) {
	prov := &handleTextProvider{reply: "街坊口吻回答"}
	m := NewManager(Config{MonthMs: 3000, AgentEnabled: true, AgentDecisionTimeoutSec: 5},
		handleTextRegistry{p: prov})
	r := m.CreateRoom("room-agent-handle-stats")
	botUsers := make(map[int]string, 10)
	botModels := make(map[int]string, 10)
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = "bot-s" + string(rune('0'+seat))
		botModels[seat] = "ModelS" + string(rune('A'+seat))
	}
	r.RegisterBotSeats(botUsers, botModels)
	// Start 先于 EnsureAgents:wakeBots 时 r.agents 为空,不抢跑月度决策
	// (确定性:令牌桶/统计计数不受后台 LLM 调用干扰)。Start 后 World 非
	// nil,AgentForSeat 方按 §3.3.1 契约放行句柄。
	if err := r.Start(nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	m.EnsureAgents(r)

	ag := r.AgentForSeat(0)
	if ag == nil {
		t.Fatal("AgentForSeat(0) = nil, want in-game agent")
	}
	// 与月度决策同一实例 ⇒ 共用令牌桶 + 共用 BindLLMCallHook。
	r.mu.Lock()
	same := ag == r.agents[0]
	r.mu.Unlock()
	if !same {
		t.Fatal("AgentForSeat 返回的不是 r.agents[0](桶/钩子不共享)")
	}

	r.llmStats.mu.Lock()
	before := r.llmStats.seatCalls[0]
	r.llmStats.mu.Unlock()

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		reply, err := ag.AnswerQuestion(ctx, vctypes.CardBrief{Name: "测试居民"}, "今天天气如何?")
		if err != nil {
			t.Fatalf("AnswerQuestion #%d: %v", i+1, err)
		}
		if reply != "街坊口吻回答" {
			t.Fatalf("AnswerQuestion #%d reply = %q, want 街坊口吻回答", i+1, reply)
		}
	}
	// 第 3 次:令牌桶(容量 2)已耗尽 ⇒ 节流错误(共享桶语义,绝不无限重试)。
	if _, err := ag.AnswerQuestion(ctx, vctypes.CardBrief{}, "再来一句"); err == nil {
		t.Fatal("第 3 次 AnswerQuestion 未被令牌桶节流(共享桶容量语义回归)")
	}

	r.llmStats.mu.Lock()
	after := r.llmStats.seatCalls[0]
	r.llmStats.mu.Unlock()
	if after-before != 2 {
		t.Fatalf("房间 LLM 统计 seatCalls 增量 = %d, want 2(BindLLMCallHook 未把对话消耗计入房间统计)", after-before)
	}
	if got := atomic.LoadInt32(&prov.calls); got != 2 {
		t.Fatalf("provider 调用 = %d, want 2(第 3 次应在发起前被节流)", got)
	}
}
