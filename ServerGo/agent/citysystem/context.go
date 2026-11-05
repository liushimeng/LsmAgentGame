// Package citysystem — context.go: 会话上下文存储(2026-11-03 §批次81;
// 2026-11-05 §批次84 增加「Agent 来信」队列)。
//
// 契约: 批次81 方案文档 §3.2「Context 管理」;
//       批次84 方案文档 §4.3.2「Agent 来信」。
//
// 键 = roomID + "/" + userID(每个用户在每个房间一条独立会话);
// 值 = []llmtypes.Message(**不含 system** —— system 每轮由
// BuildSystemBlocks() 重建,场景目录内联后恒定)。
//
// 裁剪策略:超过 MaxMessages 时保留**最近** N-10 条 + **前 10 条**
// (保底开头,避免丢失用户最初意图);不阻塞、不调 LLM 压缩(成本考量)。
// 摘要压缩 Compact 已实现接口 + 单测,**默认不触发**。
//
// 批次84 新增:每会话一份**独立来信队列**(环形 ≤20)存放其它 Agent 的
// 上行消息(IncomingMessage);不塞进 messages 历史(避免污染对话历史与
// 前端历史展示)。下一轮构造 system 提示词时 `TakeIncoming` 取最近 ≤5 条
// 注入「【来自其它 Agent 的消息】」段(时间倒序),取走即出队(同一条来信
// 只注入一次)。
package citysystem

import (
	"context"
	"fmt"
	"strings"
	"sync"

	llmtypes "LsmAgentGame/llm/types"
)

// compactMinMessages 是触发 Compact 的最小消息数(再少压缩无意义)。
const compactMinMessages = 4

// incomingRingCap 是单会话来信队列容量(环形;超出丢最旧)。
const incomingRingCap = 20

// incomingInjectMax 是单轮注入 system 的来信条数上限(契约 §4.3.2)。
const incomingInjectMax = 5

// incomingKey 是房间级来信队列的存储键。
//
// 来信是**房间级**(总线 room handler 收到的上行消息,不区分用户),故键 = 房间;
// 加 "/#incoming" 后缀与会话键 "roomID/userID" 明确区分(用户 ID 不会出现
// 这种形式)。房间内**共享收件箱**语义:任一用户开一轮即消费,同一条来信
// 只注入一次 system。
func incomingKey(roomID string) string { return roomID + "/#incoming" }

// ConversationStore 按 key 存会话消息(并发安全)。
type ConversationStore struct {
	mu            sync.RWMutex
	maxMessages   int
	conversations map[string][]llmtypes.Message
	// incoming 独立来信队列(key = 会话键;环形 ≤incomingRingCap)。
	incoming map[string][]IncomingMessage
}

// NewConversationStore 构造。maxMessages <= 0 时用 40(与 Config 缺省一致)。
func NewConversationStore(maxMessages int) *ConversationStore {
	if maxMessages <= 0 {
		maxMessages = 40
	}
	return &ConversationStore{
		maxMessages:   maxMessages,
		conversations: make(map[string][]llmtypes.Message),
		incoming:      make(map[string][]IncomingMessage),
	}
}

// AppendIncoming 把一条上行来信追加到 roomKey 的来信队列(环形 ≤20,
// 超出丢最旧)。并发安全。
func (s *ConversationStore) AppendIncoming(roomKey string, m IncomingMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := append(s.incoming[roomKey], m)
	if len(q) > incomingRingCap {
		q = q[len(q)-incomingRingCap:]
	}
	s.incoming[roomKey] = q
}

// TakeIncoming 取走 roomKey 来信队列里**最近** ≤max 条(时间倒序,即最新在前),
// 并从队列移除(同一条来信只注入一次 system)。队列为空返回 nil。
//
// max <= 0 时按 incomingInjectMax。
func (s *ConversationStore) TakeIncoming(roomKey string, max int) []IncomingMessage {
	if max <= 0 {
		max = incomingInjectMax
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.incoming[roomKey]
	if len(q) == 0 {
		return nil
	}
	if max > len(q) {
		max = len(q)
	}
	taken := make([]IncomingMessage, max)
	for i := 0; i < max; i++ {
		taken[i] = q[len(q)-1-i] // 时间倒序:最新在前
	}
	s.incoming[roomKey] = q[:len(q)-max]
	return taken
}

// PendingIncoming 返回 roomKey 来信队列当前条数(只读;诊断/测试用)。
func (s *ConversationStore) PendingIncoming(roomKey string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.incoming[roomKey])
}

