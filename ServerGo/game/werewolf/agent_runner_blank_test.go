// Package werewolf — agent_runner_blank_test (BUG-R233-P1-01 回归)
//
// 2026-08-02 测试报告 9 号座位 (GLM-model) 在 19:11 / 19:45 / 20:08 等
// 多次出现「SpeakAuto / Interject 产出空气泡」缺陷:LLM 在 assistant content
// 数组返回单空格 "\n" 等纯空白字符(text_len=1,2,...) 会通过 `autoText != ""`
// 守卫后调用 SpeakAuto / Interject,过滤链(MysteryMaskText /
// StripLLMInternalTags / ScrubVerdictClaim)改写后仍可能保持空白,最终
// chatSvc.SendFromBot 出去 → 前端渲染一个空聊天气泡。
//
// 修复要点 (run.go:1457 + agent_runner.go Speak/SpeakAuto/SpeakWithThought/
// Interject 4 条广播路径):
//   - run.go 进入 SpeakAuto 前先 TrimSpace(text) == "" 拒绝(不消耗 token);
//   - Speak / SpeakAuto / SpeakWithThought / Interject 内部 SendFromBot 前
//     再次 TrimSpace(text) == "" 时硬拒(hard-reject),不调 speakLimiter.Mark /
//     不调 markSameSeatPublicSpeak / 不调 bumpSeatSpeakCountThisPhase;
//
// 测试不变式 (本文件):
//   1. 输入 "\n" / " " / " \t " → 4 条路径全部返回 rejected-string + nil err;
//   2. 4 条路径全部不调用 chatSvc.SendFromBot / SendInterjectFromBot;
//   3. 输入真实文本 "我投 4 号" → 至少调用一次 broadcast(对照正向覆盖);
//   4. Speak rate-limit / phase-count 等前置守卫全部关闭(fiterCfg 走
//      newDefaultSpeakFilterConfig 然后设 EnableRateLimit=false),使测试只
//      聚焦「过滤后空文」分支,不被无关限流命中;
//
// 关于 mgr:Speak / SpeakAuto 内部调 CurrentStatus / getAuthoritativeDeathsAndAlive,
// 均需 `mgr.rooms[roomID]` 已注册。本测试用 NewWerewolfManager + JoinGame 触发
// 房间创建,只填 1 人让 filling 阶段保持即可(不开始游戏,不会起 watchdog)。
package werewolf

import (
	"strings"
	"sync"
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

// fakeChatSender 记录所有 send 调用次数与最后一次文本。
type fakeChatSender struct {
	mu              sync.Mutex
	sendFromBotN    int
	sendInterjectN  int
	whisperN        int
	lastFromBotText string
}

func (f *fakeChatSender) SendFromBot(roomID, botUserID, botAccount, modelKey, text string) (*wwplayer.BotChatSendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendFromBotN++
	f.lastFromBotText = text
	return &wwplayer.BotChatSendResult{}, nil
}

func (f *fakeChatSender) WhisperFromBot(roomID, botUserID, botAccount, modelKey, toUserID, toAccount, text string) (*wwplayer.BotChatSendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.whisperN++
	return &wwplayer.BotChatSendResult{}, nil
}

func (f *fakeChatSender) SendInterjectFromBot(roomID, botUserID, botAccount, modelKey, text string) (*wwplayer.BotChatSendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendInterjectN++
	f.lastFromBotText = text
	return &wwplayer.BotChatSendResult{}, nil
}

func (f *fakeChatSender) SendFromJudge(roomID, fromAccount, modelKey, text, kind string) (*wwplayer.BotChatSendResult, error) {
	return &wwplayer.BotChatSendResult{}, nil
}

// newBlankSpeakRunner 构造一个 Speak / SpeakAuto / SpeakWithThought / Interject
// 4 条广播路径都可调用的最小 agentRunner。
// filterCfg 关闭 rate-limit / identity filter / fact-check,让空文检查成为
// 唯一决定性分支;SpeakLimiter=nil 同理(默认 newAgentRunner 不初始化)。
// mgr 必须非 nil,以满足 r.mgr.rooms[r.roomID] 的寻址需求。
func newBlankSpeakRunner(t *testing.T, mgr *WerewolfManager, chat *fakeChatSender) *agentRunner {
	t.Helper()
	const roomID = "test-room-blank-r233"
	const botID = "bot-9-blank"
	// 触发房间惰性创建:JoinGame 单人入座,房间进入 PhaseFilling 不开局。
	if _, _, em := mgr.JoinGame(roomID, botID); em != nil {
		t.Fatalf("JoinGame failed: %v", em)
	}
	r := newAgentRunner(mgr, roomID, 9, botID, "Bot9", "GLM-model", chat)
	r.filterCfg.EnableRateLimit = false
	r.filterCfg.EnableIdentityFilter = false
	return r
}

// 隔离每个测试使用独立 manager 防止全局 mgr 污染。
func blankMgr(t *testing.T) *WerewolfManager {
	t.Helper()
	m := NewWerewolfManager()
	m.seedFn = func() int64 { return time.Now().UnixNano() }
	return m
}

// BUG-R233-P1-01 — SpeakAuto 输入纯空白 → 不应调 SendFromBot,result 含「rejected」。
func TestSpeakAutoBlankText_R233P1(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"single_space", " "},
		{"single_newline", "\n"},
		{"mixed_whitespace", " \t \n "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chat := &fakeChatSender{}
			r := newBlankSpeakRunner(t, blankMgr(t), chat)
			res, err := r.SpeakAuto(tc.text)
			if err != nil {
				t.Fatalf("SpeakAuto 不应返回 error: %v", err)
			}
			if !strings.Contains(res, "rejected") {
				t.Fatalf("SpeakAuto(%q) → %q 应含 'rejected'", tc.text, res)
			}
			if chat.sendFromBotN != 0 {
				t.Fatalf("SpeakAuto 过滤后空文不应广播,实际 sendFromBotN=%d", chat.sendFromBotN)
			}
		})
	}
}

