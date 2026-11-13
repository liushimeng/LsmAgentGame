// Package service — room_creator_disband_test.go: 2026-11-13 §测试房自助解散
// DisbandRoomByCreator 权限判据单测(DELETE /api/rooms/:id 的 service 侧)。
//
// 四个核心场景:创建者匹配放行(经 fakeGameSvc hook 断言 ForceDisbandRoom
// 清理链被调)、非创建者拒绝、历史房(creator 为空)拒绝、房不存在。
// 另钉死 CreateRoomWithAgents 建房落库 CreatorUserID(权限判据的写入侧)。
//
// 测试策略:本仓库 service 包无 sqlite/内存 DB 基建(go.mod 只有 mysql
// driver),复用 wallet_service_test.go 的 getDB(t) 真库直连 + 不可达即
// t.Skip;fakeGameSvc 复用 room_force_disband_test.go 的既有桩。房间行用
// util.NewUUID() 保证唯一,t.Cleanup 清理,不污染并行测试。
package service

import (
	"context"
	"testing"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/models"
	"LsmAgentGame/util"

	"gorm.io/gorm"
)

// creatorDisbandFixture 插一间房 + N 条 player 行,返回房间 ID。
// cleanup 由调用方挂 t.Cleanup(拒绝分支房行仍在,需回收)。
func creatorDisbandFixture(t *testing.T, db *gorm.DB, creator string, players int) string {
	t.Helper()
	roomID := util.NewUUID()
	room := models.TLsmGameRoom{
		ID:            roomID,
		Name:          "creator-disband-test-" + roomID[:6],
		GameKind:      "virtual_city",
		Capacity:      12,
		CurrentCount:  players,
		Status:        "playing",
		CreatorUserID: creator,
	}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("fixture: create room: %v", err)
	}
	for i := 0; i < players; i++ {
		p := models.TLsmGamePlayer{
			ID:     util.NewUUID(),
			RoomID: roomID,
			UserID: util.NewUUID(),
			Role:   models.PlayerRoleAgent,
			Seat:   i,
		}
		if err := db.Create(&p).Error; err != nil {
			t.Fatalf("fixture: create player %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		db.Where("room_id = ?", roomID).Delete(&models.TLsmGamePlayer{})
		db.Where("id = ?", roomID).Delete(&models.TLsmGameRoom{})
	})
	return roomID
}

// creatorDisbandCleanup 删除已断言被清理链干掉的房间(幂等,行已不存在时
// Delete 也不报错)。
func creatorDisbandCleanup(t *testing.T, db *gorm.DB, roomID string) {
	t.Helper()
	t.Cleanup(func() {
		db.Where("room_id = ?", roomID).Delete(&models.TLsmGamePlayer{})
		db.Where("id = ?", roomID).Delete(&models.TLsmGameRoom{})
	})
}

// assertDisbandErrCode 断言 errcode.Error 且 code 匹配(入参经 AsError 归一,
// DisbandRoomByCreator 返回签名与 ForceDisbandRoom 一致为 error)。
func assertDisbandErrCode(t *testing.T, err error, wantCode int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected errcode %d, got nil", wantCode)
	}
	e := errcode.AsError(err)
	if e.Code != wantCode {
		t.Fatalf("expected code %d, got %d (%s)", wantCode, e.Code, e.Message)
	}
}

