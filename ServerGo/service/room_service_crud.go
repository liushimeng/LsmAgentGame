package service

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"strings"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/util"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ListRooms returns lobby-visible rooms for the given game kind without
// per-room user role enrichment. Kept for legacy callers / tests that don't
// have a userID. New HTTP entry points should call ListRoomsForUser so the
// frontend can render "进入房间" / "👁 观战" / "已满" correctly after a page
// refresh (BUG-R210-01 / 2026-07-30: without my_role, RoomListTable always
// fell back to joinable() and showed "已满" for any playing-state room the
// user had previously joined).
func (s *RoomService) ListRooms(gameKind string) []RoomInfo {
	return s.ListRoomsForUser(context.Background(), gameKind, "")
}

// ListRoomsForUser returns lobby-visible rooms with `my_role` annotated for
// each room where the given userID has a player or spectator row.
//
// Algorithm:
//   1. SELECT rooms where status IN ('open','playing') — same filter as before.
//   2. SELECT role FROM t_lsm_game_player WHERE user_id = ? AND room_id IN (...)
//      — single query keyed by userID + room set, no N+1.
//   3. Build a roomID → role map and stamp MyRole on each RoomInfo.
//
// userID == "" → MyRole is left empty for every row (legacy behavior).
//
// `MyRole` values: "player" / "agent" / "spectator" — same enum as
// t_lsm_game_player.role + agent ("agent" is a bot seat, indistinguishable
// from the user's perspective: still occupies a seat, still drives the game).
func (s *RoomService) ListRoomsForUser(ctx context.Context, gameKind, userID string) []RoomInfo {
	var rooms []models.TLsmGameRoom
	s.db.WithContext(ctx).Where("game_kind = ? AND status IN ?", gameKind, []string{"open", "playing"}).
		Order("created_at ASC").Find(&rooms)

	out := make([]RoomInfo, 0, len(rooms))

	// Pre-fetch the user's rows for these rooms in one query. Empty userID
	// (legacy caller) skips this entirely.
	roleByRoom := make(map[string]string, len(rooms))
	if userID != "" && len(rooms) > 0 {
		ids := make([]string, 0, len(rooms))
		for _, r := range rooms {
			ids = append(ids, r.ID)
		}
		var rows []models.TLsmGamePlayer
		if err := s.db.WithContext(ctx).
			Where("user_id = ? AND room_id IN ?", userID, ids).
			Find(&rows).Error; err == nil {
			for _, p := range rows {
				roleByRoom[p.RoomID] = p.Role
			}
		} else {
			// Soft-fail: missing role annotation shouldn't break the lobby list.
			// RoomListTable will fall back to joinable() and render "已满".
			logger.L().Warn("ListRoomsForUser: preload my_role failed",
				zap.String("user_id", userID),
				zap.Error(err))
		}
	}

	for _, r := range rooms {
		info := RoomInfo{
			ID:           r.ID,
			Name:         r.Name,
			GameKind:     r.GameKind,
			Capacity:     r.Capacity,
			CurrentCount: r.CurrentCount,
			Status:       r.Status,
			CreatedAt:    r.CreatedAt,
		}
		// 2026-07-30 BUG-R210-01: stamp MyRole for the lobby so frontend can
		// re-render "进入房间" / "👁 观战" after a refresh.
		if userID != "" {
			if role, ok := roleByRoom[r.ID]; ok {
				info.MyRole = role
			}
		}
		// Round 23 P1 BUG FIX: same enrichment as GetRoomDetail. Only
		// applied to werewolf rooms where the hook is available; cost is
		// N map lookups + at most one lock acquisition per row (the
		// engine keeps rooms in-memory so it's not a DB hit).
		//
		// 2026-07-16 BUG-R128-03 修复: in-memory Status 是权威状态机视图,
		// 覆盖 DB 行 status。DB status 仅在 onGameOver 回调时同步,冷却期 +
		// restart_vote 阶段会滞后(显示 playing 但实际已进入终局子阶段)。
		if gameKind == "werewolf" && s.werewolfState != nil {
			if p, d, status, w, ok := s.werewolfState.WerewolfPublicState(r.ID); ok {
				info.Phase = p
				info.RoundNumber = d
				info.Status = status
				info.Winner = w
			}
		}
		out = append(out, info)
	}
	return out
}

func (s *RoomService) getOrCreateBotUserID(ctx context.Context, suffix string) (string, *errcode.Error) {
	account := "bot_" + suffix
	var u models.TLsmGameUser
	if err := s.db.WithContext(ctx).Where("account = ?", account).First(&u).Error; err == nil {
		return u.ID, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.L().Error("look up bot user", zap.Error(err), zap.String("account", account))
		return "", errcode.Code(errcode.ErrDB)
	}
	b := make([]byte, 6)
	if _, err := cryptorand.Read(b); err != nil {
		// Unlikely, but don't crash room creation for an entropy failure.
		logger.L().Warn("rand failed for bot password", zap.Error(err))
	}
	u = models.TLsmGameUser{
		ID:           util.NewUUID(),
		Account:      account,
		Nickname:     "Bot " + suffix,
		PasswordHash: "$2a$10$" + hex.EncodeToString(b), // bcrypt-shaped placeholder, never used.
		MyInviteCode: "BOT_" + account,                  // unique to satisfy uniqueIndex on my_invite_code
	}
	if err := s.db.WithContext(ctx).Create(&u).Error; err != nil {
		logger.L().Error("create bot user", zap.Error(err), zap.String("account", account))
		return "", errcode.Code(errcode.ErrDB)
	}
	// BUG-R129-03 FIX: seed the wallet row at the same time as the bot user
	// so count(is_bot=1 users) == count(bot wallets). CreateWallet is
	// idempotent (first-call path here is on a brand-new user_id, so it's
	// guaranteed to insert; for the legacy lookup path above we already
	// returned without seeding — backfill only ever applies to new bots).
	walletSvc := NewWalletService(s.db)
	if err := walletSvc.CreateWallet(ctx, u.ID, DefaultInitialBalance); err != nil {
		// Non-fatal: the bot user row already exists. Emit a clear warning so
		// the gap is visible in operations, but do NOT fail room creation —
		// downstream Credit/Debit EnsureWalletLazy will backfill on demand.
		logger.L().Warn("BUG-R129-03: bot wallet seed failed",
			zap.String("account", account),
			zap.String("user_id", u.ID),
			zap.Error(err))
	} else {
		logger.L().Info("bot wallet seeded",
			zap.String("account", account),
			zap.String("user_id", u.ID),
			zap.Int64("balance", DefaultInitialBalance))
	}
	logger.L().Info("bot user created",
		zap.String("account", account), zap.String("user_id", u.ID))
	return u.ID, nil
}

