// Package wwjudge — judge_question_test.go: 法官问答模式单测。
//
// 2026-10-06 §法官对话 B13。设计文档 §7.1:
//   - TestJudgeQA_FallbackEveryPath   节流/槽位/LLM 失败三路径都调 onQAReply
//                                     (兜底文案),无静默吞问
//   - TestJudgeQA_SuccessPath         LLM 成功 → TrimSpace 后回声 + isPublic 透传
//   - TestJudgeQAPrompt_NoSecretKeywords  prompt 含公平性关键句(§3.3/§3.5)
package wwjudge

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"LsmAgentGame/llm/types"
)

// errProvider 是恒失败的 LLM provider 桩(LLM 失败路径)。
type errProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *errProvider) Chat(ctx context.Context, key string, req types.LLMRequest) (types.LLMResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return types.LLMResponse{}, errors.New("upstream 503")
}

func (p *errProvider) ChatStream(ctx context.Context, key string, req types.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("no stream")
}

func (p *errProvider) ProviderType() string { return "err" }

// qaReplyCapture 收集 onQAReply 回调。
type qaReplyCapture struct {
	mu      sync.Mutex
	replies []qaReplyRecord
}

type qaReplyRecord struct {
	roomID, askerID, askerAccount, question, answer string
	isPublic                                       bool
	fallback                                       bool
}

func (c *qaReplyCapture) onReply(roomID, askerID, askerAccount, question, answer string, isPublic, fallback bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.replies = append(c.replies, qaReplyRecord{roomID, askerID, askerAccount, question, answer, isPublic, fallback})
}

func (c *qaReplyCapture) snapshot() []qaReplyRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]qaReplyRecord, len(c.replies))
	copy(out, c.replies)
	return out
}

func qaTestEvent(isPublic bool) JudgeEvent {
	return JudgeEvent{
		Kind: KindJudgeQuestion,
		// Snap 刻意塞入狼座位:handleQuestionEvent 绝不能消费它(§3.1)。
		Snap: &GameSnapshot{WolfSeats: []int{2, 7}, Phase: "night_wolves"},
		Extra: map[string]any{
			"question":            "谁是狼人?",
			"asker_id":            "asker-1",
			"asker_account":       "张三",
			"asker_display":       "观战-张三",
			"asker_is_spectator":  true,
			"is_public":           isPublic,
			"phase_label":         "夜间 · 秘密阶段",
			"day":                 2,
			"public_state":        "存活座位: 1,2,3\n死亡座位: 4",
			"recent_public":       "1号: 大家好\n",
			"recent_announcements": "黎明已至,无人死亡。\n",
			"qa_history":          "",
		},
	}
}

