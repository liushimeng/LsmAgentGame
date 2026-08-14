// Package wwplayer — steering_queue.go: 游戏事件实时注入通道。
// 灵感来源: PI Agent 的 PendingMessageQueue (steering/follow-up) 机制。
//
// 解决痛点: Agent 在 handleEvent 内循环执行 LLM 调用时,
// 新到达的观众消息/道具命中/阶段提示只能等下一次 handleEvent 入口才能感知。
// SteeringQueue 允许 room manager 在 agent 运行中非阻塞注入实时事件。
//
// 约束:
//   - 非阻塞写入 (channel 满时丢弃,避免 room manager 被阻塞)
//   - 每轮 LLM 调用前 drain 一次,注入到 user prompt 末尾
//   - 不破坏 Memory 的消息顺序 (作为 prompt 片段追加,不作为独立 Message)
package wwplayer

import "sync"

// AgentSteerKind 枚举注入事件类型。
type AgentSteerKind string

const (
	SteerSpectatorInquiry AgentSteerKind = "spectator_inquiry" // 观众提问
	SteerPropHit          AgentSteerKind = "prop_hit"          // 道具命中通知
	SteerPhaseHint        AgentSteerKind = "phase_hint"        // 阶段提示
	SteerWhisper          AgentSteerKind = "whisper"           // 私聊到达
)

// AgentSteerMsg 是一个注入到 agent 下一轮 LLM 调用前的实时消息。
type AgentSteerMsg struct {
	Kind    AgentSteerKind
	Content string // 注入到 user prompt 的文本
}

// SteeringQueue 是一个线程安全的非阻塞消息队列。
// 容量固定为 10;写满时最旧消息被丢弃 (FIFO)。
type SteeringQueue struct {
	ch   chan AgentSteerMsg
	mu   sync.Mutex
	drop int // 被丢弃的消息计数 (监控用)
}

// NewSteeringQueue 创建指定容量的 SteeringQueue。
func NewSteeringQueue(capacity int) *SteeringQueue {
	if capacity <= 0 {
		capacity = 10
	}
	return &SteeringQueue{
		ch: make(chan AgentSteerMsg, capacity),
	}
}

// Enqueue 非阻塞写入一条消息。队列满时丢弃最旧消息。
func (q *SteeringQueue) Enqueue(msg AgentSteerMsg) {
	select {
	case q.ch <- msg:
		// 成功入队
	default:
		// 队列满,丢弃最旧消息为新消息腾出空间
		q.mu.Lock()
		q.drop++
		q.mu.Unlock()
		select {
		case <-q.ch: // 弹出最旧
		default:
		}
		select {
		case q.ch <- msg:
		default:
			// 极端竞态:仍然满,放弃
		}
	}
}

// Drain 取出队列中所有消息并返回。无消息时返回空切片。
func (q *SteeringQueue) Drain() []AgentSteerMsg {
	var msgs []AgentSteerMsg
	for {
		select {
		case msg := <-q.ch:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

// Len 返回当前队列长度。
func (q *SteeringQueue) Len() int {
	return len(q.ch)
}

// DropCount 返回自上次 ResetDropCount 以来被丢弃的消息数。
func (q *SteeringQueue) DropCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.drop
}

// ResetDropCount 重置丢弃计数器。
func (q *SteeringQueue) ResetDropCount() {
	q.mu.Lock()
	q.drop = 0
	q.mu.Unlock()
}

// Close 关闭队列通道 (agent 生命周期结束时调用)。
func (q *SteeringQueue) Close() {
	close(q.ch)
}

// DrainAndFormat 取出所有消息并格式化为可注入 user prompt 的文本。
// 返回空串表示无排队消息。
func (q *SteeringQueue) DrainAndFormat() string {
	msgs := q.Drain()
	if len(msgs) == 0 {
		return ""
	}

	var parts []string
	for _, m := range msgs {
		switch m.Kind {
		case SteerSpectatorInquiry:
			parts = append(parts, "【观众提问】"+m.Content)
		case SteerPropHit:
			parts = append(parts, "【道具影响】"+m.Content)
		case SteerPhaseHint:
			parts = append(parts, "【阶段提示】"+m.Content)
		case SteerWhisper:
			parts = append(parts, "【私聊到达】"+m.Content)
		default:
			parts = append(parts, "【事件】"+m.Content)
		}
	}
	return "\n\n" + joinSteerParts(parts)
}

// ---------------------------------------------------------------------------
// Agent 接线 —— 2026-08-13 §20260813-04 U1
//
// 此前 steeringQueue 字段只有声明(agent.go:260)与读取(run.go:684),
// **零 setter** → 恒为 nil → 上面 149 行实现从未执行过(§130 第七次复发)。
// 文件头详细描述的「Agent 在 LLM 调用期间无法感知新到达事件」痛点是真实的:
// 慢模型单次调用可达 1-3 分钟(§197),期间的观众提问要等下一轮 wake 才被看到。
// ---------------------------------------------------------------------------

// SetSteeringQueue 注入实时事件队列。由 room manager 在 StartAgentsLocked 调用。
// 传 nil 显式关闭(run.go 的 drain 逻辑跳过)。
func (a *Agent) SetSteeringQueue(q *SteeringQueue) {
	a.Lock()
	defer a.Unlock()
	a.steeringQueue = q
}

// SteeringQueue 返回实时事件队列(room manager 入队用)。可能为 nil。
//
// 调用方必须做 nil 守卫:
//
//	if q := ag.SteeringQueue(); q != nil { q.Enqueue(...) }
//
// CloseSteeringQueue 会先置 nil 再 close,因此本方法返回非 nil 即可安全 Enqueue。
func (a *Agent) SteeringQueue() *SteeringQueue {
	a.Lock()
	defer a.Unlock()
	return a.steeringQueue
}

// CloseSteeringQueue 在 Agent 生命周期结束时释放队列。
//
// **先置 nil 再 close** —— 顺序不可颠倒: 若先 close 再置 nil,
// 并发的 SteeringQueue() 调用方可能拿到已关闭的 channel 并 Enqueue,
// 向 closed channel 发送会 panic。
func (a *Agent) CloseSteeringQueue() {
	a.Lock()
	q := a.steeringQueue
	a.steeringQueue = nil
	a.Unlock()
	if q != nil {
		q.Close()
	}
}

// joinSteerParts 拼接多个 steer 文本。
func joinSteerParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += "\n" + p
	}
	return result
}
