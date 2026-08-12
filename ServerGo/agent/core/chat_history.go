// Package agent — chat_history.go: 房间共享的 500K 字节滚动聊天历史队列
// (2026-07-09 §13 增强,2026-07-09 §13-bugfix 改造:由 per-seat 改为 per-room 共享)。
//
// 设计动机:
//   - 现有 GameContext.RecentSpeeches 仅保留最近 50 条公开 chat,whisperInbox 仅 20 条;
//     长对局(超过 ~10 轮)后 Agent 失去早期投票/共识上下文。
//   - 观众私聊(whisper)与公开 chat 也应作为 Agent 决策上下文的一部分。
//   - 公平性: 7 个 Agent 必须看到同一份共享事件流(同一玩家、同一活动、同一 phase),
//     否则推理上下文不一致导致结论分歧。
//
// 架构(2026-07-09 §13-bugfix):
//   - 之前: 每 Agent 各自维护 500K 字节 ChatHistoryQueue,appendRoomMessage 给每个
//     alive bot 复制一份(whisper 给 sender+recipient 两份)。
//     问题: 7 bot × 500K = 3.5MB / 房间 + 锁顺序复杂 + 公平性靠 push 时机的 race-free 保障。
//   - 现在: 一个房间共享一个 ChatHistoryQueue(上限 500K),每 Agent 持一个 ReadPointer
//     (atomic 计数器,标识"这个 bot 上次消费到的 message 序号")。
//     优点: 内存只剩 500K / 房间; 推送逻辑只需 append 一次; LLM prompt 注入时按
//     pointer 取该 bot 之前的快照(尾部滑窗)→ 公平性由"序号一致 + 取景窗口一致"保证。
//
// 队列内容语义:
//   - 公开 chat (IsWhisper=false): append 一次,所有 bot 可见
//   - whisper (IsWhisper=true): append 一次,由 IsWhisper 字段决定 prompt 渲染时是否
//     仅 sender/recipient 能识别内容(私密消息即使在公共队列里也只对双方有意义)
//   - 活动事件(phase 切换、vote 结果等): append 一次,IsActivity=true
//   - 人类玩家发言: append 一次,IsSpectator 或正常 IsBot=false
//
// 压缩策略 (Compress, 按优先级):
//   1. 相邻同 sender 合并: 3 条连续同 FromID+IsBot+IsWhisper → 合并为 1 条
//   2. 超长消息截断: 单条 Size > 1KB → 截断为前 200 字 + "…(truncated)"
//   3. 最旧淘汰: bytes > capBytes → 从队首 pop,直到 bytes ≤ capBytes * 0.8
//   4. 摘要 fallback: 若压缩后仍 > 500K 且 len(messages) > 100 → 把最旧 30 条压成单条摘要
//
// size 估算: chatMsgSize(text) = utf8.RuneCountInString(text) * 4 (粗估 utf-8 字节上限)
//
// ReadPointer 语义:
//   - Append 后 seq 自增;每个 Agent 持 readSeq() = "上次发往 LLM 的最后序号"。
//   - WindowFor(seat) 默认返回"自 readSeq 之后"的所有新增消息(增量注入);
//     也支持 Tail(n int) → 取末尾 n 条用于重放 / 调试面板。
//   - Advance(seq) 在 Agent 拿到 LLM 调用结果后调用,把 readSeq 推到 seq,
//     下次 WindowFor 只返回更新的部分。
package agentcore

import (
	"fmt"
	"sync"
	"time"
	"unicode/utf8"
)

