// Package ws —— 用户列表 WebSocket 服务。
//
// 登录后所有界面交互走 WS（JSON 信封）。用户列表的读取与删除通过 user.* 帧实现，
// 与 REST /api/admin/users 并存（后者保留向后兼容）。
//
// 帧类型（所有消息封装在 Envelope 中，type 字段以 "user." 开头）：
//
//   client → server
//     user.list          { skip?, limit?, sort?, online? }—— 请求用户列表（分页 + 排序 + 按调用者权限裁剪字段）
//                                                            sort ∈ {"created_at","last_login_at"}; 默认 created_at
//                                                            online ∈ {true, false}; 不传则不过滤
//     user.delete        { id }                            —— 删除单个用户（仅超级管理员）
//     user.batch_delete  { ids: [string] }                 —— 批量删除用户（仅超级管理员，上限 100）
//     user.revoke_super  { id }                            —— 撤销某用户的超级管理员身份（仅超级管理员；目标必须离线且当前就是超管）
//
//   server → client
//     user.list_resp         { users: [...], my_user_type, total, skip, limit, online } —— 用户列表（分页）
//                                                              online: 透传请求里的过滤器(true/false)
//     user.delete_resp       { id }               —— 单删回执（带 seq）
//     user.batch_delete_resp { success_ids: [...], failed: [{id, reason_code, reason}] }
//                                             —— 批删回执；failed 列出逐条失败原因
//     user.deleted           { id }               —— 某用户已被删除（广播，批删时每次成功一个发一次）
//     user.revoke_super_resp { id, new_user_type } —— 撤销超级管理员回执（仅回给调用者，new_user_type=1）
//     user.role_changed      { id, new_user_type }  —— 用户角色变更广播（撤销超管后推送，new_user_type=1）
//     user.error             { code, message }     —— 错误
//
// 字段可见性按调用者 user_type 分级：
//   普通用户（1）：仅 { id, nickname, online }
//   管理员（2）：完整字段（无密码，PasswordHash 本就 json:"-"）+ online
//   超管（3）：同管理员 + can_delete:true（前端据此显示删除按钮，仅「非超管、非自己」的行）
//
// user.list_resp 恒含 total/skip/limit（分页元数据，无敏感信息，各角色安全）。
package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/service"

	"go.uber.org/zap"
)

// UserWsService 暴露用户列表操作的 WS 处理函数。
type UserWsService struct {
	userSvc *service.UserService
	roomSvc *service.RoomService
	hub     *Hub
}

// NewUserWsService 构造用户 WS 服务。
func NewUserWsService(userSvc *service.UserService, roomSvc *service.RoomService, hub *Hub) *UserWsService {
	return &UserWsService{userSvc: userSvc, roomSvc: roomSvc, hub: hub}
}

// userListItem 是下发给客户端的单条用户记录。字段按调用者权限裁剪：
// 普通用户只填充 ID/Nickname/Online，其余字段省略（omitempty）。
type userListItem struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Online   bool   `json:"online"`

	// 以下字段仅管理员/超管可见。
	Account        string          `json:"account,omitempty"`
	Phone          string          `json:"phone,omitempty"`
	Email          string          `json:"email,omitempty"`
	UserType       models.UserType `json:"user_type,omitempty"`
	MyInviteCode   string          `json:"my_invite_code,omitempty"`
	ReferralCount  int             `json:"referral_count,omitempty"`
	ReferrerUserID string          `json:"referrer_user_id,omitempty"`
	CreatedAt      int64           `json:"created_at,omitempty"`
	LastLoginAt    *int64          `json:"last_login_at,omitempty"`

	// CanDelete 仅在调用者为超管、且该行可删除（非超管、非自己）时为 true。
	CanDelete bool `json:"can_delete,omitempty"`
}