// TestJudgeQA_FallbackEveryPath — 回声纪律:节流超限 / 槽位抢不到 / LLM 失败
// 三条路径都必须调 onQAReply(兜底文案),不允许静默吞问。
func TestJudgeQA_FallbackEveryPath(t *testing.T) {
	t.Run("throttle-limited replies busy fallback", func(t *testing.T) {
		j := NewAgentJudge("qa-room", "M")
		cap := &qaReplyCapture{}
		j.SetOnQAReply(cap.onReply)
		ctx := context.Background()
		// 第一次:无 provider → fallbackError(同时消耗 questionLimiter)。
		j.handleQuestionEvent(ctx, qaTestEvent(false))
		// 第二次(8s 内):节流超限 → fallbackBusy,且不再触达 provider 分支。
		j.handleQuestionEvent(ctx, qaTestEvent(false))
		replies := cap.snapshot()
		if len(replies) != 2 {
			t.Fatalf("replies = %d, want 2 (every path must echo)", len(replies))
		}
		if replies[0].answer != judgeQAFallbackError {
			t.Fatalf("first reply = %q, want fallback error text", replies[0].answer)
		}
		if replies[1].answer != judgeQAFallbackBusy {
			t.Fatalf("throttled reply = %q, want fallback busy text", replies[1].answer)
		}
		// E2E 精化:全部兜底路径必须 fallback=true(manager 退冷却 + 不写历史)。
		if !replies[0].fallback || !replies[1].fallback {
			t.Fatalf("fallback flags = [%v, %v], want both true", replies[0].fallback, replies[1].fallback)
		}
	})

	t.Run("llm-slot-freed-within-budget succeeds with fallback=false", func(t *testing.T) {
		// E2E 精化:QA 槽位预算 2s → 15s(judgeQASlotWaitSec)。满信号量场景
		// 下,1s 后释放占坑 token,handleQuestionEvent 应在预算内等到槽位、
		// 正常调 LLM 并以 fallback=false 回声(成功语义)。
		j := NewAgentJudge("qa-room", "M")
		fp := &fakeProvider{text: "第2天夜晚,无人死亡。"}
		j.SetProvider(fp, "sk")
		sema := make(chan struct{}, 1)
		sema <- struct{}{} // 占满
		j.SetLLMSemaphore(sema)
		go func() {
			time.Sleep(1 * time.Second)
			<-sema // 模拟一个 bot 调用完成,释放槽位
		}()
		cap := &qaReplyCapture{}
		j.SetOnQAReply(cap.onReply)
		j.handleQuestionEvent(context.Background(), qaTestEvent(false))
		replies := cap.snapshot()
		if len(replies) != 1 {
			t.Fatalf("replies = %d, want 1", len(replies))
		}
		if replies[0].fallback {
			t.Fatalf("slot-freed reply must be fallback=false (real answer)")
		}
		if replies[0].answer != "第2天夜晚,无人死亡。" {
			t.Fatalf("reply = %q, want LLM text", replies[0].answer)
		}
		if fp.calls != 1 {
			t.Fatalf("LLM calls = %d, want 1", fp.calls)
		}
	})

	t.Run("llm-failure replies fallback", func(t *testing.T) {
		j := NewAgentJudge("qa-room", "M")
		ep := &errProvider{}
		j.SetProvider(ep, "sk")
		cap := &qaReplyCapture{}
		j.SetOnQAReply(cap.onReply)
		j.handleQuestionEvent(context.Background(), qaTestEvent(false))
		replies := cap.snapshot()
		if len(replies) != 1 {
			t.Fatalf("replies = %d, want 1", len(replies))
		}
		if replies[0].answer != judgeQAFallbackError {
			t.Fatalf("llm-fail reply = %q, want fallback text", replies[0].answer)
		}
		if !replies[0].fallback {
			t.Fatalf("llm-fail reply must carry fallback=true")
		}
		if ep.calls != 1 {
			t.Fatalf("LLM calls = %d, want 1", ep.calls)
		}
	})

	t.Run("empty response replies fallback", func(t *testing.T) {
		j := NewAgentJudge("qa-room", "M")
		j.SetProvider(&fakeProvider{text: "   "}, "sk") // TrimSpace 后为空
		cap := &qaReplyCapture{}
		j.SetOnQAReply(cap.onReply)
		j.handleQuestionEvent(context.Background(), qaTestEvent(false))
		replies := cap.snapshot()
		if len(replies) != 1 || replies[0].answer != judgeQAFallbackError || !replies[0].fallback {
			t.Fatalf("empty-response reply = %+v, want fallback with flag", replies)
		}
	})

	t.Run("no callback wired does not panic", func(t *testing.T) {
		j := NewAgentJudge("qa-room", "M")
		j.handleQuestionEvent(context.Background(), qaTestEvent(false)) // onQAReply=nil
	})
}