// ChatMessage 聊天历史单条记录(2026-07-09 §13 增强,2026-07-09 §13-bugfix 扩展 Seq)。
//
// 涵盖公开 chat + 私聊(whisper) + 房间活动事件(phase / vote / kill)。
// 上层(appendRoomMessage)负责按 isWhisper 推送到共享 ChatHistoryQueue;
// 每条消息由队列分配一个全局递增 Seq,Agent 通过 ReadPointer 跟踪已消费进度。
type ChatMessage struct {
	ID          string    `json:"id"`
	Seq         uint64    `json:"seq,omitempty"`  // 2026-07-09 §13-bugfix: 全局递增序号,用于 ReadPointer 公平性
	FromSeat    int       `json:"from_seat"`     // -1 = 观战者或系统;0..6 = bot 座位
	FromID      string    `json:"from_id"`       // user_id 或 "bot:<roomID>:<seat>"
	AgentName   string    `json:"agent_name"`    // bot 显示用 model_key;人类为空
	FromAccount string    `json:"from_account"`  // 人类昵称;bot 填 "Bot N号"
	IsBot       bool      `json:"is_bot"`
	IsSpectator bool      `json:"is_spectator"`
	IsWhisper   bool      `json:"is_whisper"`
	ToSeat      int       `json:"to_seat,omitempty"` // whisper 时填;其他 0
	Text        string    `json:"text"`
	Timestamp   time.Time `json:"timestamp"`
	Size        int       `json:"size"` // utf8.RuneCountInString × 4

	// IsActivity 是 2026-07-09 §13 增强:标识本条由 ChatActivityEvent 注入
	// (phase 切换、vote 结果、kill 通知),非自然玩家消息。
	IsActivity   bool   `json:"is_activity,omitempty"`
	EventKind    string `json:"event_kind,omitempty"`     // phase / vote / kill / dawn / ...
	ActivityIcon string `json:"activity_icon,omitempty"`  // 原始 ChatActivityEvent.Icon

	// Round 是该消息所属的「游戏轮次」(2026-07-11 §126 增强)。
	// 由 room.appendToChatQueueLocked / appendActivityToChatQueueLocked
	// 在 r.State.Round 拿到时填入;LLM 看不到这个字段,但
	// ChatHistoryQueue.compressLocked 在按 round 聚合压缩时会用到。
	// 0 = 未知 / 尚未填入(向后兼容旧 message 切片)。
	Round int `json:"round,omitempty"`
}

// chatMsgSize 估算单条消息的字节数(utf-8 rune count × 4 = 字节上限粗估)。
//
// 选 4 倍而不是 len(text) 的原因: 4 字节上限涵盖 utf-8 4-byte rune(CJK 扩展);
// 同时作为压缩目标(unit of account),与"500K 字节"语义对齐。
func chatMsgSize(text string) int {
	if text == "" {
		return 0
	}
	return utf8.RuneCountInString(text) * 4
}

// ChatHistoryQueue 一个房间共享的聊天历史队列(2026-07-09 §13-bugfix 改造:
// 由 per-seat 多个队列合并为 per-room 单个队列,Append 一次全员可见)。
//
// 上限 capBytes 默认 512000 (500K) — 由 config.WerewolfConfig.ChatHistoryBytes 注入;
// 默认压缩触发阈值 capBytes(超 capBytes 时 Compress);压缩后保留 80% 缓冲。
//
// 字段对齐 BotTranscript (server → client wire):
//   - bytes         → ChatHistoryBytes
//   - capBytes      → ChatHistoryCap
//   - lastCompressionAt → LastCompressionAt (unix millis)
//   - mergeCount / truncateCount → 仅服务端日志,不下发
//
// 公平性原则: 每个 Agent 持一个 ReadPointer(seat → nextSeq),LLM prompt
// 注入时取自 read pointer 之后的尾部消息;但**早期消息**(被压缩淘汰)对所有 bot
// 一视同仁(同队列同淘汰),保证公平。
type ChatHistoryQueue struct {
	mu                sync.Mutex
	messages          []ChatMessage
	bytes             int
	capBytes          int
	lastCompressionAt int64 // unix millis;零值=从未压缩
	mergeCount        int   // 累计合并次数
	truncateCount     int   // 累计截断次数

	// nextSeq 每条消息分配一个递增序号(从不重用,以便 Agent 的 ReadPointer 永久单调)。
	// 即使 Compress 把数组 pop/合,nextSeq 仍然递增,Agent 端的 readSeq 永远有效。
	nextSeq uint64

	// readPointers: seat → 上次 LLM 消费到的 seq(含)。
	// 不存在 = 该 bot 从未消费,从 0 开始。
	readPointers map[int]uint64
}

