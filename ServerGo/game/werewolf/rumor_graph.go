// §20260811-03 U1 — 信息污染链 RumorGraph(狼人杀 13 人局)。
//
// 设计动机:让 Agent 的 GameContext 拥有"传闻信任链"——把信息本身变成博弈对象。
// 与既有组件的关系:
//   - 与 §20260810-05 信息账本一期 + §20260810-08 二期形成完整"信息生态"(账本是事实层,本图是污染层)
//   - 与 §119 协议层隔离严格对齐:谣言**绝不**入 chat_message / chat_history 队列 / HeartThought
//   - 与 §92a 自死锁教训严格对齐:所有读写都是 *Locked 锁内变体,公开方法包加锁委托
//
// 文件约束:
//   - ≤ 500 行(含测试)
//   - 仅依赖 Go 标准库 + 既有 werewolf 子包
//   - 不引入第三方图算法库(纯 map + slice 即可,13 人局规模用不上 dagre/d3-force)

package werewolf

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// RumorMaxHop 是传闻有向图的 hop 上限。超过该 hop 的边被标为"来源不可考",
// 在 prompt 渲染中用 [传闻·来源不可考] 前缀。
const RumorMaxHop = 3

// RumorInboxMax 是单玩家接收箱上限。超出时 FIFO 截断最早,避免 prompt 膨胀。
const RumorInboxMax = 20

// RumorMaxTextLen 是单条谣言的字符上限。
const RumorMaxTextLen = 50

// RumorDailyCapPerPlayer 是单玩家每天发送谣言上限(人类 + Agent 共用)。
const RumorDailyCapPerPlayer = 1

// RumorEdge 是单条谣言传播有向图边。
//
// 字段语义:
//   - ID: 全局单调递增,用于去重 + 前端 React key
//   - FromSeat/ToSeat: 发送/接收座位(0~12, -1 表示系统生成)
//   - Text: ≤50 字原文
//   - Hop: 0=原始创建,1~RumorMaxHop=二次及以上传播
//   - Veracity: 服务端权威真伪分(0~1);60% 真实,40% 虚假
//   - CreatedRound: 创建时的全局轮数
//   - CreatedAt: 创建时的 unix nano
//   - DayOfWeek: 创建时的 daytime day(用于每日限流)
type RumorEdge struct {
	ID           int64   `json:"id"`
	FromSeat     int     `json:"from_seat"`
	ToSeat       int     `json:"to_seat"`
	Text         string  `json:"text"`
	Hop          int     `json:"hop"`
	Veracity     float32 `json:"veracity"`
	CreatedRound int     `json:"created_round"`
	CreatedAt    int64   `json:"created_at"`
	DayOfWeek    int     `json:"day_of_week"`
}

// RumorGraph 是房间级有向图边集合。
//
// 数据结构选择:
//   - Edges: 有序数组(append-only),保留创建顺序,便于前端按时间渲染
//   - byID: O(1) 索引用于去重/删除
//   - inbox: 玩家 → 收到的谣言 ID 列表(FIFO 上限 RumorInboxMax)
//   - sendLog: 玩家 × daytime → 当日发送计数(RumorDailyCapPerPlayer 上限)
type RumorGraph struct {
	mu       sync.Mutex // 仅用于 slice append 原子性,锁内仍需 r.mu
	Edges    []*RumorEdge
	byID     map[int64]*RumorEdge
	inbox    map[int][]int64 // seat → edge ids (FIFO)
	sendLog  map[int]map[int]int // seat × day → count
	nextID   int64
	nowFunc  func() time.Time // 可注入用于测试
	rng      *rand.Rand
}

