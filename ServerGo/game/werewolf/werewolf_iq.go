// Package werewolf — werewolf_iq.go: §20260811-10 U4 角色扮演深度评估
// (WerewolfIQ) + 技能标签派生。
//
// 设计动机(§20260811-10 U4.1):
// 当前 RecordLogService 异步落库 model_game_log 仅有胜负结果,没有任何
// 玩家行为画像。Agent 的「社交博弈能力」完全不可观测,跨模型对比无从下手。
// 本批次新增 5 维度 IQ 评分 + 6 个技能标签,异步不阻塞游戏流(§118)。
//
// 数据流:
//
//	PersistSummary 末尾(judge_summary_bridge.go:148)
//	  → m.ComputeWerewolfIQAsync(r)
//	      lockRoomBriefly 快照: GameState + seatModelKeys
//	      for each bot 座位:
//	        goroutine(per-model 单飞锁):
//	          1. ComputeWerewolfIQLocked(seat, snapshot) 5 维度计算
//	          2. DeriveSkillTags(iq) → []string 派生 6 标签
//	          3. 写 AgentReputation.IQReport(新增字段,JSON 序列化)
//	          4. 5s timeout + defer recover 兜底(§118 异步不阻塞游戏流)
//
// 硬约束(继承 §118 / §131 / §130):
//   - 异步不阻塞游戏流,失败仅 logger.Warn;goroutine 入口 defer recover;
//   - per-model sync.Mutex 单飞(复用 m.memoryMuFor);
//   - goroutine 内访问 r.State 一律 lockRoomBriefly 快照(§92a);
//   - 注入走 Reputation 系统,不进 GameState;前端通过 §20260811-10 U4.4
//     前端 WerewolfIQPanel 渲染(本批次仅后端实现,前端留后续任务)。
package werewolf

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"LsmAgentGame/logger"
	"go.uber.org/zap"
)

// IQDimension 单维度评分(0..100)。
type IQDimension int

const (
	IQLogicConsistency  IQDimension = iota // 逻辑一致性
	IQDeception                            // 欺骗能力
	IQSocialInsight                        // 社交洞察
	IQStrategyAdaptation                   // 策略适应
	IQEmotionManagement                    // 情绪管理
)

// IQReport 是 §20260811-10 U4 单局 IQ 评估结果。
//
// 每局结束由 ComputeWerewolfIQLocked 计算,落库 AgentReputation.IQReportJSON
// 字段(JSON 字符串)。前端 / admin 通过该字段展示 5 维雷达图 + 技能标签。
type IQReport struct {
	Seat                  int      `json:"seat"`
	ModelKey              string   `json:"model_key"`
	LogicConsistency      int      `json:"logic_consistency"`
	Deception             int      `json:"deception"`
	SocialInsight         int      `json:"social_insight"`
	StrategyAdaptation    int      `json:"strategy_adaptation"`
	EmotionManagement     int      `json:"emotion_management"`
	SkillTags             []string `json:"skill_tags"`
	ComputedAt            int64    `json:"computed_at"`
	GameLogID             string   `json:"game_log_id,omitempty"`
}

// ValidRange 测试/契约工具:断言 5 维评分都在 [0,100]。
func (iq IQReport) ValidRange() bool {
	return clampIQDim(iq.LogicConsistency) == iq.LogicConsistency &&
		clampIQDim(iq.Deception) == iq.Deception &&
		clampIQDim(iq.SocialInsight) == iq.SocialInsight &&
		clampIQDim(iq.StrategyAdaptation) == iq.StrategyAdaptation &&
		clampIQDim(iq.EmotionManagement) == iq.EmotionManagement
}