// ReadPointer 标识一个 Agent 当前已消费到的 message seq。
// 用于 WindowFor(seat) 返回"自 readPointer 之后"的新增消息。
// 2026-07-09 §13-bugfix: 替代旧 per-bot 私有 Queue 的快照拷贝。
const (
	// ReadPointerNil is the zero/unset read pointer — Agent 从未消费过任何消息,
	// WindowFor 返回完整切片(自序号 0 起)。
	ReadPointerNil uint64 = 0
)

// SetReadPointer 把指定 seat 的 read pointer 强制设到 seq。
// 通常由 Agent 在初始化时(StartAgentsLocked 后)调到 nextSeq,后续由 Advance() 推进。
func (q *ChatHistoryQueue) SetReadPointer(seat int, seq uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.readPointers == nil {
		q.readPointers = make(map[int]uint64)
	}
	q.readPointers[seat] = seq
}

// ReadPointer 返回指定 seat 当前 read pointer;不存在返回 ReadPointerNil。
func (q *ChatHistoryQueue) ReadPointer(seat int) uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.readPointers == nil {
		return ReadPointerNil
	}
	return q.readPointers[seat]
}

// Advance 把指定 seat 的 read pointer 推到 seq(若 seq 更大)。通常 Agent
// 拿到 LLM 调用结果后调用,下次 WindowFor 只返回更新部分。
func (q *ChatHistoryQueue) Advance(seat int, seq uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.readPointers == nil {
		q.readPointers = make(map[int]uint64)
	}
	if cur := q.readPointers[seat]; seq <= cur {
		return
	}
	q.readPointers[seat] = seq
}

// DefaultChatHistoryCapBytes 是 ChatHistoryQueue 默认字节上限(500K = 500*1024)。
// 在 agent 包内定义,避免反向 import config 包(circular)。
// config.WerewolfConfig.ChatHistoryBytes 应使用同一默认值。
const DefaultChatHistoryCapBytes = 500 * 1024

// NewChatHistoryQueue 创建一个空的 ChatHistoryQueue,capBytes ≤ 0 时使用 500K 兜底。
func NewChatHistoryQueue(capBytes int) *ChatHistoryQueue {
	if capBytes <= 0 {
		capBytes = DefaultChatHistoryCapBytes
	}
	return &ChatHistoryQueue{
		messages:  make([]ChatMessage, 0, 64),
		capBytes:  capBytes,
	}
}

// Append 追加一条消息,自动分配 Seq、计算 Size 并按需触发 Compress。
// 2026-07-09 §13-bugfix: 同一队列现在被 7 bot 共享,Append 只 push 一次。
func (q *ChatHistoryQueue) Append(m ChatMessage) {
	if m.Size == 0 {
		m.Size = chatMsgSize(m.Text)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextSeq++
	m.Seq = q.nextSeq
	q.messages = append(q.messages, m)
	q.bytes += m.Size
	if q.bytes > q.capBytes {
		q.compressLocked()
	}
}

// WindowFor 返回"自指定 seat 的 read pointer 之后"的所有消息(防御性 copy)。
// 用于 buildAgentContextLocked: 把"该 bot 还没看过的"增量内容喂给 LLM。
// 若 read pointer 不存在(从未消费),返回整个切片(等同于初次全量)。
// 2026-07-09 §13-bugfix: 替代旧 per-bot 私有 Snapshot(),保持公平性。
func (q *ChatHistoryQueue) WindowFor(seat int) []ChatMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	var since uint64 = ReadPointerNil
	if q.readPointers != nil {
		if v, ok := q.readPointers[seat]; ok {
			since = v
		}
	}
	out := make([]ChatMessage, 0, len(q.messages))
	for _, m := range q.messages {
		if m.Seq > since {
			out = append(out, m)
		}
	}
	return out
}

// Tail 返回末尾 limit 条消息(用于前端 500K 队列查看面板 / 调试)。
// limit ≤ 0 时返回完整切片。防御性 copy。
func (q *ChatHistoryQueue) Tail(limit int) []ChatMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit <= 0 || limit > len(q.messages) {
		limit = len(q.messages)
	}
	start := len(q.messages) - limit
	out := make([]ChatMessage, limit)
	copy(out, q.messages[start:])
	return out
}

// All 返回完整快照(供前端"查看队列全部内容"用,不受 read pointer 限制)。
// 同 Snapshot()。2026-07-09 §13-bugfix 不再用于 LLM prompt 注入(改用 WindowFor)。