// HandleClientFrame 路由 user.* 帧到对应的处理器。
func (s *UserWsService) HandleClientFrame(c *Client, env Envelope) {
	switch env.Type {
	case "user.list":
		s.handleList(c, env)
	case "user.delete":
		s.handleDelete(c, env)
	case "user.batch_delete":
		s.handleBatchDelete(c, env)
	case "user.revoke_super":
		s.handleRevokeSuper(c, env)
	default:
		s.sendError(c, env.Seq, 20001, "unknown user message type: "+env.Type)
	}
}

// ─────────────────── Handlers ───────────────────

func (s *UserWsService) handleList(c *Client, env Envelope) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 分页 + 排序参数（空 payload 视为默认值）。分页钳制在 service 层完成，
	// sort 仅识别 known-good 值，其它一律回落到 created_at。
	var req struct {
		Skip   int    `json:"skip"`
		Limit  int    `json:"limit"`
		Sort   string `json:"sort"`
		Online *bool  `json:"online,omitempty"`
	}
	_ = json.Unmarshal(env.Payload, &req)
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}
	if req.Skip < 0 {
		req.Skip = 0
	}

	// 调用者的权限等级决定下发字段。
	callerType, err := s.userSvc.GetUserType(ctx, c.UserID)
	if err != nil {
		s.sendError(c, env.Seq, 40002, "无法确定调用者权限")
		return
	}

	sortMode := service.UserSortByCreated
	if req.Sort == "last_login_at" {
		sortMode = service.UserSortByLastLogin
	}

	// 当前在线 userID 集合。
	onlineIDs := s.hub.ConnectedUserIDs()
	onlineSet := make(map[string]bool, len(onlineIDs))
	for _, uid := range onlineIDs {
		onlineSet[uid] = true
	}

	// 透传 online 过滤器给 service 层做数据库约束（避免在前端 tab 切换时
	// 仍基于当前页切片分桶，否则 total 永远等于 page size）。
	onlyOnline := 0
	if req.Online != nil {
		if *req.Online {
			onlyOnline = 1
		} else {
			onlyOnline = -1
		}
	}
	views, total, err := s.userSvc.ListAllUsersWithPaging(ctx, req.Skip, req.Limit, sortMode, onlyOnline, onlineIDs)
	if err != nil {
		s.sendError(c, env.Seq, 40002, "加载用户列表失败")
		return
	}

	items := make([]userListItem, 0, len(views))
	for _, v := range views {
		items = append(items, buildUserItem(v, callerType, c.UserID, onlineSet[v.ID]))
	}

	resp := map[string]any{
		"users":        items,
		"my_user_type": callerType,
		"total":        total,
		"skip":         req.Skip,
		"limit":        req.Limit,
	}
	if req.Online != nil {
		resp["online"] = *req.Online
	}
	s.sendOK(c, env.Seq, "user.list_resp", resp)
}

// buildUserItem 按调用者权限裁剪单条用户记录的可见字段。这是纯函数，便于单测：
//   - 普通用户(<管理员)：仅 id / nickname / online
//   - 管理员及以上：完整字段（无密码）
//   - 超管：额外对「非超管且非自己」的行标记 can_delete
func buildUserItem(v service.AdminUserView, callerType models.UserType, callerID string, online bool) userListItem {
	item := userListItem{
		ID:       v.ID,
		Nickname: v.Nickname,
		Online:   online,
	}
	if callerType >= models.UserTypeAdmin {
		item.Account = v.Account
		item.Phone = v.Phone
		item.Email = v.Email
		item.UserType = v.UserType
		item.MyInviteCode = v.MyInviteCode
		item.ReferralCount = v.ReferralCount
		item.ReferrerUserID = v.ReferrerUserID
		item.CreatedAt = v.CreatedAt
		item.LastLoginAt = v.LastLoginAt
	}
	if callerType >= models.UserTypeSuper && v.UserType != models.UserTypeSuper && v.ID != callerID {
		item.CanDelete = true
	}
	return item
}

