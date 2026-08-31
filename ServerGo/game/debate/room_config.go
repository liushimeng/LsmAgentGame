// Package debate — DebateRoom ctx / spectator / view helper。
//
// 2026-08-31 §20260831-01 — DebateRoom 工具方法:
//   - cancelSetClosed:统一关闭标记 + (未来)cancel 取消房间 ctx
//   - spectator count helpers(供 view + 前端 lobby 列表)
//   - 阵营 / 模型可见性过滤 helpers
//
// 详细设计见 docs/辩论比赛/00-辩论比赛总体架构设计.md §3.1。
package debate

// ============================================================================
// DebateRoom 辅助方法(放独立文件以避免 room.go 超 1800 行)
// ============================================================================

// cancelSetClosed 标记房间为已关闭状态(被 DebateManager.Remove 调用)。
//
// 设计:当前 DebateRoom 自身不持有 context.Context;DebateEngine 持有。
// 这里保留一个统一入口便于未来扩展(例:per-room runtime budget)。
func (r *DebateRoom) cancelSetClosed() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}

// BroadcastCallback 房间对外广播回调函数签名(由 DebateManager 注入)。
//
// payload 通常是 JSON 序列化后的帧 body(map[string]any → JSON)。
// 真正的广播由 DebateManager → ws.Hub 完成,DebateRoom 不直接持有 Hub。
type BroadcastCallback func(roomID, frameType string, payload []byte)

// BroadcastToSpectators 房间对观战者广播(便于后续 view 层扩展)。
//
// 当前实现:DebateRoom 不直接广播,而是返回 frame 让 DebateManager 派发。
// 引擎层通过 DebateRoom 上注册的 BroadcastCallback 调用。
func (r *DebateRoom) NotifyFrame(frameType string, payload []byte) []byte {
	// 由 DebateEngine 在调用广播前调用此方法生成 frame,
	// DebateRoom 当前只做格式校验;未来可在此加签名校验。
	if len(payload) == 0 {
		return nil
	}
	return payload
}

// SeatKey 拼装 (team, seat) → "team:seat" key。
func SeatKey(teamID, seat int) string {
	return fmtInt(teamID) + ":" + fmtInt(seat)
}

// ParseSeatKey 解析 "team:seat" → (team, seat)。
func ParseSeatKey(k string) (int, int, bool) {
	for i := 0; i < len(k); i++ {
		if k[i] == ':' {
			t, ok1 := parseInt(k[:i])
			s, ok2 := parseInt(k[i+1:])
			return t, s, ok1 && ok2
		}
	}
	return 0, 0, false
}

// fmtInt 把 int 转 string(避免 strconv import,在小工具里复用)。
func fmtInt(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// parseInt 解析 string → int(避免 strconv import)。
func parseInt(s string) (int, bool) {
	if len(s) == 0 {
		return 0, false
	}
	neg := false
	i := 0
	if s[0] == '-' {
		neg = true
		i = 1
	} else if s[0] == '+' {
		i = 1
	}
	if i == len(s) {
		return 0, false
	}
	v := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	if neg {
		v = -v
	}
	return v, true
}

// SpeakerName 生成辩手显示名 "<立场><辩位>(<model_short>)"。
//
// 例:"正方一辩(MeiTuan)";model_short 取最后一个 "-" 后的字段(如 MeiTuan-model → MeiTuan)。
func (r *DebateRoom) SpeakerName(teamID, seat int) string {
	for _, t := range r.Config.Teams {
		if t.TeamID != teamID {
			continue
		}
		for _, a := range t.Agents {
			if a.SeatID == seat {
				return StanceLabel(t.Stance) + RoleCN(a.Role) + "(" + ModelShort(a.ModelKey) + ")"
			}
		}
	}
	return ""
}

// ModelShort 把 "Xxx-model" 截短为 "Xxx"。
func ModelShort(modelKey string) string {
	for i := len(modelKey) - 1; i >= 0; i-- {
		if modelKey[i] == '-' {
			return modelKey[:i]
		}
	}
	return modelKey
}

// JudgeName 裁判显示名 "裁判 N(<model_short>)"。
func (r *DebateRoom) JudgeName(idx int) string {
	if idx < 0 || idx >= len(r.Config.Judges) {
		return ""
	}
	return "裁判" + fmtInt(idx+1) + "(" + ModelShort(r.Config.Judges[idx].ModelKey) + ")"
}

// ============================================================================
// 阵营 / 立场 helpers
// ============================================================================

// OpposingStances 返回与传入立场集合对抗的立场集合(用于多队模式分组)。
//
// 两队模式时:Pro ↔ Con,其余立场视为"中立/其他"。
// 多队模式时:基于阵营(camp)分组,本版本仅实现 Pro/Con 二分。
func OpposingStances(stance Stance) []Stance {
	switch stance {
	case StancePro:
		return []Stance{StanceCon}
	case StanceCon:
		return []Stance{StancePro}
	default:
		// 中立 / BP / 多角度 → 无特定对抗方
		return nil
	}
}

// IsStanceIn 判断立场是否在集合内。
func IsStanceIn(s Stance, set []Stance) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

// IsMultiTeamMode 是否多队模式(>= 3 队)。
func IsMultiTeamMode(mode Mode) bool {
	return mode == ModeThreeTeam || mode == ModeFourTeam || mode == ModeFiveTeam
}