// Package werewolf — player_profile_bridge.go: 狼人杀 Agent「玩家行为画像」
// (PlayerProfile) 的房间侧集成。2026-08-11 §20260811-05 U1 新增。
//
// 设计动机:让 Agent 对「坐在对面的人类玩家」拥有跨局记忆 —— 上一局
// 「3 号人类总是首夜跳预言家诈身份」这种高价值信息不再每局清零。
//
// 数据流:
//
//	PersistSummary 成功路径(judge_summary_bridge.go)
//	  → m.IteratePlayerProfilesAsync(r)
//	      lockRoomBriefly 快照: seatModelKeys + 人类座位 (seat→userID/account) +
//	                            阵营/胜负(复用 BuildSummaryInputLocked)
//	      for each (bot modelKey × human userID):
//	        goroutine(per-model 单飞锁):
//	          1. store.LoadProfile 读旧画像
//	          2. BuildPlayerProfilePrompt(旧画像 + 本局该人类事实)
//	          3. registry.Get(modelKey).Chat(90s, AgentClassWerewolfProfileIter)
//	          4. 输出空/超限 → 保留旧画像仅更新计数器
//	          5. store.SaveIterated(version 乐观锁)
//
// 消费路径(下一局):
//
//	StartAgentsLocked / 人类入座 → m.PrefetchPlayerProfiles(r)
//	  一次性把本房间所有 (bot modelKey × human userID) 画像读进
//	  r.playerProfileCache(内存,房间生命周期 TTL);
//	buildAgentContextLocked 末尾查缓存填 gc.PlayerProfiles(纯内存读,
//	  热路径零 DB 查询,§130 教训:buildAgentContextLocked 里禁止 DB I/O)。
//
// 硬约束(继承 §131 全部教训):
//   - 异步不阻塞游戏流,失败仅 logger.Warn;goroutine 入口 defer recover;
//   - per-model sync.Mutex 单飞 + DB version 乐观锁双保险;
//   - goroutine 内访问 r.State 一律 lockRoomBriefly 快照(§92a);
//   - 隐私合规:只存 LLM 摘要后的打法画像,不存聊天原文;画像只对
//     bot 自己(prompt 注入)与 admin 可见,无前端公开接口。
//
// 详见 docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-05.md §U1。
package werewolf

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentroot "LsmWebGame/agent"
	"LsmWebGame/agent/wwtypes"
	"LsmWebGame/config"
	"LsmWebGame/llm"
	"LsmWebGame/logger"
	"LsmWebGame/models"

	"go.uber.org/zap"
)

// PlayerProfileStore 是玩家行为画像的 DB 存取窄接口。
// service.AgentPlayerProfileService 天然实现;werewolf 包只依赖此接口,
// 不依赖 service 具体类型(便于测试桩注入,与 AgentMemoryStore 同模式)。
type PlayerProfileStore interface {
	// LoadProfile 读单行画像;行不存在返回 (nil, nil)。
	LoadProfile(ctx context.Context, modelKey, userID string) (*PlayerProfileRow, error)
	// LoadProfilesForUsers 批量读某模型对一组人类的画像(房间预取用)。
	LoadProfilesForUsers(ctx context.Context, modelKey string, userIDs []string) (map[string]*PlayerProfileRow, error)
	// SaveIterated 乐观锁写回;profileMD="" 时保留旧画像仅更新计数器。
	SaveIterated(ctx context.Context, modelKey, userID, profileMD string, sameCampWin bool) error
}

// PlayerProfileRow 是 werewolf 包内的画像行镜像(避免 import service/models)。
type PlayerProfileRow struct {
	ModelKey      string
	UserID        string
	ProfileMD     string
	GamesTogether uint
}

// playerProfileCacheEntry 是房间级画像缓存的单条记录。
type playerProfileCacheEntry struct {
	ProfileMD     string
	GamesTogether uint
}

// playerProfileCacheTTL 无实际过期——缓存生命周期 = 房间生命周期
// (restartGameLocked 原地重开时保留,与 seatModelKeys 同级)。

// SetPlayerProfileStore 注入画像存取层。nil 时整链 no-op(测试/老代码路径)。
// main.go 装配:
//
//	profileSvc := service.NewAgentPlayerProfileService(gormDB)
//	gameSvcWs.WerewolfManager().SetPlayerProfileStore(profileSvc)
func (m *WerewolfManager) SetPlayerProfileStore(store PlayerProfileStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.playerProfileStore = store
}

