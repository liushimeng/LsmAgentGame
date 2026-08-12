// Package werewolf - settlement_reward.go: §20260810-11 P1 胜方道具折扣券 + 败方安慰包
//
// 设计要点:
//   - 复用 t_lsm_game_kv 表(避免新增 schema / migration)
//   - key 前缀:werewolf_settlement_reward:user:{user_id}:room:{room_id}
//   - value JSON:{reward_type, prop_key, expires_at, used}
//   - 终局路径在 r.State.Status=="over" 后由 grantSettlementRewardsLocked 写 KV
//   - 道具购买路径 UseProp 顶部查询 KV 决定 cost 折扣 / 是否免费
//   - 5 分钟 TTL 过期静默忽略(不报错)
//   - §130 接线验证:grantSettlementRewardsLocked 必须在终局路径**实际调用**
//   - §118 钱包边界:仅触及真人钱包,不污染 LLM Provider 模块
//
// 与 §132/§133 道具系统的关系:独立常量 werewolf.settlement_reward_discount=0.5
// (默认)与 EconTier 档位解耦,避免互相影响。

package werewolf

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"LsmWebGame/logger"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SettlementRewardType 奖励类型
type SettlementRewardType string

const (
	RewardTypeVictoryDiscount SettlementRewardType = "victory_discount_voucher" // 胜方:50% 折扣券
	RewardTypeConsolationProp SettlementRewardType = "consolation_prop"          // 败方:免费道具
)

// SettlementReward KV 序列化结构
type SettlementReward struct {
	RewardType SettlementRewardType `json:"reward_type"`
	PropKey    string               `json:"prop_key,omitempty"`     // 仅 consolation_prop 有
	Discount   float64              `json:"discount,omitempty"`     // 仅 victory_discount_voucher 有(0.5)
	ExpiresAt  int64                `json:"expires_at"`             // unix 秒
	Used       bool                 `json:"used"`
	GrantedAt  int64                `json:"granted_at"`             // unix 秒,审计用
	RoomID     string               `json:"room_id"`                // 冗余,便于反查
}

// SettlementRewardConfig 奖励配置(可热更新)
type SettlementRewardConfig struct {
	// VictoryDiscount 胜方折扣(0.5 = 50% off)
	VictoryDiscount float64
	// ConsolationPropKeys 败方随机候选道具池
	ConsolationPropKeys []string
	// TTL 奖励有效期
	TTL time.Duration
	// Enabled 总开关(关掉则不发放奖励)
	Enabled bool
}

// DefaultSettlementRewardConfig 默认配置。
// 折扣 0.5 / 候选 3 个低价值道具 / TTL 5 分钟 / 默认开启。
func DefaultSettlementRewardConfig() SettlementRewardConfig {
	return SettlementRewardConfig{
		VictoryDiscount: 0.5,
		ConsolationPropKeys: []string{
			"char_confuse",         // 字符欺骗(10 金币级)
			"char_confuse_human",   // 人类反制(10 金币级)
			"markdown_bomb",        // 公告轰炸(15 金币级)
		},
		TTL:     5 * time.Minute,
		Enabled: true,
	}
}

// SettlementRewardService KV-backed 奖励服务
type SettlementRewardService struct {
	mu   sync.Mutex
	db   *gorm.DB
	cfg  SettlementRewardConfig
}

// NewSettlementRewardService 构造服务
func NewSettlementRewardService(db *gorm.DB, cfg SettlementRewardConfig) *SettlementRewardService {
	if cfg.VictoryDiscount <= 0 || cfg.VictoryDiscount >= 1 {
		cfg.VictoryDiscount = 0.5
	}
	if len(cfg.ConsolationPropKeys) == 0 {
		cfg.ConsolationPropKeys = []string{"char_confuse", "char_confuse_human", "markdown_bomb"}
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 5 * time.Minute
	}
	return &SettlementRewardService{db: db, cfg: cfg}
}

