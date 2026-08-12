// Package werewolf — agent_memory_bridge.go: 狼人杀 Agent 持久化记忆(MEMORY.md)的房间侧集成。
//
// 2026-07-20 §131 新增。每局游戏结束、法官整局总结生成完成后
// (judge_summary_bridge.go 的 PersistSummary 成功路径),由本文件对本局
// 每个 bot 模型异步发起一次自我迭代:
//
//	PersistSummary → m.IterateAgentMemoriesAsync(r, judgeSummary)
//	  for each unique modelKey in r.seatModelKeys:
//	    goroutine:
//	      1. store.Load 读旧 memory_md(无则空)
//	      2. BuildIterationPrompt(旧记忆 + 本局该座位事实 + 法官总结;>80K 加压缩指令)
//	      3. registry.Get(modelKey) → provider.Chat(90s timeout,MaxTokens 取 config)
//	      4. ValidateMemorySections 不通过 → FallbackMerge 规则兜底
//	      5. >100K → HardTruncateMemory 硬截断
//	      6. store.SaveIterated(version 乐观锁写回)
//
// 硬约束:
//   - 异步且不阻塞:失败仅 logger.Warn,不影响冷却期/重开投票/关门流程
//     (对齐 §118 "异步持久化不阻塞游戏流");
//   - goroutine 入口 defer recover,绝不 panic;
//   - 同一模型的并发迭代用 manager 级 sync.Map[string]*sync.Mutex 单飞 +
//     DB version 乐观锁双保险(重开局原地复用时新旧两局可能相邻触发);
//   - goroutine 内访问 r.State / r.seatModelKeys 一律走 lockRoomBriefly 快照,
//     绝不裸持 r.mu 跨 LLM 调用(§92a)。
//
// 详见 docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md。
package werewolf

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	agentroot "LsmWebGame/agent"
	"LsmWebGame/agent/wwplayer"
	"LsmWebGame/config"
	"LsmWebGame/llm"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// AgentMemoryStore 是 agent 持久化记忆的 DB 存取窄接口。
// service.AgentMemoryService 天然实现;werewolf 包只依赖此接口,
// 不依赖 service 具体类型(便于测试桩注入)。
type AgentMemoryStore interface {
	Load(ctx context.Context, modelKey string) (string, error)
	SaveIterated(ctx context.Context, modelKey, newMD, gameID string) error
}

// SetAgentMemoryStore 注入持久化记忆存取层。2026-07-20 §131 新增。
// nil 时整链 no-op(测试 / 老代码路径)。main.go 装配:
//
//	memorySvc := service.NewAgentMemoryService(gormDB)
//	gameSvcWs.WerewolfManager().SetAgentMemoryStore(memorySvc)
func (m *WerewolfManager) SetAgentMemoryStore(store AgentMemoryStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agentMemoryStore = store
}

// memoryMuFor 返回指定 modelKey 的迭代单飞锁(manager 级 sync.Map,
// per-model *sync.Mutex)。同一模型同时只在一个房间,但重开投票原地复用时
// 新旧两局可能相邻触发 → 单飞锁 + DB version 乐观锁双保险。
func (m *WerewolfManager) memoryMuFor(modelKey string) *sync.Mutex {
	v, _ := m.memoryMus.LoadOrStore(modelKey, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// cfgAgentMemoryEnabled 安全读取 config.WerewolfConfig.AgentMemoryEnabled。
// 默认 true(与 config applyDefaults 对齐);测试环境 config.Load() panic 时
// 按"关闭"兜底,避免无配置环境下误触发 LLM 调用。
func cfgAgentMemoryEnabled() (enabled bool) {
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return false
	}
	return c.Werewolf.AgentMemoryEnabled
}

// cfgAgentMemoryMaxTokens 安全读取 config.WerewolfConfig.AgentMemoryMaxTokens。
// 默认 2048;测试环境 config.Load() panic 时按默认值兜底。
func cfgAgentMemoryMaxTokens() (n int) {
	n = 2048
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return 2048
	}
	if c.Werewolf.AgentMemoryMaxTokens > 0 {
		n = c.Werewolf.AgentMemoryMaxTokens
	}
	return n
}