// Head 返回最先 limit 条消息(队列最旧部分,用于查看早期被压缩的历史)。
func (q *ChatHistoryQueue) Head(limit int) []ChatMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit <= 0 || limit > len(q.messages) {
		limit = len(q.messages)
	}
	out := make([]ChatMessage, limit)
	copy(out, q.messages[:limit])
	return out
}

// Snapshot 保留兼容别名,等同 Tail(0):返回完整切片(防御性 copy)。
// 用于旧 API、单元测试与前端"查看完整队列"调试端点。2026-07-09 §13-bugfix
// 之后,LLM prompt 注入已改用 WindowFor(seat) — Snapshot 不参与公平性保证,
// 因为它忽略 read pointer,把所有消息一并返回。
func (q *ChatHistoryQueue) Snapshot() []ChatMessage {
	return q.Tail(0)
}

// TotalBytes 返回当前队列总字节数(用于 BotTranscript 上报)。
func (q *ChatHistoryQueue) TotalBytes() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.bytes
}

// CapBytes 返回队列上限(用于 admin UI 显示 "X KB / Y KB")。
func (q *ChatHistoryQueue) CapBytes() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.capBytes
}

// SnapshotLastSeq 返回当前最新分配的 seq(下一个 Append 会得到此值+1)。
// 用于 Agent 在 LLM 调用后调用 Advance(seat, SnapshotLastSeq()) 把 read pointer
// 推到末尾,确保下次 WindowFor(seat) 只返回新的消息。2026-07-09 §13-bugfix。
func (q *ChatHistoryQueue) SnapshotLastSeq() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.nextSeq
}

// Stats 返回 (bytes, lastCompressionAt, mergeCount, truncateCount)。
func (q *ChatHistoryQueue) Stats() (bytes, lastCompressionAt int64, merges, truncs int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return int64(q.bytes), q.lastCompressionAt, q.mergeCount, q.truncateCount
}

// Compress 强制压缩一次(供测试 + 调试 + room teardown 时调用)。
func (q *ChatHistoryQueue) Compress() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.compressLocked()
}

// compressLocked 实际压缩逻辑 — 调用方必须持锁。
//
// 按 4 级策略:
//  1. 单条 > 1KB → 截断到 200 字
//  2. 三条相邻同 FromID+IsBot+IsWhisper → 合并为 1 条
//  3. 超 capBytes → 从队首淘汰直到 ≤ capBytes*0.8
//  4. fallback (压缩后仍超 capBytes 且 messages > 100) → 最旧 30 条压成单条摘要
func (q *ChatHistoryQueue) compressLocked() {
	beforeBytes := q.bytes
	beforeCount := len(q.messages)

	// Step 1: 截断超长单条 (> 1KB)
	const longThreshold = 1024
	for i := range q.messages {
		if q.messages[i].Size > longThreshold {
			r := []rune(q.messages[i].Text)
			if len(r) > 200 {
				trunc := string(r[:200]) + "…(truncated)"
				q.messages[i].Text = trunc
				q.messages[i].Size = chatMsgSize(trunc)
				q.truncateCount++
			}
		}
	}
	q.recomputeBytesLocked()

	// Step 2: 合并相邻同 sender (3 条 → 1 条)
	q.messages = q.mergeAdjacentLocked()
	q.recomputeBytesLocked()

	// Step 2.5 (2026-07-11 §126 增强): 按 round 聚合压缩 — 优先把"最旧的完整
	// round"的所有消息(若 ≥ 3 条)聚合成 1 条 "[Day N 复盘]" 摘要。这样比
	// 直接 pop 老消息更能保留前几轮关键信息(投票/死亡/跳身份),让 LLM 在
	// Round ≥ 3 时仍能基于复盘切片做出有根据的推理。
	q.foldOldestRoundLocked()

	// Step 3: 淘汰最旧直到 ≤ capBytes * 0.8
	target := q.capBytes * 4 / 5 // 80%
	for q.bytes > target && len(q.messages) > 1 {
		removed := q.messages[0]
		q.messages = q.messages[1:]
		q.bytes -= removed.Size
		if q.bytes < 0 {
			q.bytes = 0
		}
	}

	// Step 4: fallback — 仍超 capBytes 且消息很多 → 摘要
	if q.bytes > q.capBytes && len(q.messages) > 100 {
		const foldCount = 30
		if len(q.messages) > foldCount {
			folded := q.messages[:foldCount]
			summaryText := buildSummaryText(folded)
			summary := ChatMessage{
				ID:          fmt.Sprintf("summary:%d", time.Now().UnixNano()),
				FromSeat:    -1,
				FromID:      "system:summary",
				AgentName:   "",
				FromAccount: "[摘要]",
				IsBot:       false,
				IsSpectator: false,
				IsWhisper:   false,
				Text:        summaryText,
				Timestamp:   folded[len(folded)-1].Timestamp,
				Size:        chatMsgSize(summaryText),
				// 2026-07-11 §126 增强:摘要继承最早条目的 round,便于
				// 后续 foldOldestRoundLocked 继续按 round 聚合。
				Round: folded[0].Round,
			}
			rest := q.messages[foldCount:]
			q.messages = append([]ChatMessage{summary}, rest...)
			q.recomputeBytesLocked()
		}
	}

	q.lastCompressionAt = time.Now().UnixMilli()
	if beforeBytes != q.bytes || beforeCount != len(q.messages) {
		// 调用方负责打日志(锁已释放);此处只更新内部统计
		_ = beforeBytes
	}
}

