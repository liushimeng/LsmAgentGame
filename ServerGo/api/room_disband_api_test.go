// Package api — room_disband_api_test.go: 2026-11-13 §测试房自助解散
// DELETE /api/rooms/:id handler 契约单测(权限映射 + Result 信封形状)。
//
// service 层四场景(创建者放行 / 非创建者拒 / 历史房拒 / 房不存在)见
// service/room_creator_disband_test.go;此处钉死 HTTP 侧契约:
//   - 创建者匹配 → 200 + code=0,data 带 room_id / players_deleted;
//   - 非创建者   → HTTP 403 + code=10403(脚本可直接判状态码);
//   - 房不存在   → 200 + code=30001(与 Join/Leave 同信封风格)。
//
// 测试策略:api 包无 DB 基建;经 LSM_CONF 指向 ../service/LsmAgentGame.conf
// 复用真库(conf 缺失或库不可达即 t.Skip,不污染 api/ 目录 —— config 包的
// ensureRuntimeConfigFile 会在 CWD 落一个合成 conf 文件,必须绕开)。
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"LsmAgentGame/config"
	"LsmAgentGame/db"
	"LsmAgentGame/errcode"
	"LsmAgentGame/models"
	"LsmAgentGame/service"
	"LsmAgentGame/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// roomDisbandTestDB 惰性建库连接(memoize 单句柄)。conf 缺失 / DB 不可达
// → t.Skip。LSM_CONF 必须在 config.Load 前设置(config 包 sync.Once,
// api 包内仅本文件触达 config)。
var roomDisbandDB *gorm.DB

func roomDisbandTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	if roomDisbandDB != nil {
		return roomDisbandDB
	}
	confPath := "../service/LsmAgentGame.conf"
	if _, err := os.Stat(confPath); err != nil {
		t.Skipf("skip: no local LsmAgentGame.conf at %s (api 包不 bootstrap 合成 conf)", confPath)
	}
	t.Setenv("LSM_CONF", confPath)
	cfg := config.Load()
	gormDB, err := db.Init(cfg)
	if err != nil {
		t.Skipf("skip: cannot connect to DB: %v", err)
		return nil
	}
	roomDisbandDB = gormDB
	return roomDisbandDB
}

// deleteRoomViaHandler 经 gin 引擎直调 RoomAPI.Delete(绕过 AuthRequired,
// user_id 手填,与 room_api_resident_test.go 同搭法)。
func deleteRoomViaHandler(t *testing.T, svc *service.RoomService, roomID, uid string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	a := NewRoomAPI(svc, nil)
	r := gin.New()
	r.DELETE("/api/rooms/:id", func(c *gin.Context) {
		c.Set("user_id", uid)
		a.Disband(c)
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/"+roomID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// disbandFixture 插一间 creator 房 + 1 条 player 行,返回房间 ID。
func disbandFixture(t *testing.T, g *gorm.DB, creator string) string {
	t.Helper()
	roomID := util.NewUUID()
	room := models.TLsmGameRoom{
		ID:            roomID,
		Name:          "api-disband-test-" + roomID[:6],
		GameKind:      "virtual_city",
		Capacity:      12,
		CurrentCount:  1,
		Status:        "playing",
		CreatorUserID: creator,
	}
	if err := g.Create(&room).Error; err != nil {
		t.Fatalf("fixture: create room: %v", err)
	}
	p := models.TLsmGamePlayer{
		ID:     util.NewUUID(),
		RoomID: roomID,
		UserID: util.NewUUID(),
		Role:   models.PlayerRoleAgent,
		Seat:   0,
	}
	if err := g.Create(&p).Error; err != nil {
		t.Fatalf("fixture: create player: %v", err)
	}
	t.Cleanup(func() {
		g.Where("room_id = ?", roomID).Delete(&models.TLsmGamePlayer{})
		g.Where("id = ?", roomID).Delete(&models.TLsmGameRoom{})
	})
	return roomID
}

// TestDisband_CreatorAllowed 创建者匹配 → 200 + code=0,data 带
// room_id / players_deleted,房间行被清理链删除。
func TestDisband_CreatorAllowed(t *testing.T) {
	g := roomDisbandTestDB(t)
	if g == nil {
		return
	}
	creator := util.NewUUID()
	roomID := disbandFixture(t, g, creator)
	svc := service.NewRoomService(g, nil)

	w := deleteRoomViaHandler(t, svc, roomID, creator)
	if w.Code != http.StatusOK {
		t.Fatalf("creator disband must 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			RoomID         string `json:"room_id"`
			PlayersDeleted int    `json:"players_deleted"`
			Reason         string `json:"reason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	if body.Code != errcode.OK {
		t.Fatalf("code = %d, want %d (body=%s)", body.Code, errcode.OK, w.Body.String())
	}
	if body.Data.RoomID != roomID {
		t.Fatalf("data.room_id = %q, want %q", body.Data.RoomID, roomID)
	}
	if body.Data.PlayersDeleted != 1 {
		t.Fatalf("data.players_deleted = %d, want 1", body.Data.PlayersDeleted)
	}
	if body.Data.Reason != "creator-disband" {
		t.Fatalf("data.reason = %q, want creator-disband", body.Data.Reason)
	}
	var n int64
	g.Model(&models.TLsmGameRoom{}).Where("id = ?", roomID).Count(&n)
	if n != 0 {
		t.Fatalf("room row must be deleted, got %d", n)
	}
}

// TestDisband_NotCreatorRejected 非创建者 → HTTP 403 + code=10403。
func TestDisband_NotCreatorRejected(t *testing.T) {
	g := roomDisbandTestDB(t)
	if g == nil {
		return
	}
	roomID := disbandFixture(t, g, util.NewUUID())
	svc := service.NewRoomService(g, nil)

	w := deleteRoomViaHandler(t, svc, roomID, util.NewUUID())
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-creator disband must 403, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != errcode.ErrPermissionDenied {
		t.Fatalf("code = %d, want %d", body.Code, errcode.ErrPermissionDenied)
	}
	if body.Message == "" {
		t.Fatal("403 message must explain 仅房间创建者可解散 (供脚本/前端展示)")
	}
	var n int64
	g.Model(&models.TLsmGameRoom{}).Where("id = ?", roomID).Count(&n)
	if n != 1 {
		t.Fatalf("room must survive denied disband, got %d rows", n)
	}
}

// TestDisband_RoomNotFound 房不存在 → 200 + code=30001(信封风格同 Join)。
func TestDisband_RoomNotFound(t *testing.T) {
	g := roomDisbandTestDB(t)
	if g == nil {
		return
	}
	svc := service.NewRoomService(g, nil)
	w := deleteRoomViaHandler(t, svc, util.NewUUID(), util.NewUUID())
	if w.Code != http.StatusOK {
		t.Fatalf("room-not-found must stay 200 envelope, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != errcode.ErrRoomNotFound {
		t.Fatalf("code = %d, want %d", body.Code, errcode.ErrRoomNotFound)
	}
}