func (s *RoomService) CreateRoom(gameKind, userID, name string) (*RoomDetail, *errcode.Error) {
	return s.CreateRoomWithAgents(context.Background(), gameKind, userID, name, nil, nil, "", nil, "", nil)
}

func (s *RoomService) CreateRoomWithAgents(ctx context.Context, gameKind, userID, name string, agentSeats []AgentSeatConfig, judge *JudgeConfig, agentDifficulty string, commentary *CommentaryConfig, creatorRole string, texasCfg *TexasTableConfig) (*RoomDetail, *errcode.Error) {
	// creatorRole (2026-08-06 §20260806-03 自选角色):空/"random" = 随机。
	// (原为可变参;2026-08-19 §德州扑克盲注透传 追加 texasCfg 参数时归一化为
	// 普通参数 — Go 仅允许一个可变参且必须在末位。)
	creatorRolePref := creatorRole
	// judge 房间级法官设置(可选)。nil = 默认(有 Agent 时启用 Agent 法官)。
	// 2026-07-30 §重构:仅剩两选项 agent/human,均启用 AgentJudge LLM 路径
	// (真人法官后端未实现,行为等同 agent)。因此 JudgeDesired 恒为 true,
	// 但保留变量以便未来真人法官 WS 契约上线后(§130 跟踪项)可零侵入分流。
	judgeDesired := judge == nil || judge.Mode == "" || judge.Mode == "agent" || judge.Mode == "human"
	judgeModelKey := ""
	if judge != nil {
		judgeModelKey = strings.TrimSpace(judge.ModelKey)
	}
	// Random-judge-model (R137 / Agent 法官随机分配):
	//   - explicit judgeModelKey (creator picked one) → 保留,不覆盖
	//   - judge.Mode 为空 / "agent" / "human" → 启用法官,model_key 空时
	//     从可用 provider 池(Fisher-Yates Shuffle)随机挑一个
	//   - 2026-07-30 §重构:三选项(ai/human/off)→两选项(agent/human);
	//     "off" 不再是房间级合法值,关闭走 cfg.Werewolf.JudgeMode="off"
	//   - 池空 (cfg 未配置 / 全部占位 / 空 api_key) → judgeModelKey 保持空,
	//     下游 startJudgeGoroutine / GenerateSummary 走 fallback
	//     (cfgWerewolfJudgeModelKey → seatModelKeys → "judge-default")。
	if judgeModelKey == "" && judgeDesired && len(agentSeats) > 0 {
		if pick := s.pickRandomJudgeModelKey(); pick != "" {
			judgeModelKey = pick
			logger.L().Info("CreateRoomWithAgents: random judge model assigned",
				zap.String("user_id", userID),
				zap.String("model_key", pick))
		}
	}

	// Validate agent-seat requests up front, failing fast before any DB writes.
	agentSeatSet := make(map[int]struct{}, len(agentSeats))
	for _, a := range agentSeats {
		if _, dup := agentSeatSet[a.Seat]; dup {
			return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "duplicate agent seat")
		}
		agentSeatSet[a.Seat] = struct{}{}
	}

	// 2026-08-19 §德州扑克Agent: agent_seats 从狼人杀扩展到德州扑克。
	// werewolf: 13 座位; texasholdem: 6 座位; 其他游戏暂不支持。
	maxAgentSeats := 13
	if gameKind == "texasholdem" {
		maxAgentSeats = 6
	}
	if len(agentSeats) > 0 && gameKind != "werewolf" && gameKind != "texasholdem" {
		return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "agent_seats only supported for werewolf and texasholdem")
	}
	// agent 最多 N 座(werewolf 13 人局上限=13; texasholdem 6 人局上限=6)。
	// 不设 import werewolf/texasholdem 包(避免反向依赖),直接写死常量。
	if int64(len(agentSeats)) > 0 && int64(len(agentSeats)) > int64(maxAgentSeats) {
		return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "too many agent seats")
	}
	for _, a := range agentSeats {
		if a.Seat < 0 || a.Seat >= maxAgentSeats {
			return nil, errcode.CodeMsg(errcode.ErrValidationFailed, fmt.Sprintf("agent seat out of range [0,%d]", maxAgentSeats-1))
		}
		if strings.TrimSpace(a.ModelKey) == "" {
			return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "agent seat model_key required")
		}
		// 2026-08-06 §20260806-03: 角色名白名单校验(service 层不 import
		// werewolf 包避免反向依赖,白名单与 ParseRoleName 保持同步)。
		if !isSelectableRoleName(a.Role) {
			return nil, errcode.CodeMsg(errcode.ErrValidationFailed, fmt.Sprintf("agent seat %d: invalid role %q", a.Seat, a.Role))
		}
	}
	if !isSelectableRoleName(creatorRolePref) {
		return nil, errcode.CodeMsg(errcode.ErrValidationFailed, fmt.Sprintf("invalid creator_role %q", creatorRolePref))
	}

	// 2026-08-19 §德州扑克盲注透传 — 房间级盲注/买入校验(仅 texasholdem 生效)。
	// 两字段必须同时设置或同时缺省;缺省走 manager 默认值(200/10000)。
	// 校验必须在任何 DB 写入之前完成(fail-fast)。
	if texasCfg != nil && (texasCfg.BigBlind != 0 || texasCfg.StartStack != 0) {
		if gameKind != "texasholdem" {
			return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "big_blind/start_stack only supported for texasholdem")
		}
		if texasCfg.BigBlind == 0 || texasCfg.StartStack == 0 {
			return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "big_blind and start_stack must be set together")
		}
		switch texasCfg.BigBlind {
		case 10, 50, 200, 1000, 5000: // 与前端 BLIND_TIERS 对齐
		default:
			return nil, errcode.CodeMsg(errcode.ErrValidationFailed, fmt.Sprintf("invalid big_blind %d (allowed: 10/50/200/1000/5000)", texasCfg.BigBlind))
		}
		if lo, hi := 20*texasCfg.BigBlind, 100*texasCfg.BigBlind; texasCfg.StartStack < lo || texasCfg.StartStack > hi {
			return nil, errcode.CodeMsg(errcode.ErrValidationFailed, fmt.Sprintf("start_stack must be within [20bb,100bb] = [%d,%d]", lo, hi))
		}
	}

	// R106-20260712 P0: pre-write registry validation for every agent seat's
	// model_key. Order matters — this runs AFTER the cheap structural checks
	// above (range / duplicates / empty key) but BEFORE any DB write so an
	// invalid key fails the whole request atomically instead of persisting
	// rows that agent.New will later fail to drive (registry.Get → "unknown
	// model") — the silent degradation that produced 11/13 undrivable seats
	// in R106. The agentSeater impl consults the live LLM registry; if the
	// hook is nil (legacy callers / tests) we degrade gracefully: skip
	// validation but keep warning loudly so missing wiring is visible.
	if len(agentSeats) > 0 && s.agentSeater != nil {
		if e := s.agentSeater.ValidateAgentSeats(agentSeats); e != nil {
			logger.L().Warn("CreateRoomWithAgents: agent seat model_key validation failed",
				zap.String("user_id", userID),
				zap.Int("requested_agent_seats", len(agentSeats)),
				zap.String("message", e.Message))
			return nil, e
		}
	} else if len(agentSeats) > 0 && s.agentSeater == nil {
		logger.L().Warn("CreateRoomWithAgents: agentSeater not wired; skipping model_key validation (agent seats may silently fail to start)",
			zap.String("user_id", userID),
			zap.Int("requested_agent_seats", len(agentSeats)))
	}

	// AI 填充失败提示 (Round 25 BUG-WEREWOLF-P2-NEW-8 / -NEW-9 follow-up):
	// 当用户请求任意 agent_seats 但所有可用 provider 的 api_key 都是占位符
	// (API-KEY-PLACEHOLDER) 或空时,前端创建会"静默成功"但实际
	// 房间是 0 AI。这里在写入 DB 之前主动检测,返回 ErrLLMUnavailable
	// (30016),让前端弹 toast 说明原因,而不是默默吞掉。
	//
	// 2026-08-12 切走 cfg-Provider 改造: the candidate universe is now
	// llm.DefaultProviders() (the code-level default seed list) rather than
	// s.cfg.LLM.Providers (which is deprecated and may be empty/missing after
	// the cleanup). When the live model-availability hook is wired we still
	// filter through it so an admin-disabled row is excluded.
	//
	// BUG-20260812-04-A FIX(§TestReport/20260812_203948):原实现直接数
	// seed.APIKey 是否等于占位符,但 8e68b81 切到 llm.DefaultProviders() 后
	// seed.APIKey 恒为 types.PlaceholderKey,与 DB 注册表是否已注入真实密钥
	// 完全无关 —— 100% 误判 ErrLLMUnavailable(30016),建房即拒。
	//
	// 修复策略(单一事实来源):live registry hook 存在时,30016 预检直接以
	// IsModelAvailable(modelKey) 计数;hook 不存在(单测 / 老装配)才回退
	// 检查 seed.APIKey(原行为,保留作为最后防线)。
	if len(agentSeats) > 0 {
		seeds := s.modelAvailabilitySeeds()
		if len(seeds) == 0 {
			// No default models at all — surface as LLM-unavailable rather
			// than proceeding into a 0-AI room.
			logger.L().Warn("LLM unavailable: no default providers seeded (ServerGo/llm/defaults.go empty?)",
				zap.Int("requested_agent_seats", len(agentSeats)))
			return nil, errcode.Code(errcode.ErrLLMUnavailable)
		}
		usable := 0
		if s.modelAvailability != nil {
			// 正常生产路径:逐个 seed.model 喂给 live hook,只看 hook 的判定结果。
			// hook 内部已对接 registry 的真实密钥 + enabled 开关 +
			// api_key_hint(§118),不再读 seed.APIKey 这种已被占位符化的字段。
			for _, p := range seeds {
				if s.modelAvailability.IsModelAvailable(p.Model) {
					usable++
				}
			}
		} else {
			// 兜底路径:hook 未接线(单元测试 / 老装配)。原行为保留,
			// 用 seed.APIKey 是否为占位符判定 — 在该路径下 config 字段
			// 与运行时数据同源,判定有效。
			for _, p := range seeds {
				key := strings.TrimSpace(p.APIKey)
				if key != "" && key != "API-KEY-PLACEHOLDER" {
					usable++
				}
			}
		}
		if usable == 0 {
			source := "seed.APIKey"
			if s.modelAvailability != nil {
				source = "live ModelAvailabilityHook"
			}
			logger.L().Warn("LLM unavailable: no usable provider per LLM availability probe",
				zap.Int("requested_agent_seats", len(agentSeats)),
				zap.Int("configured_providers", len(seeds)),
				zap.String("source", source))
			return nil, errcode.Code(errcode.ErrLLMUnavailable)
		}
	}

	// BUG-WEREWOLF-P0-6 FIX + AI 随机分配扩展: historically the front-end
	// "model per seat" dropdown's onChange was missed (e.g. eval_js mutated
	// the <select> value without dispatching React's synthetic change event),
	// so every seat fell back to its default (MeiTuan-model). Server-side,
	// when len(agentSeats) > 1 and multiple agents would land on the same
	// model_key, we silently re-distribute duplicates to other configured
	// models in **random** order (Fisher-Yates shuffle in
	// alternateModelsLocked). This guarantees multi-model diversity even when
	// the front-end selection regresses, while still allowing the front-end
	// to drive the choice when it works correctly.
	//
	// Behaviour:
	//   - We accept whatever the front-end sent as the *seed* model for the
	//     first occurrence of each key — distinct user-picked models stick.
	//   - Duplicates and any later occurrence get reassigned from the
	//     alternate pool in random order; if the pool can't supply enough
	//     distinct models we wrap around (round-robin on the shuffled pool).
	if len(agentSeats) > 1 {
		alternates := s.alternateModelsLocked(agentSeats)
		if len(alternates) > 0 {
			seen := make(map[string]struct{}, len(agentSeats))
			poolIdx := 0
			for i := range agentSeats {
				key := strings.TrimSpace(agentSeats[i].ModelKey)
				if _, dup := seen[key]; dup {
					// Pick the next random alternate nobody else has yet.
					pick := ""
					for tries := 0; tries < len(alternates); tries++ {
						cand := alternates[poolIdx%len(alternates)]
						poolIdx++
						if _, taken := seen[cand]; !taken {
							pick = cand
							break
						}
					}
					if pick == "" {
						// All alternates taken (more duplicates than alternates);
						// fall back to a randomly-cycled slot from the pool.
						pick = alternates[poolIdx%len(alternates)]
						poolIdx++
					}
					logger.L().Warn("BUG-WEREWOLF-P0-6: agent seat duplicate model reassigned",
						zap.String("room_id_pending", "n/a"),
						zap.Int("seat", agentSeats[i].Seat),
						zap.String("from", key),
						zap.String("to", pick))
					agentSeats[i].ModelKey = pick
					key = pick
				}
				seen[key] = struct{}{}
			}
		}
	}

	// Check max_room limit.
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.TLsmGameRoom{}).
		Where("game_kind = ? AND status = ?", gameKind, "open").Count(&count).Error; err != nil {
		return nil, errcode.Code(errcode.ErrDB)
	}
	if int(count) >= s.cfg.Game.MaxRoom {
		return nil, errcode.Code(errcode.ErrMaxRoomsReached)
	}

	cap := 2
	switch gameKind {
	case "doudizhu":
		cap = 3
	case "texasholdem":
		cap = 6
	case "werewolf_12":
		cap = 12
	case "werewolf_7":
		cap = 7
	case "werewolf", "werewolf_13":
		cap = 13
	}
	roomID := util.NewUUID()
	// 房间名称:调用方未提供时自动生成 "{中文游戏名}房间-{短ID}",保证每个房间
	// 在大厅「房间名称」列都有可读且唯一的值,可被排序/搜索。短 ID 取 UUID 前 6 位。
	roomName := strings.TrimSpace(name)
	if roomName == "" {
		roomName = fmt.Sprintf("%s房间-%s", gameKindCN(gameKind), roomID[:6])
	}
	room := models.TLsmGameRoom{
		ID:           roomID,
		Name:         roomName,
		GameKind:     gameKind,
		Capacity:     cap,
		CurrentCount: 0,
		Status:       "open",
	}

	// Human creator seat: a RANDOM non-agent seat.
	// BUG-WEREWOLF-P0-1 FIX: when all 7 seats are taken by agents (the
	// "spectator creator" case the spec calls out as a supported flow), we
	// auto-downgrade the human creator to a spectator row instead of failing
	// the whole request with `no free seat for creator`. The agents still
	// drive the game; the human watches from the sidebar.
	//
	// 2026-07-22 修复: 旧逻辑取"第一个非 agent 空位"(for i:=0; i<cap; i++),
	// 配合前端"后 N 个座位给 AI"的固定分配,导致 1人类+12bot 时人类永远坐
	// 0 号位(用户感知为"12/13 号总是人类")。改为从所有非 agent 空位中
	// Fisher-Yates 随机选一个,人类座位真正随机化 —— 满足"人类玩家可能是
	// 任意号码"的需求。
	freeSeats := make([]int, 0, cap)
	for i := 0; i < cap; i++ {
		if _, taken := agentSeatSet[i]; !taken {
			freeSeats = append(freeSeats, i)
		}
	}
	creatorSeat := -1
	creatorAsSpectator := false
	if len(freeSeats) == 0 {
		// 2026-08-19 §德州扑克Agent: texasholdem 同样允许全 AI 房间(创建者降级为观战者)。
		if gameKind != "werewolf" && gameKind != "texasholdem" {
			// Other games don't allow spectator-creator semantics.
			return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "no free seat for creator")
		}
		creatorAsSpectator = true
		creatorSeat = -1 // sentinel: no player seat
	} else {
		// Fisher-Yates shuffle then pick the first — uniform random selection.
		// 使用 math/rand/v2 的全局 Shuffle(线程安全,熵种子),避免
		// time.Now().UnixNano() 在同秒内快速多次调用时种子相同导致伪随机退化
		// 为确定值(曾导致 3 人类场景 5 次请求全部返回 11 号位)。
		mrand.Shuffle(len(freeSeats), func(i, j int) {
			freeSeats[i], freeSeats[j] = freeSeats[j], freeSeats[i]
		})
		creatorSeat = freeSeats[0]
	}

	tx := s.db.WithContext(ctx).Begin()

	if err := tx.Create(&room).Error; err != nil {
		tx.Rollback()
		logger.L().Error("create room", zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}

	players := make([]models.TLsmGamePlayer, 0, len(agentSeats)+1)

	// 1) Agent seats first.
	for _, a := range agentSeats {
		// Use first 8 chars of UUID + seat to keep account ≤ 32 chars
		// (account column is VARCHAR(32)): "bot_" + 8 + "_" + 1-2 = 14-15 chars.
		botUID, e := s.getOrCreateBotUserID(ctx, fmt.Sprintf("%s_%d", room.ID[:8], a.Seat))
		if e != nil {
			tx.Rollback()
			return nil, e
		}
		players = append(players, models.TLsmGamePlayer{
			ID:       util.NewUUID(),
			RoomID:   room.ID,
			UserID:   botUID,
			Role:     BotUserRoleAgent,
			Seat:     a.Seat,
			ModelKey: a.ModelKey,
		})
	}

	// 2) Human creator: as a player when seats remain, as a spectator when
	//    the agents already cover all 7 (BUG-WEREWOLF-P0-1 fix).
	if creatorAsSpectator {
		players = append(players, models.TLsmGamePlayer{
			ID:     util.NewUUID(),
			RoomID: room.ID,
			UserID: userID,
			Role:   models.PlayerRoleSpectator,
		})
	} else {
		players = append(players, models.TLsmGamePlayer{
			ID:     util.NewUUID(),
			RoomID: room.ID,
			UserID: userID,
			Role:   models.PlayerRolePlayer,
			Seat:   creatorSeat,
		})
	}

	// 3) Persist all players in one transaction.
	for i := range players {
		if err := tx.Create(&players[i]).Error; err != nil {
			tx.Rollback()
			logger.L().Error("create player", zap.Error(err))
			return nil, errcode.Code(errcode.ErrDB)
		}
	}

	// 4) Update room's CurrentCount.
	//
	// BUG-WEREWOLF-CAPACITY-COUNT: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	seatCount := 0
	for _, p := range players {
		if p.Role == models.PlayerRolePlayer || p.Role == BotUserRoleAgent {
			seatCount++
		}
	}
	if err := tx.Model(&models.TLsmGameRoom{}).Where("id = ?", room.ID).
		UpdateColumn("current_count", seatCount).Error; err != nil {
		tx.Rollback()
		logger.L().Error("create room: update count", zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}

	if err := tx.Commit().Error; err != nil {
		logger.L().Error("create room: commit", zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}

	logger.L().Info("room created",
		zap.String("room_id", room.ID),
		zap.String("game_kind", gameKind),
		zap.String("user_id", userID),
		zap.Int("agent_seats", len(agentSeats)),
		zap.Int("creator_seat", creatorSeat),
		zap.Any("seat_model_keys", seatModelSummary(agentSeats)))

	// Mirror seats into the in-memory game manager. For rooms with agent
	// seats, ORDER matters: we pre-register each occupied seat via the agent
	// seater callback (which calls ManagerAddPlayerAt + SetSeatModelKey under
	// the hood), so the engine's GameState.Players reflect "agent seats
	// filled" and seatModelKeys records the per-seat LLM model. Then the
	// human creator's SyncSeat call triggers JoinGame (which auto-starts when
	// the room reaches capacity and kicks off the game's Bot goroutines).
	//
	// 2026-08-20 §德州扑克Agent注册P0修复:原条件仅 `gameKind == "werewolf"`,
	// 导致 texasholdem 含 agent_seats 的房间创建后,RegisterAgentSeats 从未被
	// 调用 → in-memory TexasHoldem manager 无 Bot → IsReady 只计 1 人
	// (人类) → 游戏永远卡在 waiting。修复:外层条件改为 werewolf | texasholdem,
	// 内层把狼人杀专属配置 (Judge/Difficulty/Commentary/RolePrefs) 收敛到
	// `gameKind == "werewolf"` 分支;texasholdem 路径只走 RegisterAgentSeats。
	// 未来新增游戏只需在 isAgentSupportedGameKind(gameKind) 加枚举即可。
	if (gameKind == "werewolf" || gameKind == "texasholdem") && len(agentSeats) > 0 && s.agentSeater != nil {
		// BUG-R136-RACE-001: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		if gameKind == "werewolf" {
			judgeMode := "agent"
			if judge != nil && judge.Mode != "" {
				judgeMode = judge.Mode
			}
			if e := s.agentSeater.SetJudgeConfig(gameKind, room.ID, judgeDesired, judgeMode, judgeModelKey); e != nil {
				logger.L().Warn("agent seater set judge config failed",
					zap.String("room_id", room.ID),
					zap.Int("code", e.Code),
					zap.String("msg", e.Message))
			}
			// §20260811-09 U2 — Agent 难度分级必须在 RegisterAgentSeats 之前落地
			// (同 SetJudgeConfig 时序约束:RegisterAgentSeats 在 13/13 满座时立即
			// 触发 StartAgentsLocked → 读取 r.agentDifficulty 决定 MemoryMD 加载
			// 与假说表注入,难度晚到即被 normal 兜底覆盖)。
			if e := s.agentSeater.SetAgentDifficulty(gameKind, room.ID, agentDifficulty); e != nil {
				logger.L().Warn("agent seater set difficulty failed",
					zap.String("room_id", room.ID),
					zap.Int("code", e.Code),
					zap.String("msg", e.Message))
			}
			// §20260811-09 U1 — AI 解说配置必须在 RegisterAgentSeats 之前落地
			// (同 SetJudgeConfig 时序约束:StartAgentsLocked 末尾会启 startCommentatorGoroutine,
			// 需要先读到 commentaryDesired=true 才能启动)。
			if e := s.agentSeater.SetCommentaryConfig(gameKind, room.ID, commentary); e != nil {
				logger.L().Warn("agent seater set commentary config failed",
					zap.String("room_id", room.ID),
					zap.Int("code", e.Code),
					zap.String("msg", e.Message))
			}
			// 2026-08-06 §20260806-03: 角色偏好必须在 RegisterAgentSeats 之前落地
			// (与 SetJudgeConfig 同时序 — RegisterAgentSeats 在 13/13 满座时立即
			// 触发 ForceStartIfReady → StartGame 发牌,偏好晚到即失效)。
			seatRolePrefs := make(map[int]string, len(agentSeats))
			for _, a := range agentSeats {
				if strings.TrimSpace(a.Role) != "" {
					seatRolePrefs[a.Seat] = a.Role
				}
			}
			if len(seatRolePrefs) > 0 || strings.TrimSpace(creatorRolePref) != "" {
				if e := s.agentSeater.SetSeatRolePrefs(gameKind, room.ID, seatRolePrefs, creatorRolePref); e != nil {
					logger.L().Warn("agent seater set role prefs failed",
						zap.String("room_id", room.ID),
						zap.Int("code", e.Code),
						zap.String("msg", e.Message))
				}
			}
		}
		// RegisterAgentSeats persists (botUserID, seat, modelKey) pairings in
		// the in-memory manager *before* the human creator joins. werewolf 走
		// ws.GameService.RegisterAgentSeats 的 werewolf 分支 → ManagerAddPlayerAt
		// + SetSeatModelKey + ForceStartIfReady;texasholdem 走同函数的
		// registerTexasHoldemAgentSeats → TexasHoldemManager.AddBotSeat + 启动
		// BotDriver。两类游戏共用同一入口,分支由 gameKind 决定(见 ws/game_service.go:230)。
		if e := s.agentSeater.RegisterAgentSeats(gameKind, room.ID, agentSeats); e != nil {
			// Non-fatal: the room is already created in DB; the agent seater
			// failure only means bots won't auto-play. Surface + continue.
			logger.L().Warn("agent seater registration failed",
				zap.String("room_id", room.ID),
				zap.String("game_kind", gameKind),
				zap.Int("code", e.Code),
				zap.String("msg", e.Message))
		}
	}
	// 2026-08-19 §德州扑克盲注透传 — 下发房间级盲注/买入到 in-memory manager。
	// 必须在 gameJoiner.SyncSeat 之前:SyncSeat → JoinGame 会用该配置初始化
	// GameState(BigBlind)与玩家初始筹码(StartStack),晚到即被默认值覆盖。
	if gameKind == "texasholdem" && texasCfg != nil && texasCfg.BigBlind > 0 && s.texasHoldemConfigurer != nil {
		s.texasHoldemConfigurer(room.ID, texasCfg.BigBlind, texasCfg.StartStack)
	}
	if s.gameJoiner != nil && !creatorAsSpectator {
		// Human creator sync — this is the canonical path. For werewolf rooms
		// with agent seats, this call sees 13/13 seats and triggers auto-start.
		//
		// BUG-WEREWOLF-P0-1 FIX: skip SyncSeat when the creator was downgraded
		// to a spectator row — SyncSeat calls JoinGame which expects a player
		// seat and would ErrRoomNotIn. Auto-start in the full-AI case is
		// handled inside RegisterAgentSeats via WerewolfManager.ForceStartIfReady.
		if started, e := s.gameJoiner.SyncSeat(gameKind, room.ID, userID); e != nil {
			logger.L().Warn("room created: game joiner sync failed",
				zap.String("room_id", room.ID),
				zap.Int("code", e.Code),
				zap.String("msg", e.Message))
		} else if started {
			logger.L().Info("game auto-started via HTTP room creation",
				zap.String("room_id", room.ID))
		}
	} else if creatorAsSpectator {
		logger.L().Info("room created (creator is spectator — full AI room)",
			zap.String("room_id", room.ID),
			zap.String("user_id", userID),
			zap.Int("agent_seats", len(agentSeats)))
	}

	// P1-1 (R88) FIX: response CurrentCount must mirror the DB row written at
	// line 672-677. Using len(players) here would include the creator-as-
	// spectator row (full-AI rooms), inflating the response by 1 (e.g. 13-seat
	// werewolf would report 14/13 in the lobby and trip JoinRoom's
	// current_count >= capacity guard for the next would-be joiner). seatCount
	// is already computed above for the DB column write — reuse it.
	// BUG-R200-P2-05: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	creatorMyRole := models.PlayerRolePlayer
	if creatorAsSpectator {
		creatorMyRole = models.PlayerRoleSpectator
	}
	rd := &RoomDetail{
		RoomInfo: RoomInfo{
			ID:           room.ID,
			Name:         room.Name,
			GameKind:     room.GameKind,
			Capacity:     room.Capacity,
			CurrentCount: seatCount,
			Status:       room.Status,
			CreatedAt:    room.CreatedAt,
		},
		MyRole: creatorMyRole,
	}
	for _, p := range players {
		// Spectators go in the Spectators list, not Players. Round-tripping
		// them as Players with Seat=-1 would break lobby UI which expects
		// Seat in [0, Capacity).
		if p.Role == models.PlayerRoleSpectator {
			rd.Spectators = append(rd.Spectators, RoomSpectatorInfo{UserID: p.UserID})
			continue
		}
		// P2-1 (R88) FIX: expose the discriminator Role on each Player so the
		// lobby can distinguish agent seats ("agent") from human players
		// ("player") without an extra round-trip. Frontend already handles
		// the discriminator on /api/rooms/:id (status=playing) — this brings
		// the create-time response to parity.
		rd.Players = append(rd.Players, RoomPlayerInfo{UserID: p.UserID, Seat: p.Seat, Role: p.Role})
	}
	return rd, nil
}

func (s *RoomService) JoinRoom(roomID, userID string) (*RoomDetail, *errcode.Error) {
	var room models.TLsmGameRoom
	if err := s.db.Where("id = ?", roomID).First(&room).Error; err != nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	if room.Status != "open" {
		return nil, errcode.Code(errcode.ErrRoomFull)
	}

	// Check if already in room — idempotent: return room detail for reconnecting users.
	//
	// BUG-R210-01 (2026-07-30): HTTP /api/rooms/:id/join must stamp MyRole on
	// the response so the frontend can route to the correct page (player vs
	// spectator) after a refresh. Use the user-aware variant.
	var existing models.TLsmGamePlayer
	if err := s.db.Where("room_id = ? AND user_id = ?", roomID, userID).First(&existing).Error; err == nil {
		// BUG-R210-02: if the user is already a spectator, the legacy behavior
		// returned the room detail (now would jump them back into the player
		// route which would 30012). Surface as ErrAlreadyInOtherRole so the
		// frontend can switch to the spectator route explicitly.
		if existing.Role == models.PlayerRoleSpectator {
			return nil, errcode.Code(errcode.ErrAlreadyInOtherRole)
		}
		return s.GetRoomDetailForUser(context.Background(), roomID, userID)
	}

	if room.CurrentCount >= room.Capacity {
		return nil, errcode.Code(errcode.ErrRoomFull)
	}

	// Determine seat (next available).
	var maxSeat int
	s.db.Model(&models.TLsmGamePlayer{}).Where("room_id = ?", roomID).Select("COALESCE(MAX(seat), -1)").Scan(&maxSeat)
	seat := maxSeat + 1

	player := models.TLsmGamePlayer{
		ID:     util.NewUUID(),
		RoomID: roomID,
		UserID: userID,
		Seat:   seat,
	}

	tx := s.db.Begin()
	if err := tx.Create(&player).Error; err != nil {
		tx.Rollback()
		logger.L().Error("join room: create player", zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	if err := tx.Model(&models.TLsmGameRoom{}).Where("id = ?", roomID).
		UpdateColumn("current_count", gorm.Expr("current_count + 1")).Error; err != nil {
		tx.Rollback()
		logger.L().Error("join room: update count", zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	tx.Commit()

	logger.L().Info("player joined room",
		zap.String("room_id", roomID),
		zap.String("user_id", userID))

	// Mirror the joiner's seat into the in-memory game manager. For 3-player
	// (doudizhu) and 6-player (texasholdem) games this is what triggers the
	// auto-deal / auto-start when the room reaches capacity via HTTP join.
	if s.gameJoiner != nil {
		if started, e := s.gameJoiner.SyncSeat(room.GameKind, roomID, userID); e != nil {
			logger.L().Warn("player joined: game joiner sync failed",
				zap.String("room_id", roomID),
				zap.Int("code", e.Code),
				zap.String("msg", e.Message))
		} else if started {
			logger.L().Info("game auto-started via HTTP join",
				zap.String("room_id", roomID),
				zap.String("game_kind", room.GameKind))
		}
	}

	return s.GetRoomDetailForUser(context.Background(), roomID, userID)
}

func (s *RoomService) LeaveRoom(roomID, userID string) *errcode.Error {
	var player models.TLsmGamePlayer
	if err := s.db.Where("room_id = ? AND user_id = ?", roomID, userID).First(&player).Error; err != nil {
		return errcode.Code(errcode.ErrRoomNotIn)
	}

	tx := s.db.Begin()
	if err := tx.Delete(&player).Error; err != nil {
		tx.Rollback()
		logger.L().Error("leave room: delete player", zap.Error(err))
		return errcode.Code(errcode.ErrDB)
	}

	// Decrement or delete room.
	var room models.TLsmGameRoom
	if err := tx.Where("id = ?", roomID).First(&room).Error; err != nil {
		tx.Rollback()
		return errcode.Code(errcode.ErrRoomNotFound)
	}

	if room.CurrentCount <= 1 {
		// Last player leaving — delete the room. Cascade: drop the room's
		// chat messages so an abandoned room doesn't leak history in the
		// shared t_lsm_game_chat_message table.
		if err := tx.Where("scope = ? AND room_id = ?", "room", roomID).
			Delete(&models.TLsmGameChatMessage{}).Error; err != nil {
			tx.Rollback()
			logger.L().Error("leave room: delete chat messages", zap.Error(err))
			return errcode.Code(errcode.ErrDB)
		}
		if err := tx.Delete(&room).Error; err != nil {
			tx.Rollback()
			logger.L().Error("leave room: delete room", zap.Error(err))
			return errcode.Code(errcode.ErrDB)
		}
	} else {
		if err := tx.Model(&room).UpdateColumn("current_count", gorm.Expr("current_count - 1")).Error; err != nil {
			tx.Rollback()
			logger.L().Error("leave room: update count", zap.Error(err))
			return errcode.Code(errcode.ErrDB)
		}
	}
	tx.Commit()

	logger.L().Info("player left room",
		zap.String("room_id", roomID),
		zap.String("user_id", userID))
	return nil
}

func (s *RoomService) DeleteRoomsByUser(ctx context.Context, userID string) error {
	var players []models.TLsmGamePlayer
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Find(&players).Error; err != nil {
		return errcode.Code(errcode.ErrDB)
	}
	if len(players) == 0 {
		return nil
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Dedup room IDs — the same room may appear multiple times if the
		// user had multiple player rows (shouldn't happen, but be safe).
		roomIDs := make(map[string]struct{}, len(players))
		for _, p := range players {
			if _, ok := roomIDs[p.RoomID]; ok {
				continue
			}
			roomIDs[p.RoomID] = struct{}{}
		}

		for roomID := range roomIDs {
			// Delete every player row for this user in this room.
			if err := tx.Where("room_id = ? AND user_id = ?", roomID, userID).
				Delete(&models.TLsmGamePlayer{}).Error; err != nil {
				return err
			}
			// Count remaining players in this room (canonical, not the
			// cached current_count column — it may be stale if the room
			// was already inconsistent).
			var remaining int64
			if err := tx.Model(&models.TLsmGamePlayer{}).
				Where("room_id = ?", roomID).Count(&remaining).Error; err != nil {
				return err
			}
			if remaining == 0 {
				// Cascade: drop chat messages tied to the room so an
				// empty room doesn't leak its history.
				if err := tx.Where("scope = ? AND room_id = ?", "room", roomID).
					Delete(&models.TLsmGameChatMessage{}).Error; err != nil {
					return err
				}
				if err := tx.Where("id = ?", roomID).Delete(&models.TLsmGameRoom{}).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Model(&models.TLsmGameRoom{}).Where("id = ?", roomID).
					UpdateColumn("current_count", remaining).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// GetRoomDetail is the legacy userID-less variant. It never stamps MyRole,
// so any caller relying on the field will see an empty string. New HTTP
// entry points (Join / Spectate / Detail) should call GetRoomDetailForUser.
//
// BUG-R210-01 (2026-07-30): retained for backward compat with internal
// callers (e.g. SSE/WS broadcast prep, internal cleanup paths) that don't
// have a userID in scope. HTTP layer must use the *ForUser variant.
func (s *RoomService) GetRoomDetail(roomID string) (*RoomDetail, *errcode.Error) {
	return s.GetRoomDetailForUser(context.Background(), roomID, "")
}

// GetRoomDetailForUser is the user-aware variant. After populating the
// standard room view it looks up the caller's role (player / agent / spectator)
// and stamps it on the returned RoomDetail.MyRole so the frontend can route
// to either /werewolf/:id (player / agent) or /werewolf/spectate/:id
// (spectator) without consulting a second endpoint.
//
// userID == "" → MyRole is left empty (legacy callers that don't need it).
func (s *RoomService) GetRoomDetailForUser(ctx context.Context, roomID, userID string) (*RoomDetail, *errcode.Error) {
	var room models.TLsmGameRoom
	if err := s.db.WithContext(ctx).Where("id = ?", roomID).First(&room).Error; err != nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}

	var rows []models.TLsmGamePlayer
	s.db.WithContext(ctx).Where("room_id = ?", roomID).Order("seat ASC, joined_at ASC").Find(&rows)

	var (
		pis = make([]RoomPlayerInfo, 0)
		sis = make([]RoomSpectatorInfo, 0)
	)
	// BUG-R210-01 (2026-07-30): track the caller's row so we can stamp MyRole
	// at the end. Reuse `rows` iteration to keep this single-pass.
	var myRole string
	for _, r := range rows {
		switch r.Role {
		case models.PlayerRoleSpectator:
			sis = append(sis, RoomSpectatorInfo{UserID: r.UserID})
		default:
			pis = append(pis, RoomPlayerInfo{UserID: r.UserID, Seat: r.Seat, Role: r.Role})
		}
		// Capture caller's role if found. Multiple rows for the same user
		// (shouldn't happen — DB primary key covers user_id+room_id) collapse
		// to the last one seen; player/spectator are mutually exclusive so
		// this is safe.
		if userID != "" && r.UserID == userID && myRole == "" {
			myRole = r.Role
		}
	}

	// Round 23 P1 BUG FIX: REST /api/rooms/:id previously only echoed
	// t_lsm_game_room.status, which never advanced to "playing" mid-game in
	// the lobby view. Now we surface Phase / RoundNumber from the in-memory
	// WerewolfManager for werewolf rooms so polling clients can render the
	// stage indicator without subscribing to WS frames.
	//
	// 2026-07-16 BUG-R128-03 修复: 同时透传 in-memory Status,覆盖 DB 行 status。
	var phase string
	var roundNumber int
	var status string
	var winner string
	if room.GameKind == "werewolf" && s.werewolfState != nil {
		if p, d, st, w, ok := s.werewolfState.WerewolfPublicState(roomID); ok {
			phase = p
			roundNumber = d
			status = st
			winner = w
		}
	}

	// R100 P1 BUG FIX: REST /api/rooms/{id} 的 players[] 之前只能从 DB
	// t_lsm_game_player 拿到 user_id/seat/role(DB 行角色),完全无法反映
	// in-memory GameState 的存活/死亡/角色揭示/死因/处决决断状态。
	// 现在合并 in-memory PublicPlayerStates 进 pis:对于每个 DB 行,如果
	// werewolfState 提供了对应 seat 的公开状态,就填充 alive/role_revealed/
	// werewolf_role/faction/death_cause/death_verdict/is_sheriff 字段。
	//
	// 设计要点:
	//   - 仅 werewolf 房间走合并路径,其他 5 款游戏行为完全不变
	//   - DB 行为主键(user_id+seat),in-memory 状态为辅;in-memory 缺失的
	//     座位(罕见,如 race condition)DB 行仍保留但所有新增字段为零值
	//   - hook 不可用(nil)或房间不在内存中时,DB-only 视图(原行为)
	if room.GameKind == "werewolf" && s.werewolfState != nil {
		liveStates := s.werewolfState.WerewolfPublicPlayerStates(roomID)
		if len(liveStates) > 0 {
			liveBySeat := make(map[int]WerewolfPublicPlayerState, len(liveStates))
			for _, ls := range liveStates {
				liveBySeat[ls.Seat] = ls
			}
			for i := range pis {
				ls, ok := liveBySeat[pis[i].Seat]
				if !ok {
					continue
				}
				alive := ls.Alive
				revealed := ls.RoleRevealed
				sheriff := ls.IsSheriff
				pis[i].Alive = &alive
				pis[i].RoleRevealed = &revealed
				pis[i].WerewolfRole = ls.Role
				pis[i].Faction = ls.Faction
				pis[i].DeathCause = ls.DeathCause
				pis[i].DeathVerdict = ls.DeathVerdict
				pis[i].IsSheriff = &sheriff
			}
		}
	}

	// BUG-R128-03: in-memory Status 是权威状态机视图,覆盖 DB 行 status。
	// DB status 仅在 onGameOver 回调时同步,冷却期 + restart_vote 阶段会滞后。
	finalStatus := room.Status
	if status != "" {
		finalStatus = status
	}

	return &RoomDetail{
		RoomInfo: RoomInfo{
			ID:             room.ID,
			Name:           room.Name,
			GameKind:       room.GameKind,
			Capacity:       room.Capacity,
			CurrentCount:   room.CurrentCount,
			SpectatorCount: len(sis),
			Status:         finalStatus,
			CreatedAt:      room.CreatedAt,
			Phase:          phase,
			RoundNumber:    roundNumber,
			Winner:         winner,
		},
		Players:    pis,
		Spectators: sis,
		MyRole:     myRole, // BUG-R210-01 (2026-07-30)
	}, nil
}

func (s *RoomService) SpectateRoom(roomID, userID string) (*RoomDetail, *errcode.Error) {
	var room models.TLsmGameRoom
	if err := s.db.Where("id = ?", roomID).First(&room).Error; err != nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	if room.Status != "open" && room.Status != "playing" {
		return nil, errcode.Code(errcode.ErrRoomFull)
	}

	var existing models.TLsmGamePlayer
	err := s.db.Where("room_id = ? AND user_id = ?", roomID, userID).First(&existing).Error
	switch {
	case err == nil && existing.Role == models.PlayerRolePlayer:
		// Already a seat-taking player; refuse the role flip.
		// BUG-R210-03 (2026-07-30): the legacy 30012 here caused the
		// frontend to surface "already in this room under a different role"
		// in a global toast. The right behaviour is to surface the existing
		// room detail with MyRole="player" so the frontend can route back
		// into the player page. We return ErrAlreadyInOtherRole here ONLY
		// when the caller is on the WS /spectate path (which needs to bail),
		// but the HTTP /api/rooms/:id/spectate entry can just echo the
		// existing player role. To keep both callers happy, HTTP layer
		// explicitly routes through here — they treat 30012 as "you're a
		// player; go there instead" (see WerewolfLobbyPage.handleSpectate).
		return nil, errcode.Code(errcode.ErrAlreadyInOtherRole)
	case err == nil && existing.Role == models.PlayerRoleSpectator:
		// Idempotent — but stamp MyRole="spectator" so the frontend knows
		// to route to /werewolf/spectate/:id, not /werewolf/:id.
		return s.GetRoomDetailForUser(context.Background(), roomID, userID)
	}
	// err != nil → no existing row. Insert a spectator row, but DO NOT bump
	// current_count on the parent room — spectators don't consume capacity.
	row := models.TLsmGamePlayer{
		ID:     util.NewUUID(),
		RoomID: roomID,
		UserID: userID,
		Role:   models.PlayerRoleSpectator,
	}
	if err := s.db.Create(&row).Error; err != nil {
		logger.L().Error("spectate room: insert", zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	logger.L().Info("spectator attached to room",
		zap.String("room_id", roomID),
		zap.String("user_id", userID))
	return s.GetRoomDetailForUser(context.Background(), roomID, userID)
}

func (s *RoomService) GameKindOf(roomID string) string {
	if roomID == "" {
		return ""
	}
	var room models.TLsmGameRoom
	if err := s.db.Where("id = ?", roomID).Select("game_kind").First(&room).Error; err != nil {
		return ""
	}
	return room.GameKind
}

func (s *RoomService) LeaveSpectate(roomID, userID string) *errcode.Error {
	var row models.TLsmGamePlayer
	if err := s.db.Where("room_id = ? AND user_id = ? AND role = ?",
		roomID, userID, models.PlayerRoleSpectator).First(&row).Error; err != nil {
		return errcode.Code(errcode.ErrRoomNotIn)
	}

	tx := s.db.Begin()
	if err := tx.Delete(&row).Error; err != nil {
		tx.Rollback()
		logger.L().Error("leave spectate: delete row", zap.Error(err))
		return errcode.Code(errcode.ErrDB)
	}

	// Count remaining rows of ANY role for this room.
	var remaining int64
	if err := tx.Model(&models.TLsmGamePlayer{}).Where("room_id = ?", roomID).
		Count(&remaining).Error; err != nil {
		tx.Rollback()
		return errcode.Code(errcode.ErrDB)
	}
	if remaining == 0 {
		var room models.TLsmGameRoom
		if err := tx.Where("id = ?", roomID).First(&room).Error; err == nil {
			// Cascade: drop the room's chat messages so an empty room
			// doesn't leak history.
			if err := tx.Where("scope = ? AND room_id = ?", "room", roomID).
				Delete(&models.TLsmGameChatMessage{}).Error; err != nil {
				tx.Rollback()
				logger.L().Error("leave spectate: delete chat messages", zap.Error(err))
				return errcode.Code(errcode.ErrDB)
			}
			if err := tx.Delete(&room).Error; err != nil {
				tx.Rollback()
				logger.L().Error("leave spectate: delete empty room", zap.Error(err))
				return errcode.Code(errcode.ErrDB)
			}
		}
	}
	tx.Commit()

	logger.L().Info("spectator left room",
		zap.String("room_id", roomID),
		zap.String("user_id", userID))
	return nil
}

func (s *RoomService) DeleteRoomIfEmpty(roomID string) (bool, *errcode.Error) {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var room models.TLsmGameRoom
	if err := tx.Where("id = ?", roomID).First(&room).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			tx.Rollback()
			return false, nil
		}
		tx.Rollback()
		logger.L().Error("delete room if empty: load room", zap.Error(err))
		return false, errcode.Code(errcode.ErrDB)
	}

	var remaining int64
	if err := tx.Model(&models.TLsmGamePlayer{}).Where("room_id = ?", roomID).Count(&remaining).Error; err != nil {
		tx.Rollback()
		logger.L().Error("delete room if empty: count players", zap.Error(err))
		return false, errcode.Code(errcode.ErrDB)
	}
	if remaining > 0 {
		tx.Rollback()
		return false, nil
	}

	// Cascade: drop the room's chat messages so an empty room doesn't leak
	// history.
	if err := tx.Where("scope = ? AND room_id = ?", "room", roomID).
		Delete(&models.TLsmGameChatMessage{}).Error; err != nil {
		tx.Rollback()
		logger.L().Error("delete room if empty: delete chat messages", zap.Error(err))
		return false, errcode.Code(errcode.ErrDB)
	}
	if err := tx.Where("id = ?", roomID).Delete(&models.TLsmGameRoom{}).Error; err != nil {
		tx.Rollback()
		logger.L().Error("delete room if empty: delete room", zap.Error(err))
		return false, errcode.Code(errcode.ErrDB)
	}

	if err := tx.Commit().Error; err != nil {
		logger.L().Error("delete room if empty: commit", zap.Error(err))
		return false, errcode.Code(errcode.ErrDB)
	}

	logger.L().Info("room auto-deleted because empty",
		zap.String("room_id", roomID),
		zap.String("game_kind", room.GameKind))
	return true, nil
}


