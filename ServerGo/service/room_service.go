package service

import (
	"fmt"
	mrand "math/rand/v2"
	"strings"
	"time"
	"LsmWebGame/config"
	"LsmWebGame/errcode"
	"LsmWebGame/models"
	"LsmWebGame/util"
	"gorm.io/gorm"
)

// BotUserRoleAgent is a cross-package alias so callers that want "this user is
// a bot agent" don't need to import models.
const BotUserRoleAgent = models.PlayerRoleAgent

// AgentSeatConfig is one agent seat requested by the caller of
// CreateRoomWithAgents. Seat is the in-room seat number (0..6); ModelKey is
// the LLM model id (e.g. "MeiTuan-model") that will drive the bot.
//
// Role (2026-08-06 §20260806-03 自选角色):可选的座位角色偏好,取值见
// werewolf.ParseRoleName 白名单(werewolf/seer/witch/hunter/idiot/guard/
// knight/demon_hunter/villager),空或 "random" = 随机(默认)。服务端在发牌后
// 做"牌组内座位置换"(多重集守恒),牌组中无此角色时降级为随机。
type AgentSeatConfig struct {
	Seat     int    `json:"seat"`
	ModelKey string `json:"model_key"`
	Role     string `json:"role,omitempty"`
}

// JudgeConfig 房间级法官(主持人)设置(创建者可选)。nil = 默认(有 Agent 时启用 Agent 法官)。
// Mode: "agent" | "human";空 = "agent"。ModelKey: 空 = 服务端随机分配。
//
// 2026-07-30 §重构:三选项(ai/human/off)合并为两选项(agent/human)。
// "agent" 即原 "主持人 Agent (法官)/ AI 法官"(LLM 驱动的 AgentJudge)。
// "human" 当前后端无真人接入实现,行为等同 agent — UI 占位,等真人法官
// WS 帧契约(gdocs/狼人杀-重构方案/主持人Agent重构设计.md §3.4)落地后再分流。
type JudgeConfig struct {
	Mode     string `json:"mode"`
	ModelKey string `json:"model_key"`
}

// GameJoiner is the callback RoomService invokes after a successful CreateRoom
// / JoinRoom so the in-memory game manager (doudizhu / texasholdem / …) can
// mirror the DB-level seat. Without this, HTTP joiners never trigger the
// manager's auto-deal path (which only fires on WS game.join), leaving
// State==nil and the client receiving my_hand=null / bottom=null → render
// crash → black screen.
//
// The implementation lives in ws.GameService; the interface here avoids an
// import cycle (service → ws).
type GameJoiner interface {
	SyncSeat(gameKind, roomID, userID string) (started bool, err *errcode.Error)
}

