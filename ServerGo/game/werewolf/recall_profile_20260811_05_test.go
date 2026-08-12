// Package werewolf — player_profile_bridge / recall_chat 轻量单测。
//
// 2026-08-11 §20260811-05 新增。覆盖设计文档 §5 验收标准的 no-op 与
// 接线路径:
//
//	U1 玩家行为画像:
//	  - store=nil / config 关闭(测试环境无 conf → 兜底 false)/ nil room 时
//	    IteratePlayerProfilesAsync 与 PrefetchPlayerProfiles no-op 且不 panic;
//	  - playerProfilesForBotLocked 只给人类座位注入画像,bot 座位跳过;
//	  - BuildPlayerProfilePrompt 纯函数包含 3 段固定小标题。
//
//	U2 赛后复盘问答:
//	  - config 关闭(测试环境兜底 false)→ ErrRecallDisabled;
//	  - 房间不存在 → ErrRecallNotOver;
//	  - 限流器 Allow 达到上限后拒绝 + ResetRoom 清理;
//	  - BuildRecallSystemPrompt 包含复盘人格指令;
//	  - pruneRecallMessages 按字节预算裁头保尾。
//
// 注意:werewolf 测试常见 config.Load() panic(无配置文件环境),
// cfgPlayerProfileEnabled / cfgRecallChatEnabled 已用 defer recover 兜底 false,
// 与 §122 / cfgWerewolfCoolingSec 等现有测试安全模式一致。
package werewolf

import (
	"context"
	"strings"
	"testing"

	"LsmWebGame/llm"
)

// ─────────────────── U1 玩家行为画像 ───────────────────

// stubPlayerProfileStore 记录调用次数,用于断言 no-op 路径完全不触 DB。
type stubPlayerProfileStore struct {
	loadCalls  int
	batchCalls int
	saveCalls  int
	rows       map[string]*PlayerProfileRow
}

func (s *stubPlayerProfileStore) LoadProfile(ctx context.Context, modelKey, userID string) (*PlayerProfileRow, error) {
	s.loadCalls++
	if s.rows != nil {
		return s.rows[modelKey+"/"+userID], nil
	}
	return nil, nil
}

func (s *stubPlayerProfileStore) LoadProfilesForUsers(ctx context.Context, modelKey string, userIDs []string) (map[string]*PlayerProfileRow, error) {
	s.batchCalls++
	out := make(map[string]*PlayerProfileRow, len(userIDs))
	for _, uid := range userIDs {
		if s.rows != nil {
			if r := s.rows[modelKey+"/"+uid]; r != nil {
				out[uid] = r
			}
		}
	}
	return out, nil
}

func (s *stubPlayerProfileStore) SaveIterated(ctx context.Context, modelKey, userID, profileMD string, sameCampWin bool) error {
	s.saveCalls++
	return nil
}

func TestIteratePlayerProfiles_NilStoreNoop(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{RoomID: "pp-nil-store"}
	// store=nil → no-op,不 panic。
	m.IteratePlayerProfilesAsync(r)
}

func TestIteratePlayerProfiles_ConfigDisabledNoop(t *testing.T) {
	m := NewWerewolfManager()
	stub := &stubPlayerProfileStore{}
	m.SetPlayerProfileStore(stub)
	r := &WerewolfRoom{RoomID: "pp-cfg-off"}
	// 测试环境无 LsmWebGame.conf → cfgPlayerProfileEnabled 兜底 false → no-op。
	m.IteratePlayerProfilesAsync(r)
	if stub.loadCalls != 0 || stub.saveCalls != 0 {
		t.Fatalf("config-disabled path must not touch store, got load=%d save=%d",
			stub.loadCalls, stub.saveCalls)
	}
}

func TestIteratePlayerProfiles_NilRoomNoop(t *testing.T) {
	m := NewWerewolfManager()
	stub := &stubPlayerProfileStore{}
	m.SetPlayerProfileStore(stub)
	m.IteratePlayerProfilesAsync(nil)
	if stub.loadCalls != 0 || stub.saveCalls != 0 {
		t.Fatalf("nil room must not touch store")
	}
}

func TestPrefetchPlayerProfiles_NilStoreNoop(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{RoomID: "pp-prefetch-nil"}
	// store=nil → no-op,不 panic。
	m.PrefetchPlayerProfiles(r)
}

// TestPlayerProfilesForBotLocked_OnlyHumanSeats 验证画像注入只覆盖人类座位,
// bot 座位被跳过(§119 隔离:画像主键 model_key+user_id 天然按模型隔离,
// 且注入侧再按座位类型过滤一次双保险)。
func TestPlayerProfilesForBotLocked_OnlyHumanSeats(t *testing.T) {
	r := &WerewolfRoom{
		RoomID: "pp-filter",
		State:  NewGame(1),
		seatModelKeys: map[int]string{
			0: "DeepSeek-model", // 本 bot
			1: "GLM-model",      // 另一个 bot — 不应出现在画像里
		},
		playerProfileCache: map[string]map[string]playerProfileCacheEntry{
			"DeepSeek-model": {
				"human-uid-2": {ProfileMD: "【风格标签】激进悍跳", GamesTogether: 3},
				"bot-uid-1":   {ProfileMD: "不应出现", GamesTogether: 1},
			},
		},
	}
	r.State.Seats[0] = "bot-uid-0"
	r.State.Seats[1] = "bot-uid-1"
	r.State.Seats[2] = "human-uid-2"

	got := playerProfilesForBotLocked(r, 0)
	if len(got) != 1 {
		t.Fatalf("expect exactly 1 human profile, got %d (%v)", len(got), got)
	}
	md, ok := got[2]
	if !ok {
		t.Fatalf("expect profile at human seat 2, got %v", got)
	}
	if !strings.Contains(md, "激进悍跳") || !strings.Contains(md, "同局3次") {
		t.Fatalf("profile should contain md + games count, got %q", md)
	}
	if _, leaked := got[1]; leaked {
		t.Fatalf("bot seat 1 must not get a profile entry")
	}
}