// IterateAgentMemoriesAsync 在法官整局总结落地后,对本局每个 bot 模型
// (r.seatModelKeys 去重)异步发起一次持久化记忆自我迭代。
//
// 调用方不要求持锁;本函数内部先用 lockRoomBriefly 快照 seatModelKeys,
// 之后每模型起一个 goroutine 独立完成 Load→LLM→Save 全链。
// 开关关闭 / store 未注入 / 无 bot 座位时 no-op。
//
// 2026-07-20 §131 新增。
func (m *WerewolfManager) IterateAgentMemoriesAsync(r *WerewolfRoom, judgeSummary string) {
	if r == nil {
		return
	}
	m.mu.RLock()
	store := m.agentMemoryStore
	registry := m.registry
	m.mu.RUnlock()
	if store == nil || registry == nil {
		return
	}
	if !cfgAgentMemoryEnabled() {
		return
	}

	// 锁内快照 seatModelKeys(去重),锁外异步 — 避免 goroutine 内裸持 r.mu。
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		logger.L().Warn("werewolf: agent memory iterate snapshot lock contention, skipping",
			zap.String("room_id", r.RoomID))
		return
	}
	seatModels := make(map[int]string, len(r.seatModelKeys))
	for seat, mk := range r.seatModelKeys {
		if mk != "" {
			seatModels[seat] = mk
		}
	}
	roomID := r.RoomID
	r.mu.Unlock()

	// 去重:同一 model_key 只迭代一次(7 bot 可能 2 个座位共享模型)。
	unique := make(map[string]int, len(seatModels))
	for seat, mk := range seatModels {
		if _, ok := unique[mk]; !ok {
			unique[mk] = seat
		}
	}
	for modelKey := range unique {
		seat := unique[modelKey]
		go m.iterateOneModelMemory(r, roomID, modelKey, seat, judgeSummary, store, registry)
	}
}