// Get 返回副本(调用方随意修改,不污染存储)。
func (s *ConversationStore) Get(key string) []llmtypes.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := s.conversations[key]
	if len(msgs) == 0 {
		return nil
	}
	out := make([]llmtypes.Message, len(msgs))
	for i, m := range msgs {
		out[i] = llmtypes.Message{Role: m.Role, Content: append([]llmtypes.ContentBlock(nil), m.Content...)}
	}
	return out
}

// Append 追加消息并裁剪(保留最近 N-10 + 前 10)。
func (s *ConversationStore) Append(key string, msgs ...llmtypes.Message) {
	if len(msgs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.conversations[key]
	cur = append(cur, msgs...)
	s.conversations[key] = s.trimLocked(cur)
}

// Reset 清空指定会话(前端「清空对话」按钮)。
func (s *ConversationStore) Reset(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conversations, key)
}

// trimLocked 裁剪(调用方持锁):head = min(10, max/2) 条开头 + 最近 max-head 条。
//
// 切口**不拆散 tool_use/tool_result 对**(§14.1 配对纪律):
//   - 头部不以携带 tool_use 的 assistant 消息收尾(其 tool_result 在被丢弃的中段);
//   - 尾部不以携带 tool_result 的 user 消息开头(其 tool_use 在被丢弃的中段)。
//
// 调整后总条数 ≤ max(head 只减、尾起点只后移)。
func (s *ConversationStore) trimLocked(msgs []llmtypes.Message) []llmtypes.Message {
	max := s.maxMessages
	if max <= 0 || len(msgs) <= max {
		return msgs
	}
	head := max / 2
	if head > 10 {
		head = 10
	}
	for head > 0 && isToolUseMsg(msgs[head-1]) {
		head--
	}
	start := len(msgs) - (max - head)
	for start < len(msgs) && isToolResultMsg(msgs[start]) {
		start++
	}
	if start >= len(msgs) {
		start = len(msgs) - 1 // 兜底:至少保留最后一条(孤儿由出站闸口剔除)
	}
	out := make([]llmtypes.Message, 0, max)
	out = append(out, msgs[:head]...)
	out = append(out, msgs[start:]...)
	return out
}

// isToolUseMsg 报告消息是否携带 tool_use 块(悬空判定用)。
func isToolUseMsg(m llmtypes.Message) bool {
	if m.Role != "assistant" {
		return false
	}
	for _, b := range m.Content {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}

// isToolResultMsg 报告消息是否携带 tool_result 块(孤儿判定用)。
func isToolResultMsg(m llmtypes.Message) bool {
	if m.Role != "user" {
		return false
	}
	for _, b := range m.Content {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

// Compact 把最旧的一半交给 callLLM 压成一段中文摘要,作为一条 user 消息插回
// 会话头部(接口已就绪;**默认不触发**,见方案文档 §3.2)。
//
// 摘要消息形如:`【前文摘要】...`;压缩后 = [摘要] + 最新一半。
// callLLM 出错时返回 error 且**不改动**会话(下次可重试)。
func (s *ConversationStore) Compact(ctx context.Context, key string, callLLM func([]llmtypes.Message) (string, error)) error {
	if callLLM == nil {
		return fmt.Errorf("citysystem: compact callback is nil")
	}
	s.mu.RLock()
	msgs := s.conversations[key]
	s.mu.RUnlock()
	if len(msgs) < compactMinMessages {
		return nil
	}
	half := len(msgs) / 2
	oldest := make([]llmtypes.Message, half)
	copy(oldest, msgs[:half])
	summary, err := callLLM(oldest)
	if err != nil {
		return fmt.Errorf("citysystem: compact llm: %w", err)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return fmt.Errorf("citysystem: compact llm returned empty summary")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 期间会话可能已被追加,基于最新值重算最新一半。
	cur := s.conversations[key]
	if len(cur) < compactMinMessages {
		return nil
	}
	half = len(cur) / 2
	// 切口不拆散 tool_use/tool_result 对:rest 不以孤儿 tool_result 开头
	// (其 tool_use 已被摘要进前一半)。
	for half < len(cur) && isToolResultMsg(cur[half]) {
		half++
	}
	rest := make([]llmtypes.Message, len(cur)-half)
	copy(rest, cur[half:])
	out := make([]llmtypes.Message, 0, len(rest)+1)
	out = append(out, llmtypes.Message{
		Role:    "user",
		Content: []llmtypes.ContentBlock{{Type: "text", Text: "【前文摘要】" + summary}},
	})
	out = append(out, rest...)
	s.conversations[key] = out
	return nil
}
