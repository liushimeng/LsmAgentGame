package ws

// 2026-10-06 §法官对话 B13 — 「私聊 @法官」截获与 WhisperFromJudge 单测。
//
// 契约: docs/狼人杀/狼人杀法官对话与遗言聊天-设计与实现-20261006.md §7.1:
//   - TestWhisperToJudge_SpectatorAllowed  观战者 whisper 法官占位 → hook 被调,
//     不再被 spectator 守卫拒绝(F-1);whisper 普通玩家仍被拒。
//   - TestWhisperToJudge_Rejections         hook 返回 error → 原样透传(chat.error 20001);
//     hook 未接线(nil)→ 走既有阵营守卫(向后兼容)。
//   - TestWhisperToJudge_PersistAndEcho     hook 成功 → 落库私问行(to=法官占位)+
//     仅发送者本人收到 direct 回显(其余订阅者零帧)。
//   - TestWhisperFromJudge_PrivateOnly      私答仅投递 asker;无 BroadcastRoom* /
//     无 emitRoomMessage(F-2);落库行 from_role="judge"。
//   - TestActivityEvent_ScopeField          EmitRoomActivity payload JSON 含 "scope":"room"。
//
// DB 套路:复用 chat_service_virtual_city_test.go 的惰性 gorm.Open 模式,外加
// DryRun —— Create 只生成 SQL 不执行(nil error + 零值 ID/CreatedAt),让成功
// 落库路径可以走到「回显/投递」分支,零网络依赖。

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// ─────────────────── fake driver:零网络「落库成功」*gorm.DB ───────────────────
//
// gorm 的 DryRun 对 Create 仍会尝试真实连接(实测 connection refused),因此
// 这里用最小 database/sql/driver 桩:Prepare/Exec 恒成功(LastInsertId=42),
// 让 Whisper / WhisperFromJudge 的成功落库路径在无真实 DB 的测试环境走通
// (走到「回显/投递」分支)。Query 返回 ErrSkip —— 本文件不需要读路径。

type fakeQADriver struct{}
type fakeQAConn struct{}
type fakeQAStmt struct{}
type fakeQAResult struct{}
type fakeQATx struct{}

func (fakeQADriver) Open(name string) (driver.Conn, error) { return fakeQAConn{}, nil }
func (fakeQAConn) Prepare(q string) (driver.Stmt, error)   { return fakeQAStmt{}, nil }
func (fakeQAConn) Close() error                             { return nil }
func (fakeQAConn) Begin() (driver.Tx, error)                { return fakeQATx{}, nil }
func (fakeQATx) Commit() error                              { return nil }
func (fakeQATx) Rollback() error                            { return nil }
func (fakeQAStmt) Close() error                             { return nil }
func (fakeQAStmt) NumInput() int                            { return -1 }
func (fakeQAStmt) Exec(args []driver.Value) (driver.Result, error) {
	return fakeQAResult{}, nil
}
func (fakeQAStmt) Query(args []driver.Value) (driver.Rows, error) { return nil, driver.ErrSkip }
func (fakeQAResult) LastInsertId() (int64, error)                { return 42, nil }
func (fakeQAResult) RowsAffected() (int64, error)                { return 1, nil }

var registerFakeQADriver = func() bool {
	sql.Register("lsm_fake_qa", fakeQADriver{})
	return true
}()