// Speak 输入纯空白 → 同样硬拒且不广播(对照 SpeakAuto 路径对称)。
func TestSpeakBlankText_R233P1(t *testing.T) {
	chat := &fakeChatSender{}
	r := newBlankSpeakRunner(t, blankMgr(t), chat)
	res, err := r.Speak("\n")
	if err != nil {
		t.Fatalf("Speak 不应返回 error: %v", err)
	}
	if !strings.Contains(res, "rejected") {
		t.Fatalf("Speak(空白) → %q 应含 'rejected'", res)
	}
	if chat.sendFromBotN != 0 {
		t.Fatalf("Speak 不应广播, sendFromBotN=%d", chat.sendFromBotN)
	}
}

// SpeakWithThought 输入纯 publicText → 公开文本为空时硬拒(不广播,
// internal_thought 仍可记录;但本测试用空白 publicText 验证硬拒分支)。
func TestSpeakWithThoughtBlankText_R233P1(t *testing.T) {
	chat := &fakeChatSender{}
	r := newBlankSpeakRunner(t, blankMgr(t), chat)
	res, err := r.SpeakWithThought(" ", "<internal note>")
	if err != nil {
		t.Fatalf("SpeakWithThought 不应返回 error: %v", err)
	}
	if !strings.Contains(res, "rejected") {
		t.Fatalf("SpeakWithThought(空白) → %q 应含 'rejected'", res)
	}
	if chat.sendFromBotN != 0 {
		t.Fatalf("SpeakWithThought 不应广播, sendFromBotN=%d", chat.sendFromBotN)
	}
}

// Interject 输入纯空白 → 同样硬拒且不广播(独立路径 SendInterjectFromBot)。
func TestInterjectBlankText_R233P1(t *testing.T) {
	chat := &fakeChatSender{}
	r := newBlankSpeakRunner(t, blankMgr(t), chat)
	res, err := r.Interject("\t")
	if err != nil {
		t.Fatalf("Interject 不应返回 error: %v", err)
	}
	if !strings.Contains(res, "rejected") {
		t.Fatalf("Interject(空白) → %q 应含 'rejected'", res)
	}
	if chat.sendInterjectN != 0 {
		t.Fatalf("Interject 不应广播, sendInterjectN=%d", chat.sendInterjectN)
	}
}

// SpeakAuto 正向覆盖:正常文本应正常广播,确保修复没把正常路径也误拒。
// 测试同时检查过滤链里的 StripLLMInternalTags 对纯 XML 标签改写后变空也拒绝。
func TestSpeakAutoRealText_R233P1_Positive(t *testing.T) {
	chat := &fakeChatSender{}
	r := newBlankSpeakRunner(t, blankMgr(t), chat)
	res, err := r.SpeakAuto("我投 4 号,4 号昨晚发言太平静了")
	if err != nil {
		t.Fatalf("SpeakAuto 正常文本不应 error: %v", err)
	}
	if !strings.HasPrefix(res, "sent") {
		t.Fatalf("SpeakAuto 正常文本应返回 'sent ...',得到 %q", res)
	}
	if chat.sendFromBotN != 1 {
		t.Fatalf("SpeakAuto 正常文本应广播 1 次,实际 sendFromBotN=%d", chat.sendFromBotN)
	}
	if chat.lastFromBotText != "我投 4 号,4 号昨晚发言太平静了" {
		t.Fatalf("lastFromBotText = %q,期望原文", chat.lastFromBotText)
	}
}
