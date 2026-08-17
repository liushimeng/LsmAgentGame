// Package werewolf — commentary_enabled_view_test.go: §20260817-03 U1 回归测试。
//
// 背景:观战底栏 SpectatorCompactBar 在房间未开启 AI 解说时仍渲染整块空态占位,
// 浪费游戏界面垂直空间。修复:view.go 给 ClientGameState 新增 CommentaryEnabled
// (不带 omitempty,false 也显式下发),spectator 分支(viewer<0)从
// r.commentaryDesired 赋真实值;玩家视图恒 false(解说仅观众可见,§119)。
//
// 覆盖:
//
//	CE-01  commentaryDesired=true  → spectator 视图 CommentaryEnabled=true
//	CE-02  commentaryDesired=false → spectator 视图 CommentaryEnabled=false
//	CE-03  玩家视图(viewer>=0)恒 false,与 commentaryDesired 无关
//	CE-04  JSON wire 形状:false 显式出现(无 omitempty),true 同
package werewolf

import (
	"encoding/json"
	"strings"
	"testing"
)

func newCommentaryViewRoom(desired bool) *WerewolfRoom {
	r := &WerewolfRoom{RoomID: "commentary-view"}
	r.State = NewGame(20260817)
	r.State.SeatCount = MaxPlayers
	r.commentaryDesired = desired
	return r
}

func TestCommentaryEnabled_CE01_SpectatorSeesTrue(t *testing.T) {
	r := newCommentaryViewRoom(true)
	cs := BuildClientStateWithRoom(r.RoomID, r, -1)
	if cs == nil {
		t.Fatal("spectator 视图不应为 nil")
	}
	if !cs.CommentaryEnabled {
		t.Fatal("commentaryDesired=true 时 spectator 视图 CommentaryEnabled 应为 true")
	}
}

func TestCommentaryEnabled_CE02_SpectatorSeesFalse(t *testing.T) {
	r := newCommentaryViewRoom(false)
	cs := BuildClientStateWithRoom(r.RoomID, r, -1)
	if cs == nil {
		t.Fatal("spectator 视图不应为 nil")
	}
	if cs.CommentaryEnabled {
		t.Fatal("commentaryDesired=false 时 spectator 视图 CommentaryEnabled 应为 false")
	}
}

func TestCommentaryEnabled_CE03_PlayerViewAlwaysFalse(t *testing.T) {
	for _, desired := range []bool{true, false} {
		r := newCommentaryViewRoom(desired)
		cs := BuildClientStateWithRoom(r.RoomID, r, 3)
		if cs == nil {
			t.Fatalf("desired=%v: 玩家视图不应为 nil", desired)
		}
		if cs.CommentaryEnabled {
			t.Fatalf("desired=%v: 玩家视图 CommentaryEnabled 必须恒 false(解说仅观众可见,§119)", desired)
		}
	}
}

func TestCommentaryEnabled_CE04_JSONWireExplicitFalse(t *testing.T) {
	// 不带 omitempty:即便 false 也必须显式下发,否则前端无法区分
	// 「未开启」与「老服务端未下发」。
	r := newCommentaryViewRoom(false)
	cs := BuildClientStateWithRoom(r.RoomID, r, -1)
	b, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"commentary_enabled":false`) {
		t.Fatalf("JSON 必须显式包含 \"commentary_enabled\":false,实际: %.200s…", s)
	}
	// 反向:开启时为 true。
	r2 := newCommentaryViewRoom(true)
	cs2 := BuildClientStateWithRoom(r2.RoomID, r2, -1)
	b2, _ := json.Marshal(cs2)
	if !strings.Contains(string(b2), `"commentary_enabled":true`) {
		t.Fatal("commentaryDesired=true 时 JSON 应包含 \"commentary_enabled\":true")
	}
}
