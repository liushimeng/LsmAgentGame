// Package agent — judge_test.go: 主持人 Agent(法官)单元测试。
//
// 2026-07-16 主持人重构。覆盖三个阻塞缺陷的修复:
//   - 🔴1 Provider 注入:SetProvider 后 judgeChatOrFallback 真正走进 LLM 路径;
//   - D/F  活动流 + 广播回调:DispatchJudgeTool 成功后 appendActivity + 调 onAnnounce;
//   - 🟡3 秘密阶段静默(映射表测试见 game/werewolf 包)。
package wwjudge

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"LsmWebGame/llm"
	"LsmWebGame/llm/types"
)

// fakeProvider 是 llm.LLMProvider 的最小替身,用于验证 Provider 注入后
// judgeChatOrFallback 真正走进 LLM 路径。返回纯 text 响应(无 tool_use),走
// recordAnnouncement 的 "llm_text" 分支。
type fakeProvider struct {
	mu    sync.Mutex
	calls int
	text  string
}

func (p *fakeProvider) Chat(ctx context.Context, key string, req types.LLMRequest) (types.LLMResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return types.LLMResponse{
		Model:   req.Model,
		Content: []types.ContentBlock{{Type: "text", Text: p.text}},
	}, nil
}

func (p *fakeProvider) ChatStream(ctx context.Context, key string, req types.LLMRequest) (io.ReadCloser, error) {
	return nil, nil
}

func (p *fakeProvider) ProviderType() string { return "fake" }

// TestJudge_ProviderInjection 验证缺陷 🔴1 修复:NewAgentJudge 后必须经
// SetProvider 注入,否则 judgeChatOrFallback 入口守卫永远成立(旁白永不调 LLM)。
func TestJudge_ProviderInjection(t *testing.T) {
	j := NewAgentJudge("r1", "model-x")

	// 未注入前:Provider/apiKey 为空。
	if j.Provider != nil || j.apiKey != "" {
		t.Fatalf("fresh judge should have nil provider/empty key; got provider=%v key=%q", j.Provider, j.apiKey)
	}

	// 注入后:provider + apiKey 均非空(setter 可重复调用,幂等)。
	var prov llm.LLMProvider = &fakeProvider{text: "黎明已至,请查看昨夜伤亡。"}
	j.SetProvider(prov, "sk-judge-123")
	// 重复注入可覆盖(幂等),不 panic。
	j.SetProvider(prov, "sk-judge-456")
}

// TestJudge_ChatPathAfterInjection 验证注入 Provider 后 judgeChatOrFallback
// 真正走进 LLM 路径(recordAnnouncement 的 "llm_text" 分支),而非走 fallback。
func TestJudge_ChatPathAfterInjection(t *testing.T) {
	j := NewAgentJudge("r1", "model-x")
	fp := &fakeProvider{text: "黎明已至,请查看昨夜伤亡。"}
	// 注入真实形状的 provider:fp 实现了 llm.LLMProvider。
	var prov llm.LLMProvider = fp
	j.SetProvider(prov, "sk-judge-123")

	// 绕过 handleEvent 的限流 + quarantine 分支,直接调 judgeChatOrFallback。
	ok := j.judgeChatOrFallback(context.Background(), JudgePendingDawnAnnounce, JudgeEvent{Kind: JudgePendingDawnAnnounce})
	if !ok {
		t.Fatal("judgeChatOrFallback should return true when provider injected + fake returns text")
	}
	if fp.calls != 1 {
		t.Fatalf("fake provider Chat calls = %d, want 1 (LLM path should be invoked)", fp.calls)
	}
	// 读 transcript:recordAnnouncement("...", "llm_text") 写入 LastTool="llm_text"。
	tr := j.JudgeTranscript()
	if tr.LastTool != "llm_text" {
		t.Errorf("LastTool = %q, want llm_text (LLM path marker)", tr.LastTool)
	}
	if tr.LastAnnouncement != fp.text {
		t.Errorf("LastAnnouncement = %q, want %q", tr.LastAnnouncement, fp.text)
	}
}

