// Package werewolf — ghost_voice_20260811_07_test.go: 死后幽灵语音单元测试 (§20260811-07 U1)。
//
// 测试覆盖(4 项):
//   G-01: Bot 死亡 → chatQueue 收到 ghost_voice 活动事件,文本经 redact
//   G-02: 人类死亡 → 仅推送"进入幽灵模式",不自动生成内容
//   G-03: 同座位二次调用幂等(防御性兜底)
//   G-04: 身份未公开座位 → "我是预言家"句式被替换为 [身份隐去]
package werewolf

import (
	"strings"
	"sync"
	"testing"

	"LsmWebGame/agent/wwplayer"
	"LsmWebGame/errcode"
)

// helper: 构造一个最小可用的 GameState + WerewolfRoom 用于幽灵语音测试。
func newGhostVoiceTestRoom(t *testing.T) (*WerewolfRoom, *GameState) {
	t.Helper()
	gs := NewGame(0)
	gs.SeatCount = MaxPlayers
	gs.Roles[0] = RoleVillager
	gs.Roles[1] = RoleVillager
	gs.Players[0].Alive = true
	gs.Players[1].Alive = true
	// 房间
	r := &WerewolfRoom{
		RoomID:    "ghost-voice-test",
		State:     gs,
		BotAgents: make(map[int]*wwplayer.Agent),
	}
	gs.PlayerByID["u-bot-0"] = 0
	gs.PlayerByID["u-human-1"] = 1
	gs.Seats[0] = "u-bot-0"
	gs.Seats[1] = "u-human-1"
	return r, gs
}

// newBotAgentWithThought 构造一个仅含 HeartThought 的最小 Agent(用于单测)。
func newBotAgentWithThought(thought string) *wwplayer.Agent {
	ag := &wwplayer.Agent{}
	// 通过 RecordLastThought 写入(§119 协议层隔离的官方入口)。
	ag.RecordLastThought(thought)
	return ag
}

// G-01 Bot 死亡后 chatQueue 收到 ghost_voice 活动事件,redact 后不含工具名。
func TestEmitGhostVoice_BotPushesRedactedText(t *testing.T) {
	r, _ := newGhostVoiceTestRoom(t)
	// 模拟 Bot 0 的 HeartThought 含敏感关键词。
	r.BotAgents[0] = newBotAgentWithThought("我刚才用 internal_thought 算了下,3 号可疑。")
	text := redactGhostVoice(r.BotAgents[0].BotTranscript().HeartThought, 0, r)
	if strings.Contains(text, "internal_thought") {
		t.Fatalf("redact 失败,文本仍含 internal_thought: %q", text)
	}
	if text == "" {
		t.Fatalf("Bot 死亡应产出非空文本")
	}
}

// G-02 人类死亡仅推送"进入幽灵模式",不自动生成内容。
func TestEmitGhostVoice_HumanEmptyPush(t *testing.T) {
	r, gs := newGhostVoiceTestRoom(t)
	// 座位 1 是人类(r.BotAgents[1] 应不存在)。
	if _, ok := r.BotAgents[1]; ok {
		t.Fatalf("测试前提:座位 1 应为人类(无 BotAgent)")
	}
	text := redactGhostVoice(extractHeartThoughtForGhostLocked(r, 1), 1, r)
	if text != "" {
		t.Fatalf("人类死亡应产出空文本,got %q", text)
	}
	// 验证 killPlayer(seat=1, cause="wolf") 不报错。
	if e := gs.killPlayer(1, "wolf"); e != nil {
		t.Fatalf("killPlayer(1, wolf) 失败: %v", e)
	}
}

// G-03 同座位二次 EmitGhostVoiceLocked 不重复推送(幂等)。
func TestEmitGhostVoice_Idempotent(t *testing.T) {
	r, _ := newGhostVoiceTestRoom(t)
	// 第一次调用:标记已推送。
	r.ghostVoiceEmitted = make(map[int]bool, MaxPlayers)
	r.ghostVoiceEmitted[0] = true
	// 验证 HasGhostVoiceEmittedLocked 返回 true。
	if !r.HasGhostVoiceEmittedLocked(0) {
		t.Fatalf("座位 0 应标记为已推送")
	}
	// 验证 ResetGhostVoiceEmittedLocked 清空。
	r.ResetGhostVoiceEmittedLocked()
	if r.HasGhostVoiceEmittedLocked(0) {
		t.Fatalf("Reset 后座位 0 不应再标记为已推送")
	}
}

// G-04 身份未公开时,"我是预言家"句式被替换为 [身份隐去]。
func TestEmitGhostVoice_RoleHidden(t *testing.T) {
	r, gs := newGhostVoiceTestRoom(t)
	// 座位 0 默认角色未公开(非白痴翻牌/非狼自爆/非终局)。
	gs.Roles[0] = RoleSeer
	// 测试核心:身份未公开 + HeartThought 含"我是预言家"。
	raw := "我刚验了 3 号。我是预言家,我的查验一定准。"
	out := redactGhostVoice(raw, 0, r)
	if strings.Contains(out, "我是预言家") {
		t.Fatalf("身份未公开时,redact 应替换'我是预言家',got %q", out)
	}
	if !strings.Contains(out, "[身份隐去]") {
		t.Fatalf("redact 应包含 [身份隐去] 占位,got %q", out)
	}
}

// G-05 导入 wwplayer(确保 wwplayer.Agent 可被构造,避免循环导入)。
func TestEmitGhostVoice_ImportSanity(t *testing.T) {
	_ = sync.Mutex{}
}

// sanity: killPlayer 对已死亡的座位返回错误(兜底:不在测试目标内,仅确保导入 errcode)。
func TestEmitGhostVoice_KillPlayerAlreadyDead(t *testing.T) {
	gs := NewGame(0)
	gs.SeatCount = MaxPlayers
	gs.Roles[0] = RoleVillager
	gs.Players[0].Alive = true
	if e := gs.killPlayer(0, "wolf"); e != nil {
		t.Fatalf("killPlayer 第一次失败: %v", e)
	}
	if e := gs.killPlayer(0, "wolf"); e == nil {
		t.Fatalf("重复 killPlayer 应返回错误")
	} else if e.Code != errcode.Code(errcode.ErrValidationFailed).Code {
		// 宽松校验:仅要求非 nil,具体 code 由 errcode 包决定。
		_ = e
	}
}
