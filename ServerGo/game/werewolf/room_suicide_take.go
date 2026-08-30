// room_suicide_take.go — §20260830-02 自爆带走(房间/管理层)
//
// 设计文档:docs/狼人杀-角色设计/狼人杀自爆遗言与带走设计-20260830-02.md
//
// 人类(WS 帧 game.werewolf_suicide_take)与 Agent(工具 wolf_suicide_take)
// 同源进入 Action_SuicideTake → suicideTakeLocked(公平性不变式 3)。
// watchdog / quarantine 兜底也走 suicideTakeLocked(NoSeat = 放弃带走)。
package werewolf

import (
	"fmt"
	"time"

	"LsmAgentGame/errcode"
)

// Action_SuicideTake 自爆狼提交带走选择(公开入口,自身持 r.mu)。
// target=-1(NoSeat)表示放弃带走。
func (m *WerewolfManager) Action_SuicideTake(roomID, userID string, target Seat) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := m.suicideTakeLocked(r, userID, target); e != nil {
		return nil, e
	}
	return r, nil
}

// suicideTakeLocked 是 wolf_suicide_take 的 lock-held 派发。调用方必须持有 r.mu。
func (m *WerewolfManager) suicideTakeLocked(r *WerewolfRoom, userID string, target Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.SuicideTake(seat, target); e != nil {
		return e
	}
	actor := int(seat)
	// §20260830-02 — 信息账本:自爆带走(实际带走才公开;与 hunter_shot 同级)。
	if target != NoSeat {
		r.ledgerAppendLocked(InfoSourceSuicideTake,
			fmt.Sprintf("suicide_take actor=%d target=%d", actor, int(target)),
			aliveKnowerSetLocked(r), time.Now().UnixMilli())
	}
	// §115 房间聊天 — 自爆带走活动事件广播(含放弃)。
	m.EmitSuicideTake(r, actor, int(target))
	// 战报触发器:自爆狼带走玩家。
	if target != NoSeat && r.State != nil {
		r.appendBattleReportTriggerLocked(HighlightKindSuicideTake,
			int(target), r.State.DayNumber,
			fmt.Sprintf("自爆狼 %d 号 带走 %d 号", seat+1, int(target)+1))
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}