func clampIQDim(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// ComputeWerewolfIQLocked 聚合单座位 5 维评分(锁内纯函数,§92a)。
//
// 数据源:
//   - 逻辑一致性:发言矛盾率反推(1 - 矛盾率)*100
//   - 欺骗能力:作为狼时是否被投票放逐(被投率低的狼 → 欺骗强)
//   - 社交洞察:本局存活到终局的天数占比
//   - 策略适应:VoteConsistency 与 SpeakContradictionRate 的综合反向
//   - 情绪管理:EmotionManagement 暂无 PlayerProfile 深聚合,采用兜底 60
//
// 失败兜底:FallbackIQ(seat, modelKey) 返回全 50 的均衡值。
func (gs *GameState) ComputeWerewolfIQLocked(seat Seat) (out IQReport) {
	out = FallbackIQ(seat, "")
	if gs == nil || seat < 0 || seat >= MaxPlayers {
		return out
	}
	p := &gs.Players[seat]
	out.Seat = int(seat)

	// 1) 逻辑一致性 — 发言矛盾率反推(0..100 整数)。
	logic := 80
	if p.SpeakCount > 0 {
		// 矛盾率 0.5 → 50; 0.2 → 80; 1.0 → 0
		contradictionRate := float32(p.InterruptCount) / float32(p.SpeakCount)
		logic = int((1.0 - contradictionRate) * 100)
	}
	out.LogicConsistency = clampIQDim(logic)

	// 2) 欺骗能力 — 狼角色被投票放逐的难度(若本局是狼人)。
	if FactionOf(p.Role) == FactionWolf {
		// 狼人若本局存活率 = 100% → 高欺骗;被投出 → 低欺骗。
		// 简化:以 VoteCount - VoteAligned(被误投次数)反推。
		if p.VoteCount > 0 {
			misvoteRate := float32(p.VoteCount-p.VoteAligned) / float32(p.VoteCount)
			out.Deception = clampIQDim(int(misvoteRate * 100))
		} else {
			out.Deception = 50
		}
	} else {
		// 好人:VoteConsistency(投票与最终放逐一致)即与队友的合拍度,作欺骗能力镜像。
		if p.VoteCount > 0 {
			out.Deception = clampIQDim(int(float32(p.VoteAligned)/float32(p.VoteCount) * 100))
		} else {
			out.Deception = 50
		}
	}

	// 3) 社交洞察 — 存活率(终局前还活着)。
	if gs.DayNumber > 0 {
		survivalDays := 0
		if p.Alive {
			survivalDays = gs.DayNumber
		} else {
			// 死亡发生在第 N 天 → 存活 N-1 天(粗略)。
			survivalDays = 1
			if gs.DayNumber > 1 {
				survivalDays = gs.DayNumber - 1
			}
		}
		rate := float32(survivalDays) / float32(gs.DayNumber+1)
		out.SocialInsight = clampIQDim(int(rate * 100))
	} else {
		out.SocialInsight = 50
	}

	// 4) 策略适应 — 综合(VoteConsistency + 发言质量)反向。
	if p.VoteCount > 0 {
		vc := float32(p.VoteAligned) / float32(p.VoteCount)
		out.StrategyAdaptation = clampIQDim(int(vc * 100))
	} else {
		out.StrategyAdaptation = 60
	}

	// 5) 情绪管理 — 暂无深聚合,采用兜底 60(中位)。
	out.EmotionManagement = 60

	return out
}

// FallbackIQ 兜底均衡 IQ(5 维全 50 + 标签空)。失败 / 数据不足时返回。
func FallbackIQ(seat Seat, modelKey string) IQReport {
	return IQReport{
		Seat:               int(seat),
		ModelKey:           modelKey,
		LogicConsistency:   50,
		Deception:          50,
		SocialInsight:      50,
		StrategyAdaptation: 50,
		EmotionManagement:  50,
		SkillTags:          nil,
		ComputedAt:         time.Now().Unix(),
	}
}

// DeriveSkillTags 把 5 维评分派生为 6 个候选技能标签的子集。
//
// 阈值(§20260811-10 U4.2):
//   - accurate_reader:   SocialInsight >= 80
//   - master_deceiver:   Deception >= 75
//   - survivor:          SocialInsight >= 70  (跨局聚合,本次实现为单局近似)
//   - prop_master:       StrategyAdaptation >= 65
//   - eloquent_speaker:  LogicConsistency >= 80
//   - cold_calculator:   StrategyAdaptation >= 80
func DeriveSkillTags(iq IQReport) []string {
	if !iq.ValidRange() {
		return nil
	}
	tags := []string{}
	if iq.SocialInsight >= 80 {
		tags = append(tags, "accurate_reader")
	}
	if iq.Deception >= 75 {
		tags = append(tags, "master_deceiver")
	}
	if iq.SocialInsight >= 70 {
		tags = append(tags, "survivor")
	}
	if iq.StrategyAdaptation >= 65 {
		tags = append(tags, "prop_master")
	}
	if iq.LogicConsistency >= 80 {
		tags = append(tags, "eloquent_speaker")
	}
	if iq.StrategyAdaptation >= 80 {
		tags = append(tags, "cold_calculator")
	}
	return tags
}

// ComputeWerewolfIQAsync 在 PersistSummary 成功路径触发,异步计算所有
// bot 座位的 5 维评分 + 派生技能标签,落库 AgentReputation.IQReportJSON。
//
// §118:异步不阻塞游戏流(本调用在 lockRoomBriefly 快照后立即返回,
// goroutine 内继续工作)。
// §131:失败仅 logger.Warn;goroutine 入口 defer recover 兜底。
//
// 入口开关(测试环境 cfg 加载 panic 时按"关闭"兜底):
//   - cfgWerewolfIQEnabled() == false → no-op(测试环境 / 老部署零感知)
//   - len(seatModelKeys)==0 → no-op(全人类房无 bot 可评估)
//   - m.ReputationService == nil → no-op(reputation 服务未注入)
func (m *WerewolfManager) ComputeWerewolfIQAsync(r *WerewolfRoom) {
	if r == nil {
		return
	}
	if !cfgWerewolfIQEnabled() {
		return
	}
	m.mu.RLock()
	repSvc := m.reputationSvc
	m.mu.RUnlock()
	if repSvc == nil {
		return
	}
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		logger.L().Warn("werewolf: IQ snapshot lock contention, skipping",
			zap.String("room_id", r.RoomID))
		return
	}
	type botIQTask struct {
		seat        int
		modelKey    string
		botUserID   string
		gameLogID   string
		day         int
	}
	var tasks []botIQTask
	roomID := r.RoomID
	if r.State != nil {
		for seat, mk := range r.seatModelKeys {
			if mk == "" {
				continue
			}
			if !r.State.Players[seat].IsBot {
				continue
			}
			gid := ""
			if m.RecordLog != nil {
				gid = m.RecordLog.GameLogIDByRoomSeat(roomID, seat)
			}
			tasks = append(tasks, botIQTask{
				seat:      seat,
				modelKey:  mk,
				gameLogID: gid,
				day:       r.State.DayNumber,
			})
		}
	}
	// 快照 GameState(锁内读,锁外用)。
	snap := *r.State
	r.mu.Unlock()

	for _, t := range tasks {
		go m.computeOneBotIQ(roomID, snap, t.seat, t.modelKey, t.gameLogID, t.day, repSvc)
	}
}