// kvKey 拼 key 前缀
func (s *SettlementRewardService) kvKey(userID, roomID string) string {
	return fmt.Sprintf("werewolf_settlement_reward:user:%s:room:%s", userID, roomID)
}

// GrantVictoryDiscount 发胜方折扣券(纯函数;KV 持久化由 service 实现)。
// userID 必为真人(IsBot=false);roomID 用于前缀与审计;now 用于 TTL 计算。
func (s *SettlementRewardService) GrantVictoryDiscount(ctx context.Context, userID, roomID string, now time.Time) error {
	if !s.cfg.Enabled {
		return nil
	}
	r := SettlementReward{
		RewardType: RewardTypeVictoryDiscount,
		Discount:   s.cfg.VictoryDiscount,
		ExpiresAt:  now.Add(s.cfg.TTL).Unix(),
		GrantedAt:  now.Unix(),
		RoomID:     roomID,
	}
	return s.saveKV(ctx, userID, roomID, r)
}

// GrantConsolationProp 发败方安慰包(随机 1 个 prop_key)。
func (s *SettlementRewardService) GrantConsolationProp(ctx context.Context, userID, roomID string, now time.Time) error {
	if !s.cfg.Enabled {
		return nil
	}
	// 简单确定性随机(基于 userID+roomID+now 哈希)——非密码学安全,仅避免
	// 同一败方在多局中得到完全相同的 prop_key 模式。
	idx := (hashCode(userID+":"+roomID) + int(now.Unix()/60)) % len(s.cfg.ConsolationPropKeys)
	if idx < 0 {
		idx = -idx
	}
	propKey := s.cfg.ConsolationPropKeys[idx]
	r := SettlementReward{
		RewardType: RewardTypeConsolationProp,
		PropKey:    propKey,
		ExpiresAt:  now.Add(s.cfg.TTL).Unix(),
		GrantedAt:  now.Unix(),
		RoomID:     roomID,
	}
	return s.saveKV(ctx, userID, roomID, r)
}

// Lookup 查询用户在该房间的奖励(返回 nil 表示无 / 过期 / 已用)。
func (s *SettlementRewardService) Lookup(ctx context.Context, userID, roomID string, now time.Time) *SettlementReward {
	if s.db == nil {
		return nil
	}
	var row struct {
		Key   string `gorm:"primaryKey"`
		Value string
	}
	if err := s.db.WithContext(ctx).Table("t_lsm_game_kv").
		Where("`key` = ?", s.kvKey(userID, roomID)).
		Scan(&row).Error; err != nil {
		return nil
	}
	if row.Value == "" {
		return nil
	}
	var r SettlementReward
	if err := json.Unmarshal([]byte(row.Value), &r); err != nil {
		return nil
	}
	if r.Used {
		return nil
	}
	if now.Unix() > r.ExpiresAt {
		return nil
	}
	return &r
}

// MarkUsed 标记奖励已用(消费后调用)。
func (s *SettlementRewardService) MarkUsed(ctx context.Context, userID, roomID string) error {
	if s.db == nil {
		return nil
	}
	r := s.Lookup(ctx, userID, roomID, time.Now())
	if r == nil {
		return nil
	}
	r.Used = true
	return s.saveKV(ctx, userID, roomID, *r)
}