// mergeAdjacentLocked 把 3 条连续同 FromID+IsBot+IsWhisper 的消息合并为 1 条。
// 调用方必须持锁。
func (q *ChatHistoryQueue) mergeAdjacentLocked() []ChatMessage {
	if len(q.messages) < 3 {
		return q.messages
	}
	out := make([]ChatMessage, 0, len(q.messages))
	i := 0
	for i < len(q.messages) {
		cur := q.messages[i]
		// 检查连续 2 条同 sender
		if i+2 < len(q.messages) {
			n1 := q.messages[i+1]
			n2 := q.messages[i+2]
			if sameSender(cur, n1) && sameSender(n1, n2) {
				mergedText := cur.Text + " | " + n1.Text + " | " + n2.Text
				merged := ChatMessage{
					ID:          cur.ID,
					FromSeat:    cur.FromSeat,
					FromID:      cur.FromID,
					AgentName:   cur.AgentName,
					FromAccount: cur.FromAccount,
					IsBot:       cur.IsBot,
					IsSpectator: cur.IsSpectator,
					IsWhisper:   cur.IsWhisper,
					ToSeat:      cur.ToSeat,
					Text:        mergedText,
					Timestamp:   n2.Timestamp, // 最新时间戳
					Size:        0,             // recomputeBytesLocked 会重算
				}
				merged.Size = chatMsgSize(mergedText)
				out = append(out, merged)
				q.mergeCount++
				i += 3
				continue
			}
		}
		out = append(out, cur)
		i++
	}
	return out
}

// sameSender 判断两条消息是否同 sender (whisper 维度也必须一致)。
func sameSender(a, b ChatMessage) bool {
	return a.FromID == b.FromID && a.IsBot == b.IsBot && a.IsWhisper == b.IsWhisper
}

// recomputeBytesLocked 重算 bytes(用于压缩过程中 Size 改变后)。
func (q *ChatHistoryQueue) recomputeBytesLocked() {
	total := 0
	for i := range q.messages {
		if q.messages[i].Size == 0 {
			q.messages[i].Size = chatMsgSize(q.messages[i].Text)
		}
		total += q.messages[i].Size
	}
	q.bytes = total
}

// buildSummaryText 把一组消息压成单条摘要文本(行数 = 1,约 100-200 字)。
func buildSummaryText(msgs []ChatMessage) string {
	if len(msgs) == 0 {
		return "[摘要: 空]"
	}
	fromStart := msgs[0].Timestamp.Format("15:04:05")
	fromEnd := msgs[len(msgs)-1].Timestamp.Format("15:04:05")
	// 统计 sender 分布
	senders := map[string]int{}
	for _, m := range msgs {
		key := m.FromAccount
		if key == "" {
			key = m.AgentName
		}
		if key == "" {
			key = "?"
		}
		senders[key]++
	}
	// top 3 senders
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(senders))
	for k, v := range senders {
		pairs = append(pairs, kv{k, v})
	}
	// 简单排序(数量降序)
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].v > pairs[i].v {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	top := ""
	for i := 0; i < len(pairs) && i < 3; i++ {
		if i > 0 {
			top += ", "
		}
		top += pairs[i].k + "×" + itoa(pairs[i].v)
	}
	return fmt.Sprintf("[摘要 %s→%s 共 %d 条 · 主要: %s]", fromStart, fromEnd, len(msgs), top)
}