// AgentSeater is the callback RoomService uses to register agent bot seats
// into the werewolf manager BEFORE the human creator joins, so that the
// manager's GameState.Players + seatModelKeys reflect the bot seats. Without
// this, CreateRoomWithAgents persisted the DB rows but the in-memory engine
// never saw them → ManagerAddPlayerAt / SetSeatModelKey were never called
// (Phase 4 wiring, fixes P0 chain break from werewolf Agent e2e test 20260706).
//
// The implementation lives in ws.GameService; the interface here avoids an
// import cycle (service → ws).
//
// R106-20260712 P0: ValidateAgentSeats is the pre-write validation hook that
// CreateRoomWithAgents consults BEFORE committing any DB rows. Without it,
// invalid model_keys (e.g. "Doubao-model" instead of "DouBao-model", or
// registry-absent keys like "GPT-4o") are silently persisted; agent.New later
// fails registry.Get and the seat degrades to human, leaving watchdog to emit
// "agent_model=human" warnings and auto-skip every turn — 0% speak coverage.
// Returning a non-nil error fails the room-create request with a clear 400
// message so the caller learns the exact invalid keys instead of seeing a
// misleading "agent_seats_count: 13, all role=agent" with 11/13 seats
// undrivable.
type AgentSeater interface {
	RegisterAgentSeats(gameKind, roomID string, seats []AgentSeatConfig) *errcode.Error
	// ValidateAgentSeats checks every requested model_key against the live
	// LLM registry (must be registered, enabled, and carry a non-placeholder
	// api_key). Returns nil on success; a non-nil error fails the entire
	// room-create request with a clear validation message listing bad keys.
	ValidateAgentSeats(seats []AgentSeatConfig) *errcode.Error
	// 2026-07-16 主持人重构 — SetJudgeConfig 把房间级法官设置落到 in-memory
	// WerewolfRoom 上(JudgeDesired / JudgeMode / JudgeModelKey)。由
	// CreateRoomWithAgents 在 RegisterAgentSeats 之前调用(RegisterAgentSeats
	// 内部在座位 13/13 已满时会立即触发 ForceStartIfReady → startJudgeGoroutine,
	// 后者依赖 JudgeDesired 已置位,否则法官 goroutine 永不启动 — BUG-R136-RACE-001)。
	//
	// 2026-07-30 §重构:加 mode 参数透传 JudgeConfig.Mode("agent"/"human"),字段
	// 已落在 WerewolfRoom.JudgeMode;当前两值行为等价,字段保留便于未来分流。
	SetJudgeConfig(gameKind, roomID string, desired bool, mode string, modelKey string) *errcode.Error

	// 2026-08-11 §20260811-09 U2 — 把 Agent 难度分级落到 in-memory WerewolfRoom
	// (r.agentDifficulty)。由 CreateRoomWithAgents 在 SetJudgeConfig 之后、
	// RegisterAgentSeats 之前调用(同 SetJudgeConfig 时序约束 —— RegisterAgentSeats
	// 在 13/13 满座时会立即触发 ForceStartIfReady → StartAgentsLocked → 读取
	// r.agentDifficulty 决定 MemoryMD 加载 + 假说表注入,难度必须先就位)。
	// difficulty: easy/normal/hard/hell;空 / 未知值在 manager 内归一化为 normal。
	SetAgentDifficulty(gameKind, roomID string, difficulty string) *errcode.Error

	// 2026-08-11 §20260811-09 U1 — AI 实时解说(观战模式 🎙️ 解说席)。
	// 由 CreateRoomWithAgents 在 SetJudgeConfig 之后、RegisterAgentSeats 之前
	// 调用;manager.SetCommentaryConfig 把房间级 commentary* 写入 WerewolfRoom。
	// nil = 关闭(默认);非 nil 时按 Enabled/Style/ModelKey 启用。
	SetCommentaryConfig(gameKind, roomID string, cfg *CommentaryConfig) *errcode.Error

	// SetSeatRolePrefs 2026-08-06 §20260806-03 — 把座位级角色偏好(agent 座位 +
	// 创建者人类座位)落到 in-memory WerewolfRoom(seatPreferredRoles +
	// pendingCreatorRolePref)。由 CreateRoomWithAgents 在 RegisterAgentSeats
	// 之前调用(与 SetJudgeConfig 同时序约束:RegisterAgentSeats 在 13/13 满座
	// 时可能立即触发 ForceStartIfReady → StartGame 发牌,偏好必须先就位)。
	// prefs: seat → 角色名字符串;creatorPref: 创建者角色名;"random"/"" = 不指定。
	SetSeatRolePrefs(gameKind, roomID string, prefs map[int]string, creatorPref string) *errcode.Error
}

// WerewolfPublicPlayerState is a service-package-local mirror of
// game/werewolf.PublicPlayerState. We intentionally re-declare the struct
// here instead of importing werewolf directly to avoid an import cycle
// (service ← werewolf ← service via record_log). The ws-layer concrete
// adapter in ws/game_service.go projects the werewolf struct into this
// shape before returning.
//
// Field semantics mirror werewolf.PublicPlayerState verbatim — keep both
// in sync when extending. R100 P1 BUG FIX.
type WerewolfPublicPlayerState struct {
	Seat         int    `json:"seat"`
	UserID       string `json:"user_id"`
	Alive        bool   `json:"alive"`
	RoleRevealed bool   `json:"role_revealed"`
	Role         string `json:"role,omitempty"`
	Faction      string `json:"faction,omitempty"`
	DeathCause   string `json:"death_cause,omitempty"`
	DeathVerdict string `json:"death_verdict,omitempty"`
	IsSheriff    bool   `json:"is_sheriff"`
}

// CommentaryConfig 是 §20260811-09 U1 房间级解说设置(service-package-local mirror,
// 与 JudgeConfig 同款避免循环 import)。与 werewolf.CommentaryConfig 字段
// 完全一致,ws-layer concrete adapter 在 SetCommentaryConfig 内做转换。
type CommentaryConfig struct {
	Enabled  bool   `json:"enabled,omitempty"`
	Style    string `json:"style,omitempty"`     // "pro" | "fun";空/未知值 → "pro"
	ModelKey string `json:"model_key,omitempty"` // 空 → 复用 JudgeModelKey;再空 → 随机
}
// engine's current phase/day/winner for werewolf rooms. Returns ("", 0, "", false) when
// the room has no live in-memory state yet (e.g. lobby list before the first
// player joins) or when the hook is nil (unit tests). Round 23 P1 BUG 修复:
// the previous RoomDetail / RoomInfo payload only echoed `status` from
// t_lsm_game_room, which never transitioned to "playing" mid-game in the
// lobby view — `phase` was therefore always "?" to REST clients even though
// the WS broadcast had already moved on. Surfacing phase/day/winner here is
// safe: all three fields are public to all viewers (no seat-specific secrets).
//
// The implementation lives in ws.GameService.WerewolfManager(); the interface
// here avoids an import cycle (service → ws).
//
// R100 P1 BUG FIX: 新增 WerewolfPublicPlayerStates(roomID) 方法让
// RoomService.GetRoomDetail 能用 in-memory GameState 填充 players[] 的
// alive/role/death_cause/death_verdict 字段,把 REST 详情接口从「DB-only
// 镜像」升级为「DB + 引擎实时状态」合并视图,与前端 UI 的死亡徽章 +
// 角色揭示 + 处决/死亡决断显示保持一致。
type WerewolfStateHook interface {
	// WerewolfPublicState 返回房间的公开对局状态。
	// 2026-07-16 BUG-R128-03 修复: 新增 status 返回值,让房间列表 API 能
	// 使用 in-memory GameState.Status 覆盖 DB 行 status,避免 restart_vote
	// 阶段 DB status 仍为 "playing" 的脱钩问题。
	WerewolfPublicState(roomID string) (phase string, day int, status string, winner string, ok bool)
	// WerewolfPublicPlayerStates 返回每个占用座位的公开对局状态
	// (alive/role_revealed/role/death_cause/death_verdict/is_sheriff)。
	// 返回 nil 表示房间不存在或 hook 不可用,调用方降级为 DB-only 视图。
	WerewolfPublicPlayerStates(roomID string) []WerewolfPublicPlayerState
}

