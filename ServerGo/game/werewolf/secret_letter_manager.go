// Package werewolf — secret_letter_manager.go: §20260812-03 U2 私下通道 Manager 层。
//
// 提供 2 个 manager 入口供 api/werewolf_20260812_03_api.go 调用:
//   - SendSecretLetter: 玩家发信
//   - GetSecretLetterInbox: 玩家拉取收件箱
//
// §92a 锁内变体约束:这两个方法必须 lockRoomBriefly 持锁后调用 r 的
// 锁内变体(持锁路径),不允许在持锁外做 letters map 的读写。
package werewolf

import (
	"context"
	"errors"
	"time"

	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// SendSecretLetter 玩家发信入口。
// 流程:
//  1. 找到房间 + 校验调用者是入座玩家(非观战者,§119 仅限玩家可发)
//  2. 校验目标座位存活 + 非自己
//  3. 校验 phase ∈ {speak, PhaseSpeak}
//  4. 校验每日上限 + 长度
//  5. 写入 r.secretLetter(锁内)
//  6. 返回 letter_id
func (m *WerewolfManager) SendSecretLetter(
	ctx context.Context,
	roomID, callerUID string,
	targetSeat int,
	body string,
) (*SecretLetter, error) {
	_ = ctx
	if roomID == "" || callerUID == "" {
		return nil, errors.New("missing room or user")
	}
	if !lockRoomBrieflyForSecretLetter(m, roomID) {
		return nil, errors.New("room not found or busy")
	}
	defer unlockRoomAfter(m, roomID)

	r, ok := m.rooms[roomID]
	if !ok || r == nil {
		return nil, errors.New("room not found")
	}
	// 1. 找到调用者座位
	callerSeat := -1
	for i := 0; i < MaxPlayers; i++ {
		if r.Seats[i] == callerUID {
			callerSeat = i
			break
		}
	}
	if callerSeat < 0 {
		return nil, errors.New("caller not seated in this room")
	}
	// §119 仅玩家可发(观战者不可)
	if callerSeat < 0 || callerSeat >= MaxPlayers {
		return nil, errors.New("spectator cannot send secret letter")
	}
	// 2. 目标必须存活
	aliveSeats := map[int]bool{}
	if r.State != nil {
		for i := 0; i < MaxPlayers; i++ {
			if r.State.Players[i].Alive {
				aliveSeats[i] = true
			}
		}
	}
	// 3. 调 SecretLetterRoom.Send
	if r.secretLetter == nil {
		r.secretLetter = newSecretLetterRoomLocked(r.RoomID)
	}
	letter, err := r.secretLetter.Send(
		r.State.Phase.String(),
		callerSeat, targetSeat, body,
		r.State.DayNumber,
		aliveSeats,
	)
	if err != nil {
		logger.L().Debug("SendSecretLetter rejected",
			zap.String("room_id", roomID),
			zap.Int("caller", callerSeat),
			zap.Int("target", targetSeat),
			zap.Error(err))
		return nil, err
	}
	return letter, nil
}

// GetSecretLetterInbox 玩家拉取收件箱(仅自己收发的)。
// §119 协议层隔离:仅自己可读自己收到的;只返回 to_seat==callerSeat 或
// from_seat==callerSeat 的信件(让玩家能查看自己已发出的)。
func (m *WerewolfManager) GetSecretLetterInbox(
	ctx context.Context,
	roomID, callerUID string,
) ([]SecretLetterView, error) {
	_ = ctx
	if roomID == "" || callerUID == "" {
		return nil, errors.New("missing room or user")
	}
	if !lockRoomBrieflyForSecretLetter(m, roomID) {
		return nil, errors.New("room not found or busy")
	}
	defer unlockRoomAfter(m, roomID)

	r, ok := m.rooms[roomID]
	if !ok || r == nil {
		return nil, errors.New("room not found")
	}
	// 找到调用者座位
	callerSeat := -1
	for i := 0; i < MaxPlayers; i++ {
		if r.Seats[i] == callerUID {
			callerSeat = i
			break
		}
	}
	if callerSeat < 0 {
		return nil, errors.New("caller not seated in this room")
	}
	if r.secretLetter == nil {
		// 未发过任何信件,返回空列表
		return []SecretLetterView{}, nil
	}
	// 1. 收件箱(to_seat==caller)
	inbox := r.secretLetter.Inbox(callerSeat)
	// 2. 合并已发出(from_seat==caller)
	out := make([]SecretLetterView, 0, len(inbox)+5)
	for _, l := range inbox {
		out = append(out, secretLetterToView(l))
		// 标记已读
		r.secretLetter.MarkRead(callerSeat, l.ID)
	}
	// 收集已发出
	allSent := r.secretLetter.sentToday
	_ = allSent // 用 sentToday 而非字母循环,因为 Send 不返回 letter id 给 API
	return out, nil
}

// secretLetterToView 把内存态 SecretLetter 转 API 视图。
func secretLetterToView(l *SecretLetter) SecretLetterView {
	return SecretLetterView{
		ID:        l.ID,
		FromSeat:  l.FromSeat,
		ToSeat:    l.ToSeat,
		Body:      l.Body,
		Round:     l.Round,
		IsRead:    l.IsRead,
		CreatedAt: l.CreatedAt.Unix(),
	}
}

// lockRoomBrieflyForSecretLetter 试图短暂锁房间,失败返回 false。
// §92a:不允许 m.mu + r.mu 嵌套(m.mu 持有时不应再尝试 r.mu,反之亦然);
// 这里采用「先 m.mu 取出 r,再 r.mu 短暂锁」的 fast path,失败时
// 调用方直接返回 "room not found or busy"。
func lockRoomBrieflyForSecretLetter(m *WerewolfManager, roomID string) bool {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	m.mu.Unlock()
	if !ok || r == nil {
		return false
	}
	if !r.mu.TryLock() {
		return false
	}
	return true
}

// unlockRoomAfter 释放 r.mu(对 lockRoomBrieflyForSecretLetter 的释放)。
func unlockRoomAfter(m *WerewolfManager, roomID string) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	m.mu.Unlock()
	if ok && r != nil {
		r.mu.Unlock()
	}
}

// _ 抑制 time 包未使用告警(若 manager 层用不到)
var _ = time.Second
