// Package werewolf — prop_engine.go: 道具使用引擎。
//
// 处理道具使用的完整流程：前置校验 → 金币扣款 → 分配 → 注入 → 中招判定 → 日志。
// 本文件是道具系统的核心协调层，被 WerewolfManager 调用（人类玩家路径）
// 和 agentRunner 调用（Agent 路径）。
//
// 金币分配（设计文档 §1.2）：
//   - 50% 回滚到游戏彩池（r.propPotBonus，结算时按比例发放给胜方）
//   - 30% 系统吸收（永久销毁）
//   - 20% 补偿被击中玩家（若中招则发放）
//
// 中招判定：服务端权威骰点（不依赖 LLM）。中招后效果 = 在 GameContext 中加入
// "干扰信号"，LLM 自己决定如何响应（保护 Agent 自主权）。
//
// 2026-07-21 道具系统设计（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md）。
package werewolf

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"LsmWebGame/logger"
	"LsmWebGame/models"
	"LsmWebGame/service"
	"LsmWebGame/util"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PropUseRequest 是道具使用的请求参数。
type PropUseRequest struct {
	RoomID      string // 房间 ID
	FromSeat    int    // 使用者座位（0-indexed）
	FromUserID  string // 使用者 user_id
	ToSeat      int    // 目标座位（0-indexed，AOE 时填 -1）
	ToUserID    string // 目标 user_id（AOE 时填 ""）
	PropKey     string // 道具 key
	Payload     string // 使用者自定义文本（可选）
	RoleTo      string // 目标角色（用于效果判定）
	PhaseAtUse  string // 使用时的阶段
	RoundAtUse  int    // 使用时的轮数
}

// PropUseResult 是道具使用的结果。
type PropUseResult struct {
	Success      bool   // 是否成功使用
	ErrorCode    int    // 错误码（失败时）
	ErrorMsg     string // 错误信息（失败时）
	PricePaid    int64  // 实际支付金币
	PotReturn    int64  // 回滚彩池金额
	SystemAbsorb int64  // 系统吸收金额
	TargetCompens int64 // 目标补偿金额（若中招）
	Hit          bool   // 是否中招
	InjectResult PropInjectResult
}

// PropEngine 是道具使用引擎。
type PropEngine struct {
	db        *gorm.DB
	walletSvc *service.WalletService
	catalog   *PropCatalog
	// §20260810-11 P1 — 终局奖励服务(可选,空时不打折)
	rewardSvc *SettlementRewardService
}

// NewPropEngine 构造道具引擎。
func NewPropEngine(db *gorm.DB, walletSvc *service.WalletService, catalog *PropCatalog) *PropEngine {
	return &PropEngine{
		db:        db,
		walletSvc: walletSvc,
		catalog:   catalog,
	}
}

// SetRewardService 注入奖励服务(可选)。
func (e *PropEngine) SetRewardService(svc *SettlementRewardService) {
	e.rewardSvc = svc
}

