// Package router wires Gin routes for the HTTP listener.
//
// Layout:
//
//	/                          static (ClientWeb/dist)
//	/api/health                health probe
//	/api/version               app version + build time
//	/api/games                 public lobby catalog
//	/api/captcha               public captcha issue
//	/api/auth/register         public
//	/api/auth/login            public
//	/api/auth/logout           public (clears cookie)
//	/api/auth/refresh          protected
//	/api/user/profile          protected
//	/api/user/language         protected (PATCH)
package router

import (
	"strings"

	"LsmAgentGame/api"
	"LsmAgentGame/config"
	"LsmAgentGame/middleware"

	"github.com/gin-gonic/gin"
)

// New constructs the *gin.Engine.
func New(cfg *config.Config, authAPI *api.AuthAPI, gameAPI *api.GameAPI, captchaAPI *api.CaptchaAPI, versionAPI *api.VersionAPI, userAPI *api.UserAPI, gitLogAPI *api.GitLogAPI, roomAPI *api.RoomAPI, adminAPI *api.AdminAPI, walletAPI *api.WalletAPI, llmAPI *api.LlmAPI, wikiAPI *api.WikiAPI, modelAdminAPI *api.ModelAdminAPI, modelLogAPI *api.ModelLogAPI, modelWalletAPI *api.ModelWalletAPI, modelGrantAPI *api.ModelGrantAPI, modelAgentMemoryAPI *api.ModelAgentMemoryAPI, propAPI *api.PropAPI, sourceStatsAPI *api.SourceStatsAPI, recallChatAPI *api.RecallChatAPI, werewolf20260812API *api.Werewolf20260812API, werewolfReviewAPI *api.WerewolfReviewAPI, debateAPI *api.DebateAPI) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestID(), middleware.Logging(), middleware.CORS(cfg))

	// Static cache policy. Registered BEFORE the static routes so the headers
	// land on the response that r.Static / r.StaticFile write:
	//   - /assets/* (content-hashed JS/CSS/images) → long-lived cache (1 year).
	//     Vite includes the file hash in the name, so a new deploy ships a new
	//     URL and old caches are simply not referenced.
	//   - everything else static (/, /index.html, /favicon.svg, SPA fallback,
	//     /xiangqi, /chess/:roomId, …) → no-cache. These are not content-hashed;
	//     the browser must revalidate them on every request so a fresh deploy
	//     is picked up immediately (otherwise users keep loading stale JS that
	//     references the old asset hash and missing the latest bug fixes).
	r.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		// Leave API + WS upgrade paths alone — they handle their own caching.
		if strings.HasPrefix(path, "/api/") || path == "/ws" || strings.HasPrefix(path, "/ws?") {
			c.Next()
			return
		}
		if strings.HasPrefix(path, "/assets/") {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		}
		c.Next()
	})

	// Static assets built from ClientWeb/dist and copied to ServerGo/static.
	// The router reads them from ./ServerGo/static (relative to the binary's CWD,
	// which is the project root when launched via rebuild_restart_app.sh).
	r.Static("/assets", "./ServerGo/static/assets")
	// Game rule markdown files served at /rules/<kind>.md. Sourced from
	// ClientWeb/dist/rules/ (built by `npm run build`) and copied to
	// ServerGo/static/rules/ by rebuild_restart_app.sh. The frontend's
	// RulesViewer fetches these directly — no API contract, just static
	// text. If a kind's file is missing the fetch returns 404 and the
	// viewer shows a friendly error card.
	//
	// This route MUST be registered before the SPA fallback (r.NoRoute)
	// below, otherwise GET /rules/<x>.md falls through to index.html and
	// the markdown is never returned.
	r.Static("/rules", "./ServerGo/static/rules")
	r.StaticFile("/favicon.svg", "./ServerGo/static/favicon.svg")
	r.StaticFile("/index.html", "./ServerGo/static/index.html")

	// SPA fallback: any unmatched non-API GET returns index.html so client-side
	// routes (e.g. /game/lobby-puzzle) work after a hard reload.
	//
	// IMPORTANT: skip the WebSocket upgrade path here. Gin matches NoRoute for
	// any un-registered handler, but the GET /ws route is added in main.go AFTER
	// router.New() returns. In dev with a hot-restart race, a request arriving
	// before the route is appended can fall through to NoRoute — without this
	// guard we'd serve index.html (200 OK) and silently swallow the upgrade.
	r.NoRoute(func(c *gin.Context) {
		// Leave /ws alone — its handler is registered later in main.go and
		// uses the websocket Upgrader. Returning 404 here would mask real
		// upgrade failures with a misleading "no route" status.
		if c.Request.URL.Path == "/ws" {
			c.JSON(404, gin.H{"code": 404, "message": "ws route not yet registered"})
			return
		}
		if c.Request.Method == "GET" && !strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.File("./ServerGo/static/index.html")
			return
		}
		c.JSON(404, gin.H{"code": 404, "message": "not found"})
	})

	// Health & public endpoints.
	r.GET("/api/health", authAPI.Health)
	r.GET("/api/version", versionAPI.Get)
	r.GET("/api/games", gameAPI.List)
	// Captcha issue — public; rate-limited at the proxy layer if needed.
	r.POST("/api/captcha", captchaAPI.Issue)

	// 提交记录 —— 公开接口,任何已加载页面的访客都能查看 git 历史摘要。
	git := r.Group("/api/git")
	{
		git.GET("/log", gitLogAPI.List)
		git.GET("/log/:id", gitLogAPI.Detail)
	}

	// Wiki —— 项目文档列表与内容查看。docs/ 目录是公开知识库,无需鉴权。
	// 安全性由 WikiAPI.Content 的 baseName + .md 白名单 + 大小上限兜底。
	wiki := r.Group("/api/wiki")
	{
		wiki.GET("/list", wikiAPI.List)
		wiki.GET("/content", wikiAPI.Content)
	}

	// 源码统计 —— 公开接口(任何已加载页面的访客都能查看代码体量)。
	// 扫描目标目录在构造期硬编码,query 不接受任意路径;SourceStatsAPI
	// 内置文件数 / 单文件大小 / 递归深度三道防御。
	r.GET("/api/source-stats", sourceStatsAPI.Stats)

	// Auth.
	auth := r.Group("/api/auth")
	{
		auth.POST("/register", authAPI.Register)
		auth.POST("/login", authAPI.Login)
		auth.POST("/logout", authAPI.Logout)
		auth.POST("/refresh", middleware.AuthRequired(cfg), authAPI.Refresh)
	}

	// User preferences — all protected.
	user := r.Group("/api/user")
	user.Use(middleware.AuthRequired(cfg))
	{
		user.GET("/profile", userAPI.GetProfile)
		user.PATCH("/language", userAPI.UpdateLanguage)
		user.PATCH("/nickname", userAPI.UpdateNickname)
	}

	// Wallet — all protected.
	wallet := r.Group("/api/wallet")
	wallet.Use(middleware.AuthRequired(cfg))
	{
		wallet.GET("/balance", walletAPI.GetBalance)
		wallet.GET("/transactions", walletAPI.ListTransactions)
		wallet.POST("/claim-daily", walletAPI.ClaimDaily)
	}

	// Room management — all protected.
	rooms := r.Group("/api")
	rooms.Use(middleware.AuthRequired(cfg))
	{
		rooms.GET("/games/:kind/rooms", roomAPI.List)
		rooms.POST("/games/:kind/rooms", roomAPI.Create)
		rooms.POST("/rooms/:id/join", roomAPI.Join)
		rooms.POST("/rooms/:id/leave", roomAPI.Leave)
		// Spectator endpoints — observers attach to a room without consuming
		// a seat and without affecting the players' UI.
		rooms.POST("/rooms/:id/spectate", roomAPI.Spectate)
		rooms.POST("/rooms/:id/leave_spectate", roomAPI.LeaveSpectate)
		rooms.GET("/rooms/:id", roomAPI.Detail)
	}

	// Werewolf prop REST (2026-07-21 §道具系统):
	//   GET  /api/games/werewolf/props       — 列出已启用道具(前端抽屉)
	//   POST /api/games/werewolf/props/use   — HTTP 形式使用道具(WS 断线兜底)
	//   GET  /api/games/werewolf/rooms/:roomId/prop_history  — 房间内最近 N 条道具使用记录(v3 §G5)
	werewolfGames := r.Group("/api/games/werewolf")
	werewolfGames.Use(middleware.AuthRequired(cfg))
	{
		werewolfGames.GET("/props", propAPI.ListProps)
		werewolfGames.POST("/props/use", propAPI.UseProp)
		werewolfGames.GET("/rooms/:roomId/prop_history", propAPI.GetPropHistory)
		// §20260813-02 U2 (T13) — 道具经济分析(使用/命中率/金币四流向聚合)。
		werewolfGames.GET("/prop-economy", propAPI.GetPropEconomy)
		// 2026-08-11 §20260811-05 U2 — 赛后复盘问答:终局后玩家/观战者
		// 向本局 bot 座位提问(冻结 Memory 快照单轮问答,不写回 Memory)。
		werewolfGames.POST("/rooms/:roomId/recall_chat", recallChatAPI.RecallChat)
		// BUG-R200-P2-05 (2026-07-30): 此前只有 /api/rooms/:id 详情,
		// 同命名空间下 /api/games/werewolf/rooms/:roomId 返回 404 容易让
		// 自动化测试误判接口缺失。补一个简单 alias,直接复用 RoomAPI.Detail。
		werewolfGames.GET("/rooms/:roomId", func(c *gin.Context) {
			// 把 :roomId 改写成 :id 以匹配 RoomAPI.Detail(c.Param("id")) 的取参键。
			c.Params = []gin.Param{{Key: "id", Value: c.Param("roomId")}}
			roomAPI.Detail(c)
		})
		// 2026-08-12 §20260812-03 U1 — 阵营胜率热力图(仅观战者可见,§132)。
		werewolfGames.GET("/rooms/:roomId/win-probability", werewolf20260812API.GetWinProbability)
		// §20260812-03 U2 — 暗线信件发送(待 U2 落地后接入实际 manager 方法)。
		werewolfGames.POST("/rooms/:roomId/secret-letter", werewolf20260812API.SendSecretLetter)
		werewolfGames.GET("/rooms/:roomId/secret-letter/inbox", werewolf20260812API.GetSecretLetterInbox)
		// §20260812-03 U3 — 阵营赌注系统。
		werewolfGames.POST("/rooms/:roomId/faction-bet", werewolf20260812API.PlaceFactionBet)
		werewolfGames.GET("/rooms/:roomId/faction-bet-status", werewolf20260812API.GetFactionBetStatus)
		// 2026-08-14 §20260814-01 U1 — 个人复盘 4 维聚合。
		// 路径与前端 PersonalReviewPanel.tsx:81 已写死的 URL 逐字符对齐;
		// §135:只能查看自己的复盘(handler 内校验 :userId == 调用者)。
		werewolfGames.GET("/rooms/:roomId/review/:userId", werewolfReviewAPI.GetPersonalReview)
	}

	// 2026-08-31 §20260831-01 — 辩论比赛 REST 入口。
	// 路径设计对齐 docs/辩论比赛/00 §4.1;HTTP 入口保护由 AuthRequired 中间件保证。
	debateGames := r.Group("/api/games/debate")
	debateGames.Use(middleware.AuthRequired(cfg))
	{
		// 辩题池
		debateGames.GET("/topics", debateAPI.Topics)
		// §20260831-08 — 辩题详情 + 管理员添加自定义辩题(docs/辩论比赛/03 §2.4)。
		debateGames.GET("/topics/:id", debateAPI.TopicDetail)
		debateGames.POST("/topics", debateAPI.CreateTopic)
		debateGames.GET("/stats", debateAPI.Stats) // §20260831-06 模型胜率统计

		// 房间管理
		debateGames.POST("/rooms", debateAPI.Create)
		debateGames.GET("/rooms", debateAPI.List)
		debateGames.GET("/rooms/:id", debateAPI.Detail)
		debateGames.POST("/rooms/:id/spectate", debateAPI.Spectate)
		debateGames.POST("/rooms/:id/leave_spectate", debateAPI.LeaveSpectate)
		debateGames.POST("/rooms/:id/start", debateAPI.Start)
		debateGames.GET("/rooms/:id/history", debateAPI.History)
		debateGames.DELETE("/rooms/:id", debateAPI.Disband)

		// §20260831-08 — 历史对局(已结束比赛分页列表 + 复盘详情,
		// 数据来自 t_lsm_game_debate_* 表)。
		debateGames.GET("/history", debateAPI.HistoryList)
		debateGames.GET("/history/:id", debateAPI.HistoryDetail)
	}

	// LLM model metadata — protected, returns the safe (key-free) list of
	// configured LLM providers for the werewolf AI-player picker.
	llm := r.Group("/api/llm")
	llm.Use(middleware.AuthRequired(cfg))
	{
		llm.GET("/models", llmAPI.List)
		// §20260810-03 F3 — 模型天梯只读聚合。按 model_key GROUP BY games/wins/
		// avg_tokens/net_coins,数据完全来自 t_lsm_game_model_game_log(已逐局写入)。
		llm.GET("/leaderboard", llmAPI.Leaderboard)
		// §20260812-02 U1 — 5 维能力雷达图聚合(win_rate / wolf_win_rate /
		// good_win_rate / token_eff / coin_per_game)。
		llm.GET("/radar", llmAPI.Radar)
		// ROUND 25 BUG-WEREWOLF-P0-NEW-7 follow-up: HEAD-probe the upstream
		// LLM proxy on demand so the front-end / test harness can show a
		// banner when the proxy is unreachable. Cheap (3s HEAD), no admin
		// role required — every logged-in user benefits.
		llm.GET("/health", llmAPI.Health)
		// §20260813-02 U1 (T12) — 胜率趋势追踪:按模型/角色/座位 + 按日趋势。
		llm.GET("/win-trends", llmAPI.WinTrends)
	}

	// Admin management — all protected, requires admin or super admin role.
	admin := r.Group("/api/admin")
	admin.Use(middleware.AuthRequired(cfg))
	{
		admin.GET("/users", adminAPI.ListUsers)
		admin.DELETE("/users/:id", adminAPI.DeleteUser)
		admin.POST("/rooms/cleanup", adminAPI.CleanupStaleRooms)
		// Force-disband: super-admin "kill switch" for a stuck room.
		// Tears down in-memory GameState + deletes DB rows + broadcasts
		// `game.removed` to every connected client. Optional `reason`
		// query parameter is forwarded to the audit log and the
		// client-side frame.
		admin.DELETE("/rooms/:room_id", adminAPI.ForceDisbandRoom)
		// Chat cleanup: admin + super admin can delete lobby chat messages by time range.
		admin.POST("/chat/cleanup", adminAPI.CleanupChatMessages)
		// Werewolf 500K chat history queue viewer (2026-07-09 §13-bugfix)
		admin.GET("/werewolf/rooms/:room_id/chat_history", adminAPI.GetWerewolfChatHistory)

		// Werewolf prop admin (2026-07-21 §道具系统):
		//   GET  /werewolf/props         — 列出全部道具(含 disabled)
		//   PUT  /werewolf/props/:key    — 更新 enabled / price / cooldown_sec / description
		//   GET  /werewolf/props/usage   — 道具使用日志(分页 + prop_key 过滤)
		// 注:放在 werewolf 段而非 llm 段,因为道具是狼人杀子系统。
		admin.GET("/werewolf/props", propAPI.AdminListProps)
		admin.PUT("/werewolf/props/:key", propAPI.AdminUpdateProp)
		admin.GET("/werewolf/props/usage", propAPI.AdminListUsage)

		// LLM provider / model admin endpoints — Phase 5 model management UI.
		// All endpoints require admin; POST /reload and POST adjust require
		// super admin (enforced in the handler). API keys are never returned
		// to clients (only api_key_hint).
		adminLLM := admin.Group("/llm")
		{
			// Provider CRUD
			adminLLM.GET("/providers", modelAdminAPI.ListProviders)
			adminLLM.POST("/providers", modelAdminAPI.CreateProvider)
			adminLLM.PUT("/providers/:id", modelAdminAPI.UpdateProvider)
			adminLLM.DELETE("/providers/:id", modelAdminAPI.DeleteProvider)
			adminLLM.POST("/providers/:id/test", modelAdminAPI.TestProvider)
			adminLLM.POST("/providers/reload", modelAdminAPI.ReloadProviders)

			// Per-provider game log
			adminLLM.GET("/providers/:id/games", modelLogAPI.ListProviderGames)
			adminLLM.GET("/games/:gameLogID", modelLogAPI.GetGameLog)
			adminLLM.GET("/games/:gameLogID/messages", modelLogAPI.ListGameMessages)

			// 2026-07-20 §131 — Agent 持久化记忆(MEMORY.md)管理端点。
			// GET 查看该模型 MEMORY.md 原文 + version/game_count;
			// DELETE 清空记忆(软重置,version+1,memory_md="")。
			adminLLM.GET("/providers/:id/memory", modelAgentMemoryAPI.GetMemory)
			adminLLM.DELETE("/providers/:id/memory", modelAgentMemoryAPI.ClearMemory)

			// Bot wallet
			adminLLM.GET("/bots/:botUserID/wallet", modelWalletAPI.GetBotWallet)
			adminLLM.POST("/bots/:botUserID/wallet/adjust", modelWalletAPI.AdjustBotWallet)
			// Super-admin daily grant (§135) — credits every (or one) LLM
			// provider's bot wallet with `Amount` coins, once per UTC+8 day,
			// enforced by t_lsm_game_admin_grant.(provider_id, grant_date)
			// composite unique key.
			adminLLM.POST("/bots/grant-daily", modelGrantAPI.GrantDaily)
		}
	}

	return r
}
