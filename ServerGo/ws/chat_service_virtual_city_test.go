package ws

// 2026-09-25 §23 房间聊天删除与3D语音气泡 — 虚拟城市房间禁言单测。
//
// 契约: lag_docs/虚拟城市/已实现/23-房间聊天删除与3D语音气泡/01-方案与实施记录-v1.md §3.2。
//
//	(a) virtual_city 房间 Send  → 35104 ErrVirtualCityRoomChatDisabled
//	(b) virtual_city 房间 Whisper → 35104
//	(c) 非 virtual_city 房间(xiangqi 等)/ lobby scope 不被拦截
//
// bot 发言通道(SendFromBot / WhisperFromBot)不经此拦截,不在本文件覆盖。

import (
	"database/sql"
	"errors"
	"testing"

	"LsmAgentGame/errcode"

	_ "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// stubGameKindLookup 是 gameKindLookuper 的测试桩:GameKindOf 恒返回固定
// game_kind,模拟 RoomService 反查 t_lsm_game_room.game_kind 的结果。
type stubGameKindLookup struct{ kind string }

func (s stubGameKindLookup) GameKindOf(roomID string) string { return s.kind }

// assertRoomChatDisabled 断言 err 是 code=35104 的 *errcode.Error。
func assertRoomChatDisabled(t *testing.T, err error) {
	t.Helper()
	var ce *errcode.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v (%T), want *errcode.Error code %d",
			err, err, errcode.ErrVirtualCityRoomChatDisabled)
	}
	if ce.Code != errcode.ErrVirtualCityRoomChatDisabled {
		t.Fatalf("code = %d, want %d (virtual_city room chat disabled)",
			ce.Code, errcode.ErrVirtualCityRoomChatDisabled)
	}
}

// assertNotRoomChatDisabled 断言 err 不是 35104(允许其它错误,例如无真实
// DB 时的 errDB —— 用例只关心「未被 35104 拦截」)。
func assertNotRoomChatDisabled(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want non-nil (no real DB wired); " +
			"35104 interception must not fire either")
	}
	var ce *errcode.Error
	if errors.As(err, &ce) && ce.Code == errcode.ErrVirtualCityRoomChatDisabled {
		t.Fatalf("err = %v, non-virtual_city room must NOT be rejected with 35104", err)
	}
}

// (a) virtual_city 房间的 chat.send → 35104。
//
// 故意使用 nil db / nil hub:拦截必须发生在 lookupAccount / 落库 / 广播
// 之前,一旦拦截点被意外下移,本测试会以 panic 暴露回归。
func TestVirtualCityRoomChat_SendRejected(t *testing.T) {
	svc := &ChatService{roomSvc: stubGameKindLookup{kind: "virtual_city"}}
	c := &Client{UserID: "human-1"}

	_, err := svc.Send(c, "room", "vc-room-1", "hello")
	assertRoomChatDisabled(t, err)
}

// (b) virtual_city 房间的 chat.whisper → 35104(入口处拦截,同 nil db/hub 套路)。
func TestVirtualCityRoomChat_WhisperRejected(t *testing.T) {
	svc := &ChatService{roomSvc: stubGameKindLookup{kind: "virtual_city"}}
	c := &Client{UserID: "human-1"}

	_, err := svc.Whisper(c, "vc-room-1", "human-2", "", "psst")
	assertRoomChatDisabled(t, err)
}

// 判定函数本身的表驱动覆盖:kind / roomID / nil roomSvc 各分支。
func TestVirtualCityChatDisabled_Predicate(t *testing.T) {
	cases := []struct {
		name    string
		roomSvc gameKindLookuper // nil 表示未接线
		roomID  string
		want    bool
	}{
		{"virtual_city room intercepted", stubGameKindLookup{kind: "virtual_city"}, "r1", true},
		{"xiangqi room untouched", stubGameKindLookup{kind: "xiangqi"}, "r1", false},
		{"werewolf room untouched", stubGameKindLookup{kind: "werewolf"}, "r1", false},
		{"unknown kind untouched", stubGameKindLookup{kind: ""}, "r1", false},
		{"empty roomID never intercepted", stubGameKindLookup{kind: "virtual_city"}, "", false},
		{"nil roomSvc backward compatible", nil, "r1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &ChatService{roomSvc: tc.roomSvc}
			if got := svc.virtualCityChatDisabled(tc.roomID); got != tc.want {
				t.Fatalf("virtualCityChatDisabled(%q) = %v, want %v", tc.roomID, got, tc.want)
			}
		})
	}
}

// newFailingChatDB 返回一个「必定查询失败」的 *gorm.DB:sql.Open 惰性建连,
// DSN 指向关闭的端口(127.0.0.1:1),首次查询即 connection refused。
// 用于让 Send/Whisper 走通 35104 拦截点之后的代码路径,验证非 virtual_city
// 房间不会被误拦(最终落库失败返回 errDB 而非 35104)。
func newFailingChatDB(t *testing.T) *gorm.DB {
	t.Helper()
	// sql.Open 不建立连接;把 *sql.DB 作为 Conn 传入,gorm.Open 不再 ping。
	sqlDB, err := sql.Open("mysql", "root@tcp(127.0.0.1:1)/lsm_chat_test?timeout=1s")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		// gorm.Open 默认 Ping 一次;关掉以保持完全惰性,测试零网络依赖。
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return db
}

// (c) 非 virtual_city 房间不被拦截:xiangqi 房间的 Send/Whisper 走完整
// 持久化路径(此处因无真实 DB 落库失败),但错误绝不是 35104。
func TestVirtualCityRoomChat_OtherGamesUntouched(t *testing.T) {
	hub := NewHub()
	c := &Client{UserID: "human-1", send: make(chan Envelope, 16)}

	t.Run("xiangqi room send not intercepted", func(t *testing.T) {
		svc := NewChatService(newFailingChatDB(t), hub, nil)
		svc.roomSvc = stubGameKindLookup{kind: "xiangqi"}
		_, err := svc.Send(c, "room", "xq-room-1", "hello")
		assertNotRoomChatDisabled(t, err)
	})

	t.Run("xiangqi room whisper not intercepted", func(t *testing.T) {
		svc := NewChatService(newFailingChatDB(t), hub, nil)
		svc.roomSvc = stubGameKindLookup{kind: "xiangqi"}
		_, err := svc.Whisper(c, "xq-room-1", "human-2", "", "psst")
		assertNotRoomChatDisabled(t, err)
	})

	t.Run("lobby scope never intercepted", func(t *testing.T) {
		svc := NewChatService(newFailingChatDB(t), hub, nil)
		svc.roomSvc = stubGameKindLookup{kind: "virtual_city"}
		_, err := svc.Send(c, "lobby", "", "hello lobby")
		assertNotRoomChatDisabled(t, err)
	})
}