// UseProp 执行道具使用流程。
//
// 流程：
//  1. 前置校验（道具存在/启用/阶段/冷却/次数上限/余额）
//  2. 金币扣款（走 WalletService.Debit）
//  3. 分配（彩池/系统/补偿）
//  4. 生成注入文本
//  5. 中招判定（服务端骰点）
//  6. 写使用日志
func (e *PropEngine) UseProp(ctx context.Context, req PropUseRequest, r *WerewolfRoom) PropUseResult {
	// 1. 前置校验
	entry, ok := e.catalog.GetEnabled(req.PropKey)
	if !ok {
		return PropUseResult{Success: false, ErrorCode: 40001, ErrorMsg: "道具不存在或已禁用"}
	}
	if e.walletSvc == nil {
		return PropUseResult{Success: false, ErrorCode: 50001, ErrorMsg: "钱包服务不可用"}
	}

	// 阶段限制：仅白天发言阶段可使用
	if !isPropUsablePhase(req.PhaseAtUse) {
		return PropUseResult{Success: false, ErrorCode: 40002, ErrorMsg: "当前阶段不可使用道具（仅白天发言阶段可用）"}
	}

	// 目标存活校验
	if !entry.IsAOE && (req.ToSeat < 0 || !isSeatAlive(r, req.ToSeat)) {
		return PropUseResult{Success: false, ErrorCode: 40003, ErrorMsg: "目标玩家已死亡或不存在"}
	}

	// 2026-08-07 §20260807-04 P0-3:人类反制道具(TargetCamp=="human")的目标是
	// 真人玩家,注入文本对真人无意义,因此跳过注入文本生成;且目标必须是非 bot。
	if entry.TargetCamp == "human" {
		if req.ToSeat < 0 || req.ToSeat >= len(r.State.Players) {
			return PropUseResult{Success: false, ErrorCode: 40009, ErrorMsg: "人类反制道具必须指定目标座位"}
		}
		if r.State.Players[req.ToSeat].IsBot {
			return PropUseResult{Success: false, ErrorCode: 40009, ErrorMsg: "人类反制道具只能对真人玩家使用"}
		}
	}

	// 阵营保护：狼人不能对狼人队友使用身份暴露类道具
	if !entry.IsAOE && isWolfTeammate(r, req.FromSeat, req.ToSeat) && isExposeProp(entry.InjectType) {
		return PropUseResult{Success: false, ErrorCode: 40004, ErrorMsg: "不能对狼人队友使用身份暴露类道具"}
	}

	// 冷却校验
	if r.isPropCooldownLocked(req.FromSeat, entry.CooldownSec) {
		remain := r.propCooldownRemainLocked(req.FromSeat, entry.CooldownSec)
		return PropUseResult{Success: false, ErrorCode: 40005, ErrorMsg: fmt.Sprintf("道具冷却中（剩余 %d 秒）", remain)}
	}

	// 次数上限校验
	if r.propCountForSeatLocked(req.FromSeat) >= entry.MaxPerGame {
		return PropUseResult{Success: false, ErrorCode: 40006, ErrorMsg: fmt.Sprintf("本局已使用 %d 个道具（上限 %d）", r.propCountForSeatLocked(req.FromSeat), entry.MaxPerGame)}
	}

	// 余额校验
	bal, err := e.walletSvc.GetBalance(ctx, req.FromUserID)
	if err != nil {
		return PropUseResult{Success: false, ErrorCode: 50002, ErrorMsg: "查询余额失败"}
	}
	if bal < entry.Price {
		return PropUseResult{Success: false, ErrorCode: 40007, ErrorMsg: fmt.Sprintf("余额不足（需 %d 金币，当前 %d）", entry.Price, bal)}
	}

	// 2. 金币扣款前记录价格（供 v2 全局预算校验 + 彩池分配使用）。
	price := entry.Price
	// §20260810-11 P1 — 终局奖励折扣:若用户持有胜方折扣券,按 discount 打折。
	// 折扣计算后立即 MarkUsed,避免重复使用。
	if e.rewardSvc != nil && r != nil {
		if rw := e.rewardSvc.Lookup(ctx, req.FromUserID, req.RoomID, time.Now()); rw != nil {
			switch rw.RewardType {
			case RewardTypeVictoryDiscount:
				if rw.Discount > 0 && rw.Discount < 1 {
					discounted := int64(float64(price) * rw.Discount)
					if discounted < price {
						price = discounted
					}
					_ = e.rewardSvc.MarkUsed(ctx, req.FromUserID, req.RoomID)
				}
			case RewardTypeConsolationProp:
				// 败方安慰包:仅对 prop_key 匹配的目标道具免费;不匹配则忽略。
				if rw.PropKey == entry.PropKey {
					price = 0
					_ = e.rewardSvc.MarkUsed(ctx, req.FromUserID, req.RoomID)
				}
			}
		}
	}
	// v2：全局道具预算校验（道具是稀缺资源，逼人类/Agent 博弈）。
	if r != nil && price > 0 {
		if maxBudget := r.roomPropBudget(); maxBudget > 0 && r.roomPropBudgetUsed+price > maxBudget {
			return PropUseResult{Success: false, ErrorCode: 40008, ErrorMsg: fmt.Sprintf("本局道具全局预算耗尽（剩余 %d 币，需 %d 币）", maxBudget-r.roomPropBudgetUsed, price)}
		}
	}

	// 扣款
	debitErr := e.walletSvc.Debit(ctx, req.FromUserID,
		"werewolf_prop_purchase", "werewolf_game", req.RoomID, "werewolf",
		fmt.Sprintf("狼人杀道具「%s」购买", entry.NameZh), price)
	if debitErr != nil {
		logger.L().Error("prop purchase debit failed",
			zap.String("user_id", req.FromUserID), zap.Int64("price", price),
			zap.String("prop", req.PropKey), zap.Error(debitErr))
		return PropUseResult{Success: false, ErrorCode: 50003, ErrorMsg: "扣款失败"}
	}

	// 3. 分配（v4 §13.2 经济档位感知）
	// v3 硬切 50/30/20 在通胀房间无法抑制道具刷屏;v4 改为按房间总金币存量
	// 动态调整销毁比例(Health=30%/Caution=40%/Danger=50%)。
	tier := EconHealth
	if r != nil {
		tier = ComputeEconTier(r.roomTotalCoin())
	}
	tierSpec := GetEconTierSpec(tier)
	potReturn := price * int64(tierSpec.PotReturnPct) / 100
	systemAbsorb := price * int64(tierSpec.SystemAbsorbPct) / 100
	// 目标补偿 = 余数（避免整数除法丢精度;合计恒等于 price）
	targetCompens := price - potReturn - systemAbsorb

	// 4. 生成注入文本（v2：按 prop_key 走 InjectRegistry，保证 DB 新道具无需改代码即可生成）。
	// 2026-08-07 §20260807-04 P0-3:人类反制道具的目标不是 Agent,注入文本对人类
	// 无意义,跳过生成(避免无意义文本被入队)。Agent 使用者的回执信息由
	// 调用方的「人类反制」专属路径(含 HumanDebuff 落地 + propHitSummary)承担。
	var injResult PropInjectResult
	if entry.TargetCamp != "human" {
		toFaction := ""
		if !entry.IsAOE && req.ToSeat >= 0 && req.ToSeat < len(r.State.Roles) {
			toFaction = factionString(FactionOf(r.State.Roles[req.ToSeat]))
		}
		injResult = GenerateInjectByKey(entry.ResolveInjectGenKey(), req.FromSeat, req.ToSeat, req.Payload, req.RoleTo, toFaction)
	}

	// 5. 中招判定
	hit := e.rollHit(entry, r, req.ToSeat, bal)

	// 6. 若中招：发放目标补偿 + 注入文本与干扰信号入队。
	if hit && !entry.IsAOE && req.ToUserID != "" {
		compErr := e.walletSvc.Credit(ctx, req.ToUserID,
			"werewolf_prop_compensation", "werewolf_game", req.RoomID, "werewolf",
			fmt.Sprintf("被道具「%s」击中补偿", entry.NameZh), targetCompens)
		if compErr != nil {
			logger.L().Warn("prop target compensation credit failed",
				zap.String("user_id", req.ToUserID), zap.Int64("amount", targetCompens),
				zap.Error(compErr))
			// 补偿失败不阻塞流程，仅 log
		}
	}

	// §20260811-10 U1 / U2 — 命中后的道具特化落地。
	// §92a:此处不持 r.mu(PropEngine 自身无锁),调用方持锁调用本函数;
	// 落地函数内部用各自的 *Locked 变体,在调用方已有的锁内完成。
	//
	// 注:本节逻辑在 hit==true 后执行;若失败兜底(mirror_check 100% 必中,
	// behavior_analyze 100% 必中),即便 hit==false 也不走特化路径。
	if hit {
		switch entry.InjectType {
		case PropMirrorCheck:
			// §20260811-10 U1 — 照妖镜:标记目标 bot 下一次 LLM 必须写真实身份。
			// 调用方需持 r.mu;此处仅调用公共路径(内部 SetMirrorExposeActiveLocked)。
			if r != nil && req.ToSeat >= 0 && req.ToSeat < MaxPlayers {
				r.SetMirrorExposeActiveLocked(Seat(req.ToSeat))
			}
		case PropBehaviorAnalyze:
			// §20260811-10 U2 — 心理侧写:聚合 4 维画像推送给购买者。
			// 纯查询:不写 propInjectQueue,不进 chat 表,仅通过 prop.behavior_report 单推。
			if r != nil && req.ToSeat >= 0 && req.ToSeat < MaxPlayers {
				report := r.ComputeBehaviorReportLocked(Seat(req.ToSeat))
				r.SetPendingBehaviorReportLocked(report)
			}
		}
	}

	// v2：命中后把注入文本 + 干扰信号入队（PropEngine 无锁，由调用方持锁后消费）。
	// enqueuePropInjectEntry 由调用方（agent_runner / ws handler）在获得结果后锁内入队，
	// 这里只把信息附加到返回值，让调用方一起处理。
	// （保留 injResult 给日志用；调用方若有 room 引用可额外入队——见 enqueuePropInjectEntry 辅助。）

	// 7. 更新房间级状态（price 完整价格用于 v2 全局/个人预算累加）。
	r.recordPropUseLocked(req.FromSeat, price, potReturn)

	// 8. 写使用日志
	logErr := e.writePropUsageLog(ctx, req, price, potReturn, systemAbsorb, targetCompens, hit, injResult.EffectHint)
	if logErr != nil {
		logger.L().Warn("prop usage log write failed", zap.Error(logErr))
		// 日志失败不阻塞流程
	}

	logger.L().Info("prop used",
		zap.String("room_id", req.RoomID),
		zap.Int("from_seat", req.FromSeat),
		zap.Int("to_seat", req.ToSeat),
		zap.String("prop", req.PropKey),
		zap.Int64("price", price),
		zap.Bool("hit", hit))

	return PropUseResult{
		Success:       true,
		PricePaid:     price,
		PotReturn:     potReturn,
		SystemAbsorb:  systemAbsorb,
		TargetCompens: targetCompens,
		Hit:           hit,
		InjectResult:  injResult,
	}
}