// ModelAvailabilityHook reports whether a model_key is currently usable by a
// bot seat / judge goroutine (registered && enabled && non-placeholder
// api_key) according to the live LLM registry. The implementation lives in
// ws.GameService (via WerewolfManager.Registry().IsAvailable); the interface
// here avoids an import cycle (service → ws / service → llm).
//
// R187-2: usableProviderModels filters cfg.LLM.Providers statically (non-empty
// + non-placeholder key) but cannot see runtime state — a provider disabled
// via /api/admin/llm/providers/:id, or one whose DB row no longer matches
// cfg, would still be picked by the duplicate-reassignment Fisher-Yates
// allocator, landing a bot seat on a model that agent.New can never drive.
// When this hook is wired, usableProviderModels additionally filters each
// candidate through IsModelAvailable. Nil hook (unit tests / legacy wiring)
// keeps the previous cfg-only behaviour.
type ModelAvailabilityHook interface {
	IsModelAvailable(modelKey string) bool
}

// RoomService manages game rooms (create / join / leave / list).
type RoomService struct {
	db          *gorm.DB
	cfg         *config.Config
	gameJoiner  GameJoiner
	agentSeater AgentSeater
	// gameSvc is the optional in-memory game state controller used by
	// ForceDisbandRoom to tear down per-manager GameState + broadcast
	// `game.removed` to connected clients. Wired via SetGameServiceHook
	// from main.go; nil in unit tests that don't exercise force disband.
	gameSvc GameServiceAPI
	// hubHook is the optional hub probe used by BootCleanupOrphanedAgentRooms
	// to skip rooms that still have connected players. Wired via SetHubHook
	// from main.go.
	hubHook HubAPI
	// werewolfState is the optional accessor used by GetRoomDetail to surface
	// the in-memory WerewolfManager's current phase/day for werewolf rooms.
	// Wired via SetWerewolfStateHook from main.go; nil-safe (Phase/RoundNumber
	// simply stay "" / 0 for werewolf rooms when the hook is missing).
	werewolfState WerewolfStateHook
	// modelAvailability is the optional live-registry probe used by
	// usableProviderModels to drop runtime-disabled providers from the
	// random-reassignment pool (R187-2). Wired via SetModelAvailabilityHook
	// from main.go; nil keeps the legacy cfg-only filter.
	modelAvailability ModelAvailabilityHook
}

// NewRoomService builds a RoomService.
func NewRoomService(db *gorm.DB, cfg *config.Config) *RoomService {
	return &RoomService{db: db, cfg: cfg}
}

// SetGameJoiner registers the optional post-join callback. Must be called
// before the service starts serving requests.
func (s *RoomService) SetGameJoiner(gj GameJoiner) {
	s.gameJoiner = gj
}

// SetAgentSeater registers the optional callback that mirrors agent bot seats
// into the in-memory werewolf manager (used by CreateRoomWithAgents). Must be
// called before the service starts serving requests.
func (s *RoomService) SetAgentSeater(as AgentSeater) {
	s.agentSeater = as
}

// SetWerewolfStateHook registers the optional accessor used by GetRoomDetail
// to enrich RoomDetail.Phase / RoomDetail.RoundNumber for werewolf rooms.
// nil-safe: the hook is allowed to stay nil in tests, in which case the new
// fields simply stay "" / 0 and REST callers fall back to the t_lsm_game_room
// row (Round 23 P1 BUG behaviour).
func (s *RoomService) SetWerewolfStateHook(h WerewolfStateHook) {
	s.werewolfState = h
}

// SetModelAvailabilityHook registers the live-registry availability probe
// (R187-2). nil-safe: when unset, usableProviderModels keeps its cfg-only
// filter behaviour.
func (s *RoomService) SetModelAvailabilityHook(h ModelAvailabilityHook) {
	s.modelAvailability = h
}