// TestJudge_NoProviderFallback 验证未注入 Provider 时 judgeChatOrFallback 返回
// false(走 fallback),这是缺陷 🔴1 修复前的「永远 fallback」行为。
func TestJudge_NoProviderFallback(t *testing.T) {
	j := NewAgentJudge("r1", "model-x")
	ok := j.judgeChatOrFallback(context.Background(), JudgePendingDawnAnnounce, JudgeEvent{})
	if ok {
		t.Fatal("without provider, judgeChatOrFallback should return false (fallback)")
	}
}

// TestJudge_ActivitiesAppended 验证缺陷 D 修复:DispatchJudgeTool 成功后
// appendActivity 追加「一举一动」记录(超 30 条队首淘汰)。
func TestJudge_ActivitiesAppended(t *testing.T) {
	j := NewAgentJudge("r1", "model-x")
	var prov llm.LLMProvider = &fakeProvider{}
	j.SetProvider(prov, "k")

	tr := &JudgeTranscript{}
	// announce 成功 → 1 条 activity(追加到 j.transcript,经 JudgeTranscript() 读)。
	DispatchJudgeTool("announce", map[string]any{"kind": "judge_dawn_announce", "text": "黎明已至"}, j, tr)
	if got := len(j.JudgeTranscript().Activities); got != 1 {
		t.Fatalf("announce: activities = %d, want 1", got)
	}
	if j.JudgeTranscript().Activities[0].Tool != "announce" {
		t.Errorf("activity tool = %q, want announce", j.JudgeTranscript().Activities[0].Tool)
	}
	// idle_silent → 又 1 条。
	DispatchJudgeTool("idle_silent", map[string]any{"reason": "夜间观察"}, j, tr)
	if got := len(j.JudgeTranscript().Activities); got != 2 {
		t.Fatalf("idle_silent: activities = %d, want 2", got)
	}

	// 验证 30 条上限:一次性追加 35 条 → 保留最近 30。
	j2 := NewAgentJudge("r2", "model-x")
	j2.SetProvider(prov, "k")
	for i := 0; i < 35; i++ {
		DispatchJudgeTool("announce", map[string]any{"text": "x"}, j2, &JudgeTranscript{})
	}
	if got := len(j2.JudgeTranscript().Activities); got != 30 {
		t.Fatalf("cap: activities = %d, want 30", got)
	}
}

// TestJudge_ActivityInputTruncated 验证活动流输入摘要截到 ≤120 字符。
func TestJudge_ActivityInputTruncated(t *testing.T) {
	j := NewAgentJudge("r1", "model-x")
	var prov llm.LLMProvider = &fakeProvider{}
	j.SetProvider(prov, "k")
	tr := &JudgeTranscript{}
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	DispatchJudgeTool("announce", map[string]any{"text": string(long)}, j, tr)
	acts := j.JudgeTranscript().Activities
	if len(acts) != 1 {
		t.Fatalf("activities = %d, want 1", len(acts))
	}
	if len([]rune(acts[0].Input)) > 121 { // 120 + "…"
		t.Errorf("input not truncated: %d runes", len([]rune(acts[0].Input)))
	}
}