// 中招率修正常量（设计文档 docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §1.3）。
const (
	// propRichAuraBalance 触发「富人光环」修正的使用者余额门槛。
	propRichAuraBalance int64 = 2000
	// propRichAuraBonus 「富人光环」中招率加成（绝对百分点）。
	propRichAuraBonus = 5
	// propHitRateCap 中招率硬上限 —— 防止修正叠加后道具变成必中。
	propHitRateCap = 70
)

// rollHit 服务端权威骰点判定是否中招。
// 基础中招率 + 修正系数（目标 consecutiveFailures > 2 → +10%，使用者余额 > 2000 → +5%）。
//
// §20260810-02 E1 修复：「使用者余额 > 2000 → +5%」此前只存在于本函数注释与设计
// 文档 §1.3 中，代码从未实现（K3-Surpport-01 §1 F4）。userBalance 由调用方传入
// —— UseProp 在余额校验时已经取到（见 :133），无需二次查询钱包。
func (e *PropEngine) rollHit(entry *PropCatalogEntry, r *WerewolfRoom, toSeat int, userBalance int64) bool {
	rate := entry.BaseHitRate
	// 修正：目标心态崩了（consecutiveFailures > 2）→ +10%
	if toSeat >= 0 && toSeat < len(r.State.Players) {
		if r.State.Players[toSeat].IsBot {
			// 通过 BotAgents 取 consecutiveFailures（若可访问）
			if agent, ok := r.BotAgents[toSeat]; ok {
				if agent.ConsecutiveFailures() > 2 {
					rate += 10
				}
			}
		}
	}
	// 修正：使用者「富人光环」（余额 > 2000）→ +5%（设计文档 §1.3 心理暗示）
	if userBalance > propRichAuraBalance {
		rate += propRichAuraBonus
	}
	if rate > propHitRateCap {
		rate = propHitRateCap
	}
	if rate < 0 {
		rate = 0
	}
	roll := rand.Intn(100) // 0-99
	return roll < rate
}

