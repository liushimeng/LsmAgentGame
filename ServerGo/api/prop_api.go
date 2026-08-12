// Package api — prop_api.go: 狼人杀 13 人局道具系统的 REST 入口(2026-07-21)。
//
// 用户侧:
//
//	GET    /api/games/werewolf/props                 — 列出已启用的道具(前端道具抽屉)
//	POST   /api/games/werewolf/props/use             — HTTP 形式使用道具(对齐 WS 路径)
//
// 管理员侧:
//
//	GET    /api/admin/werewolf/props                 — 列出全部道具(含 disabled)
//	POST   /api/admin/werewolf/props                 — 创建新道具
//	PUT    /api/admin/werewolf/props/:key            — 更新道具配置(enabled / price / description)
//	GET    /api/admin/werewolf/props/usage           — 道具使用日志(分页 + prop_key 过滤)
//
// 设计动机:
//
//   - WS 路径(`game.werewolf_use_prop`)已在 ws/game_service.go 实现;
//     REST 入口主要给管理后台 / 自动化测试用,以及给前端兜底(WS 断线时)。
//   - 严格 JSON 校验(§84b DisallowUnknownFields)。
//   - 用户/管理员分层:admin 路径要求 UserType >= UserTypeAdmin。
//   - 道具使用走 WerewolfManager.Action_UseProp,与 WS / Agent bot 路径共享
//     PropEngine + WalletService + 公开广播(单一真相源)。
package api

import (
	"net/http"
	"strconv"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/werewolf"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// PropAPI exposes the werewolf prop REST endpoints. The manager/propSvc are
// nil-safe at construction time but required for any prop operation; handlers
// return 500 if either is missing.
type PropAPI struct {
	manager   *werewolf.WerewolfManager
	propSvc   *werewolf.PropService
	userSvc   UserRoleChecker
	walletSvc *service.WalletService
}

// NewPropAPI wires the handler with its dependencies.
// walletSvc 可选(nil-safe):用于 GET /api/games/werewolf/props 回填 my_balance;
// 未注入时 my_balance 返回 -1(前端按"未知"处理,不阻塞道具目录渲染)。
func NewPropAPI(manager *werewolf.WerewolfManager, propSvc *werewolf.PropService, userSvc UserRoleChecker, walletSvc ...*service.WalletService) *PropAPI {
	var ws *service.WalletService
	if len(walletSvc) > 0 {
		ws = walletSvc[0]
	}
	return &PropAPI{manager: manager, propSvc: propSvc, userSvc: userSvc, walletSvc: ws}
}

// ─────────────────── 用户侧:列出/使用 ───────────────────

// ListProps GET /api/games/werewolf/props — 列出已启用道具 + 回填我的余额/剩余次数/冷却。
// 无需鉴权:道具目录是公开元数据(类比 LLM 模型列表 GET /api/llm/models)。
// 但为与项目其它游戏接口一致,仍要求登录(否则返回 401)。
// 响应字段:
//   - props[]         已启用道具目录
//   - total           道具数
//   - my_balance      当前用户金币余额;walletSvc 未注入或查询失败时 -1
//   - my_props_remaining    本局剩余可购买次数(默认 3 - 已用)
//   - cooldown_remaining_sec 冷却剩余秒数(0 = 可立即使用)
//
// 设计(R173 修复,BUG-R173-P1):GET 道具目录时回填 per-user/per-room 状态,
// 避免前端只拿到 props,total 导致 PropPanel 显示 0/0 的问题。
func (h *PropAPI) ListProps(c *gin.Context) {
	if h.propSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "prop service not wired",
		})
		return
	}
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	props, err := h.propSvc.ListEnabledProps(c.Request.Context())
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	if props == nil {
		props = []models.TLsmGameProp{}
	}
	myBalance := int64(-1)
	myPropsRemaining := 0
	cooldownRemainingSec := 0
	if h.walletSvc != nil {
		if bal, werr := h.walletSvc.GetBalance(c.Request.Context(), uid); werr == nil {
			myBalance = bal
		} else {
			logger.L().Warn("ListProps: wallet GetBalance failed",
				zap.String("uid", uid), zap.Error(werr))
		}
	}
	var econTierName string
	var econTierAbsorbPct int
	if room, seat := h.manager.FindUserRoom(uid); room != nil && seat >= 0 {
		werewolf.RoomPropPerSeatSnapshot(room, seat, &myPropsRemaining, &cooldownRemainingSec)
		// v5 EconTier 5 档 — 从房间总金币存量计算 + 输出销毁比例。
		totalCoin := werewolf.RoomTotalCoin(room)
		tier := werewolf.ComputeEconTier(totalCoin)
		econTierName = string(tier)
		econTierAbsorbPct = werewolf.EconTierAbsorbPct(tier)
	}
	// §R197-P4 修复:统一包成 {code, message, data} 信封。前端 ClientWeb/src/services/http.ts::http<T>
	// 在 'code' in body 为假时直接返回原始 text (line 109),导致 resp.props / resp.my_balance
	// 全部是 undefined → PropPanel 显示「暂无可用道具」+「金币余额 同步中…」。
	// 这条端点之前直接 gin.H{...} 漏掉了 code/data 包装;现在补回,前端 http<T> 走
	// body.data 解包路径,PropListResponse 字段对齐。
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"props":                  props,
			"total":                  len(props),
			"my_balance":             myBalance,
			"my_props_remaining":     myPropsRemaining,
			"cooldown_remaining_sec": cooldownRemainingSec,
			// v5 EconTier 5 档徽章字段(供 PropPanel.tsx 顶端展示)。
			"econ_tier":            econTierName,
			"econ_tier_absorb_pct": econTierAbsorbPct,
		},
	})
}