// iterateOneModelMemory 执行单个模型的记忆迭代全链。
// 全程失败仅 logger.Warn;goroutine 入口 defer recover 兜底,绝不 panic。
func (m *WerewolfManager) iterateOneModelMemory(
	r *WerewolfRoom, roomID, modelKey string, seat int, judgeSummary string,
	store AgentMemoryStore, registry *llm.Registry,
) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.L().Warn("werewolf: agent memory iterate panicked",
				zap.String("room_id", roomID),
				zap.String("model_key", modelKey),
				zap.Any("recover", rec))
		}
	}()

	// 单飞锁:同一模型跨房间/跨局的迭代串行化(DB version 乐观锁之外的第二保险)。
	mu := m.memoryMuFor(modelKey)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// 1. 读旧记忆(行不存在返回 "")。
	oldMD, err := store.Load(ctx, modelKey)
	if err != nil {
		logger.L().Warn("werewolf: agent memory load failed",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		return
	}

	// 2. 快照本局该座位事实(简短文本,锁内构造锁外用)。
	seatFacts := m.buildSeatMemoryFacts(r, roomID, seat, modelKey)

	// 3. 构造迭代 prompt;旧记忆 > 80K 时要求 LLM 主动瘦身。
	compress := len(oldMD) > wwplayer.MemoryCompressThresholdBytes
	prompt := wwplayer.BuildIterationPrompt(oldMD, seatFacts, judgeSummary, compress)

	// 4. 用该模型自己的 provider 调 LLM。
	provider, key, err := registry.Get(modelKey)
	if err != nil {
		logger.L().Warn("werewolf: agent memory registry.Get failed",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		return
	}
	req := llm.LLMRequest{
		Model:     modelKey,
		Messages:  []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: prompt}}}},
		MaxTokens: cfgAgentMemoryMaxTokens(),
		// 2026-08-06 §AgentClassName 增强:记忆迭代是独立的 Agent 类别
		// (读旧记忆 + 本局事实 + 法官总结 → 生成新 MEMORY.md),与玩家 Bot /
		// 法官调用分开计费/归因。常量集中在 ServerGo/agent/class_names.go。
		AgentClassName: string(agentroot.AgentClassWerewolfMemoryIter),
	}
	resp, err := provider.Chat(ctx, key, req)
	var newMD string
	switch {
	case err != nil:
		// LLM 调用失败 → 规则兜底:旧记忆 + 追加一行本局 note。
		logger.L().Warn("werewolf: agent memory LLM iterate failed, fallback merge",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		newMD = wwplayer.FallbackMerge(oldMD, fmt.Sprintf("本局(%s)自我迭代 LLM 调用失败,仅保留旧记忆", roomID))
	default:
		text := strings.TrimSpace(resp.Text())
		if text == "" || !wwplayer.ValidateMemorySections(text) {
			// 输出不合格(空 / 4 段标题不全) → 规则兜底,不丢旧记忆。
			logger.L().Warn("werewolf: agent memory LLM output invalid, fallback merge",
				zap.String("room_id", roomID),
				zap.String("model_key", modelKey),
				zap.Bool("empty", text == ""))
			newMD = wwplayer.FallbackMerge(oldMD, fmt.Sprintf("本局(%s)自我迭代输出不合格,仅保留旧记忆", roomID))
		} else {
			newMD = text
		}
	}

	// 5. 超 100K 硬上限 → rune 安全硬截断(最后兜底;主路径是 LLM 主动瘦身)。
	if len(newMD) > wwplayer.MemoryMaxBytes {
		logger.L().Warn("werewolf: agent memory exceeds 100K, hard truncate",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Int("bytes", len(newMD)))
		newMD = wwplayer.HardTruncateMemory(newMD, wwplayer.MemoryMaxBytes)
	}

	// 6. 乐观锁写回(冲突重读合并重试 1 次在 store 内完成)。
	if err := store.SaveIterated(ctx, modelKey, newMD, roomID); err != nil {
		logger.L().Warn("werewolf: agent memory save failed",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		return
	}
	logger.L().Info("werewolf: agent memory iterated",
		zap.String("room_id", roomID),
		zap.String("model_key", modelKey),
		zap.Int("old_bytes", len(oldMD)),
		zap.Int("new_bytes", len(newMD)))
}

// buildSeatMemoryFacts 在 lockRoomBriefly 快照下构造"本局该座位事实"简短文本。
// 从 BuildSummaryInputLocked 提取该座位的角色 / 阵营 / 胜负 / 死活等信息,
// 作为迭代 prompt 的【本局事实】输入。锁争用时返回简短降级文本。
//
// §20260810-04 U4 — 在末尾追加本局真实「座位 → model_key」映射(数据源
// r.seatModelKeys),让记忆迭代第 3 段「其他模型特点分析」有据可依,
// 关闭 LongCat-D4 报告的「结构性虚构」问题(LLM 没有数据支撑只能编造)。
func (m *WerewolfManager) buildSeatMemoryFacts(r *WerewolfRoom, roomID string, seat int, modelKey string) string {
	fallback := fmt.Sprintf("房间 %s;你是 %d 号位(模型 %s)", roomID, seat+1, modelKey)
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		return fallback + "(锁争用,无本局详情)"
	}
	defer r.mu.Unlock()
	if r.State == nil {
		return fallback
	}
	in := r.BuildSummaryInputLocked()
	var sb strings.Builder
	fmt.Fprintf(&sb, "房间 %s;你坐 %d 号位(模型 %s);", roomID, seat+1, modelKey)
	if role, ok := in.Roles[seat]; ok {
		fmt.Fprintf(&sb, "本局角色 %s;", role)
	}
	switch in.Winner {
	case "wolf":
		sb.WriteString("胜方: 狼人阵营;")
	case "good":
		sb.WriteString("胜方: 好人阵营;")
	default:
		sb.WriteString("胜方: 未定;")
	}
	alive := false
	for _, s := range in.AliveSeats {
		if s == seat {
			alive = true
			break
		}
	}
	if alive {
		sb.WriteString("本局存活到终局;")
	} else {
		sb.WriteString("本局中途出局;")
	}
	fmt.Fprintf(&sb, "本局共进行约 %d 天。", in.DayNumber)
	// §20260810-04 U4 — 注入本局对手真实模型映射(同局多模型同台的元数据)。
	// 数据已在 r.seatModelKeys 锁内快照中,直接遍历;跳过自己座位(避免重复)。
	if len(r.seatModelKeys) > 0 {
		fmt.Fprintf(&sb, " 本局对手模型映射:")
		for s, mk := range r.seatModelKeys {
			if s == seat || mk == "" {
				continue
			}
			fmt.Fprintf(&sb, " %d号=%s;", s+1, mk)
		}
	} else {
		sb.WriteString(" 本局无其他模型玩家。")
	}
	return sb.String()
}