// cfgPlayerProfileEnabled 安全读取 config.WerewolfConfig.PlayerProfileEnabled。
// 默认 true;测试环境 config.Load() panic 时按"关闭"兜底。
func cfgPlayerProfileEnabled() (enabled bool) {
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return false
	}
	return c.Werewolf.PlayerProfileEnabled
}

// cfgPlayerProfileMaxTokens 安全读取 PlayerProfileMaxTokens(默认 1024)。
func cfgPlayerProfileMaxTokens() (n int) {
	n = 1024
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return 1024
	}
	if c.Werewolf.PlayerProfileMaxTokens > 0 {
		n = c.Werewolf.PlayerProfileMaxTokens
	}
	return n
}

// ─────────────────── 消费路径:预取缓存 + GameContext 注入 ───────────────────

// PrefetchPlayerProfiles 在 StartAgentsLocked / 人类入座后调用,一次性把
// 本房间所有 (bot modelKey × human userID) 画像读进 r.playerProfileCache。
// 之后 buildAgentContextLocked 纯内存读,热路径零 DB 查询。
//
// 调用方不要求持锁;本函数内部 lockRoomBriefly 快照座位表,锁外查 DB,
// 再 lockRoomBriefly 写缓存。失败仅 logger.Warn(画像缺失不阻塞开局)。
func (m *WerewolfManager) PrefetchPlayerProfiles(r *WerewolfRoom) {
	if r == nil {
		return
	}
	m.mu.RLock()
	store := m.playerProfileStore
	m.mu.RUnlock()
	if store == nil || !cfgPlayerProfileEnabled() {
		return
	}

	// 锁内快照:bot 座位模型集合 + 人类座位 userID 集合。
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		logger.L().Warn("werewolf: player profile prefetch lock contention, skipping",
			zap.String("room_id", r.RoomID))
		return
	}
	modelKeys := make(map[string]struct{})
	humanIDs := make([]string, 0, 4)
	if r.State != nil {
		for i := 0; i < MaxPlayers; i++ {
			uid := r.State.Seats[i]
			if uid == "" {
				continue
			}
			if mk, ok := r.seatModelKeys[i]; ok && mk != "" {
				modelKeys[mk] = struct{}{}
			} else {
				humanIDs = append(humanIDs, uid)
			}
		}
	}
	r.mu.Unlock()
	if len(modelKeys) == 0 || len(humanIDs) == 0 {
		return // 全 AI 或全人类房间:无 (bot × human) 组合,no-op
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cache := make(map[string]map[string]playerProfileCacheEntry, len(modelKeys))
	for mk := range modelKeys {
		rows, err := store.LoadProfilesForUsers(ctx, mk, humanIDs)
		if err != nil {
			logger.L().Warn("werewolf: player profile prefetch failed",
				zap.String("room_id", r.RoomID),
				zap.String("model_key", mk),
				zap.Error(err))
			continue
		}
		for uid, row := range rows {
			if row == nil || row.ProfileMD == "" {
				continue
			}
			if cache[mk] == nil {
				cache[mk] = make(map[string]playerProfileCacheEntry, len(rows))
			}
			cache[mk][uid] = playerProfileCacheEntry{
				ProfileMD:     row.ProfileMD,
				GamesTogether: row.GamesTogether,
			}
		}
	}

	// 写回房间缓存(锁内)。
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		return
	}
	r.playerProfileCache = cache
	r.mu.Unlock()
	logger.L().Info("werewolf: player profiles prefetched",
		zap.String("room_id", r.RoomID),
		zap.Int("models", len(cache)))
}

// playerProfilesForBotLocked 查房间缓存,返回该 bot 座位视角的
// seat → 画像摘要 map(仅人类座位有值)。调用方必须持 r.mu(§92a)。
// profileInjectMaxRunes 是单条画像注入 prompt 的长度上限(约 200 token)。
const profileInjectMaxRunes = 400

func playerProfilesForBotLocked(r *WerewolfRoom, botSeat int) map[int]string {
	mk, ok := r.seatModelKeys[botSeat]
	if !ok || mk == "" || len(r.playerProfileCache) == 0 || r.State == nil {
		return nil
	}
	byUser := r.playerProfileCache[mk]
	if len(byUser) == 0 {
		return nil
	}
	var out map[int]string
	for i := 0; i < MaxPlayers; i++ {
		uid := r.State.Seats[i]
		if uid == "" {
			continue
		}
		if _, isBot := r.seatModelKeys[i]; isBot {
			continue // 只给人类座位注入画像
		}
		entry, ok := byUser[uid]
		if !ok || entry.ProfileMD == "" {
			continue
		}
		md := entry.ProfileMD
		if len([]rune(md)) > profileInjectMaxRunes {
			md = string([]rune(md)[:profileInjectMaxRunes]) + "…"
		}
		if out == nil {
			out = make(map[int]string, 2)
		}
		out[i] = fmt.Sprintf("%s(同局%d次)", md, entry.GamesTogether)
	}
	return out
}