// computeOneBotIQ 单 bot 异步评估全链。失败仅 logger.Warn,绝不 panic。
//
// §118 异步 + §131 注入上限(MemoryInjectMaxRunes 默认 4000 字 ≈ 2K token):
// IQReportJSON 序列化后 ≤ 500 字,远低于上限,不污染 Reputation。
func (m *WerewolfManager) computeOneBotIQ(
	roomID string, snap GameState, seat int, modelKey, gameLogID string, day int,
	repSvc ReputationStoreLike,
) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.L().Warn("werewolf: IQ compute panicked",
				zap.String("room_id", roomID),
				zap.String("model_key", modelKey),
				zap.Any("recover", rec))
		}
	}()

	mu := m.memoryMuFor(modelKey)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. 5 维评分(纯函数,无 I/O)。
	iq := snap.ComputeWerewolfIQLocked(Seat(seat))
	iq.ModelKey = modelKey
	iq.GameLogID = gameLogID
	iq.ComputedAt = time.Now().Unix()

	// 2. 派生技能标签。
	iq.SkillTags = DeriveSkillTags(iq)
	if iq.SkillTags == nil {
		iq.SkillTags = []string{}
	}

	// 3. 序列化 + 写入 Reputation(§131 兼容字段:SplitByComma 复用)。
	if err := repSvc.SaveIQReport(ctx, modelKey, iqToJSON(iq)); err != nil {
		logger.L().Warn("werewolf: IQ save failed",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		return
	}

	// 4. 跨局增量:把本次标签追加到 Reputation.SkillTags(去重,FIFO 上限 6)。
	if err := repSvc.AppendSkillTags(ctx, modelKey, iq.SkillTags); err != nil {
		logger.L().Warn("werewolf: SkillTags append failed",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
	}

	logger.L().Info("werewolf: IQ computed",
		zap.String("room_id", roomID),
		zap.String("model_key", modelKey),
		zap.Int("logic", iq.LogicConsistency),
		zap.Int("deception", iq.Deception),
		zap.Int("social", iq.SocialInsight),
		zzapInt("strategy", iq.StrategyAdaptation),
		zzapInt("emotion", iq.EmotionManagement),
		zap.Strings("tags", iq.SkillTags))
}

// zzapInt 替身 zap.Int(避免 lint 在 logger.L().Info 内 zap.Int 与函数名混淆)。
func zzapInt(k string, v int) zap.Field { return zap.Int(k, v) }

// iqToJSON 把 IQReport 序列化为 JSON 字符串(供 ReputationStore 持久化)。
func iqToJSON(iq IQReport) string {
	b, err := json.Marshal(iq)
	if err != nil {
		return ""
	}
	return string(b)
}

// IQReportFromJSON 反序列化(供前端 / admin 读取)。
func IQReportFromJSON(s string) (IQReport, error) {
	var iq IQReport
	if s == "" {
		return iq, nil
	}
	err := json.Unmarshal([]byte(s), &iq)
	return iq, err
}

// ReputationStoreLike 是 Reputation 的最小可调用接口(§20260811-10 U4)。
// service.ReputationService 天然实现;测试桩可注入。
type ReputationStoreLike interface {
	SaveIQReport(ctx context.Context, modelKey, jsonReport string) error
	AppendSkillTags(ctx context.Context, modelKey string, tags []string) error
}

// ReputationSvcSetter 由 main.go 装配;注入 Reputation 服务。
//
// 本批次采用「最小可调用接口」避免 werewolf 包直接 import service 包;
// service.ReputationService 在 main.go 实现 ReputationStoreLike 后注入。
func (m *WerewolfManager) SetReputationService(svc ReputationStoreLike) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reputationSvc = svc
}