// TestJudge_OnAnnounceBroadcast 验证缺陷 F 修复:announce/declare_cause 成功后
// 调 onAnnounce 回调送公屏(nil 回调不 panic)。
func TestJudge_OnAnnounceBroadcast(t *testing.T) {
	j := NewAgentJudge("r1", "model-x")
	var prov llm.LLMProvider = &fakeProvider{}
	j.SetProvider(prov, "k")

	// nil 回调:不 panic。
	tr := &JudgeTranscript{}
	DispatchJudgeTool("announce", map[string]any{"text": "欢迎"}, j, tr)

	// 注入回调:验证 announce 与 declare_cause 触发,idle_silent 不触发。
	var calls []string
	j.SetOnAnnounceBroadcast(func(roomID, text, kind string) {
		calls = append(calls, kind)
	})
	DispatchJudgeTool("announce", map[string]any{"kind": "judge_pre_wolves", "text": "天黑请闭眼"}, j, tr)
	DispatchJudgeTool("declare_cause", map[string]any{"verdict": "death", "cause": "wolf", "text": "3号死亡"}, j, tr)
	DispatchJudgeTool("idle_silent", map[string]any{"reason": "夜间"}, j, tr)

	if len(calls) != 2 {
		t.Fatalf("onAnnounce calls = %d, want 2; kinds=%v", len(calls), calls)
	}
	if calls[0] != "judge_pre_wolves" {
		t.Errorf("first call kind = %q, want judge_pre_wolves", calls[0])
	}
	if calls[1] != "declare_cause:death" {
		t.Errorf("second call kind = %q, want declare_cause:death", calls[1])
	}
}

// TestJudge_SetOnAnnounceNilSafe 验证 SetOnAnnounceBroadcast(nil) 后
// DispatchJudgeTool 不 panic。
func TestJudge_SetOnAnnounceNilSafe(t *testing.T) {
	j := NewAgentJudge("r1", "model-x")
	var prov llm.LLMProvider = &fakeProvider{}
	j.SetProvider(prov, "k")
	j.SetOnAnnounceBroadcast(nil)
	tr := &JudgeTranscript{}
	DispatchJudgeTool("announce", map[string]any{"text": "x"}, j, tr)
}

// TestJudge_BuildJudgeMetadataUserID_WireShape 验证法官 metadata.user_id
// 的 stringified JSON blob 形态与 §14.1 ClaudeCode 协议对齐:
//   - 以 `{"device_id":` 开头、以 `}` 结尾;
//   - device_id = "bot:room-<roomID>:role-judge";
//   - account_uuid = modelKey;
//   - session_id = "<roomID>:judge";
//   - 长度 ≤ 256(Anthropic API 上限)。
func TestJudge_BuildJudgeMetadataUserID_WireShape(t *testing.T) {
	cases := []struct {
		name     string
		roomID   string
		modelKey string
	}{
		{name: "normal", roomID: "room-abc-123", modelKey: "MeiTuan-model"},
		{name: "uuid_room", roomID: "ee4180da-06a6-46ad-9d27-49fbd991c967", modelKey: "MinMax-model"},
		{name: "empty_modelKey", roomID: "r1", modelKey: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildJudgeMetadataUserID(tc.roomID, tc.modelKey)
			if len(got) == 0 {
				t.Fatalf("empty blob")
			}
			if len(got) > 256 {
				t.Fatalf("blob len %d > 256", len(got))
			}
			if got[0] != '{' || got[len(got)-1] != '}' {
				t.Fatalf("missing outer braces: %q", got)
			}
			wantDevice := fmt.Sprintf("bot:room-%s:role-judge", tc.roomID)
			if !strings.Contains(got, wantDevice) {
				t.Fatalf("missing device_id %q in %q", wantDevice, got)
			}
			wantAccount := fmt.Sprintf("%q", tc.modelKey)
			if !strings.Contains(got, wantAccount) {
				t.Fatalf("missing account_uuid %s in %q", wantAccount, got)
			}
			wantSession := fmt.Sprintf("%q", tc.roomID+":judge")
			if !strings.Contains(got, wantSession) {
				t.Fatalf("missing session_id %s in %q", wantSession, got)
			}
		})
	}

	// 256 字符硬上限:超长 roomID 也会被截断到 ≤ 256。
	longRoom := strings.Repeat("a", 1024)
	blob := BuildJudgeMetadataUserID(longRoom, "X")
	if len(blob) > 256 {
		t.Fatalf("overlong blob len %d", len(blob))
	}
}