// writePropUsageLog 写道具使用日志到 DB。
func (e *PropEngine) writePropUsageLog(ctx context.Context, req PropUseRequest, price, potReturn, systemAbsorb, targetCompens int64, hit bool, effectText string) error {
	if e.db == nil {
		return nil
	}
	if len(effectText) > 200 {
		effectText = effectText[:200]
	}
	log := models.TLsmGamePropUsageLog{
		ID:            util.NewUUID(),
		RoomID:        req.RoomID,
		PropID:        req.PropKey, // 用 prop_key 作为标识（简化）
		FromSeat:      req.FromSeat,
		FromUserID:    req.FromUserID,
		ToSeat:        req.ToSeat,
		ToUserID:      req.ToUserID,
		PricePaid:     price,
		PotReturn:     potReturn,
		SystemAbsorb:  systemAbsorb,
		TargetCompens: targetCompens,
		Hit:           hit,
		EffectText:    effectText,
		PhaseAtUse:    req.PhaseAtUse,
		RoundAtUse:    req.RoundAtUse,
		CreatedAt:     time.Now(),
	}
	return e.db.WithContext(ctx).Create(&log).Error
}

// ─── 辅助函数 ───

// isPropUsablePhase 判断当前阶段是否可使用道具。
func isPropUsablePhase(phase string) bool {
	switch phase {
	case "PhaseSpeak", "speak":
		return true
	}
	return false
}