// ─────────────────── 按 round 复盘压缩(2026-07-11 §126 增强)───────────────────

// foldOldestRoundLocked 把"最旧的完整 round"的所有消息(若 ≥ 3 条)聚合成
// 1 条 "[Day N 复盘]" 摘要,替换原消息切片,大幅节省字节数。调用方必须持锁。
//
// 设计动机:狼人杀 13 人局典型 5-7 轮,长对局后期 500K 队列容易爆。
// 旧策略是"从队首 pop 直到 ≤ 80% 字节" — 这会**彻底丢失**早期 round 的
// 关键事件(投票分布 / 跳身份 / 死亡),LLM 失去「前几轮上下文」。
//
// 新策略:在 pop 之前先尝试"按 round 聚合" — 把整个 round 的多条消息
// 聚合成 1 条 200 字内的复盘(包含事件类型分布 + 主要参与者 + 关键
// 票型变化),既保留信息又省字节。
//
// 限制:
//   - 只折叠 ≥ 3 条的 round(单条 / 两条聚合反而占空间);
//   - 不折叠最末 round(LLM 决策需要最新信息);
//   - 不折叠已经是"摘要"或"复盘"的消息(避免递归压缩丢细节);
//   - 一轮只折叠一次;fold 之后该 round 在数组里就是 1 条占位,后续
//     compressLocked 不会再 fold 同一 round。
func (q *ChatHistoryQueue) foldOldestRoundLocked() {
	if len(q.messages) < 3 {
		return
	}
	// 找最早的非零 round;若全是 0(向后兼容)则不折叠。
	earliest := -1
	for _, m := range q.messages {
		if m.Round > 0 {
			if earliest == -1 || m.Round < earliest {
				earliest = m.Round
			}
		}
	}
	if earliest == -1 {
		return
	}
	// 找到最末 round(不能折叠它)
	latest := earliest
	for _, m := range q.messages {
		if m.Round > latest {
			latest = m.Round
		}
	}
	if earliest == latest {
		// 整个队列还在同一 round → 不折叠
		return
	}
	// 折叠 earliest round
	var (
		indices   []int
		recapMsgs []ChatMessage
	)
	for i, m := range q.messages {
		if m.Round == earliest {
			indices = append(indices, i)
			recapMsgs = append(recapMsgs, m)
		}
	}
	if len(recapMsgs) < 3 {
		return
	}
	// 生成复盘文本
	recap := buildRoundRecap(earliest, recapMsgs)
	summary := ChatMessage{
		ID:          fmt.Sprintf("recap:%d:%d", earliest, time.Now().UnixNano()),
		FromSeat:    -1,
		FromID:      "system:recap",
		AgentName:   "recap",
		FromAccount: "[Day " + itoa(earliest) + " 复盘]",
		IsBot:       false,
		IsSpectator: false,
		IsWhisper:   false,
		Text:        recap,
		Timestamp:   recapMsgs[len(recapMsgs)-1].Timestamp,
		Size:        chatMsgSize(recap),
		Round:       earliest,
	}
	// 替换: 把 q.messages 里 [indices[0]..indices[last]] 替换为 summary
	first := indices[0]
	last := indices[len(indices)-1]
	out := make([]ChatMessage, 0, len(q.messages)-len(indices)+1)
	out = append(out, q.messages[:first]...)
	out = append(out, summary)
	out = append(out, q.messages[last+1:]...)
	q.messages = out
	q.recomputeBytesLocked()
	q.mergeCount++ // 复用 mergeCount 字段(语义上仍是合并操作)
}