func (s *UserWsService) handleDelete(c *Client, env Envelope) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.ID == "" {
		s.sendError(c, env.Seq, 20001, "invalid user.delete payload")
		return
	}

	// 仅超管可删除。
	callerType, err := s.userSvc.GetUserType(context.Background(), c.UserID)
	if err != nil {
		s.sendError(c, env.Seq, 40002, "无法确定调用者权限")
		return
	}
	if callerType < models.UserTypeSuper {
		s.sendError(c, env.Seq, 40003, "需要超级管理员权限")
		return
	}

	// 禁止删除自己。
	if req.ID == c.UserID {
		s.sendError(c, env.Seq, 20002, "不能删除自己")
		return
	}

	// Generous timeout — InnoDB rollback on cancelled context is more
	// expensive than waiting a few extra seconds for the commit.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ok, reasonCode, reasonMsg := s.deleteOne(ctx, req.ID)
	if !ok {
		s.sendError(c, env.Seq, reasonCode, reasonMsg)
		return
	}

	logger.L().Info("user deleted via ws",
		zap.String("admin_id", c.UserID),
		zap.String("target_id", req.ID))

	// Step 3: Kick all WS connections of the deleted user (after DB commit).
	s.hub.KickUser(req.ID)

	// Step 4: Notify everyone to refresh their user list.
	s.sendOK(c, env.Seq, "user.delete_resp", map[string]any{"id": req.ID})
	s.hub.BroadcastAll(Envelope{Type: "user.deleted", Payload: mustMarshal(map[string]any{"id": req.ID})})
}

// handleBatchDelete 批量删除用户（仅超管，上限 100）。逐 ID 处理，每个 ID
// 的成败独立回报到 success_ids / failed 数组，便于前端精确定位哪条失败。
//
// 失败原因码（与现有 user.delete 对齐）：
//   - 10403 (ErrPermissionDenied)  目标为其它超管
//   - 10101 (ErrAuthAccountNotFound) 目标用户不存在
//   - 40002 (ErrDB)                 数据库错误（仅记日志，不向客户端返详细 err）
//
// 注：与 handleDelete 不同，self-delete 在调用方处显式拦截并报 10403，
// 不计入"成功"路径。
func (s *UserWsService) handleBatchDelete(c *Client, env Envelope) {
	// 1. 超管权限校验
	callerType, err := s.userSvc.GetUserType(context.Background(), c.UserID)
	if err != nil {
		s.sendError(c, env.Seq, errcode.ErrDB, "无法确定调用者权限")
		return
	}
	if callerType < models.UserTypeSuper {
		s.sendError(c, env.Seq, errcode.ErrPermissionDenied, "需要超级管理员权限")
		return
	}

	// 2. 解析 ids
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || len(req.IDs) == 0 {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "ids 不能为空")
		return
	}
	if len(req.IDs) > 100 {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "批量删除上限 100 个")
		return
	}

	// 3. 逐 ID 处理。
	// 每个 ID 用独立的 timeout context，避免一个长事务拖垮整批。
	// 失败原因以 (reason_code, reason) 二元组回报；成功 ID 进入 success_ids。
	successIDs := make([]string, 0, len(req.IDs))
	failed := make([]map[string]any, 0)

	for _, id := range req.IDs {
		if id == "" {
			continue
		}
		if id == c.UserID {
			failed = append(failed, map[string]any{
				"id":          id,
				"reason_code": errcode.ErrPermissionDenied,
				"reason":      "不能删除自己",
			})
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		ok, reasonCode, reasonMsg := s.deleteOne(ctx, id)
		cancel()
		if ok {
			successIDs = append(successIDs, id)
		} else {
			failed = append(failed, map[string]any{
				"id":          id,
				"reason_code": reasonCode,
				"reason":      reasonMsg,
			})
		}
	}

	// 4. 回执（caller）。
	s.sendOK(c, env.Seq, "user.batch_delete_resp", map[string]any{
		"success_ids": successIDs,
		"failed":      failed,
	})

	// 5. 广播 + 踢人（每个成功 ID 一次，沿用 user.delete 的 user.deleted 语义）。
	for _, id := range successIDs {
		s.hub.BroadcastAll(Envelope{Type: "user.deleted", Payload: mustMarshal(map[string]any{"id": id})})
		s.hub.KickUser(id)
	}

	// 6. 日志
	logger.L().Info("user batch deleted via ws",
		zap.String("admin_id", c.UserID),
		zap.Int("success_count", len(successIDs)),
		zap.Int("failed_count", len(failed)),
		zap.Int("requested_count", len(req.IDs)),
	)
}

