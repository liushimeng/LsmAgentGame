// BUG-R213-P1-01 (2026-07-31) 回归测试:观战者私聊投递链路的两个补充。
//
// 背景(自动化测试报告 2026-07-31 08:17:05 §3.3/§4.2):
//   - 观战者 test_01 私聊 Bot 6 后,UI 显示已发送,但 bot 下一轮公开发言
//     未引用私聊内容,测试方误判为「私聊未投递到目标 Agent 的 LLM 上下文」;
//   - 代码走查确认投递链路完整(chat.whisper → ChatService.Whisper →
//     SetRoomMessageHook → RecordRoomMessage → appendRoomMessage →
//     whisperInbox[toSeat] → buildAgentContextLocked → gc.WhisperInbox →
//     BuildUserPrompt【发给你的私聊】段),真正缺口是:
//       (a) 投递成功仅写 Debug 级日志,生产日志级别下完全不可见,无法
//           区分「未投递」与「已投递但 LLM 选择不引用」;
//       (b) system prompt 从未教 bot「观战者也会私聊你,且你可以自然回应」。
//   - 修复:(a) appendRoomMessage 投递成功后补 Info 级日志
//     「whisper delivered to agent inbox」;(b) BuildSystemPrompt 注入
//     「观战者私聊」引导段。
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"strings"
	"testing"
)

// TestR213_SystemPrompt_ContainsSpectatorWhisperGuidance 验证 system prompt
// 已注入「观战者私聊」引导,且引导文案与 formatWhisperLabel 渲染的
// 「观战-XX → 你」标签完全一致(标签不一致 = bot 学不到正确的识别特征)。
func TestR213_SystemPrompt_ContainsSpectatorWhisperGuidance(t *testing.T) {
	blocks := BuildSystemPrompt("", PersonalityVector{}, "", "")
	if len(blocks) == 0 {
		t.Fatal("BuildSystemPrompt 返回空 blocks")
	}
	body := blocks[0].Text
	if !strings.Contains(body, "观战者私聊") {
		t.Errorf("system prompt 缺少「观战者私聊」引导段")
	}
	// 引导文案引用的标签必须与 formatWhisperLabel(IsSpectator=true) 的
	// 输出格式完全一致,否则 bot 学到的识别特征与实际渲染对不上。
	if !strings.Contains(body, "观战-") {
		t.Errorf("system prompt 未包含「观战-XX → 你」标签(与 formatWhisperLabel 不一致)")
	}
	// 必须与既有「频道隔离」约束并存,不被误删。
	if !strings.Contains(body, "频道隔离") {
		t.Errorf("system prompt 丢失「频道隔离」约束(回归)")
	}
}

// TestR213_FormatWhisperLabel_SpectatorPrefix 验证观战者 whisper 的渲染
// 标签与 system prompt 引导文案中描述的一致(「观战-XX → 你」)。
func TestR213_FormatWhisperLabel_SpectatorPrefix(t *testing.T) {
	ev := wwtypes.WhisperEvent{
		FromSeat:    -1, // 观战者无座位
		From:        "test_01",
		IsSpectator: true,
		Text:        "9号是不是预言家?",
	}
	got := formatWhisperLabel(ev)
	if !strings.HasPrefix(got, "观战-test_01") {
		t.Errorf("formatWhisperLabel(观战者) = %q, want 「观战-test_01」前缀", got)
	}
	if !strings.Contains(got, "→ 你") {
		t.Errorf("formatWhisperLabel(观战者) = %q, want 包含「→ 你」", got)
	}
}