// buildRoundRecap 把一个 round 的所有消息聚合成 1 条 200 字内的复盘文本。
// 设计原则:
//   - 第一行: "[Day N 复盘] HH:MM:SS→HH:MM:SS 共 N 条"
//   - 第二行起: 按事件类型分组(投票 / 死亡 / 跳身份 / 关键推理发言)
//     给出每个类型的主要参与者与简短描述;
//   - 控制总长度 ≤ 250 字,保证单条 ≤ 1KB 截断阈值。
//
// 复盘文本由 ChatHistoryQueue.foldOldestRoundLocked 调用;若失败(空输入)
// 返回 "[Day N 复盘] (无内容)"。
func buildRoundRecap(round int, msgs []ChatMessage) string {
	if len(msgs) == 0 {
		return "[Day " + itoa(round) + " 复盘] (无内容)"
	}
	// 统计维度
	var (
		votes    []string // 投票(以 "X号投Y号" 形式记录,这里只能从 text 推)
		deaths   []string // 死亡通知
		jumps    []string // 跳身份发言(典型 "我是预言家/女巫/猎人")
		otherKey []string // 关键推理发言(短摘)
		activity []string // 活动事件(phase 切换 / vote 结果)
	)
	// sender 频率统计(用于识别主要发言者)
	senderCount := map[string]int{}
	for _, m := range msgs {
		key := m.FromAccount
		if key == "" {
			key = m.AgentName
		}
		if key == "" {
			key = "?"
		}
		senderCount[key]++
	}
	for _, m := range msgs {
		who := m.FromAccount
		if who == "" {
			who = m.AgentName
		}
		if who == "" {
			who = "?"
		}
		t := m.Text
		if t == "" {
			continue
		}
		switch {
		case m.IsActivity:
			// 活动事件: 截前 30 字
			r := []rune(t)
			if len(r) > 30 {
				t = string(r[:30]) + "…"
			}
			activity = append(activity, t)
		case m.IsWhisper:
			// whisper 不复盘内容(隐私) — 只标记 "X号 私聊 N 条"
			// 已在 senderCount 统计
		default:
			// 公开 chat: 启发式判断事件类型
			lower := t
			// 简化: 只用 text 子串做粗略分类
			if containsAny(lower, "投给", "票给", "我跟票", "弃票") {
				r := []rune(t)
				if len(r) > 30 {
					t = string(r[:30]) + "…"
				}
				votes = append(votes, who+":"+t)
			} else if containsAny(lower, "死了", "走了", "没了", "被处决", "被刀", "出局") {
				r := []rune(t)
				if len(r) > 30 {
					t = string(r[:30]) + "…"
				}
				deaths = append(deaths, who+":"+t)
			} else if containsAny(lower, "我是预言家", "我是女巫", "我是猎人", "我是白痴") {
				r := []rune(t)
				if len(r) > 30 {
					t = string(r[:30]) + "…"
				}
				jumps = append(jumps, who+":"+t)
			} else if senderCount[who] >= 3 {
				// 多次发言的"主要发言者" — 摘其首条
				r := []rune(t)
				if len(r) > 30 {
					t = string(r[:30]) + "…"
				}
				if len(otherKey) < 2 {
					otherKey = append(otherKey, who+":"+t)
				}
			}
		}
	}
	// 拼接
	ts := msgs[0].Timestamp.Format("15:04:05") + "→" + msgs[len(msgs)-1].Timestamp.Format("15:04:05")
	out := "[Day " + itoa(round) + " 复盘] " + ts + " 共 " + itoa(len(msgs)) + " 条"
	if len(activity) > 0 {
		out += "\n· 活动: " + joinShort(activity, 3, " | ")
	}
	if len(jumps) > 0 {
		out += "\n· 跳身份: " + joinShort(jumps, 3, " | ")
	}
	if len(deaths) > 0 {
		out += "\n· 死亡: " + joinShort(deaths, 2, " | ")
	}
	if len(votes) > 0 {
		out += "\n· 票型: " + joinShort(votes, 3, " | ")
	}
	if len(otherKey) > 0 {
		out += "\n· 关键推理: " + joinShort(otherKey, 2, " | ")
	}
	// 主要发言者
	if len(senderCount) > 0 {
		top := topSenders(senderCount, 3)
		out += "\n· 主要发言者: " + top
	}
	// 长度硬截断到 250 字(1KB byte 上限内)
	r := []rune(out)
	if len(r) > 250 {
		out = string(r[:250]) + "…"
	}
	return out
}