// TestDisbandRoomByCreator_CreatorAllowed 创建者匹配 → 放行,且复用
// ForceDisbandRoom 整条清理链:fakeGameSvc hook 收到 RemoveRoomState(停
// Agent/LLM 循环)与 BroadcastRoomRemoved(reason=creator-disband),DB 侧
// player/room 行事务删除,DisbandResult.PlayersDeleted 如实计数。
func TestDisbandRoomByCreator_CreatorAllowed(t *testing.T) {
	db := getDB(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	creator := util.NewUUID()
	roomID := creatorDisbandFixture(t, db, creator, 3)
	creatorDisbandCleanup(t, db, roomID)

	fake := &fakeGameSvc{}
	s := NewRoomService(db, nil)
	s.SetGameServiceHook(fake)

	res, derr := s.DisbandRoomByCreator(ctx, roomID, creator)
	if derr != nil {
		ce := errcode.AsError(derr)
		t.Fatalf("creator disband must succeed: code=%d msg=%s", ce.Code, ce.Message)
	}
	if res.RoomID != roomID {
		t.Fatalf("res.RoomID = %q, want %q", res.RoomID, roomID)
	}
	if res.PlayersDeleted != 3 {
		t.Fatalf("res.PlayersDeleted = %d, want 3", res.PlayersDeleted)
	}
	if res.Reason != "creator-disband" {
		t.Fatalf("res.Reason = %q, want creator-disband (审计日志与管理员强删区分)", res.Reason)
	}
	// 清理链被调:RemoveRoomState(房间内存态/Agent 循环)必须先于 DB 删除。
	if len(fake.removeCalls) != 1 || fake.removeCalls[0] != roomID {
		t.Fatalf("RemoveRoomState calls = %v, want [%s]", fake.removeCalls, roomID)
	}
	if len(fake.bcastCalls) != 1 || fake.bcastCalls[0].RoomID != roomID ||
		fake.bcastCalls[0].Reason != "creator-disband" {
		t.Fatalf("BroadcastRoomRemoved calls = %v, want room=%s reason=creator-disband",
			fake.bcastCalls, roomID)
	}
	// DB 行已删(player + room)。
	var n int64
	if err := db.Model(&models.TLsmGamePlayer{}).Where("room_id = ?", roomID).Count(&n).Error; err != nil {
		t.Fatalf("count players: %v", err)
	}
	if n != 0 {
		t.Fatalf("player rows must be deleted, got %d", n)
	}
	if err := db.Model(&models.TLsmGameRoom{}).Where("id = ?", roomID).Count(&n).Error; err != nil {
		t.Fatalf("count room: %v", err)
	}
	if n != 0 {
		t.Fatalf("room row must be deleted, got %d", n)
	}
}

// TestDisbandRoomByCreator_NotCreatorRejected 非创建者 → ErrPermissionDenied,
// 房间原样保留,清理链(hook)不得被触发。
func TestDisbandRoomByCreator_NotCreatorRejected(t *testing.T) {
	db := getDB(t)
	if db == nil {
		return
	}
	creator := util.NewUUID()
	roomID := creatorDisbandFixture(t, db, creator, 1)

	fake := &fakeGameSvc{}
	s := NewRoomService(db, nil)
	s.SetGameServiceHook(fake)

	_, e := s.DisbandRoomByCreator(context.Background(), roomID, util.NewUUID())
	assertDisbandErrCode(t, e, errcode.ErrPermissionDenied)
	var n int64
	db.Model(&models.TLsmGameRoom{}).Where("id = ?", roomID).Count(&n)
	if n != 1 {
		t.Fatalf("room must survive a denied disband, got %d rows", n)
	}
	if len(fake.removeCalls) != 0 {
		t.Fatalf("RemoveRoomState must NOT fire on denied disband, got %v", fake.removeCalls)
	}
}

// TestDisbandRoomByCreator_LegacyEmptyCreatorRejected 历史房(creator 为空,
// 加列前的存量行)→ ErrPermissionDenied:空值无主,任何登录用户都不得自助
// 清掉升级前的房间(只能管理员强删)。
func TestDisbandRoomByCreator_LegacyEmptyCreatorRejected(t *testing.T) {
	db := getDB(t)
	if db == nil {
		return
	}
	roomID := creatorDisbandFixture(t, db, "", 0)

	s := NewRoomService(db, nil)
	_, e := s.DisbandRoomByCreator(context.Background(), roomID, util.NewUUID())
	assertDisbandErrCode(t, e, errcode.ErrPermissionDenied)
	var n int64
	db.Model(&models.TLsmGameRoom{}).Where("id = ?", roomID).Count(&n)
	if n != 1 {
		t.Fatalf("legacy room must survive, got %d rows", n)
	}
}

// TestDisbandRoomByCreator_RoomNotFound 房不存在 → ErrRoomNotFound。
func TestDisbandRoomByCreator_RoomNotFound(t *testing.T) {
	db := getDB(t)
	if db == nil {
		return
	}
	s := NewRoomService(db, nil)
	_, e := s.DisbandRoomByCreator(context.Background(), util.NewUUID(), util.NewUUID())
	assertDisbandErrCode(t, e, errcode.ErrRoomNotFound)
}

// TestDisbandRoomByCreator_EmptyRoomID 入参守卫:空 room_id 在触达 DB 前拒绝。
func TestDisbandRoomByCreator_EmptyRoomID(t *testing.T) {
	s := &RoomService{} // db == nil:空 room_id 分支必须先于 db 判空返回
	_, e := s.DisbandRoomByCreator(context.Background(), "  ", "u")
	assertDisbandErrCode(t, e, errcode.ErrValidationFailed)
}

// TestCreateRoomWithAgents_RecordsCreatorUserID 写入侧钉死:CreateRoom 落库
// creator_user_id = 建房 uid(虚拟城市 full-agent 房创建者不入座,但 HTTP
// 建房一定带 token,uid 一定有 → 列恒非空)。doudizhu 无 agent_seats,整条
// 路径不触碰 LLM/agentSeater hook。
func TestCreateRoomWithAgents_RecordsCreatorUserID(t *testing.T) {
	db := getDB(t)
	if db == nil {
		return
	}
	cfg := config.Load()
	creator := util.NewUUID()
	s := NewRoomService(db, cfg)

	detail, e := s.CreateRoom("doudizhu", creator, "")
	if e != nil {
		t.Fatalf("CreateRoom: code=%d msg=%s", e.Code, e.Message)
	}
	creatorDisbandCleanup(t, db, detail.ID)

	var room models.TLsmGameRoom
	if err := db.Where("id = ?", detail.ID).First(&room).Error; err != nil {
		t.Fatalf("load room: %v", err)
	}
	if room.CreatorUserID != creator {
		t.Fatalf("room.CreatorUserID = %q, want %q", room.CreatorUserID, creator)
	}
}