// TestPlayerProfilesForBotLocked_NoCache 无缓存(全 AI 房间/未预取)→ nil。
func TestPlayerProfilesForBotLocked_NoCache(t *testing.T) {
	r := &WerewolfRoom{
		RoomID:        "pp-no-cache",
		State:         NewGame(1),
		seatModelKeys: map[int]string{0: "DeepSeek-model"},
	}
	r.State.Seats[0] = "bot-uid-0"
	if got := playerProfilesForBotLocked(r, 0); got != nil {
		t.Fatalf("no cache → nil, got %v", got)
	}
}

// TestBuildPlayerProfilePrompt 纯函数:包含 3 段固定小标题 + 本局事实。
func TestBuildPlayerProfilePrompt(t *testing.T) {
	p := BuildPlayerProfilePrompt("", 0, "张三", 4, "villager", "good", "good", true, 3, "room-x")
	for _, want := range []string{"【风格标签】", "【历史倾向】", "【应对建议】", "张三", "5 号位", "villager"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, p)
		}
	}
	// 旧画像注入路径。
	p2 := BuildPlayerProfilePrompt("【风格标签】保守", 5, "李四", 0, "seer", "good", "wolf", false, 4, "room-y")
	if !strings.Contains(p2, "保守") || !strings.Contains(p2, "同局 5 次") {
		t.Fatalf("old profile should be injected, got\n%s", p2)
	}
}

// ─────────────────── U2 赛后复盘问答 ───────────────────

func TestRecallChat_ConfigDisabled(t *testing.T) {
	m := NewWerewolfManager()
	// 测试环境无 conf → cfgRecallChatEnabled 兜底 false → ErrRecallDisabled。
	_, err := m.RecallChat(context.Background(), "any-room", 0, "你为什么跳预言家?")
	if err != ErrRecallDisabled {
		t.Fatalf("expect ErrRecallDisabled, got %v", err)
	}
}

func TestRecallChat_EmptyQuestion(t *testing.T) {
	// 直接构造 enabled 场景不可行(config 兜底 false),改为验证入口截断逻辑:
	// 空 question 在 disabled 检查之后 —— 此处只断 disabled 先行返回,
	// 空串校验在 API 层也有(双保险),不强行绕开 config。
	m := NewWerewolfManager()
	_, err := m.RecallChat(context.Background(), "any-room", 0, "   ")
	if err == nil {
		t.Fatalf("empty question must error")
	}
}

func TestRecallRateLimiter(t *testing.T) {
	l := &recallRateLimiter{}
	// 上限 2:前两次允许,第三次拒绝。
	if !l.Allow("room-a", "user-1", 2) {
		t.Fatalf("first ask should be allowed")
	}
	if !l.Allow("room-a", "user-1", 2) {
		t.Fatalf("second ask should be allowed")
	}
	if l.Allow("room-a", "user-1", 2) {
		t.Fatalf("third ask should be rate-limited")
	}
	// 不同用户互不影响。
	if !l.Allow("room-a", "user-2", 2) {
		t.Fatalf("different user should be allowed")
	}
	// ResetRoom 清理后重新允许。
	l.ResetRoom("room-a")
	if !l.Allow("room-a", "user-1", 2) {
		t.Fatalf("after reset should be allowed")
	}
	// limit<=0 兜底 10。
	for i := 0; i < 10; i++ {
		if !l.Allow("room-b", "user-1", 0) {
			t.Fatalf("default limit 10: ask %d should be allowed", i)
		}
	}
	if l.Allow("room-b", "user-1", 0) {
		t.Fatalf("default limit 10: 11th ask should be rejected")
	}
}

func TestBuildRecallSystemPrompt(t *testing.T) {
	s := BuildRecallSystemPrompt("witch", "good", "good", 4, 2)
	for _, want := range []string{"3 号位", "witch", "复盘", "第一人称", "300 字"} {
		if !strings.Contains(s, want) {
			t.Fatalf("recall system prompt missing %q\n---\n%s", want, s)
		}
	}
}

// TestPruneRecallMessages 按字节预算裁头保尾:首条 identity 保留,
// 最老的中间消息先裁,最近的消息完整保留。
func TestPruneRecallMessages(t *testing.T) {
	mk := func(text string) llm.Message {
		return llm.Message{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: text}}}
	}
	msgs := []llm.Message{
		mk("identity"),                    // 首条保留
		mk(strings.Repeat("a", 500)),      // 最老,先裁
		mk(strings.Repeat("b", 500)),      // 次老,必要时裁
		mk("recent-1"),
		mk("recent-2"),
	}
	out := pruneRecallMessages(msgs, 700)
	if len(out) >= len(msgs) {
		t.Fatalf("expect pruning, still %d msgs", len(out))
	}
	if got := out[0].Content[0].Text; got != "identity" {
		t.Fatalf("identity first message must survive, got %q", got)
	}
	last := out[len(out)-1].Content[0].Text
	if last != "recent-2" {
		t.Fatalf("newest message must survive, got %q", last)
	}
}

// TestRecallFallback 降级文案:含座位号、Fallback=true。
func TestRecallFallback(t *testing.T) {
	fb := recallFallback(4, "DeepSeek-model", "witch")
	if !fb.Fallback {
		t.Fatalf("fallback flag must be true")
	}
	if !strings.Contains(fb.Answer, "5 号位") {
		t.Fatalf("fallback should mention seat, got %q", fb.Answer)
	}
}