// ─────────────────── 写入路径:赛后异步迭代 ───────────────────

// IteratePlayerProfilesAsync 在法官整局总结落地后(PersistSummary 成功路径,
// 与 IterateAgentMemoriesAsync 同点触发),对本局每个 (bot modelKey × human
// userID) 组合异步发起一次画像迭代。
//
// 调用方不要求持锁;内部 lockRoomBriefly 快照后每组合一个 goroutine。
// 开关关闭 / store 未注入 / 无 (bot × human) 组合时 no-op。
func (m *WerewolfManager) IteratePlayerProfilesAsync(r *WerewolfRoom) {
	if r == nil {
		return
	}
	m.mu.RLock()
	store := m.playerProfileStore
	registry := m.registry
	m.mu.RUnlock()
	if store == nil || registry == nil {
		return
	}
	if !cfgPlayerProfileEnabled() {
		return
	}

	if !lockRoomBriefly(r, 500*time.Millisecond) {
		logger.L().Warn("werewolf: player profile iterate snapshot lock contention, skipping",
			zap.String("room_id", r.RoomID))
		return
	}
	type botHumanPair struct {
		modelKey   string
		botSeat    int
		humanSeat  int
		humanUID   string
		humanAcct  string
		humanRole  string
		botFaction string // bot 自己的阵营(用于 sameCampWin 判定)
		humFaction string
		winner     string
		aliveHuman bool
		dayNumber  int
	}
	var pairs []botHumanPair
	roomID := r.RoomID
	if r.State != nil {
		in := r.BuildSummaryInputLocked()
		winner := r.State.Winner
		day := in.DayNumber
		accts := map[int]string{}
		for _, b := range buildAllPlayersLocked(r) {
			accts[b.Seat] = b.Account
		}
		for botSeat, mk := range r.seatModelKeys {
			if mk == "" {
				continue
			}
			for humanSeat := 0; humanSeat < MaxPlayers; humanSeat++ {
				uid := r.State.Seats[humanSeat]
				if uid == "" {
					continue
				}
				if _, isBot := r.seatModelKeys[humanSeat]; isBot {
					continue
				}
				alive := r.State.AliveSeat(Seat(humanSeat))
				pairs = append(pairs, botHumanPair{
					modelKey:   mk,
					botSeat:    botSeat,
					humanSeat:  humanSeat,
					humanUID:   uid,
					humanAcct:  accts[humanSeat],
					humanRole:  in.Roles[humanSeat],
					botFaction: factionOfRoleString(in.Roles[botSeat]),
					humFaction: factionOfRoleString(in.Roles[humanSeat]),
					winner:     winner,
					aliveHuman: alive,
					dayNumber:  day,
				})
			}
		}
	}
	r.mu.Unlock()

	for _, p := range pairs {
		go m.iterateOnePlayerProfile(r, roomID, p.modelKey, p.humanUID, p.humanAcct,
			p.humanSeat, p.humanRole, p.botFaction, p.humFaction, p.winner,
			p.aliveHuman, p.dayNumber, store, registry)
	}
}

// factionOfRoleString 从 role 字符串粗判阵营(与引擎 Role.Faction 对齐的
// 轻量镜像,避免在快照路径上引入引擎依赖)。
func factionOfRoleString(role string) string {
	switch role {
	case "werewolf", "wolf_king":
		return "wolf"
	case "":
		return ""
	default:
		return "good"
	}
}

