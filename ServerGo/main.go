// LsmAgentGame — entrypoint.
//
// Boots config → logger → DB → WS hub → HTTP and WSS listeners, then waits
// for SIGTERM/SIGINT to shut down gracefully.
package main

/*
#include <stdio.h>
char * GetBuildDateTime(void) {
    static char buffer[128] = {0};
    // CGo __DATE__/__TIME__ 注入：保留为默认/兼容路径——
    //   - 手工 `go build`（无 ldflags）走这里，build_time 永远是
    //     "该 .go 文件被预处理那一刻"的真实时间；
    //   - rebuild_restart_app.sh 走 -ldflags -X 强制覆盖（见 buildDateTime
    //     的优先级逻辑），并 `go clean -cache` 防止 cgo 缓存复用。
    snprintf(buffer, 128, "%s %s", __DATE__, __TIME__);
    return buffer;
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"LsmAgentGame/agent/wwjudge"
	"LsmAgentGame/api"
	"LsmAgentGame/config"
	"LsmAgentGame/db"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/werewolf"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/router"
	"LsmAgentGame/service"
	"LsmAgentGame/util"
	"LsmAgentGame/ws"

	"go.uber.org/zap"
)

// AppVersion 程序版本号。
// 默认值；rebuild_restart_app.sh 通过 -ldflags "-X main.AppVersion=<ver>"
// 在编译期注入更精确的版本字符串（包含 git short SHA）。这里给出兜底值，
// 防止手工 `go build` 时编译失败或 ldflags 漏传。
var AppVersion = "v1.0.0"

// gitShortSHA 编译时通过 -ldflags "-X main.gitShortSHA=$(git rev-parse --short HEAD)"
// 注入。空字符串代表非 git 构建（开发/IDE 场景）。
var gitShortSHA = ""

// buildDateTime 编译时间。
//
// 注入优先级（init() 中按此顺序确定最终值）：
//  1. ldflags -X main.buildDateTime=... （rebuild_restart_app.sh 强制路径）
//  2. CGo __DATE__ __TIME__             （手工 `go build` 兜底路径）
//  3. 兜底常量 "unknown-build-time"     （理论不会触发）
//
// 为什么保留 cgo 注入（即使 cgo 字符串会受 Go 编译缓存影响）：
//   - 前端 /api/version 一直消费 buildDateTime 这个字段；
//   - 手工 `go build` 不带 ldflags 时，cgo 路径仍然能给出真实的编译时间；
//   - rebuild_restart_app.sh 在编译前 `go clean -cache` 强制重编，
//     cgo 缓存命中问题由脚本兜底，不影响主流程行为。
var buildDateTime string

// ldflagsBuildTime 由 -ldflags "-X main.ldflagsBuildTime=..." 注入。
// 留空 = 走 cgo 默认路径。
var ldflagsBuildTime = ""

// init 在进程启动时按优先级确定 buildDateTime 的最终值。
// 这里不读文件系统、不读 time.Now()——ldflags 注入和 cgo 注入都是
// 编译期常量，纯内存赋值即可；进程启动时确定的 buildDateTime 永远
// 等于"该二进制编译那一刻"的时间。
func init() {
	if ldflagsBuildTime != "" {
		buildDateTime = ldflagsBuildTime
		return
	}
	// cgo 路径：保留兼容性，手工 `go build` 仍然能给真实编译时间。
	buildDateTime = C.GoString(C.GetBuildDateTime())
	if buildDateTime == "" {
		buildDateTime = "unknown-build-time"
	}
}

// gitShortSHAFallback 在没有 -ldflags 注入时，尝试用 `git rev-parse --short HEAD`
// 取得当前 HEAD short SHA；失败则返回 "nogit"。
func gitShortSHAFallback() string {
	if gitShortSHA != "" {
		return gitShortSHA
	}
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "nogit"
	}
	return strings.TrimSpace(string(out))
}

// seatOrNegOne 把 *int(可为 nil) 转为 int,nil 视作 -1。用于
// ChatActivityEvent.RefSeat(指针类型,wire JSON omitempty 友好)。
func seatOrNegOne(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

func main() {
	cfg := config.Load()
	if err := logger.Init(cfg); err != nil {
		panic(err)
	}
	defer logger.Sync()

	// §安全:LLM endpoint 不再硬编码默认值。若 LsmAgentGame.conf 同时
	// 缺 llm.endpoint 与 llm.endpoints,所有 LLM 调用都会失败。这里仅
	// 输出 WARN,具体失败在 llm/anthropic.NewProvider 中按"endpoint
	// 不能为空"严格返回错误。
	if cfg.LLM.Endpoint == "" && len(cfg.LLM.Endpoints) == 0 {
		logger.L().Warn("cfg.llm.endpoint 与 cfg.llm.endpoints 都为空 — 启动后任何 LLM 调用都会失败,请在 LsmAgentGame.conf 中显式配置")
	}

	logger.L().Info("LsmAgentGame starting",
		zap.String("version", AppVersion),
		zap.String("build_time", buildDateTime))

	// DB init MUST come first now — the LLM registry is DB-first (loads from
	// t_lsm_game_llm_provider, falls back to seeding from cfg.LLM.Providers on
	// empty DB). The wallet + bot-user services are needed too: the seed path
	// provisions a backing bot user per provider.
	gormDB, err := db.Init(cfg)
	if err != nil {
		logger.L().Fatal("db init failed", zap.Error(err))
	}

	walletSvc := service.NewWalletService(gormDB)
	botUserSvc := service.NewBotUserService(gormDB, walletSvc)

	// 2026-08-13 §config-auto-bootstrap — 在构造 Registry 之前,先把
	// cfg.LLM.Providers 同步到 t_lsm_game_llm_provider(去重 upsert):
	//   - DB 已有同 model 行 → 只更新元数据(agent_name / thinking_*),
	//     保留 DB 里的 api_key_enc 与 endpoint(operator 走 admin UI 改 key);
	//   - DB 没有 → 加密 api_key 后插入新行。
	// 这一步是「配置 → DB 迁移」的核心;若 cfg.LLM.Providers 为空则 no-op,
	// 不影响 §118 之后的纯 DB 模式(operator 已通过 /admin/models 管理)。
	migCtx, migCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if inserted, updated, err := llm.MigrateConfigProvidersToDB(migCtx, gormDB, cfg.LLM); err != nil {
		// 迁移失败不致命:Registry 构造时仍会走「DB 空就 seed」回退到
		// llm.DefaultProviders() 兜底,系统可用,只是失去 conf 里的旧 key。
		logger.L().Warn("llm cfg → DB migrate failed (registry will fall back to defaults)",
			zap.Error(err))
	} else if inserted > 0 || updated > 0 {
		logger.L().Info("llm cfg → DB migrate",
			zap.Int("inserted", inserted),
			zap.Int("updated", updated))
	}
	migCancel()

	// 2026-08-13 §config-auto-bootstrap — 迁移完成后,把 cfg.LLM.Providers
	// 段从 LsmAgentGame.conf 里剔除并写回磁盘,避免敏感 api_key 长期
	// 以明文形式躺在仓库根目录(万一运维误 `git add` 即泄漏)。非敏感
	// 字段(endpoint / endpoints / timeout_ms / max_retries 等)保留。
	if cfg.LLM.Providers != nil {
		stripped, perr := cfg.PersistToFile("./LsmAgentGame.conf")
		if perr != nil {
			logger.L().Warn("write stripped LsmAgentGame.conf failed", zap.Error(perr))
		} else {
			logger.L().Info("llm: stripped LLM providers from LsmAgentGame.conf (migrated to DB)",
				zap.Int("stripped_providers", stripped))
		}
	}

	// Build the LLM provider registry (DB-first; auto-seed from cfg when the
	// table is empty). The pure-cfg fallback path is preserved when gormDB is
	// nil so unit tests in agent/ don't need to change. The adapter strips
	// the concrete *models.TLsmGameUser return that BotUserService produces
	// into the any-typed interface the llm package expects.
	llmRegistry := llm.NewRegistryWithDB(cfg.LLM, gormDB, botUserProvisionerAdapter{svc: botUserSvc})
	if llmRegistry == nil {
		logger.L().Info("llm: no providers configured, disabling LLM features")
	} else {
		// Inject User-Agent so every outbound LLM HTTP request identifies this
		// server instance. Format: "LsmAgentGame/<version> <build_time>".
		llmRegistry.SetUserAgent(fmt.Sprintf("LsmAgentGame/%s %s", AppVersion, buildDateTime))
		// Inject the `x-anthropic-billing-header` value so Anthropic-side
		// proxies / Datadog attribute traffic to this call site. Mirrors the
		// ClaudeCode reference (`cc_version=2.1.195.58c; cc_entrypoint=cli;`)
		// — see CluadeCode请求RequestBody的Anthropic协议定义数据用例.json.
		llmRegistry.SetBillingHeader(fmt.Sprintf("LsmAgentGame/%s %s; entrypoint=server;",
			AppVersion, buildDateTime))
		logger.L().Info("llm registry loaded",
			zap.String("source", llmRegistry.Source()),
			zap.Int("total", len(llmRegistry.List())),
			zap.Int("usable", llmRegistry.Count()),
			zap.String("endpoint", cfg.LLM.Endpoint))

		// BUG-R115-01: placeholder / missing / invalid api_key detection. In
		// DB-wins mode a provider whose api_key was never replaced (still the
		// literal "API-KEY-PLACEHOLDER" sentinel or empty) — or that holds a key
		// the upstream rejected with 401 — stays `available=false` silently, and
		// the only signal was N/13 agents getting permanently quarantined
		// mid-game with a confusing upstream 401. Surface the exact list of
		// unconfigured models at boot, with a pointer to the fix docs, so the
		// operator fixes the keys *before* creating a multi-agent room instead
		// of diagnosing a failed run.
		if unusable := llmRegistry.UnusableProviders(); len(unusable) > 0 {
			models := make([]string, 0, len(unusable))
			placeholderCount := 0
			for _, u := range unusable {
				models = append(models, fmt.Sprintf("%s(%s)", u.Model, u.Reason))
				if u.Reason == "placeholder" || u.Reason == "empty_key" {
					placeholderCount++
				}
			}
			logger.L().Warn("llm registry: providers with unusable api_key detected — agents on these models will be permanently quarantined",
				zap.Int("unusable", len(unusable)),
				zap.Int("usable", llmRegistry.Count()),
				zap.Int("placeholder_or_empty", placeholderCount),
				zap.Strings("models", models),
				zap.String("fix_docs", "docs/LLM与Agent/LLM供应商设计.md"),
				zap.String("fix_ui", "/api/admin/llm/providers (or set real api_key in LsmAgentGame.conf)"))
		}

		// ROUND 25 BUG-WEREWOLF-P0-NEW-7 follow-up: do a one-shot HEAD probe of
		// the upstream LLM proxy at startup so a misconfigured endpoint /
		// placeholder-key / 401-class auth failure is logged *now* instead of
		// surfacing 7 minutes into a full-AI room when the first agent's call
		// fails. Operators see "endpoint unreachable / 401" immediately.
		startCtx, startCancel := context.WithTimeout(context.Background(), 5*time.Second)
		probe := llmRegistry.HealthCheck(startCtx)
		startCancel()
		if probe.OK {
			logger.L().Info("llm upstream reachable",
				zap.String("endpoint", probe.Endpoint),
				zap.Int("usable_keys", probe.UsableKeys))
		} else {
			logger.L().Warn("llm upstream unhealthy at startup — AI agent rooms will fail until fixed",
				zap.String("endpoint", probe.Endpoint),
				zap.String("last_error", probe.LastError),
				zap.Int("usable_keys", probe.UsableKeys))
		}

		// Periodically re-probe every 5 minutes. Stops when the process exits
		// — kept simple (no shutdown plumbing) because the ticker only does
		// outbound HEADs that take <3s; if the process is shutting down, the
		// next tick won't run. Cheap: a single HEAD per tick.
		go func() {
			t := time.NewTicker(5 * time.Minute)
			defer t.Stop()
			for range t.C {
				pCtx, pCancel := context.WithTimeout(context.Background(), 3*time.Second)
				s := llmRegistry.HealthCheck(pCtx)
				pCancel()
				if !s.OK {
					logger.L().Warn("llm upstream probe failed",
						zap.String("endpoint", s.Endpoint),
						zap.String("last_error", s.LastError))
				}
			}
		}()
	}

	captchaStore := util.NewCaptchaStore()
	captchaJanitorStop := make(chan struct{})
	go captchaStore.Janitor(30*time.Second, captchaJanitorStop)

	authSvc := service.NewAuthService(gormDB, cfg, captchaStore)

	// Seed a genesis root user on first run so the referrer-gated registration
	// flow has a valid starting referrer code. No-op once any user exists.
	//
	// 凭据与邀请码必须从 LsmAgentGame.conf 读取,绝不在源码中硬编码。
	// 空 DB 时若未配置 root.password,则随机生成一个强密码并仅通过 INFO 日志
	// 输出一次(供运维首次登录后立即轮换)。RootInviteCode 同理:缺省随机生成。
	rootAccount := cfg.Server.RootAccount
	rootPassword := cfg.Server.RootPassword
	if rootAccount == "" {
		rootAccount = "lsm_root"
	}
	if cfg.Server.RootInviteCode != "" {
		service.RootInviteCode = cfg.Server.RootInviteCode
	}
	inviteCode := service.RootInviteCode
	if inviteCode == "" || inviteCode == "ROOT_INVITE_CODE_FROM_CONFIG_OR_RANDOM" {
		// 一次性随机生成(开发模式兜底)。生产部署必须显式设置 cfg.server.root_invite_code。
		inviteCode = util.RandomStrongPassword(16)
		service.RootInviteCode = inviteCode
	}
	if rootPassword == "" {
		// 一次性随机生成(开发模式兜底)。生产部署必须显式设置 cfg.server.root_password。
		rootPassword = util.RandomStrongPassword(20)
		logger.L().Warn("cfg.server.root_password 未配置,已随机生成并通过 INFO 日志输出一次,生产部署必须显式设置")
	}
	rootCtx, rootCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if created, err := authSvc.SeedRootUserIfEmpty(rootCtx, rootAccount, rootPassword, inviteCode); err != nil {
		logger.L().Warn("root user seed failed", zap.Error(err))
	} else if created {
		logger.L().Info("root user seed created",
			zap.String("account", rootAccount),
			zap.String("invite_code", inviteCode),
			zap.String("generated_password", rootPassword),
		)
	}
	rootCancel()

	gameSvc := service.NewGameService(gormDB, cfg)
	userSvc := service.NewUserService(gormDB)
	roomSvc := service.NewRoomService(gormDB, cfg)
	hub := ws.NewHub()

	// Wire hub → room service for real-time lobby refresh + 15s auto-kick.
	// Adapter implements ws.RoomStateBroadcaster without forcing ws to import
	// service.RoomInfo (which would create an import cycle).
	hub.SetRoomService(roomStateAdapter{roomSvc: roomSvc})
	hub.SetLeaveRoomFunc(roomSvc.LeaveRoom)
	hub.SetLeaveSpectateFunc(roomSvc.LeaveSpectate)

	// walletSvc was already created above so the LLM registry could use it
	// for bot-user provisioning. Wire it into auth so registration also seeds
	// the wallet and login credits the daily bonus.
	authSvc.SetWalletService(walletSvc)

	authAPI := api.NewAuthAPI(authSvc, cfg)
	gameAPI := api.NewGameAPI(gameSvc)
	captchaAPI := api.NewCaptchaAPI(cfg, captchaStore)
	versionAPI := api.NewVersionAPI(AppVersion, buildDateTime, gitShortSHAFallback())
	userAPI := api.NewUserAPI(userSvc)
	chatSvc := ws.NewChatService(gormDB, hub, llmRegistry)
	// Pass llmRegistry into the WS game service so the werewolf manager can look
	// up providers for agent seats. A nil registry is tolerated (agent seats run
	// as placeholder-only until wired in Phase 4).
	gameSvcWs := ws.NewGameService(hub, roomSvc, llmRegistry)
	// §20260810-03 F1 — 狼人杀房间 whisper 阵营守卫:ChatService 需要 roomSvc
	// 查 game_kind,以及 werewolfMgr 提供的 FactionByUserID 查阵营。两路注入
	// 都 nil-safe:旧部署/测试桩不接也不影响 chat 链路。
	chatSvc.SetRoomService(roomSvc)
	if werewolfMgrRef := gameSvcWs.WerewolfManager(); werewolfMgrRef != nil {
		chatSvc.SetFactionLookup(werewolfMgrRef.FactionByUserID)
	}
	// BUG-R7-P0-disconnect-stuck: 真人中途掉线时,除 DB 清理外,还需把 GameState
	// 中该座位标记为死亡,避免 acting seat 永久卡在已断线的座位上。
	// HandleDisconnect 对非狼人杀房间是 no-op(getRoom 返回 nil)。
	werewolfMgr := gameSvcWs.WerewolfManager()
	hub.SetLeaveRoomFunc(func(roomID, userID string) *errcode.Error {
		if e := werewolfMgr.HandleDisconnect(roomID, userID); e != nil {
			logger.L().Warn("werewolf HandleDisconnect failed",
				zap.String("room_id", roomID),
				zap.String("user_id", userID),
				zap.Error(e))
		}
		return roomSvc.LeaveRoom(roomID, userID)
	})
	adminAPI := api.NewAdminAPI(userSvc, roomSvc, werewolfMgr, gormDB)
	roomAPI := api.NewRoomAPI(roomSvc, hub)
	gitLogSvc := service.NewGitLogService(".")
	gitLogAPI := api.NewGitLogAPI(gitLogSvc)
	walletAPI := api.NewWalletAPI(walletSvc)
	// Phase 5 model admin APIs. ModelLogService encapsulates GORM access to the
	// 5 new tables so handlers stay thin. modelWalletAPI needs both walletSvc
	// (for Credit/Debit) and gormDB (to verify the target user is actually a
	// bot row before allowing manual adjustments).
	modelLogSvc := service.NewModelLogService(gormDB)
	// §20260810-03 F3 — NewLlmAPI 现在接收 modelLogSvc 以支持 /api/llm/leaderboard。
	llmAPI := api.NewLlmAPI(llmRegistry, modelLogSvc)
	modelAdminAPI := api.NewModelAdminAPI(userSvc, llmRegistry, gormDB, botUserSvc, modelLogSvc, walletSvc)
	modelLogAPI := api.NewModelLogAPI(modelLogSvc, userSvc)
	modelWalletAPI := api.NewModelWalletAPI(walletSvc, userSvc, modelLogSvc, gormDB)
	// 2026-07-14 §135 — 超级管理员每日 grant 端点。复用 walletSvc 的 Credit
	// 双簿记 + botUserSvc 的「provider→bot user」解析,与现有 admin 端点风格对齐。
	modelGrantAPI := api.NewModelGrantAPI(walletSvc, botUserSvc, userSvc, gormDB)
	// 2026-07-20 §131 新增 — Agent 持久化记忆(MEMORY.md)。
	// memorySvc 同时供 admin 管理端点(GET/DELETE memory)与狼人杀 manager
	// (每局结束自我迭代)使用;WerewolfManager 只依赖窄接口 AgentMemoryStore。
	agentMemorySvc := service.NewAgentMemoryService(gormDB)
	modelAgentMemoryAPI := api.NewModelAgentMemoryAPI(userSvc, gormDB, agentMemorySvc)
	// Wiki —— 项目根 docs/ 目录的内容查看器。与 rebuild_restart_app.sh
	// 启动 CWD 一致(项目根),docs/ 在仓库根。
	wikiAPI := api.NewWikiAPI("./docs")
	// 源码统计 —— 标题栏"源码统计"按钮触发的弹窗数据源。
	// 扫描前端 ClientWeb/src + 后端 ServerGo 的代码文件,统计文件数/行数/字节数。
	sourceStatsAPI := api.NewSourceStatsAPI([]struct{ Name, Path string }{
		{"前端", "./ClientWeb/src"},
		{"后端", "./ServerGo"},
	})
	roomWsSvc := ws.NewRoomWsService(roomSvc, hub)
	userWsSvc := ws.NewUserWsService(userSvc, roomSvc, hub)

	// Mirror DB-level seats (HTTP CreateRoom/JoinRoom) into the in-memory game
	// managers so doudizhu / texasholdem auto-deal even when players join via
	// HTTP. Without this, HTTP joiners never trigger the manager's auto-deal
	// path and the client sees my_hand=null / bottom=null → render crash.
	roomSvc.SetGameJoiner(gameSvcWs)
	// Phase 4 wiring: mirror agent bot seats handled by CreateRoomWithAgents
	// into the in-memory werewolf manager (ManagerAddPlayerAt + SetSeatModelKey)
	// so BotAgents can be constructed on auto-start. Without this, HTTP-created
	// werewolf rooms persisted DB rows but never registered agent seats, leaving
	// Agent.Run unstarted — the P0 e2e break in 20260706_161542.
	roomSvc.SetAgentSeater(gameSvcWs)

	// Phase 4 wiring: give the werewolf manager a ChatService so bot agents
	// can broadcast speech via SendFromBot / WhisperFromBot. Without this,
	// agent speech would be silently dropped. Reuses the same chatSvc already
	// created above for WS handlers.
	gameSvcWs.WerewolfManager().SetChatService(chatSvc)
	gameSvcWs.WerewolfManager().SetActivityEmitter(chatSvc) // §115 房间聊天 — 活动事件
	// 2026-07-15 狼人杀 13 人局金币系统 — 注入 WS 金币推送回调。
	// hub.PushBalanceChange 结算成功后向用户所有连接推送 wallet.balance 帧,
	// 前端 useWallet hook 自动订阅并刷新余额显示。
	gameSvcWs.WerewolfManager().SetBalancePusher(hub.PushBalanceChange)
	// 2026-07-17 金池结算 UI — 注入 per-user 结算明细推送回调。
	// 走 hub.SendToUser 保证仅该玩家收到自己的结算明细(避免泄漏胜方/败方身份)。
	gameSvcWs.WerewolfManager().SetSettlementPusher(func(userID string, payload map[string]any) {
		if userID == "" {
			return
		}
		hub.SendToUser(userID, ws.Envelope{Type: "game.settlement", Payload: ws.MustMarshal(payload)})
	})

	// 2026-07-21 道具系统 — wire 道具目录 + 引擎。
	// PropService 从 DB 加载道具目录（空表时 seed from code 内嵌默认值），
	// 构造 PropEngine 处理道具使用流程（校验/扣款/分配/中招判定/日志）。
	propSvc := werewolf.NewPropService(gormDB, walletSvc)
	propCatalog, _ := propSvc.LoadCatalog(context.Background())
	propEngine := werewolf.NewPropEngine(gormDB, walletSvc, propCatalog)
	gameSvcWs.WerewolfManager().SetPropCatalog(propCatalog)
	gameSvcWs.WerewolfManager().SetPropEngine(propEngine)

	// 2026-07-21 v5 重构 — EconTier 5 档阈值(从 LsmAgentGame.conf 注入)。
	// 4 个字段全部显式非零时调 werewolf.ConfigureEconTier;任意字段为 0 走
	// werewolf 包常量默认值。配置错误(非单调)由 werewolf 包 panic 兜底。
	if cfg != nil {
		b, c, d, cr := cfg.Werewolf.EconTierBoomThreshold,
			cfg.Werewolf.EconTierCautionThreshold,
			cfg.Werewolf.EconTierDangerThreshold,
			cfg.Werewolf.EconTierCriticalThreshold
		if b > 0 && c > 0 && d > 0 && cr >= 0 {
			werewolf.ConfigureEconTier(b, c, d, cr)
		}
	}

	// 2026-07-21 §道具系统 — PropAPI 暴露 /api/games/werewolf/props 和
	// /api/admin/werewolf/props(/usage)路由,与 WS `game.werewolf_use_prop`
	// 共用 WerewolfManager.Action_UseProp + PropService。
	// §20260813-02 U2 — 注入 modelLogSvc 供 GET /api/games/werewolf/prop-economy
	// 聚合 t_lsm_game_prop_usage_log(该日志表此前只写不读,§130 高危信号)。
	propAPI := api.NewPropAPI(gameSvcWs.WerewolfManager(), propSvc, userSvc, modelLogSvc, walletSvc)

	// 2026-08-11 §20260811-05 U2 — 赛后复盘问答 REST 入口。
	// POST /api/games/werewolf/rooms/:roomId/recall_chat,终局后玩家/观战者
	// 向本局 bot 座位提问(冻结 Memory 快照单轮问答,不走 WS、不进 chat 表)。
	recallChatAPI := api.NewRecallChatAPI(gameSvcWs.WerewolfManager())
	// 2026-08-12 §20260812-03 — 三项升级的 REST 入口(U1 胜率 / U2 暗线信件 / U3 阵营赌注)。
	werewolf20260812API := api.NewWerewolf20260812API(gameSvcWs.WerewolfManager())

	// 2026-07-10 §125 增强 — 注入法官总结所需回调。
	// 1) Manager 单例(让 judge goroutine 内部能拿到 registry 调 LLM)。
	werewolf.SetSummaryManagerInstance(gameSvcWs.WerewolfManager())
	// §20260811-09 U1 — 注册 AI 实时解说 spectator-only 广播钩子(werewolf 包全局 var)。
	// 解说 goroutine 走此钩子 → GameService.BroadcastCommentarySpectator → Hub.BroadcastRoomSpectators。
	werewolf.SetCommentarySpectatorHook(gameSvcWs.BroadcastCommentarySpectator)
	// 2026-07-20 §131 新增 — 注入 Agent 持久化记忆存取层。
	// 每局法官整局总结落地后(PersistSummary),manager 对本局每个 bot 模型
	// 异步自我迭代 t_lsm_game_agent_memory;下一局 StartAgentsLocked 注入。
	gameSvcWs.WerewolfManager().SetAgentMemoryStore(agentMemorySvc)
	// 2026-08-11 §20260811-05 U1 新增 — 注入 Agent 玩家行为画像存取层。
	// 每局法官整局总结落地后(PersistSummary),manager 对本局每个
	// (bot model_key × 人类 user_id) 组合异步迭代 t_lsm_game_agent_player_profile;
	// 下一局 StartAgentsLocked 经房间级预取缓存注入 GameContext.PlayerProfiles。
	playerProfileSvc := service.NewAgentPlayerProfileService(gormDB)
	gameSvcWs.WerewolfManager().SetPlayerProfileStore(
		werewolf.PlayerProfileStoreAdapter{Svc: playerProfileSvc})
	// 2026-08-10 §20260810-10 U2 新增 — 注入模型自画像聚合读取层。
	// 开局 StartAgentsLocked 按 modelKey 聚合 t_lsm_game_model_game_log
	// 生成「🪞 模型自画像」段注入 Agent system prompt 末尾;nil/查询失败
	// 降级为通用自画像,不阻塞游戏流。
	gameSvcWs.WerewolfManager().SetSelfPortraitReader(
		werewolf.SelfPortraitReaderAdapter{Svc: modelLogSvc})
	// 2) Bridge: WerewolfRoom 实现 wwjudge.JudgeSummaryBridge;judge goroutine 收到
	// JudgePendingGameOverSummary 后调 bridge.GenerateSummary / PersistSummary。
	// 由于房间是按需创建的,这里只注入 nil-safe 的 wrapper;具体房间的桥接
	// 由房间创建时的 SetSummaryEntry()(在 startJudgeGoroutine 内)动态完成。
	_ = wwjudge.SetSummaryBridge // 触发包初始化(judge_summary_bridge.go 已实现方法)

	// Wire ForceDisbandRoom + BootCleanupOrphanedAgentRooms hooks. Both
	// interfaces live in package service (the dependency direction is
	// service ← ws, never the reverse) so this is a safe wiring — the
	// service never imports ws.
	roomSvc.SetGameServiceHook(gameSvcWs)
	roomSvc.SetHubHook(hub)
	// Round 23 P1 BUG FIX: wire the in-memory werewolf manager so
	// RoomService.GetRoomDetail can surface Phase/RoundNumber in the REST
	// response. nil-safe when the manager is missing (tests).
	roomSvc.SetWerewolfStateHook(gameSvcWs)

	// R187-2: wire the live-registry availability probe so the random
	// duplicate-reassignment allocator (usableProviderModels) drops
	// runtime-disabled models from the pick pool.
	roomSvc.SetModelAvailabilityHook(gameSvcWs)

	// BUG: 狼人杀 7 人局 Agent 多轮上下文 — wire the chat service's
	// onRoomMessage hook to the werewolf manager so each room-scoped
	// chat.message / chat.whisper is recorded into the per-room rolling
	// transcript and the per-seat whisper inbox. The hook projects the
	// ws.ChatMessage into a werewolf.ChatMessageLike (structurally
	// compatible subset) so the werewolf package doesn't need to import
	// ws.
	chatSvc.SetRoomMessageHook(func(msg *ws.ChatMessage) {
		gameSvcWs.WerewolfManager().RecordRoomMessage(msg.RoomID, werewolf.ChatMessageLike{
			FromUserID:    msg.FromUserID,
			FromAccount:   msg.FromAccount,
			FromRole:      msg.FromRole,
			FromAgentName: msg.FromAgentName,
			ToUserID:      msg.ToUserID,
			ToAccount:     msg.ToAccount,
			Whisper:       msg.Whisper,
			Text:          msg.Text,
			TS:            msg.TS,
		})
	})

	// §115 房间聊天 — 活动事件 hook。ChatService.EmitRoomActivity 触发,
	// 由 werewolf manager 决定是否注入 500K 队列(只看 silent_for_bots 标志)。
	chatSvc.SetRoomActivityHook(func(ev *ws.ActivityEvent) {
		gameSvcWs.WerewolfManager().RecordRoomActivity(&werewolf.ChatActivityEvent{
			RoomID:        ev.RoomID,
			EventKind:     ev.EventKind,
			Text:          ev.Text,
			Phase:         ev.Phase,
			RoundNumber:   ev.RoundNumber,
			Severity:      ev.Severity,
			Icon:          ev.Icon,
			RefSeat:       seatOrNegOne(ev.RefSeat),
			RefSeat2:      seatOrNegOne(ev.RefSeat2),
			SilentForBots: ev.SilentForBots,
			TS:            ev.TS,
		})
	})

	// BUG-WEREWOLF-P0-7 FIX + BUG-WEREWOLF-SPECTATE-FILLING FIX (Round 24):
	// install a hydrator that restores BOTH bot and human player seats from
	// the persisted t_lsm_game_player rows. The old hydrator only restored
	// agents, which meant a mixed (human + bot) room surviving a restart
	// could never reach 7/7 in memory → SpectateGame's force-start branch
	// never fired → spectator was stuck on "👁 观战中（等待 7 位玩家入座…）"
	// even though the DB row said status=playing.
	gameSvcWs.WerewolfManager().SetHydrator(func(roomID string) ([]werewolf.AgentSeatInfo, error) {
		seats, err := roomSvc.SeatsForRoom(roomID)
		if err != nil {
			return nil, err
		}
		out := make([]werewolf.AgentSeatInfo, 0, len(seats))
		for _, s := range seats {
			out = append(out, werewolf.AgentSeatInfo{
				Seat:     s.Seat,
				UserID:   s.UserID,
				ModelKey: s.ModelKey, // empty for human players; agent seats carry the LLM model key
			})
		}
		return out, nil
	})

	// Wire the onGameStarted callback so the DB room status is updated from
	// "open" to "playing" when a werewolf game starts. Without this, the room
	// API returns status="open" even after the game has started.
	gameSvcWs.WerewolfManager().SetOnGameStarted(func(roomID string) {
		if err := roomSvc.UpdateRoomStatus(roomID, "playing"); err != nil {
			logger.L().Warn("failed to update room status to playing",
				zap.String("room_id", roomID), zap.Error(err))
		}
	})

	// BUG-R48-P0-4: Wire onGameOver callback so DB room status updates from
	// "playing" to "over" when the engine detects a winner. Without this,
	// phase="over" but status="playing" — a terminal state contradiction.
	gameSvcWs.WerewolfManager().SetOnGameOver(func(roomID string) {
		if err := roomSvc.UpdateRoomStatus(roomID, "over"); err != nil {
			logger.L().Warn("failed to update room status to over",
				zap.String("room_id", roomID), zap.Error(err))
		}
	})

	// 2026-07-30 解决和设计方案-20260730-03 Fix-A1/A3/C: 终局收编广播。
	// 进入冷却期 / EmitGameOver 后,由 manager 回调触发 per-seat game.state
	// (phase=over, bot_contexts 已清场) + game.over(winner) 帧广播,
	// 消除「对局结束 vs 等待阶段推进… / N 名 Agent 响应中」UI 矛盾。
	gameSvcWs.WerewolfManager().SetOnGameOverBroadcast(func(roomID, winner string) {
		gameSvcWs.BroadcastWerewolfGameOverFinal(roomID, winner)
	})

	// 2026-07-12 §129 增强 — 冷却期人类在线探针。
	// 一局结束后进入冷却期, cooling watchdog 每 30s 探测一次人类存在,
	// 决定是继续延长冷却窗口还是超时关门。
	// hub.IsRoomEmpty 检查 hub.rooms + hub.spectators 两个集合;
	// 取反后 = "至少一名人类玩家 / 观察者在房间里"。
	gameSvcWs.WerewolfManager().SetCoolingHumanPresence(func(roomID string) bool {
		return !hub.IsRoomEmpty(roomID)
	})

	// 12 人局警徽流结算回调:engine 在 dawn 结算警徽流后通过此钩子委托 ws 层
	// 广播 game.sheriff_stream_settle 帧(前端据此渲染移交/撕警徽动效)。
	// 规则详见 docs/狼人杀13人标准局规则.md §7.4。
	gameSvcWs.WerewolfManager().SetOnSheriffStreamSettle(func(roomID string, payload map[string]any) {
		hub.BroadcastRoom(roomID, ws.Envelope{
			Type:    "game.sheriff_stream_settle",
			Payload: ws.MustMarshal(payload),
		})
	})

	// 12 人局白痴翻牌结算回调:engine 在 IdiotReveal 结算后通过此钩子委托 ws 层
	// 广播 game.idiot_revealed 帧(前端据此渲染翻牌结果动效)。
	// 规则详见 docs/狼人杀13人标准局规则.md §3.5。
	gameSvcWs.WerewolfManager().SetOnIdiotRevealed(func(roomID string, seat int, choice string, revealed bool) {
		hub.BroadcastRoom(roomID, ws.Envelope{
			Type: "game.idiot_revealed",
			Payload: ws.MustMarshal(map[string]any{
				"room_id":  roomID,
				"seat":     seat,
				"choice":   choice,
				"revealed": revealed,
			}),
		})
	})

	// 2026-07-23 §道具特效:道具使用独立广播帧(game.werewolf_prop_used)。
	// engine 在 broadcastPropUseLocked 内通过此钩子委托 ws 层发送完整道具事件
	// (from/target/prop_key/emoji/hit),驱动前端 PropUseOverlay 特效叠加 UI。
	// 该前端帧原已解析(useWerewolf.ts),但后端长期未发送而沦为死代码;此钩子
	// 让道具特效叠加 UI 成为可能。详见 docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md。
	gameSvcWs.WerewolfManager().SetOnPropUsed(func(roomID string, payload map[string]any) {
		hub.BroadcastRoom(roomID, ws.Envelope{
			Type:    "game.werewolf_prop_used",
			Payload: ws.MustMarshal(payload),
		})
	})

	// 2026-07-30 BUG-FIX: §130 人类等待窗口广播。tryStartWithHumanWaitLocked 触发时
	// engine 经此钩子委托 ws 层广播 game.pre_wait 帧,前端据此把"等待 13 位玩家
	// 入座…"改画为"等待人类玩家… (N 秒后自动开始)"。未接线时静默丢弃,回退到
	// 旧行为(客户端永远看到"等待 13 位玩家入座…"直到 StartGame 真正触发)。
	// BUG-R200-P2-05:此前 payload 在 engine 内部构建后丢弃,永远未广播 → 12AI+1
	// 人类房间客户端永久"等待入座",界面卡死。此钩子是修复的关键接线点。
	gameSvcWs.WerewolfManager().SetOnPreWait(func(roomID string, payload map[string]any) {
		hub.BroadcastRoom(roomID, ws.Envelope{
			Type:    "game.pre_wait",
			Payload: ws.MustMarshal(payload),
		})
	})

	// Wire Hub → RoomService/GameService for auto-deletion of rooms that have
	// been vacant for 5 minutes. The callbacks are invoked outside the Hub lock.
	hub.SetDeleteRoomIfEmptyFunc(func(roomID string) (bool, *errcode.Error) {
		return roomSvc.DeleteRoomIfEmpty(roomID)
	})
	hub.SetGameManagerCleanupFunc(gameSvcWs.RemoveRoomState)

	httpHandler := router.New(cfg, authAPI, gameAPI, captchaAPI, versionAPI, userAPI, gitLogAPI, roomAPI, adminAPI, walletAPI, llmAPI, wikiAPI, modelAdminAPI, modelLogAPI, modelWalletAPI, modelGrantAPI, modelAgentMemoryAPI, propAPI, sourceStatsAPI, recallChatAPI, werewolf20260812API)
	// Mount WS upgrade handler on the HTTPS server so the frontend can connect
	// to the same host:port as the page (wss://HOST:39001/ws). The separate WSS
	// server on port 39002 remains for backward compatibility.
	httpHandler.GET("/ws", ws.Handler(cfg, hub, chatSvc, gameSvcWs, roomWsSvc, userWsSvc))
	wssHandler := wsHandler(cfg, hub, chatSvc, gameSvcWs, roomWsSvc, userWsSvc)

	httpsSrv := &http.Server{
		Addr:              cfg.Server.HTTPSAddr,
		Handler:           httpHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	wssSrv := &http.Server{
		Addr:              cfg.Server.WSSAddr,
		Handler:           wssHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	hubStop := make(chan struct{})
	go hub.RunHeartbeat(hubStop)

	// Room janitor: periodically cleans up status='open' rooms with zero
	// players. The WS hub's 5-minute vacancy timer only fires for rooms
	// observed going empty at runtime, so a server restart orphans those
	// timers — the janitor is the durable backstop. Runs an immediate sweep
	// on boot to clear any stragglers left over from a prior crash.
	janitorStop := make(chan struct{})
	// BUG-WEREWOLF-ZOMBIE-LOBBY: also flip long-stuck status='playing' rooms
	// to 'over' on a 4h cutoff so restarted servers don't leak them back into
	// the lobby. Paired with JanitorSweep (empty-rooms) and JanitorSweepStale
	// (player-rows-present-but-still-old), this is the durable backstop.
	go roomSvc.RunJanitor(10*time.Minute, 30*time.Minute, 30*time.Minute, 4*time.Hour, janitorStop)

	// Boot orphan-agent cleanup: every werewolf room that contains at
	// least one role='agent' row gets reconciled with the hub's in-memory
	// state. Status='open' + no humans → hard delete from DB (no in-mem
	// GameState to tear down). Status='playing' + no humans + hub says
	// empty + updated_at > 5min ago → ForceDisbandRoom (also fires
	// BroadcastRoomRemoved, no-op when nobody is connected).
	//
	// Runs in its own goroutine with a 60s timeout so the boot path never
	// blocks the HTTPS/WSS listeners coming up.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		stats := roomSvc.BootCleanupOrphanedAgentRooms(ctx)
		logger.L().Info("boot orphan agent room cleanup",
			zap.Int("scanned", stats.Scanned),
			zap.Int("disbanded", stats.Disbanded),
			zap.Int("hard_deleted", stats.HardDeleted),
			zap.Int("skipped", stats.Skipped))
	}()

	// BUG-WEREWOLF-RESTART-CLEANUP (Round 34): on a fresh process boot every
	// werewolf room from the previous process is already gone from memory
	// (the manager is freshly constructed). But the DB rows hang around for
	// 30+ minutes until the regular JanitorSweepStale / JanitorSweepZombiePlaying
	// passes pick them up. Run an immediate, targeted wipe right now so the
	// lobby list + room detail endpoint stop leaking stale werewolf rows
	// within seconds of restart instead of after the long janitor cutoff.
	//
	// BUG-WEREWOLF-P0-NEW-43 (Round 39): Run synchronously BEFORE the
	// HTTPS/WSS listeners accept traffic so reconnecting clients do not
	// rejoin a stale DB row that the cleanup is about to delete. The
	// previous async-on-goroutine race let a client enter a doomed room
	// for ~2s before it vanished from under them. Capped at 30s to keep
	// boot-time bounded; ForceDisbandRoom also broadcasts `game.removed`
	// to any still-connected client (no-op when nobody is connected
	// post-restart).
	{
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		stats := roomSvc.BootCleanupStaleWerewolfRooms(ctx)
		cancel()
		logger.L().Warn("boot restart stale werewolf room cleanup",
			zap.Int("scanned", stats.Scanned),
			zap.Int("disbanded", stats.Disbanded),
			zap.Int("hard_deleted", stats.HardDeleted),
			zap.Int("skipped", stats.Skipped))
	}

	go func() {
		logger.L().Info("https listen",
			zap.String("addr", cfg.Server.HTTPSAddr),
			zap.String("cert", cfg.Server.TLSCert))
		if err := httpsSrv.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.L().Fatal("https serve", zap.Error(err))
		}
	}()
	go func() {
		logger.L().Info("wss listen",
			zap.String("addr", cfg.Server.WSSAddr))
		if err := wssSrv.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.L().Fatal("wss serve", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.L().Info("shutdown signal received")
	close(hubStop)
	close(captchaJanitorStop)
	close(janitorStop)

	// BUG-WEREWOLF-RESTART-CLEANUP (Round 34): on a clean shutdown, fan a
	// `game.removed` envelope to every still-connected werewolf client BEFORE
	// we tear down the in-memory state. The client-side WS layer drops the
	// room from its local state on receiving this envelope, so a reconnect
	// attempt after the new process boots does not rejoin a stale GameState.
	// We do this before httpsSrv.Shutdown to give the WS writes a chance
	// to drain (Sync() below is non-blocking on the WS side; the broadcast
	// helpers are best-effort).
	if roomIDs := gameSvcWs.WerewolfRoomIDs(); len(roomIDs) > 0 {
		logger.L().Info("shutdown: broadcasting game.removed to active werewolf clients",
			zap.Int("rooms", len(roomIDs)))
		for _, rid := range roomIDs {
			gameSvcWs.BroadcastRoomRemoved(rid, "server-restart")
		}
	}

	// Wipe in-memory werewolf state (kills every agent goroutine via
	// stopAgentsLocked inside WipeAllRooms, capped at 5s). DB rows are NOT
	// touched — the next process boot's BootCleanupStaleWerewolfRooms runs
	// the proper DELETE via RoomService.ForceDisbandRoom.
	if wiped := gameSvcWs.WipeAllWerewolfRooms(); len(wiped) > 0 {
		logger.L().Info("shutdown: wiped in-memory werewolf state",
			zap.Int("rooms", len(wiped)))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpsSrv.Shutdown(ctx)
	_ = wssSrv.Shutdown(ctx)
	logger.L().Info("bye")
}

// wsHandler mounts the WSS upgrade on /ws.
//
// BUG-WEREWOLF-P0-NEW-37: 直接调用 ws.ServeWS 而非 gin.CreateTestContext 包装,
// 避免 gorilla 接管连接后 responseWriter 留在半 hijacked 状态导致部分客户端
// game.spectate ack 丢失。
func wsHandler(cfg *config.Config, hub *ws.Hub, chat *ws.ChatService, game *ws.GameService, room *ws.RoomWsService, user *ws.UserWsService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(cfg, hub, chat, game, room, user, w, r)
	})
	return mux
}

// roomStateAdapter lets the WS hub talk to *service.RoomService without
// importing the service package from inside the ws package (would be a
// cycle). The adapter exposes just the (id, game_kind, capacity,
// current_count, status, ok) tuple the hub needs to build a `room.state`
// broadcast envelope.
type roomStateAdapter struct {
	roomSvc *service.RoomService
}

// GetRoomInfo implements ws.RoomStateBroadcaster. Returns ok=false when the
// room was deleted (e.g., last player just left) so the hub emits a
// placeholder payload the frontend can use to drop the room from its list.
func (a roomStateAdapter) GetRoomInfo(roomID string) (string, string, int, int, string, bool) {
	detail, err := a.roomSvc.GetRoomDetail(roomID)
	if err != nil || detail == nil {
		return roomID, "", 0, 0, "removed", false
	}
	return detail.ID, detail.GameKind, detail.Capacity, detail.CurrentCount, detail.Status, true
}

// botUserProvisionerAdapter wraps *service.BotUserService so it satisfies
// llm.BotUserProvisioner. Lives here (not in the llm package) so llm does
// not have to import service. The adapter discards the returned
// *models.TLsmGameUser pointer because the registry only uses it for
// logging.
type botUserProvisionerAdapter struct {
	svc *service.BotUserService
}

func (a botUserProvisionerAdapter) EnsureBotUserForProvider(ctx context.Context, p *models.TLsmGameLlmProvider) (interface{}, error) {
	return a.svc.EnsureBotUserForProvider(ctx, p)
}