// containsAny 判断 s 是否包含 subs 中任一子串(简化版,无 allocations)。
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) == 0 {
			continue
		}
		if len(s) >= len(sub) && indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

// indexOf 简化 strings.Index(避免 import strings 在此 hot path)。
func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	if len(sub) > len(s) {
		return -1
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// joinShort 把切片拼接成 "a | b | c",取前 maxN 个,中间用 sep 连接。
func joinShort(items []string, maxN int, sep string) string {
	n := len(items)
	if n > maxN {
		n = maxN
	}
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += sep
		}
		out += items[i]
	}
	if len(items) > maxN {
		out += sep + "…等" + itoa(len(items)) + "条"
	}
	return out
}

// topSenders 取出 senderCount 中 top N sender,格式 "A×3,B×2,C×1"。
func topSenders(counts map[string]int, n int) string {
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	// 简单冒泡排序(数量降序)
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].v > pairs[i].v {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	out := ""
	for i := 0; i < len(pairs) && i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += pairs[i].k + "×" + itoa(pairs[i].v)
	}
	return out
}

// RecapLastNRounds 返回最近 N 轮的复盘切片(每轮 1 条 "[Day K 复盘]" 消息),
// 供 BuildUserPrompt 在 [500K 聊天上下文] 末尾追加,让 LLM 在 Round ≥ 3 时
// 看到前几轮的精华。调用方需自己加锁或本方法在持锁状态下被调用。
//
// 行为:
//   - 遍历队列找到所有 Round ∈ [latest-N+1, latest] 的消息;
//   - 对每个 round 调 buildRoundRecap 生成复盘文本;
//   - 跳过已经被折叠的旧 round(若该 round 已经在队列里只有 1 条
//     "[Day K 复盘]" 占位,直接复用,避免重复聚合)。
//
// 返回:[]ChatMessage,长度 ≤ N,按 round 升序。
func (q *ChatHistoryQueue) RecapLastNRounds(n int) []ChatMessage {
	if n <= 0 || len(q.messages) == 0 {
		return nil
	}
	// 找最新 round
	latest := 0
	for _, m := range q.messages {
		if m.Round > latest {
			latest = m.Round
		}
	}
	if latest == 0 {
		return nil
	}
	// 按 round 分组(只关心最近 n 个 round)
	startRound := latest - n + 1
	if startRound < 1 {
		startRound = 1
	}
	out := make([]ChatMessage, 0, n)
	for r := startRound; r <= latest; r++ {
		// 该 round 已有 "recap:" 占位 → 直接复用
		var existing *ChatMessage
		var collected []ChatMessage
		for i := range q.messages {
			m := &q.messages[i]
			if m.Round != r {
				continue
			}
			if m.FromID == "system:recap" {
				existing = m
				break
			}
			collected = append(collected, *m)
		}
		if existing != nil {
			out = append(out, *existing)
			continue
		}
		if len(collected) == 0 {
			continue
		}
		// 生成新复盘
		text := buildRoundRecap(r, collected)
		out = append(out, ChatMessage{
			ID:          fmt.Sprintf("recap:%d:on-demand", r),
			FromSeat:    -1,
			FromID:      "system:recap",
			AgentName:   "recap",
			FromAccount: "[Day " + itoa(r) + " 复盘]",
			IsBot:       false,
			IsSpectator: false,
			IsWhisper:   false,
			Text:        text,
			Timestamp:   collected[len(collected)-1].Timestamp,
			Size:        chatMsgSize(text),
			Round:       r,
		})
	}
	return out
}
// itoa 是 strconv.Itoa 的轻量内联版,避免在热路径 import strconv。
// (2026-08-06 §Agent 重构:从原 ServerGo/agent/memory.go 复制保留;
//  memory.go 搬到 wwplayer 后会另留一份本地 itoa 给 wwplayer 内部使用,
//  本包不依赖 memory.go,故此处独立保留。)
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	out := string(buf[pos:])
	if neg {
		return "-" + out
	}
	return out
}