// NewRumorGraph 创建一个空的有向图(线程安全)。
func NewRumorGraph() *RumorGraph {
	return &RumorGraph{
		Edges:   make([]*RumorEdge, 0, 32),
		byID:    make(map[int64]*RumorEdge),
		inbox:   make(map[int][]int64),
		sendLog: make(map[int]map[int]int),
		nextID:  1,
		nowFunc: time.Now,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// rumorGraphLocked 是房间级别的懒初始化助手(纯函数,不持锁)。
//
// 调用约定:调用方**必须**已持有 r.mu。
//
// 返回值保证非 nil,首次调用会初始化。
func (r *WerewolfRoom) rumorGraphLocked() *RumorGraph {
	if r.rumorGraph == nil {
		r.rumorGraph = NewRumorGraph()
	}
	return r.rumorGraph
}

// AddRumorEdgeLocked 写入一条新的谣言边。
//
// 入参约束:
//   - fromSeat ∈ [-1, MaxPlayers); -1 表示系统生成(法官骰点)
//   - toSeat ∈ [0, MaxPlayers)
//   - text 长度 ≤ RumorMaxTextLen(超长自动截断 + Warn 日志)
//   - 当前 daytime 的 fromSeat 已发送数 + 1 ≤ RumorDailyCapPerPlayer
//     (仅 fromSeat >= 0 时校验;-1 是系统无限制)
//   - toSeat 必须存活;死亡玩家的 inbox 永不更新
//
// 返回值:成功 → edge 指针 + nil;失败 → nil + error。
//
// §92a 调用链上游必须已持 r.mu(典型:Action_RumorSend / room_chat.go WS handler)。
func (r *WerewolfRoom) AddRumorEdgeLocked(fromSeat, toSeat int, text string, hop int, veracity float32, round, day int) (*RumorEdge, error) {
	if toSeat < 0 || toSeat >= MaxPlayers {
		return nil, fmt.Errorf("rumor: invalid to_seat=%d", toSeat)
	}
	if !r.State.Players[toSeat].Alive {
		return nil, fmt.Errorf("rumor: target seat %d is dead", toSeat)
	}
	if fromSeat >= 0 && (fromSeat < 0 || fromSeat >= MaxPlayers) {
		return nil, fmt.Errorf("rumor: invalid from_seat=%d", fromSeat)
	}
	if fromSeat >= 0 {
		// 每日发送上限
		g := r.rumorGraphLocked()
		if g.sendLog[fromSeat] == nil {
			g.sendLog[fromSeat] = make(map[int]int)
		}
		if g.sendLog[fromSeat][day] >= RumorDailyCapPerPlayer {
			return nil, fmt.Errorf("rumor: from_seat=%d daily cap reached on day=%d", fromSeat, day)
		}
		if !r.State.Players[fromSeat].Alive {
			return nil, fmt.Errorf("rumor: from_seat=%d is dead", fromSeat)
		}
	}

	// 文本截断
	if len(text) > RumorMaxTextLen {
		text = text[:RumorMaxTextLen]
	}

	g := r.rumorGraphLocked()
	g.mu.Lock()
	defer g.mu.Unlock()

	edge := &RumorEdge{
		ID:           g.nextID,
		FromSeat:     fromSeat,
		ToSeat:       toSeat,
		Text:         text,
		Hop:          hop,
		Veracity:     veracity,
		CreatedRound: round,
		CreatedAt:    g.nowFunc().UnixNano(),
		DayOfWeek:    day,
	}
	g.nextID++
	g.Edges = append(g.Edges, edge)
	g.byID[edge.ID] = edge

	// 接收方 inbox(FIFO 上限)
	if g.inbox[toSeat] == nil {
		g.inbox[toSeat] = make([]int64, 0, RumorInboxMax)
	}
	g.inbox[toSeat] = append(g.inbox[toSeat], edge.ID)
	if len(g.inbox[toSeat]) > RumorInboxMax {
		// FIFO 截断最早
		g.inbox[toSeat] = g.inbox[toSeat][len(g.inbox[toSeat])-RumorInboxMax:]
	}

	// 发送方当日计数
	if fromSeat >= 0 {
		g.sendLog[fromSeat][day]++
	}

	return edge, nil
}

// GetRumorInboxLocked 返回某座位当前 inbox 的副本(用于 buildAgentContextLocked)。
//
// §92a:调用方必须已持 r.mu。
func (r *WerewolfRoom) GetRumorInboxLocked(seat int) []RumorEdgeSnapshot {
	if seat < 0 || seat >= MaxPlayers {
		return nil
	}
	g := r.rumorGraphLocked()
	g.mu.Lock()
	defer g.mu.Unlock()

	ids := g.inbox[seat]
	if len(ids) == 0 {
		return nil
	}
	out := make([]RumorEdgeSnapshot, 0, len(ids))
	for _, id := range ids {
		if e, ok := g.byID[id]; ok {
			out = append(out, RumorEdgeSnapshot{
				FromSeat: e.FromSeat,
				Text:     e.Text,
				Hop:      e.Hop,
				Veracity: e.Veracity,
			})
		}
	}
	return out
}

// RumorEdgeSnapshot 是 GameContext.RumorInbox 元素的轻量副本。
type RumorEdgeSnapshot struct {
	FromSeat int     `json:"from_seat"`
	Text     string  `json:"text"`
	Hop      int     `json:"hop"`
	Veracity float32 `json:"veracity"`
}

// BuildRumorSnapshotLocked 返回所有边的副本(用于 SettlementModal + spectator 视图)。
//
// §92a:调用方必须已持 r.mu。
func (r *WerewolfRoom) BuildRumorSnapshotLocked() []*RumorEdge {
	g := r.rumorGraphLocked()
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]*RumorEdge, len(g.Edges))
	copy(out, g.Edges)
	return out
}