// newFakeChatDB 返回挂 fake driver 的 *gorm.DB(Create 恒成功)。
func newFakeChatDB(t *testing.T) *gorm.DB {
	t.Helper()
	if !registerFakeQADriver {
		t.Fatalf("fake driver registration failed")
	}
	db, err := gorm.Open(mysql.New(mysql.Config{
		DriverName:                "lsm_fake_qa",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return db
}

// judgeQATestRig 聚合截获测试的公共脚手架。
type judgeQATestRig struct {
	svc      *ChatService
	hub      *Hub
	sender   *Client
	hookGot  []judgeQAHookCall // judgeQuestionHook 的调用记录
	msgHooks int               // onRoomMessage 计数(断言私问不进 transcript)
}

type judgeQAHookCall struct {
	roomID, askerID, askerAccount, text string
}

// newJudgeQARig 构造 werewolf 房间聊天环境:roomSvc 恒报 "werewolf",
// factionLookup 恒报 spectator(模拟全 AI 房创建者的观战者身份),
// judgeQuestionHook 记录调用并按 hookErr 决定返回值;同时注册
// onRoomMessage 计数 hook(断言私问不进 transcript)。
func newJudgeQARig(t *testing.T, hookErr error) *judgeQATestRig {
	t.Helper()
	rig := &judgeQATestRig{hub: NewHub()}
	rig.svc = NewChatService(newFakeChatDB(t), rig.hub, nil)
	rig.svc.roomSvc = stubGameKindLookup{kind: "werewolf"}
	rig.svc.SetFactionLookup(func(roomID, userID string) (string, bool, bool) {
		return "unknown", false, true // isSpectator=true
	})
	rig.msgHooks = 0
	rig.svc.SetRoomMessageHook(func(msg *ChatMessage) { rig.msgHooks++ })
	rig.svc.SetJudgeQuestionHook(func(roomID, askerID, askerAccount, text string) error {
		rig.hookGot = append(rig.hookGot, judgeQAHookCall{roomID, askerID, askerAccount, text})
		return hookErr
	})
	rig.sender = &Client{UserID: "asker-1", send: make(chan Envelope, 16)}
	return rig
}

// drainFrames 非阻塞收集 client 当前缓冲内的全部帧。
func drainFrames(c *Client) []Envelope {
	var out []Envelope
	for {
		select {
		case env := <-c.send:
			out = append(out, env)
		default:
			return out
		}
	}
}

// TestWhisperToJudge_SpectatorAllowed — F-1:观战者私聊法官占位 uuid 时,
// 截获分支先于 §20260810-03 F1 spectator 守卫执行:hook 被调 + 无
// errSpectatorWhisperForbidden;同一发送者 whisper 普通玩家目标仍被拒。
func TestWhisperToJudge_SpectatorAllowed(t *testing.T) {
	rig := newJudgeQARig(t, nil)

	msg, err := rig.svc.Whisper(rig.sender, "ww-room-1", JudgeFromUserID, "", "现在到什么阶段了?")
	if err != nil {
		t.Fatalf("whisper-to-judge should pass the spectator guard, got err=%v", err)
	}
	if msg == nil {
		t.Fatalf("whisper-to-judge should return a message")
	}
	if len(rig.hookGot) != 1 {
		t.Fatalf("judgeQuestionHook calls = %d, want 1", len(rig.hookGot))
	}
	if rig.hookGot[0].roomID != "ww-room-1" || rig.hookGot[0].askerID != "asker-1" ||
		rig.hookGot[0].text != "现在到什么阶段了?" {
		t.Fatalf("hook payload mismatch: %+v", rig.hookGot[0])
	}
	// 落库行形状:to=法官占位, from=发送者(DryRun 下从返回 msg 断言)。
	if msg.ToUserID != JudgeFromUserID {
		t.Fatalf("persisted row to_user_id = %q, want judge placeholder", msg.ToUserID)
	}
	if !msg.Whisper {
		t.Fatalf("whisper marker missing")
	}
	// 发送者收到 direct 回显(chat.whisper 帧)。
	frames := drainFrames(rig.sender)
	if len(frames) != 1 || frames[0].Type != "chat.whisper" {
		t.Fatalf("sender echo frames = %d, want 1 chat.whisper", len(frames))
	}

	// 对照组:同一观战者 whisper 普通玩家 → spectator 守卫照常拒绝。
	_, err = rig.svc.Whisper(rig.sender, "ww-room-1", "player-9", "", "psst")
	if err == nil || err.Error() != errSpectatorWhisperForbidden.Error() {
		t.Fatalf("whisper to normal player err = %v, want spectator forbidden", err)
	}
}

// TestWhisperToJudge_Rejections — hook 返回 error → 原样透传(上层 sendOpError
// → chat.error 20001);hook 未接线(nil,如旧部署/问答关闭)→ 法官目标也走
// 既有阵营守卫(观战者被拒,向后兼容)。
func TestWhisperToJudge_Rejections(t *testing.T) {
	sentinel := errors.New("judge Q&A cooldown: retry in 12s")
	rig := newJudgeQARig(t, sentinel)

	_, err := rig.svc.Whisper(rig.sender, "ww-room-1", JudgeFromUserID, "", "谁是狼人?")
	if err == nil || err.Error() != sentinel.Error() {
		t.Fatalf("err = %v, want hook sentinel %q", err, sentinel)
	}
	// hook 失败路径不得回显发送者(提问未被接受)。
	if frames := drainFrames(rig.sender); len(frames) != 0 {
		t.Fatalf("rejected whisper must not echo sender, got %d frames", len(frames))
	}

	// hook 未接线:法官目标退回阵营守卫 → 观战者被拒(F-1 的关闭态)。
	rig2 := newJudgeQARig(t, nil)
	rig2.svc.SetJudgeQuestionHook(nil)
	_, err = rig2.svc.Whisper(rig2.sender, "ww-room-1", JudgeFromUserID, "", "?")
	if err == nil || err.Error() != errSpectatorWhisperForbidden.Error() {
		t.Fatalf("unwired hook err = %v, want spectator forbidden", err)
	}
}

// TestWhisperToJudge_PersistAndEcho — hook 成功后:私问行落库路径走到位,
// 且只回显发送者本人 —— 房间内**其他订阅者**(玩家/观战者)一帧都收不到
// (不广播脱敏帧,公平性 F-2:其他客户端感知不到「有人问过法官」)。
func TestWhisperToJudge_PersistAndEcho(t *testing.T) {
	rig := newJudgeQARig(t, nil)
	other := &Client{UserID: "other-2", send: make(chan Envelope, 16)}
	rig.hub.SubscribeRoom("ww-room-1", other)

	_, err := rig.svc.Whisper(rig.sender, "ww-room-1", JudgeFromUserID, "", "警徽流是什么规则?")
	if err != nil {
		t.Fatalf("whisper-to-judge failed: %v", err)
	}
	// emitRoomMessage 不被调用:私问不进 bot 500K 队列 / judge transcript(F-2)。
	if rig.msgHooks != 0 {
		t.Fatalf("emitRoomMessage must NOT fire for whisper-to-judge, got %d calls", rig.msgHooks)
	}
	if frames := drainFrames(other); len(frames) != 0 {
		t.Fatalf("other subscriber must see nothing, got %d frames", len(frames))
	}
	if frames := drainFrames(rig.sender); len(frames) != 1 || frames[0].Type != "chat.whisper" {
		t.Fatalf("sender echo = %d frames, want 1 chat.whisper", len(frames))
	}
}

// TestWhisperFromJudge_PrivateOnly — F-2:私答只投递 asker;
// 无 BroadcastRoom*(房间其他订阅者零帧)、无 emitRoomMessage。
func TestWhisperFromJudge_PrivateOnly(t *testing.T) {
	hub := NewHub()
	svc := NewChatService(newFakeChatDB(t), hub, nil)
	asker := &Client{UserID: "asker-1", send: make(chan Envelope, 16)}
	observer := &Client{UserID: "observer-2", send: make(chan Envelope, 16)}
	hub.SubscribeRoom("ww-room-1", observer)
	hub.SubscribeRoom("ww-room-1", asker)
	hub.Register(asker) // SendToUser 按 h.clients[userID] 索引投递
	msgHooks := 0
	svc.SetRoomMessageHook(func(msg *ChatMessage) { msgHooks++ })

	if err := svc.WhisperFromJudge("ww-room-1", "asker-1", "张三", "GLM-model", "警徽流:预言家警长..."); err != nil {
		t.Fatalf("WhisperFromJudge failed: %v", err)
	}
	if msgHooks != 0 {
		t.Fatalf("emitRoomMessage must NOT fire for judge private replies, got %d calls", msgHooks)
	}
	// asker 收到 1 帧 chat.whisper,from_role="judge"。
	frames := drainFrames(asker)
	if len(frames) != 1 || frames[0].Type != "chat.whisper" {
		t.Fatalf("asker frames = %d, want 1 chat.whisper", len(frames))
	}
	var payload ChatMessage
	if err := json.Unmarshal(frames[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal whisper payload: %v", err)
	}
	if payload.FromRole != "judge" || payload.FromUserID != JudgeFromUserID {
		t.Fatalf("judge whisper from_role=%q from_user_id=%q, want judge", payload.FromRole, payload.FromUserID)
	}
	if payload.ToUserID != "asker-1" || payload.ToAccount != "张三" {
		t.Fatalf("judge whisper to=%q/%q, want asker-1/张三", payload.ToUserID, payload.ToAccount)
	}
	// 房间内其他订阅者零帧 —— 没有任何广播发生。
	if frames := drainFrames(observer); len(frames) != 0 {
		t.Fatalf("observer must see nothing (no broadcast), got %d frames", len(frames))
	}
}

// TestActivityEvent_ScopeField — §遗言聊天 P3 主修复:EmitRoomActivity 的
// payload JSON 必须含 "scope":"room",否则前端 useChat 按 scope 过滤时整帧丢弃。
func TestActivityEvent_ScopeField(t *testing.T) {
	hub := NewHub()
	svc := NewChatService(newFakeChatDB(t), hub, nil)
	c := &Client{UserID: "viewer-1", send: make(chan Envelope, 16)}
	hub.SubscribeRoom("ww-room-2", c)

	if ok := svc.EmitRoomActivity("ww-room-2", ActivityEventKindPhaseTransition,
		"→ 遗言阶段", "death_lyric", 2, "info", "💀", 3, -1, false); !ok {
		t.Fatalf("EmitRoomActivity returned false")
	}
	frames := drainFrames(c)
	if len(frames) != 1 || frames[0].Type != "chat.activity" {
		t.Fatalf("activity frames = %d, want 1 chat.activity", len(frames))
	}
	var raw map[string]any
	if err := json.Unmarshal(frames[0].Payload, &raw); err != nil {
		t.Fatalf("unmarshal activity payload: %v", err)
	}
	if raw["scope"] != "room" {
		t.Fatalf("activity payload scope = %v, want \"room\"", raw["scope"])
	}
	// 结构体字段直检(单测表:json.Marshal 断言 scope)。
	ev := ActivityEvent{RoomID: "r", EventKind: "k", Scope: "room", TS: time.Now().UnixMilli()}
	b, _ := json.Marshal(&ev)
	if !jsonContainsScopeRoom(string(b)) {
		t.Fatalf("marshalled ActivityEvent lacks \"scope\":\"room\": %s", b)
	}
}

func jsonContainsScopeRoom(s string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return false
	}
	return m["scope"] == "room"
}