// BotUserIDForSeat returns the bot userID occupying `seat` in `roomID`. Used by
// the agent seater to resolve the freshly-created bot user row without a second
// round-trip query by the caller. Returns "" with a ErrRoomNotFound-like error
// if no bot is at that seat.
func (s *RoomService) BotUserIDForSeat(roomID string, seat int) (string, error) {
	var p models.TLsmGamePlayer
	if err := s.db.Where("room_id = ? AND seat = ? AND role = ?", roomID, seat, BotUserRoleAgent).First(&p).Error; err != nil {
		return "", err
	}
	return p.UserID, nil
}

// AgentSeatInfo describes a persisted agent seat (used for restart recovery and
// for back-filling the in-memory werewolf manager). ModelKey is the LLM model
// id; UserID is the bot user row.
type AgentSeatInfo struct {
	Seat     int    `json:"seat"`
	UserID   string `json:"user_id"`
	ModelKey string `json:"model_key"`
}

// BotSeatsForRoom returns every agent seat configured for `roomID`, ordered by
// seat. Used by the werewolf manager's restart-recovery path to rebuild the
// in-memory room + seatModelKeys from the persisted t_lsm_game_player rows.
//
// BUG-WEREWOLF-P0-7 FIX: without this, the in-memory manager forgets bot seats
// on restart (only the DB rows survive), and the next spectator/player join
// creates an empty room that never force-starts.
func (s *RoomService) BotSeatsForRoom(roomID string) ([]AgentSeatInfo, error) {
	var ps []models.TLsmGamePlayer
	if err := s.db.Where("room_id = ? AND role = ?", roomID, BotUserRoleAgent).
		Order("seat ASC").Find(&ps).Error; err != nil {
		return nil, err
	}
	out := make([]AgentSeatInfo, 0, len(ps))
	for _, p := range ps {
		out = append(out, AgentSeatInfo{
			Seat:     p.Seat,
			UserID:   p.UserID,
			ModelKey: p.ModelKey,
		})
	}
	return out, nil
}

// SeatsForRoom returns every seat-having player row for `roomID` — both
// human "player" rows AND agent rows, ordered by seat. Used by the werewolf
// manager's mixed-room restart-recovery path: if a server restart forgets the
// in-memory GameState, the next spectator attach must repopulate ALL seats
// (human + bot) so IsReady()==13/13 and the force-start path can fire. Without
// this, mixed (human + bots) rooms stay stuck at phase=filling forever even
// though the DB row says status=playing.
//
// BUG-WEREWOLF-SPECTATE-FILLING FIX (Round 24): the older BotSeatsForRoom
// only returned agent seats, so a mixed room's r.Occupied() never reached
// MaxPlayers after restart. The new SpectateGame branch that synthesizes a
// fresh NewGame when r.State == nil never fired for mixed rooms.
func (s *RoomService) SeatsForRoom(roomID string) ([]AgentSeatInfo, error) {
	var ps []models.TLsmGamePlayer
	if err := s.db.Where("room_id = ? AND seat >= 0", roomID).
		Where("role IN ?", []string{BotUserRoleAgent, models.PlayerRolePlayer}).
		Order("seat ASC").Find(&ps).Error; err != nil {
		return nil, err
	}
	out := make([]AgentSeatInfo, 0, len(ps))
	for _, p := range ps {
		out = append(out, AgentSeatInfo{
			Seat:     p.Seat,
			UserID:   p.UserID,
			ModelKey: p.ModelKey, // empty for human players; agent seats carry the LLM model key
		})
	}
	return out, nil
}