// isSeatAlive 判断座位上的玩家是否存活。
func isSeatAlive(r *WerewolfRoom, seat int) bool {
	if r.State == nil || seat < 0 || seat >= len(r.State.Players) {
		return false
	}
	return r.State.Players[seat].Alive
}

// isWolfTeammate 判断两个座位是否都是狼人队友。
func isWolfTeammate(r *WerewolfRoom, seat1, seat2 int) bool {
	if r.State == nil || seat1 < 0 || seat2 < 0 {
		return false
	}
	if seat1 >= len(r.State.Roles) || seat2 >= len(r.State.Roles) {
		return false
	}
	f1 := FactionOf(r.State.Roles[seat1])
	f2 := FactionOf(r.State.Roles[seat2])
	return f1 == FactionWolf && f2 == FactionWolf
}

// isExposeProp 判断道具是否是身份暴露类。
//
// §20260811-10 U1 扩展:PropMirrorCheck(照妖镜)也是身份暴露类 ——
// 强制目标 bot 必写真实身份,语义与 markdown_bomb / nested_maze 同源;
// 因此 §132 三处同步(身份暴露不可对狼队友使用)的判定必须包含。
func isExposeProp(injType PropInjectType) bool {
	switch injType {
	case PropMarkdownBomb, PropNestedMaze, PropTaskDisguise, PropTaskDisguiseV3,
		PropMirrorCheck:
		return true
	}
	return false
}

// PropDistributePotBonus 在结算时把道具彩池回滚金额按比例发放给胜方。
// 返回每位胜者应分得的金额。
func PropDistributePotBonus(propPotBonus int64, winCount int) int64 {
	if propPotBonus <= 0 || winCount <= 0 {
		return 0
	}
	return propPotBonus / int64(winCount)
}

// factionString 把 Faction 枚举转为注入生成器用的阵营字符串（"wolf"/"good"/""）。
func factionString(f Faction) string {
	switch f {
	case FactionWolf:
		return "wolf"
	case FactionGood:
		return "good"
	}
	return ""
}