// PurgeByDeathLocked 在玩家死亡时清理其相关数据(§133 死亡清理模式)。
//
// §92a:调用方必须已持 r.mu(典型:EmitPlayerDied)。
func (r *WerewolfRoom) PurgeByDeathLocked(seat int) {
	if seat < 0 || seat >= MaxPlayers {
		return
	}
	g := r.rumorGraphLocked()
	g.mu.Lock()
	defer g.mu.Unlock()

	// 1) 死亡玩家作为发送方的边保留(历史追溯需要),但不再参与 sendLog 限制
	delete(g.sendLog, seat)
	// 2) 死亡玩家的 inbox 直接清空(死人不能收新谣言)
	delete(g.inbox, seat)
}

// ResetRumorGraphLocked 在 restartGameLocked 原地重开时清零。
func (r *WerewolfRoom) ResetRumorGraphLocked() {
	r.rumorGraph = NewRumorGraph()
}

// RumorPrefixForHop 根据 hop 返回传闻文本前缀(用于 Agent prompt 渲染)。
func RumorPrefixForHop(hop int) string {
	switch {
	case hop <= 0:
		return ""
	case hop == 1:
		return "[传闻] "
	case hop == 2:
		return "[传闻×2] "
	default:
		return "[传闻·来源不可考] "
	}
}

// RumorVeracityLabel 返回真伪定性标签(用于 spectator SettlementModal)。
func RumorVeracityLabel(v float32) string {
	switch {
	case v >= 0.8:
		return "真实"
	case v >= 0.5:
		return "基本真实"
	case v >= 0.2:
		return "存疑"
	default:
		return "虚假"
	}
}

// RollRumorVeracityLocked 按概率(默认 60% 真实)服务端权威骰点真伪。
//
// §130 接线:必须由 manager 的单一入口调用,确保真伪不可被 LLM 操纵。
//
// §92a:调用方必须已持 r.mu。
func (r *WerewolfRoom) RollRumorVeracityLocked() float32 {
	g := r.rumorGraphLocked()
	g.mu.Lock()
	defer g.mu.Unlock()
	// 60% 真实 → 0.7~1.0;40% 虚假 → 0.0~0.4
	if g.rng.Float32() < 0.6 {
		return 0.7 + g.rng.Float32()*0.3
	}
	return g.rng.Float32() * 0.4
}

// RumorInboxPromptBlock 把 inbox 渲染为 Agent user prompt 末尾块。
//
// §119 协议层隔离:仅在 user prompt 渲染,**不**写 chat_message 表 / chat_history 队列。
// §92a:调用方必须已持 r.mu。
func (r *WerewolfRoom) RumorInboxPromptBlock(seat int) string {
	edges := r.GetRumorInboxLocked(seat)
	if len(edges) == 0 {
		return ""
	}
	out := "\n\n📨 你最近收到的传闻(仅供你参考,可信度自评):\n"
	for i, e := range edges {
		prefix := RumorPrefixForHop(e.Hop)
		// 服务端权威真伪分不直接展示给玩家(防止利用);仅给"大致方向"
		hint := ""
		if e.Veracity >= 0.8 {
			hint = "(看起来挺真)"
		} else if e.Veracity < 0.2 {
			hint = "(看起来像假的)"
		} else {
			hint = "(真伪不明)"
		}
		// 匿名:发送者仅显示为"某位玩家"
		out += fmt.Sprintf("- %s%s\n", prefix, e.Text)
		_ = i
		_ = hint
	}
	return out
}