// handleRevokeSuper 撤销指定用户的超级管理员身份（user_type 3 → 1）。
// 仅超管可调用；目标必须离线、当前就是超管、且不能是调用者本人。
//
// 校验顺序（每一道失败都立刻返回 user.error，不进入 DB 写路径）：
//  1. 解析 payload（DisallowUnknownFields 严格字段校验）
//  2. 调用者必须是超管
//  3. 目标 ID 非空且 ≠ 调用者
//  4. 目标必须处于离线状态（hub.ConnectedUserIDs 检查）
//  5. 目标当前 user_type 必须是 UserTypeSuper
//  6. service.DemoteFromSuper 执行实际 UPDATE（带 AND user_type = ? 守卫）
//
// 成功路径：回 user.revoke_super_resp 给 caller，广播 user.role_changed。
func (s *UserWsService) handleRevokeSuper(c *Client, env Envelope) {
	var req struct {
		ID string `json:"id"`
	}
	// §84b — 严格字段校验,拒绝任何额外字段,以免调用方把 typo 的字段当成「无害噪声」吞掉。
	if err := decodeJSONStrictFromBytes(env.Payload, &req); err != nil {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "id 不能为空")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 2. 调用者必须是超管。
	callerType, err := s.userSvc.GetUserType(ctx, c.UserID)
	if err != nil {
		s.sendError(c, env.Seq, errcode.ErrDB, "无法确定调用者权限")
		return
	}
	if callerType < models.UserTypeSuper {
		s.sendError(c, env.Seq, errcode.ErrPermissionDenied, "需要超级管理员权限")
		return
	}

	// 3. 自保护。
	if req.ID == c.UserID {
		s.sendError(c, env.Seq, errcode.ErrPermissionDenied, "不能撤销自己的超级管理员")
		return
	}

	// 4. 目标必须离线。online 检查放在 super 检查之前/之后都不影响正确性,
	//    但放在这里可以在目标在线时立刻短路,避免不必要的 DB 写。
	onlineIDs := s.hub.ConnectedUserIDs()
	for _, uid := range onlineIDs {
		if uid == req.ID {
			s.sendError(c, env.Seq, errcode.ErrPermissionDenied, "该用户在线,不能撤销超级管理员")
			return
		}
	}

	// 5. 目标必须是当前超管(否则 DemoteFromSuper 的 AND user_type=? 守卫也会失败,
	//    这里先做显式检查便于给客户端更精确的错误消息)。
	targetType, err := s.userSvc.GetUserType(ctx, req.ID)
	if err != nil {
		s.sendError(c, env.Seq, errcode.ErrAuthAccountNotFound, "目标用户不存在")
		return
	}
	if targetType < models.UserTypeSuper {
		s.sendError(c, env.Seq, errcode.ErrPermissionDenied, "目标用户不是超级管理员")
		return
	}

	// 6. 实际降级。
	if err := s.userSvc.DemoteFromSuper(ctx, req.ID); err != nil {
		ce := errcode.AsError(err)
		logger.L().Error("revoke super failed",
			zap.String("admin_id", c.UserID),
			zap.String("target_id", req.ID),
			zap.Int("code", ce.Code),
			zap.String("msg", ce.Message))
		s.sendError(c, env.Seq, ce.Code, ce.Message)
		return
	}

	logger.L().Info("user.revoke_super ok",
		zap.String("admin_id", c.UserID),
		zap.String("target_id", req.ID))

	// 7. 回执 + 广播。广播语义与 user.deleted 对齐 —— 让所有正在监听 user list 的
	//    客户端把这一行的 UserType / CanDelete 状态刷新成「普通用户」。
	s.sendOK(c, env.Seq, "user.revoke_super_resp", map[string]any{
		"id":            req.ID,
		"new_user_type": int(models.UserTypeNormal),
	})
	s.hub.BroadcastAll(Envelope{Type: "user.role_changed", Payload: mustMarshal(map[string]any{
		"id":            req.ID,
		"new_user_type": int(models.UserTypeNormal),
	})})
}