// UseProp POST /api/games/werewolf/props/use — HTTP 形式使用道具。
// 与 WS 路径共享 WerewolfManager.Action_UseProp(单一真相源)。
// payload 字段(严格):
//   - room_id  string(必填)
//   - prop_key string(必填)
//   - target   int   (可选;-1 = AOE)
//   - payload  string(可选,≤ 200 字)
func (h *PropAPI) UseProp(c *gin.Context) {
	if h.manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "werewolf manager not wired",
		})
		return
	}
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	var req struct {
		RoomID  string `json:"room_id"`
		PropKey string `json:"prop_key"`
		Target  int    `json:"target"`
		Payload string `json:"payload,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.RoomID == "" || req.PropKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "room_id and prop_key required",
		})
		return
	}
	if len(req.Payload) > 200 {
		req.Payload = req.Payload[:200]
	}
	_, result, em := h.manager.Action_UseProp(req.RoomID, uid, req.PropKey, req.Target, req.Payload)
	if em != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": em.Code, "message": em.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"prop_key":   req.PropKey,
		"hit":        result.Hit,
		"price_paid": result.PricePaid,
		"pot_return": result.PotReturn,
		"target":     req.Target,
	})
}

// GetPropHistory GET /api/games/werewolf/rooms/:roomId/prop_history — 返回本房间最近 N 条
// 公开道具使用记录（v3 §G5）。用于前端道具使用动态展示 + 与 prop_history Agent 工具共享数据源。
// limit ≤ 20，默认 10。
func (h *PropAPI) GetPropHistory(c *gin.Context) {
	if h.manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "werewolf manager not wired",
		})
		return
	}
	roomID := c.Param("roomId")
	if roomID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "roomId required",
		})
		return
	}
	// 解析 limit（≤ 20）
	limit := 10
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n > 0 && n <= 20 {
				limit = n
			} else if n > 20 {
				limit = 20
			}
		}
	}
	r := h.manager.GetRoom(roomID) // 内部会走 m.rooms；若无方法则使用 mgr.rooms map 直接读取
	if r == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code": errcode.ErrRoomNotFound, "message": "room not found",
		})
		return
	}
	hist := r.GetPropHistoryForAPI(limit)
	// 0-indexed 输出(与 DB / players[*].seat / RoleOf / 所有 WS 帧的 seat 字段一致;
	// 前端显示层自行 +1)。EffectHint 已是 1-indexed 展示文本(写入 DB 前由
	// prop_inject.go 用 seatFrom+1 格式化),保持原样。
	type OutRec struct {
		FromSeat   int    `json:"from_seat"`
		ToSeat     int    `json:"to_seat"`
		PropKey    string `json:"prop_key"`
		PropNameZh string `json:"prop_name_zh"`
		Hit        bool   `json:"hit"`
		EffectHint string `json:"effect_hint"`
		Phase      string `json:"phase"`
		Round      int    `json:"round"`
		CreatedAt  int64  `json:"created_at"`
	}
	out := make([]OutRec, 0, len(hist))
	for _, h := range hist {
		out = append(out, OutRec{
			FromSeat:   h.FromSeat,
			ToSeat:     h.ToSeat,
			PropKey:    h.PropKey,
			PropNameZh: h.PropNameZh,
			Hit:        h.Hit,
			EffectHint: h.EffectHint,
			Phase:      h.Phase,
			Round:      h.Round,
			CreatedAt:  h.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"room_id": roomID,
			"count":   len(out),
			"history": out,
		},
	})
}

// ─────────────────── 管理员侧:CRUD + usage log ───────────────────

// requireAdmin checks the caller's role >= admin. Returns false after writing
// the JSON error response.
func (h *PropAPI) requireAdmin(c *gin.Context) (string, bool) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return "", false
	}
	if h.userSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "user service not wired",
		})
		return "", false
	}
	userType, err := h.userSvc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return "", false
	}
	if userType < models.UserTypeAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要管理员权限",
		})
		return "", false
	}
	return uid, true
}

// AdminListProps GET /api/admin/werewolf/props — 列出全部道具(含 disabled)。
func (h *PropAPI) AdminListProps(c *gin.Context) {
	if _, ok := h.requireAdmin(c); !ok {
		return
	}
	if h.propSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "prop service not wired",
		})
		return
	}
	props, err := h.propSvc.ListEnabledProps(c.Request.Context())
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	if props == nil {
		props = []models.TLsmGameProp{}
	}
	c.JSON(http.StatusOK, gin.H{
		"props": props,
		"total": len(props),
	})
}

// AdminUpdateProp PUT /api/admin/werewolf/props/:key
// 仅允许更新 enabled / price / cooldown_sec / description / max_uses_per_game 字段;
// prop_key / inject_type 不可改(防止破坏 Agent prompt 字段映射)。
func (h *PropAPI) AdminUpdateProp(c *gin.Context) {
	if _, ok := h.requireAdmin(c); !ok {
		return
	}
	if h.propSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "prop service not wired",
		})
		return
	}
	propKey := c.Param("key")
	if propKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "key required",
		})
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "invalid json body",
		})
		return
	}
	// 白名单字段,防止 mass-assignment 改坏只读列。
	allowed := map[string]bool{
		"enabled":           true,
		"price":             true,
		"cooldown_sec":      true,
		"description":       true,
		"max_uses_per_game": true,
		"name_zh":           true,
		"name_en":           true,
	}
	updates := map[string]interface{}{}
	for k, v := range body {
		if allowed[k] {
			updates[k] = v
		}
	}
	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "no allowed fields to update",
		})
		return
	}
	if err := h.propSvc.UpdateProp(c.Request.Context(), propKey, updates); err != nil {
		logger.L().Error("admin update prop failed",
			zap.String("prop_key", propKey), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": errcode.DefaultMessages[errcode.ErrDB],
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated_fields": updates})
}

// AdminListUsage GET /api/admin/werewolf/props/usage?prop_key=&limit=&offset=
func (h *PropAPI) AdminListUsage(c *gin.Context) {
	if _, ok := h.requireAdmin(c); !ok {
		return
	}
	if h.propSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "prop service not wired",
		})
		return
	}
	propKey := c.Query("prop_key")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	rows, total, err := h.propSvc.ListPropUsage(c.Request.Context(), propKey, limit, offset)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	if rows == nil {
		rows = []models.TLsmGamePropUsageLog{}
	}
	c.JSON(http.StatusOK, gin.H{
		"rows":   rows,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