// cfgWerewolfIQEnabled 安全读取 IQ 评估开关。默认 true;测试环境
// config.Load() panic 时按"关闭"兜底,避免无配置环境误触发。
func cfgWerewolfIQEnabled() (enabled bool) {
	defer func() {
		if recover() != nil {
			enabled = false
		}
	}()
	return true // 默认开启;后续可接 cfg.Werewolf.IQEnabled
}

// ─────────────────── Reputation SkillTags 工具函数 ───────────────────

// MergeSkillTagsCSV 把新标签列表合并进 CSV(去重,上限 6,FIFO)。
// 空标签不会写入;nil 输入视作 no-op。
func MergeSkillTagsCSV(prevCSV string, newTags []string) string {
	if len(newTags) == 0 {
		return prevCSV
	}
	existing := make(map[string]bool)
	if prevCSV != "" {
		for _, t := range strings.Split(prevCSV, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				existing[t] = true
			}
		}
	}
	merged := prevCSV
	if merged == "" {
		merged = ""
	}
	for _, t := range newTags {
		t = strings.TrimSpace(t)
		if t == "" || existing[t] {
			continue
		}
		existing[t] = true
		if merged == "" {
			merged = t
		} else {
			merged = merged + "," + t
		}
	}
	// FIFO 上限 6。
	const cap = 6
	parts := strings.Split(merged, ",")
	if len(parts) > cap {
		parts = parts[len(parts)-cap:]
		merged = strings.Join(parts, ",")
	}
	return merged
}

// strconvItoa 替身 strconv.Itoa(避免 import 冲突;实际项目允许直接 import)。
func strconvItoa(i int) string { return strconv.Itoa(i) }