// deleteOne 删除单个用户（不含权限校验 / 自删校验；调用方须先做）。
// 返回 (ok, reason_code, reason_msg)。失败时 reason_code 与 user.error.code 对齐。
//
// 业务流程：
//  1. 拿目标 user_type：超管 → 拒绝（保护其它超管）
//  2. 房间清理（非致命，warn 日志）
//  3. DeleteUserWithRelatedData：chat / session / player / user 4 表事务
func (s *UserWsService) deleteOne(ctx context.Context, targetID string) (bool, int, string) {
	// 1. 保护其它超管。
	targetType, err := s.userSvc.GetUserType(ctx, targetID)
	if err != nil {
		// GetUserType 内部会把 ErrRecordNotFound 映射到 ErrAuthAccountNotFound。
		return false, errcode.ErrAuthAccountNotFound, "目标用户不存在"
	}
	if targetType >= models.UserTypeSuper {
		return false, errcode.ErrPermissionDenied, "不能删除超级管理员"
	}

	// 2. 先清房间（非致命）。一旦 user 行被删除，t_lsm_game_player 也级联
	// 删完，就查不到房间关联了，所以必须先做。
	if s.roomSvc != nil {
		if err := s.roomSvc.DeleteRoomsByUser(ctx, targetID); err != nil {
			logger.L().Warn("room cleanup after user delete",
				zap.String("target_id", targetID), zap.Error(err))
		}
	}

	// 3. 删 user + chat / session / player 行（同一事务）。
	if err := s.userSvc.DeleteUserWithRelatedData(ctx, targetID); err != nil {
		logger.L().Error("delete user failed",
			zap.String("target_id", targetID), zap.Error(err))
		return false, errcode.ErrDB, "删除失败"
	}
	return true, 0, ""
}

// ─────────────────── Helpers ───────────────────

func (s *UserWsService) sendOK(c *Client, seq int64, msgType string, payload any) {
	c.send <- Envelope{Type: msgType, Seq: seq, Payload: mustMarshal(payload)}
}

func (s *UserWsService) sendError(c *Client, seq int64, code int, msg string) {
	c.send <- Envelope{Type: "user.error", Seq: seq, Payload: mustMarshal(map[string]any{
		"code": code, "message": msg,
	})}
}

// decodeJSONStrictFromBytes decodes payload into dst using json.Decoder with
// DisallowUnknownFields (§84b — strict validation). Used by WS handlers whose
// payload arrives as json.RawMessage (the Envelope.Payload bytes). Mirrors
// the pattern in api/model_admin_api.go's decodeJSONStrict so the front-end
// gets a clear "invalid body: ..." message instead of a silent zero value.
func decodeJSONStrictFromBytes(payload json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errJSONTrailing
	}
	return nil
}

// errJSONTrailing is returned when JSON payload has trailing data after the
// top-level value (two objects concatenated in one frame, for example).
var errJSONTrailing = jsonDecodeTrailingError("trailing data after JSON body")

// jsonDecodeTrailingError is a typed string error so callers can distinguish
// "unknown field" from "trailing garbage" in their messages if they want to.
// Implemented as a plain type to avoid importing fmt for one Error() method.
type jsonDecodeTrailingError string

func (e jsonDecodeTrailingError) Error() string { return string(e) }

// ─────────────────── Proto 消息注册 ───────────────────

// registerProtoMessages 在 proto 路由器中注册用户服务的 proto 消息
func (s *UserWsService) registerProtoMessages(reg *ProtoRegistry) {
	// TODO: 迁移 user.list / user.delete / user.batch_delete / user.revoke_super 等
}