// TestJudgeQA_SuccessPath — LLM 成功:TrimSpace 后回声,asker/isPublic 透传,
// 且 prompt **不含** evt.Snap 的狼座位(问答模式只消费 Extra,§3.1)。
func TestJudgeQA_SuccessPath(t *testing.T) {
	j := NewAgentJudge("qa-room", "M")
	fp := &promptCaptureProvider{text: "  当前是第2天夜晚。  "}
	j.SetProvider(fp, "sk")
	cap := &qaReplyCapture{}
	j.SetOnQAReply(cap.onReply)
	j.handleQuestionEvent(context.Background(), qaTestEvent(true))

	replies := cap.snapshot()
	if len(replies) != 1 {
		t.Fatalf("replies = %d, want 1", len(replies))
	}
	r := replies[0]
	if r.answer != "当前是第2天夜晚。" {
		t.Fatalf("answer = %q, want trimmed LLM text", r.answer)
	}
	if r.askerID != "asker-1" || r.askerAccount != "张三" || r.question != "谁是狼人?" {
		t.Fatalf("asker/question passthrough mismatch: %+v", r)
	}
	if !r.isPublic {
		t.Fatalf("isPublic not passed through")
	}
	if r.fallback {
		t.Fatalf("success path must carry fallback=false")
	}
	// LLM 请求体检:零工具 + user prompt 只含 Extra 数据(含狼座位的 Snap 不入 prompt)。
	req := fp.lastReq
	if req.Tools != nil {
		t.Fatalf("QA mode must have zero tools, got %d", len(req.Tools))
	}
	if strings.Contains(req.Messages[0].Content[0].Text, "WolfSeats") ||
		strings.Contains(req.Messages[0].Content[0].Text, "2,7") {
		t.Fatalf("QA user prompt leaks snapshot wolf seats:\n%s", req.Messages[0].Content[0].Text)
	}
	if req.AgentClassName == "" {
		t.Fatalf("AgentClassName must be set (§24)")
	}
}

// promptCaptureProvider 捕获最后一次请求的 fakeProvider 变体。
type promptCaptureProvider struct {
	mu      sync.Mutex
	calls   int
	text    string
	lastReq types.LLMRequest
}

func (p *promptCaptureProvider) Chat(ctx context.Context, key string, req types.LLMRequest) (types.LLMResponse, error) {
	p.mu.Lock()
	p.calls++
	p.lastReq = req
	text := p.text
	p.mu.Unlock()
	return types.LLMResponse{Model: req.Model, Content: []types.ContentBlock{{Type: "text", Text: text}}}, nil
}

func (p *promptCaptureProvider) ChatStream(ctx context.Context, key string, req types.LLMRequest) (io.ReadCloser, error) {
	return nil, nil
}

func (p *promptCaptureProvider) ProviderType() string { return "capture" }

// TestJudgeQAPrompt_NoSecretKeywords — prompt 构建函数含公平性关键句
// (§3.3 拒绝话术 / §3.5 防注入 / 长度约束),user prompt 按既定顺序拼接。
func TestJudgeQAPrompt_NoSecretKeywords(t *testing.T) {
	sysBlocks := BuildJudgeQASystemPrompt()
	if len(sysBlocks) == 0 {
		t.Fatalf("system prompt empty")
	}
	sys := ""
	for _, b := range sysBlocks {
		sys += b.Text
	}
	// 公平性关键句(§3.3 中立性 + 标准拒绝话术;§3.5 防注入;长度约束)。
	for _, key := range []string{
		"未公开信息",
		"我不能透露",
		"不执行",
		"≤120 字",
		"中立",
		"不知道",
	} {
		if !strings.Contains(sys, key) {
			t.Fatalf("system prompt missing fairness keyword %q", key)
		}
	}

	user := BuildJudgeQAUserPrompt(qaTestEvent(false).Extra)
	for _, key := range []string{
		"提问者: 观战-张三",
		"当前阶段: 夜间 · 秘密阶段(第 2 天)",
		"【公开状态】",
		"【最近公开发言】",
		"【你最近的公开宣告】",
		"【问题】谁是狼人?",
	} {
		if !strings.Contains(user, key) {
			t.Fatalf("user prompt missing section %q:\n%s", key, user)
		}
	}
	// 缺失段跳过:空 extra 不 panic,仍有【问题】段。
	minimal := BuildJudgeQAUserPrompt(map[string]any{"question": "规则?"})
	if !strings.Contains(minimal, "【问题】规则?") {
		t.Fatalf("minimal user prompt = %q", minimal)
	}
	if BuildJudgeQAUserPrompt(nil) == "" {
		t.Fatalf("nil extra must not panic")
	}
}