// saveKV 写 KV(更新而非插入,GrantedAt 保留)
func (s *SettlementRewardService) saveKV(ctx context.Context, userID, roomID string, r SettlementReward) error {
	if s.db == nil {
		return nil
	}
	val, err := json.Marshal(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// upsert
	return s.db.WithContext(ctx).Table("t_lsm_game_kv").
		Save(map[string]interface{}{
			"key":   s.kvKey(userID, roomID),
			"value": string(val),
		}).Error
}

// hashCode FNV-1a 简化版(用于确定性随机;不用于密码学场景)
func hashCode(s string) int {
	h := 2166136261
	for i := 0; i < len(s); i++ {
		h ^= int(s[i])
		h *= 16777619
	}
	return h
}

// ─────────────────── 终局发放入口 ───────────────────

// grantSettlementRewardsLocked §20260810-11 P1 — 终局时遍历存活真人玩家,按胜/败发奖。
// caller 必须持 r.mu;svc 可为 nil(未启用时直接返回)。
//
// 胜方判定:r.State.Winner == p.Faction(简单映射;若 Winner 字段尚未填
// 则从 alive 玩家数与角色分布反推,详见 checkWinnerLocked)。
//
// 接入位置:r.checkWinner() 之后,EmitGameOver 之前;caller 负责顺序。
func (m *WerewolfManager) grantSettlementRewardsLocked(r *WerewolfRoom, svc *SettlementRewardService) {
	if r == nil || r.State == nil || svc == nil {
		return
	}
	if r.State.Status != "over" {
		return
	}
	// §20260811-08 U2 — 幂等守卫。发放已收口进 EmitGameOver,而后者在
	// watchdog tick / 冷却期结束 / force-close 多条路径上可能被重复触达;
	// 且 §129 冷却期 + 重开局让同一 room 对象经历多局,restartGameLocked
	// 负责重置本标志。
	if r.settlementRewarded {
		return
	}
	r.settlementRewarded = true
	winner := r.State.Winner
	if winner == "" {
		// fallback:用 faction 反推(粗粒度,但作为兜底足够)
		winner = deriveWinnerFromAliveLocked(r)
		if winner == "" {
			return
		}
	}
	now := time.Now()
	for seat, p := range r.State.Players {
		if p.IsBot {
			continue
		}
		// §20260811-08 U2 — 不再过滤 !p.Alive。
		//
		// 「阵营胜利」与「个人存活」是两个正交概念:胜方阵营里被刀/被毒/被票
		// 出局的人类同样属于胜方。旧版的 alive 过滤既无设计文档依据,也与同
		// 一终局路径上的金币结算(computeCoinDelta 不看 alive)自相矛盾。
		userID := r.Seats[seat]
		if userID == "" {
			continue
		}
		faction := factionOfRole(r.State.Roles[seat])
		var err error
		rewardKind := "consolation_prop"
		if faction == winner {
			rewardKind = "victory_discount"
			err = svc.GrantVictoryDiscount(context.Background(), userID, r.RoomID, now)
		} else {
			err = svc.GrantConsolationProp(context.Background(), userID, r.RoomID, now)
		}
		// §118 异步持久化不阻塞游戏流:失败仅 log 不中断循环。
		// 旧版 `_ =` 完全静默,是本 bug 拖到 §20260811-08 才被发现的次要原因。
		if err != nil {
			logger.L().Warn("werewolf: grant settlement reward failed",
				zap.String("room_id", r.RoomID),
				zap.String("user_id", userID),
				zap.Int("seat", seat),
				zap.String("reward", rewardKind),
				zap.Error(err))
		}
	}
}

// deriveWinnerFromAliveLocked 兜底:从存活角色反推胜方。
// 返回 "wolf" / "good" / "" (无法判定)。
func deriveWinnerFromAliveLocked(r *WerewolfRoom) string {
	if r == nil || r.State == nil {
		return ""
	}
	wolfCount, goodCount := 0, 0
	for seat, p := range r.State.Players {
		if !p.Alive {
			continue
		}
		role := r.State.Roles[seat]
		if role == 0 { // Role 0 = 未分配
			continue
		}
		if isWolfRole(role) {
			wolfCount++
		} else {
			goodCount++
		}
	}
	if wolfCount == 0 && goodCount > 0 {
		return "good"
	}
	if goodCount == 0 && wolfCount > 0 {
		return "wolf"
	}
	return ""
}

// factionOfRole 角色 → 阵营(简化版,与 prop_blocks / wwtypes 一致)。
func factionOfRole(role Role) string {
	if role == RoleWerewolf {
		return "wolf"
	}
	return "good"
}

// isWolfRole 判断是否为狼阵营
func isWolfRole(role Role) bool {
	return role == RoleWerewolf
}