// iterateOnePlayerProfile 执行单个 (model × human) 画像迭代全链。
// 全程失败仅 logger.Warn;goroutine 入口 defer recover 兜底,绝不 panic。
func (m *WerewolfManager) iterateOnePlayerProfile(
	r *WerewolfRoom, roomID, modelKey, humanUID, humanAcct string,
	humanSeat int, humanRole, botFaction, humFaction, winner string,
	aliveHuman bool, dayNumber int,
	store PlayerProfileStore, registry *llm.Registry,
) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.L().Warn("werewolf: player profile iterate panicked",
				zap.String("room_id", roomID),
				zap.String("model_key", modelKey),
				zap.String("user_id", humanUID),
				zap.Any("recover", rec))
		}
	}()

	// 单飞锁:同一模型的迭代串行化(与 §131 memory 同模式,复用 memoryMus)。
	mu := m.memoryMuFor(modelKey)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// 同阵营且该阵营胜利 → wins_together+1。
	sameCampWin := botFaction != "" && botFaction == humFaction && winner == botFaction

	// 1. 读旧画像(行不存在返回 nil)。
	old, err := store.LoadProfile(ctx, modelKey, humanUID)
	if err != nil {
		logger.L().Warn("werewolf: player profile load failed",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		return
	}
	oldMD := ""
	games := uint(0)
	if old != nil {
		oldMD = old.ProfileMD
		games = old.GamesTogether
	}

	// 2. 构造迭代 prompt。
	prompt := BuildPlayerProfilePrompt(oldMD, games, humanAcct, humanSeat,
		humanRole, humFaction, winner, aliveHuman, dayNumber, roomID)

	// 3. 用该模型自己的 provider 调 LLM。
	provider, key, err := registry.Get(modelKey)
	if err != nil {
		logger.L().Warn("werewolf: player profile registry.Get failed",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		return
	}
	req := llm.LLMRequest{
		Model:     modelKey,
		Messages:  []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: prompt}}}},
		MaxTokens: cfgPlayerProfileMaxTokens(),
		// §24 AgentClassName:画像迭代是独立的 Agent 类别,与玩家 Bot /
		// 法官 / 记忆迭代调用分开计费/归因。
		AgentClassName: string(agentroot.AgentClassWerewolfProfileIter),
	}
	resp, err := provider.Chat(ctx, key, req)
	newMD := ""
	if err != nil {
		logger.L().Warn("werewolf: player profile LLM iterate failed, keep old",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
	} else {
		text := strings.TrimSpace(resp.Text())
		if text != "" && len(text) <= playerProfileMaxBytes {
			newMD = text
		} else if len(text) > playerProfileMaxBytes {
			// 超限 → rune 安全截断(画像比 MEMORY.md 小一个数量级,直接硬截)。
			text = string([]rune(text)[:playerProfileMaxRunesHard])
			newMD = text
		}
		// text == "" → newMD 保持 "",SaveIterated 走计数器-only 更新。
	}

	// 4. 乐观锁写回(profileMD="" 时保留旧画像仅更新计数器)。
	if err := store.SaveIterated(ctx, modelKey, humanUID, newMD, sameCampWin); err != nil {
		logger.L().Warn("werewolf: player profile save failed",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		return
	}
	logger.L().Info("werewolf: player profile iterated",
		zap.String("room_id", roomID),
		zap.String("model_key", modelKey),
		zap.String("user_id", humanUID),
		zap.Bool("updated", newMD != ""))
}

// 画像存储上限:8KB(远小于 §131 MEMORY.md 的 100KB——画像是单玩家摘要)。
const (
	playerProfileMaxBytes     = 8 * 1024
	playerProfileMaxRunesHard = 4000
)

// BuildPlayerProfilePrompt 构造画像迭代 prompt(纯函数,便于测试)。
// 输出要求:≤ 400 字的结构化打法画像,3 段固定小标题。
func BuildPlayerProfilePrompt(oldMD string, gamesTogether uint, acct string, seat int,
	role, faction, winner string, alive bool, dayNumber int, roomID string) string {
	var sb strings.Builder
	sb.WriteString("你是狼人杀 AI 玩家的「对手分析官」。请根据本局事实,更新你对这位人类玩家的打法画像。\n\n")
	sb.WriteString("【该玩家旧画像】\n")
	if oldMD == "" {
		sb.WriteString("(尚无画像,这是你们第一次同局)\n\n")
	} else {
		fmt.Fprintf(&sb, "%s\n\n(你们已累计同局 %d 次)\n\n", oldMD, gamesTogether)
	}
	sb.WriteString("【本局事实】\n")
	fmt.Fprintf(&sb, "- 房间 %s,共进行约 %d 天\n", roomID, dayNumber)
	fmt.Fprintf(&sb, "- 该玩家: %s,坐 %d 号位,本局角色 %s(%s阵营)\n", acct, seat+1, role, faction)
	if winner == faction {
		sb.WriteString("- 该玩家所在阵营获胜\n")
	} else if winner != "" {
		sb.WriteString("- 该玩家所在阵营落败\n")
	}
	if alive {
		sb.WriteString("- 该玩家存活到终局\n")
	} else {
		sb.WriteString("- 该玩家中途出局\n")
	}
	sb.WriteString("\n【输出要求】\n")
	sb.WriteString("用不超过 400 字输出该玩家的打法画像,严格使用以下 3 段小标题:\n")
	sb.WriteString("【风格标签】3-5 个词概括(如:激进悍跳/保守潜水/情绪型发言)\n")
	sb.WriteString("【历史倾向】该玩家在各局势下的典型行为模式(基于旧画像迭代,不要丢掉仍然成立的旧观察)\n")
	sb.WriteString("【应对建议】下次同局时应该采取的策略(1-2 句)\n")
	sb.WriteString("只输出画像本身,不要输出任何解释、前言或 JSON 包装。\n")
	return sb.String()
}

// ─────────────────── GameContext 字段接线(§130 教训:必须真实消费) ───────────────────

// fillPlayerProfilesLocked 在 buildAgentContextLocked 末尾调用,把房间缓存中
// 本 bot 视角的人类玩家画像注入 GameContext。调用方必须持 r.mu(§92a)。
func fillPlayerProfilesLocked(r *WerewolfRoom, seat int, gc *wwtypes.GameContext) {
	if gc == nil {
		return
	}
	gc.PlayerProfiles = playerProfilesForBotLocked(r, seat)
}

// PlayerProfileStoreAdapter 把 service.AgentPlayerProfileService 适配为
// werewolf 包窄接口 PlayerProfileStore。main.go wire 时使用:
//
//	werewolfManager.SetPlayerProfileStore(werewolf.PlayerProfileStoreAdapter{Svc: profileSvc})
//
// 之所以需要 adapter 而非直接断言接口:Go 接口方法签名中的 struct 类型必须
// 完全一致,werewolf 包不能 import service(避免反向依赖),故做一层显式拷贝
// (与 SelfPortraitReaderAdapter 同模式)。
type PlayerProfileStoreAdapter struct {
	Svc profileStoreBackend
}

// profileStoreBackend 是 adapter 依赖的最小后端接口。
// service.AgentPlayerProfileService 天然实现(方法签名一致)。
type profileStoreBackend interface {
	LoadProfile(ctx context.Context, modelKey, userID string) (*models.TLsmGameAgentPlayerProfile, error)
	LoadProfilesForUsers(ctx context.Context, modelKey string, userIDs []string) (map[string]*models.TLsmGameAgentPlayerProfile, error)
	SaveIterated(ctx context.Context, modelKey, userID, profileMD string, sameCampWin bool) error
}

// LoadProfile 实现 PlayerProfileStore。
func (a PlayerProfileStoreAdapter) LoadProfile(ctx context.Context, modelKey, userID string) (*PlayerProfileRow, error) {
	if a.Svc == nil {
		return nil, nil
	}
	row, err := a.Svc.LoadProfile(ctx, modelKey, userID)
	if err != nil || row == nil {
		return nil, err
	}
	return &PlayerProfileRow{
		ModelKey:      row.ModelKey,
		UserID:        row.UserID,
		ProfileMD:     row.ProfileMD,
		GamesTogether: row.GamesTogether,
	}, nil
}

// LoadProfilesForUsers 实现 PlayerProfileStore。
func (a PlayerProfileStoreAdapter) LoadProfilesForUsers(ctx context.Context, modelKey string, userIDs []string) (map[string]*PlayerProfileRow, error) {
	out := make(map[string]*PlayerProfileRow, len(userIDs))
	if a.Svc == nil {
		return out, nil
	}
	raw, err := a.Svc.LoadProfilesForUsers(ctx, modelKey, userIDs)
	if err != nil {
		return nil, err
	}
	for uid, row := range raw {
		if row == nil {
			continue
		}
		out[uid] = &PlayerProfileRow{
			ModelKey:      row.ModelKey,
			UserID:        row.UserID,
			ProfileMD:     row.ProfileMD,
			GamesTogether: row.GamesTogether,
		}
	}
	return out, nil
}

// SaveIterated 实现 PlayerProfileStore。
func (a PlayerProfileStoreAdapter) SaveIterated(ctx context.Context, modelKey, userID, profileMD string, sameCampWin bool) error {
	if a.Svc == nil {
		return nil
	}
	return a.Svc.SaveIterated(ctx, modelKey, userID, profileMD, sameCampWin)
}