// usableProviderModels returns the configured LLM model keys that are actually
// safe to drive a bot seat / judge goroutine: model name must be non-empty,
// the provider's API key must be non-empty and NOT the documented
// "API-KEY-PLACEHOLDER" sentinel, and the returned list is deduplicated by
// model (first occurrence wins — the canonical config.LLM.Providers order).
//
// Used by both the seat-duplicate reassignment path (BUG-WEREWOLF-P0-6 +
// AI 随机分配扩展) and the random-judge-model pick. Centralising the filter
// rules here keeps the two consumers in lock-step so e.g. a future
// "enabled" toggle or new placeholder value only has to land in one place.
//
// Returns an empty slice when no cfg is wired (unit tests) or when no
// provider passes the filter.
func (s *RoomService) usableProviderModels() []string {
	if s.cfg == nil || s.cfg.LLM.Providers == nil {
		return nil
	}
	out := make([]string, 0, len(s.cfg.LLM.Providers))
	seen := make(map[string]struct{}, len(s.cfg.LLM.Providers))
	for _, p := range s.cfg.LLM.Providers {
		// R187-2: sanitize so this pool stays consistent with the registry
		// (which sanitizes at load) even if cfg was pasted with invisible
		// Cf runes.
		k := util.SanitizeModelKey(p.Model)
		if k == "" {
			continue
		}
		// Skip placeholder / empty keys — picking one of those would just
		// fail at LLM call time with "no usable api_key". Hide them from
		// the pool entirely.
		if strings.TrimSpace(p.APIKey) == "" || p.APIKey == "API-KEY-PLACEHOLDER" {
			continue
		}
		// R187-2: when the live registry probe is wired, also drop models
		// that are runtime-unavailable (disabled via admin API, DB row
		// diverged from cfg, placeholder re-detected at registry load).
		// Without this the duplicate-reassignment allocator could reassign
		// a seat to a key that ValidateAgentSeats / agent.New would reject.
		if s.modelAvailability != nil && !s.modelAvailability.IsModelAvailable(k) {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

// pickRandomJudgeModelKey returns a uniformly-random model_key from the
// usable provider pool (Fisher-Yates shuffle on a copy + take [0]). Returns
// "" when no usable model exists — caller is expected to leave judgeModelKey
// empty so the downstream code (startJudgeGoroutine, GenerateSummary) keeps
// its existing recovery fallback (cfgWerewolfJudgeModelKey → seatModelKeys
// → "judge-default").
//
// Uses math/rand/v2's global entropy-seeded source (thread-safe, no manual
// seeding). We don't need crypto-strength randomness for "which model hosts
// the judge in this room".
func (s *RoomService) pickRandomJudgeModelKey() string {
	pool := s.usableProviderModels()
	if len(pool) == 0 {
		return ""
	}
	mrand.Shuffle(len(pool), func(i, j int) {
		pool[i], pool[j] = pool[j], pool[i]
	})
	return pool[0]
}

// alternateModelsLocked returns the configured LLM model keys excluding the
// ones already requested by `seats`. Used by the duplicate-reassignment path
// (BUG-WEREWOLF-P0-6 + AI 随机分配扩展) so that when the front-end submits
// the same model_key for every seat we can re-distribute duplicates to models
// that are actually available. The returned slice is in **random order** so
// the round-robin-style reassignment picks a different model on every call,
// not just the alphabetically-first one.
//
// Filtering rules (model non-empty / API key non-empty / no placeholder /
// dedup-by-model) are delegated to usableProviderModels so this helper
// stays a thin "exclude + shuffle" wrapper. Returns an empty slice when no
// cfg is wired (unit tests) or when only one model is configured (nothing
// to alternate with).
func (s *RoomService) alternateModelsLocked(seats []AgentSeatConfig) []string {
	pool := s.usableProviderModels()
	if len(pool) == 0 {
		return nil
	}
	used := make(map[string]struct{}, len(seats))
	for _, a := range seats {
		used[util.SanitizeModelKey(a.ModelKey)] = struct{}{}
	}
	out := make([]string, 0, len(pool))
	for _, k := range pool {
		if _, dup := used[k]; dup {
			continue
		}
		out = append(out, k)
	}
	// Fisher-Yates shuffle using math/rand/v2's global entropy-seeded source
	// (thread-safe, no manual seeding). Consecutive rooms pick different
	// alternates — math/rand is fine here, we don't need crypto-strength
	// randomness for "which model drives this bot seat".
	mrand.Shuffle(len(out), func(i, j int) {
		out[i], out[j] = out[j], out[i]
	})
	return out
}

// seatModelSummary renders a compact `[seat:model, ...]` summary of the
// final agent-seat → model_key mapping. BUG-WEREWOLF-P0-6 helper: makes the
// "room created" log line self-documenting so duplicate-model regressions
// are visible in one log line instead of needing to cross-reference 7
// "agent started" lines.
func seatModelSummary(seats []AgentSeatConfig) string {
	if len(seats) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(seats))
	for _, a := range seats {
		parts = append(parts, fmt.Sprintf("%d:%s", a.Seat, a.ModelKey))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// isSelectableRoleName 校验创建房间时的角色名偏好(2026-08-06 §20260806-03)。
// 白名单与 game/werewolf.ParseRoleName 保持同步 — service 层不 import
// werewolf 包(避免 service ← werewolf ← service 循环依赖),此处独立维护。
// ""/"random" 合法(= 随机,不注入)。
func isSelectableRoleName(name string) bool {
	switch strings.TrimSpace(name) {
	case "", "random",
		"werewolf", "seer", "witch", "hunter", "idiot",
		"guard", "knight", "demon_hunter", "villager":
		return true
	default:
		return false
	}
}

// RoomInfo is the public view of a room.
type RoomInfo struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	GameKind       string    `json:"game_kind"`
	Capacity       int       `json:"capacity"`
	CurrentCount   int       `json:"current_count"`
	SpectatorCount int       `json:"spectator_count,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`

	// MyRole (2026-07-30 BUG-R210-01): 当前请求者在该房间的角色。
	// 取值: "player"(真人玩家)/ "agent"(bot 座位)/ "spectator"(观战者)/ ""(无关)。
	// 仅在 ListRooms(大厅列表)里填充;Detail 接口的 MyRole 在 RoomDetail 上,
	// 因为 Detail 的 MyRole 语义与 list 一致,前端 lobby 用 RoomInfo.MyRole 即可。
	// 之前 ListRooms 不下发,刷新后 lobby 看不到"我是不是在这个房间里",按钮
	// 退化为 joinable() 兜底 → 永远显示"已满"。
	MyRole string `json:"my_role,omitempty"`

	// Phase 是当前对局阶段机所处的环节(如 "night_wolves"/"speak"/"vote"/
	// "over"/"filling"),仅 werewolf 房间由 in-memory WerewolfManager 提供,
	//其余 5 款游戏本字段为 ""。Round 23 P1 BUG 修复:REST 房间详情接口
	//此前未暴露该字段,前端无法获取阶段显示。
	Phase string `json:"phase,omitempty"`
	// RoundNumber 是当前对局天/回合数(狼人杀 DayNumber),仅 werewolf
	// 房间填充;其余游戏为 0。
	RoundNumber int `json:"round_number,omitempty"`
	// Winner 是狼人杀对局胜方("wolf"|"good"|""),由 in-memory 引擎在
	// Status 切到 "over" 时填入(R66 defect 3.2 修复:此前 REST 详情接口
	// 只回显 DB status="over"但不暴露胜方,客户端/测试报告无法确认谁赢)。
	// 其余 4 款游戏本字段为 ""。R66 修复: 作为 in-memory 字段透传,不新增
	// DB 列,不写 DB,纯内存快照。
	Winner string `json:"winner,omitempty"`
}

// gameKindCN maps a game-kind code to its Chinese display name, used to
// auto-generate a human-readable room name when the caller doesn't supply one.
// Keep this in sync with the frontend i18n game titles.
func gameKindCN(kind string) string {
	switch kind {
	case "xiangqi":
		return "象棋"
	case "chess":
		return "国际象棋"
	case "junqi":
		return "军棋"
	case "doudizhu":
		return "斗地主"
	case "texasholdem":
		return "德州扑克"
	case "werewolf":
		return "狼人杀"
	default:
		return kind
	}
}

// RoomPlayerInfo describes a player in a room.
//
// R100 P1 BUG FIX: 新增 alive/role_revealed/role(death 后揭示)/
// faction/death_cause/death_verdict/is_sheriff 字段,仅 werewolf 房间由
// in-memory WerewolfManager 填充;其他 5 款游戏保持空值/零值(原 RoomInfo
// 行为不变)。所有新增字段都是 omitempty,旧客户端解析不会出问题。
//
// 字段含义与 game/werewolf/PublicPlayerState 完全对齐,前端可据此渲染
// 死亡徽章 / 角色揭示 / 处决/死亡决断(§123)显示。
type RoomPlayerInfo struct {
	UserID string `json:"user_id"`
	Seat   int    `json:"seat"`
	// Role 是 DB 行的角色判别符: "player"(真人)或 "agent"(LLM bot 座位)。
	// 注意:这里的 Role 是 DB 层概念,不要与下面的 werewolf_role(身份牌)混淆。
	// 可选字段,保留向后兼容。
	Role string `json:"role,omitempty"`
	// Alive 标记玩家是否存活。werewolf 房间由 in-memory GameState 填充;
	// 其他游戏为 nil(false)。
	Alive *bool `json:"alive,omitempty"`
	// RoleRevealed 标记"该座位的身份牌是否可公开"。仅当玩家已死亡或
	// GameOver 时为 true。werewolf 专属字段,其他游戏省略。
	RoleRevealed *bool `json:"role_revealed,omitempty"`
	// WerewolfRole 仅在 RoleRevealed=true 时填充,枚举字符串
	// (werewolf/seer/witch/hunter/idiot/villager/...)。非 werewolf 房间省略。
	WerewolfRole string `json:"werewolf_role,omitempty"`
	// Faction 阵营: "wolf"|"good"|"third"。仅 werewolf 房间且 RoleRevealed=true。
	Faction string `json:"faction,omitempty"`
	// DeathCause: wolf/vote/hunter/witch_poison/suicide,未死亡为空。werewolf 专属。
	DeathCause string `json:"death_cause,omitempty"`
	// DeathVerdict: execution/death(§123 处决/死亡决断),未死亡为空。werewolf 专属。
	DeathVerdict string `json:"death_verdict,omitempty"`
	// IsSheriff 警长标记,仅 werewolf 房间且为警长时为 true。
	IsSheriff *bool `json:"is_sheriff,omitempty"`
}

// RoomSpectatorInfo describes a spectator in a room. Only the userID is exposed;
// the lobby uses this for the "观战 N 人" indicator.
type RoomSpectatorInfo struct {
	UserID string `json:"user_id"`
}

// RoomDetail is the full room view including the player + spectator lists.
//
// MyRole (2026-07-30 BUG-R200-P2-05): 显式告知"当前请求者在该房间的角色"。
// 取值: "player"(真人玩家) / "agent"(bot 座位) / "spectator"(观战者) / ""(无关)。
// 仅在创建/加入/观战等有"当前请求者"语义的响应里填充;列表接口(ListRooms)留空。
// 前端据此决定走玩家路由(/werewolf/:id)还是观众路由(/werewolf/spectate/:id),
// 不再依赖 `agent_seats.length >= capacity` 推断。
type RoomDetail struct {
	RoomInfo
	Players    []RoomPlayerInfo    `json:"players"`
	Spectators []RoomSpectatorInfo `json:"spectators,omitempty"`
	MyRole     string              `json:"my_role,omitempty"`
}

// ListRooms returns every lobby-visible room for the given game kind.
//
// Both "open" (waiting for players) and "playing" (already started) rooms
// are returned so users can spectate active matches or join mid-game rooms
// (e.g. werewolf 13-seat games can be entered while a game is in progress
// only as a spectator — see game_service spectator handling). Closed
// ("over"/"closed") rooms are filtered out so the lobby stays clean.
//
// Spectator counts are not pre-fetched here (that would require a per-room
// GROUP BY join against t_lsm_game_player on every poll). The lobby can fetch
// them lazily via GetRoomDetail when needed.

// getOrCreateBotUserID returns the userID of a bot user, creating it on
// demand. The account follows the documented `bot_<suffix>` convention. We
// keep accounts unique per (seat, room) so GameState.PlayerByID disambiguates
// agent seats; bot users are never expected to log in via the auth flow and
// their rows are only ever consumed by the werewolf Agent runner.
//
// BUG-R129-03 FIX (R129 报告 P1): for newly created bot users we now seed a
// t_lsm_game_wallet row immediately via WalletService.CreateWallet so the
// coin system (§135 / commit 9161113 Bot 金币目标) sees the same row count
// in t_lsm_game_user (is_bot=1) and t_lsm_game_wallet. R129 created 12 Bots
// across 8 LLM models but only 9 wallets showed up — the missing ones were
// the freshly-created bot users whose wallet seed was never called from the
// getOrCreateBotUserID path. CreateWallet is idempotent, so the existing
// 9-wallet rows are left alone and only the missing 3 get backfilled.
//
// Failure to register the bot user is fatal for room creation — the
// caller cannot meaningfully run a bot seat without a backing user row, so
// we return an error rather than a partial room.

// CreateRoom creates a new room and adds the creator as the first player.
// Implemented on top of CreateRoomWithAgents for a single source of truth.
// `name` may be empty — CreateRoomWithAgents auto-generates a default name.

// CreateRoomWithAgents creates a new room with the given human creator plus
// optional AI agent seats. agentSeats may be nil / empty to mean "no agents"
// and behave identically to CreateRoom.
//
// Behaviour:
//   - capacity is set per game kind (werewolf=13).
//   - Each agent seat pre-registers a `TLsmGamePlayer` row with Role="agent",
//     the requested Seat number, and a freshly-created bot user with
//     account="bot_<room>_<seat>". Its ModelKey is stored on the row and
//     mirrored into the in-memory werewolf manager via SetSeatModelKey.
//   - The human creator is placed in a RANDOM empty non-agent seat
//     (uniform over all non-agent seats, Fisher-Yates). They get Role="player".
//   - All occupied seats are mirrored into the in-memory game manager via
//     SyncSeat, which can auto-start when all 13 seats are filled.
//
// Validation (for werewolf agent-seats requests):
//   - at most 7 agent seats
//   - seats are unique and in [0,6]
//   - ModelKey non-empty
//   - a seat is not simultaneously agent + creator

// JoinRoom adds a user to an open room.

// LeaveRoom removes a user from a room. Deletes the room if it becomes empty.

// DeleteRoomsByUser removes the given user from all rooms they belong to.
// For each room, the player row is deleted and the room's CurrentCount is
// decremented; if the count drops to zero the room itself is deleted.
//
// Runs in a single transaction so the operation is atomic and fast.
// Called by the admin user-delete flow after DeleteUserWithRelatedData.

// GetRoomDetail returns the room, its player list (seat order) and its
// spectator list (join order).

// SpectateRoom registers a user as an observer of a room. Idempotent:
// if the user is already a spectator of the room, the room detail is
// returned unchanged. If the user is already a player of the room, returns
// ErrAlreadyInOtherRole.
//
// BUG-WEREWOLF-SPECTATE-FULL FIX: status gate changed from == "open" to
// ∈{"open","playing"}. Spectators must be able to enter an in-progress
// 13/13 werewolf room to watch the live game — that's the primary use of the
// "观战" button in the lobby. The previous gate rejected exactly that case
// with the misleading ErrRoomFull ("room is full") message, leaving
// spectators stranded on the lobby. Only finished rooms ("over"/"closed")
// refuse new spectators, since the live broadcast has already wound down.

// GameKindOf 返回 roomID 对应的 t_lsm_game_room.game_kind。找不到房间时返回
// 空字符串。
//
// BUG-R118-02 (2026-07-14): ws.game_service.handleSpectate 在 payload 未带
// game_kind 时回退到默认 "xiangqi",导致狼人杀房间的 `game.spectated`
// 响应 game_kind 错乱(xiangqi),前端据此加载错误 UI。调用方应在 payload 缺
// game_kind 时以此方法反查权威值,避免兜底默认值误导路由。

// LeaveSpectate removes a spectator row from a room. If the room has no
// remaining rows (no players, no spectators), the room itself is deleted.
//
// Refusing to delete a non-empty room guards against the spectator leaving
// while a player is mid-game: the WS broadcast target (`hub.rooms`) is a
// connection-level set, not a DB one, so the player is unaffected anyway.

// DeleteRoomIfEmpty removes a room from the database only if it has no
// remaining players or spectators. It is idempotent: if the room is already
// gone, or if it still has occupants, it returns false without error.

// JanitorStats is a snapshot of one JanitorSweep pass — used both for logging
// and for the /api/health response so operators can see "did the sweep run?".

// UpdateRoomStatus updates the status of a room in the database.
// Typically called when a game starts (status="open" → "playing") or ends
// (status="playing" → "over").

// JanitorSweep runs one pass of "clean up orphaned rooms":
//
//  1. List every room whose status='open' AND created_at < cutoff
//     (i.e. rooms that have been around long enough that any real player
//     would have started a game by now).
//  2. For each candidate, count t_lsm_game_player rows.
//  3. If zero, call DeleteRoomIfEmpty to drop the row.
//
// The WS hub already runs a 5-minute "vacancy" timer for rooms that are
// observed going empty at runtime, but those timers live in process memory.
// A server restart orphans them — the timer never fires. JanitorSweep
// closes that loop with a durable, DB-driven check, and also acts as
// backstop for any other source of orphaned status='open' rows (e.g. an
// older test harness that crashed mid-flow).
//
// Safe to call concurrently with Hub/vacancy timer; DeleteRoomIfEmpty is
// idempotent and the row is selected with a row-level lock from MySQL.

// RunJanitor runs JanitorSweep on a fixed interval until stopCh is closed.
// Intended to be launched once from main() in a dedicated goroutine.
//
// Defaults: 10-minute interval, 30-minute age cutoff. Both are hard-coded
// on purpose — these are operational constants, not user-tunable knobs.
//
// staleMaxAge controls the "stale room" sweep that fires on boot AND on every
// tick. Stale = status='open' AND created_at < now-staleMaxAge AND the player
// row count matches current_count (consistency guard). Force-deletes both the
// player rows and the room row in a single transaction. Default 30 minutes —
// RunJanitor runs all cleanup sweeps (zombie-playing + stale + empty) on a
// fixed cadence until stopCh is closed.
//
// sweepZombiePlaying is the BUG-WEREWOLF-ZOMBIE-LOBBY helper — it flips
// long-stuck status='playing' rows to 'over' so they drop out of the lobby
// after the configured zombieMaxAge. A 0/negative value disables that pass
// (default 4h; main.go sets the value explicitly).

// JanitorSweepStale clears "leaked" rooms whose player rows are still present
// but the room itself has been around for longer than `maxAge` with no
// observable activity. Designed to undo the test-automation accumulation
// scenario where a script creates many rooms, leaves them at current_count=1,
// and walks away — none of these rooms have current_count==0, so the regular
// JanitorSweep (which checks only player-row count) skips them.
//
// Consistency guard: a room is only considered stale when the player-row count
// matches the cached `current_count` column. That excludes rooms whose state
// is mid-transition (join/leave in flight) and rooms where the cached count
// drifted from reality — both of which JanitorSweep already handles via the
// "delete only if remaining==0" path.
//
// Operational safety: this is the same code path the admin endpoint
// (`POST /api/admin/rooms/cleanup`) invokes on demand, so behaviour is
// identical to a manual sweep.

// JanitorSweepZombiePlaying marks long-stuck `status='playing'` rooms as
// `status='over'` so they stop showing up in the lobby. The cutoff is
// intentionally generous: a real 13-seat werewolf match (with auto-skip
// fallbacks and timeouts) resolves well within 60 minutes, so anything
// still `playing` after `zombieMaxAge` (default 4h) is by definition
// dead — typically a server restart that orphaned an in-process game
// state, with the in-memory GameState gone but the DB row left behind.
//
// BUG-WEREWOLF-ZOMBIE-LOBBY FIX: prior to this method, the WS hub's
// per-process vacancy timer was the only thing hiding orphaned rooms.
// On every process restart those timers reset silently, so DB rows
// stayed at status='playing' indefinitely and accumulated in the lobby
// as `8/7 playing 9h+ ago`-style ghost entries. This pass closes the
// loop with a single UPDATE — we don't delete the room (the player
// rows are still meaningful for any post-game audit), we only flip
// the status so ListRooms / useLobbyLiveUpdate can drop it.
//
// Like JanitorSweepStale / JanitorSweep this is safe to call on a
// fixed cadence (`RunJanitor` below schedules it alongside the others).
