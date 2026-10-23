// Package virtual_city — view_world_event.go: 批次80 世界事件流下发
// (game.state.world_events 前端契约)。
//
// 契约出处:lag_docs/虚拟城市/已实现/80-真实世界事件自动注入引擎/01-方案设计.md
// §3.6(前端契约)。
//
// 独立成文件的原因:view.go 已 1642 行(硬上限 1800),新增逻辑另置。
//
// 零 rand;纯函数(持锁由调用方管理)。
package virtual_city

// WorldEventsJSON 是 game.state.world_events 的载荷(前端契约 §3.6)。
//
// ⚠ items 恒为数组(空 = [])—— Go nil slice → JSON null → 前端 map 崩溃
// (与批次 70「空数组 vs null」纪律一致)。
type WorldEventsJSON struct {
	// AutoEnabled 自动注入总开关(false ⇒ 前端显示「自动注入已关闭」)。
	AutoEnabled bool `json:"auto_enabled"`
	// UpdatedAt 最近一次事件派生时刻(unix 秒;0 = 从未派生)。
	UpdatedAt int64 `json:"updated_at"`
	// InjectedThisMonth 本月已自动注入条数。
	InjectedThisMonth int `json:"injected_this_month"`
	// Items 事件记录(新→旧,≤20 条;后端环 50 取前 20)。
	Items []WorldEventRecord `json:"items"`
}

// worldEventViewCap 下发条数上限(契约 §3.6:≤20)。
const worldEventViewCap = 20

// BuildWorldEventsJSON 构造 game.state.world_events 快照。
//
// 调用方持房间锁(view 内部不加锁);world==nil ⇒ 返回 nil(未开局不下发)。
func BuildWorldEventsJSON(w *World) *WorldEventsJSON {
	if w == nil {
		return nil
	}
	out := &WorldEventsJSON{
		AutoEnabled: w.WorldEventAutoEnabled,
		// 本月自动注入条数:口径 = 刚结束的那次月结(进入当前月)里触发的注入数
		// (注入记录 InjectedMonth = 触发时的主钟月份)。
		InjectedThisMonth: w.worldEventInjectedEffectiveThisMonth(),
		Items:             make([]WorldEventRecord, 0, worldEventViewCap),
	}
	// 更新时刻 = 最近一条被观测事件的时间(无事件 ⇒ 0)。
	if len(w.WorldEventLog) > 0 {
		out.UpdatedAt = w.WorldEventLog[0].ObservedAt
	}
	// 日志环已是「新→旧」(worldEventLogAdd 头插),直接取前 20 条。
	for i := range w.WorldEventLog {
		if i >= worldEventViewCap {
			break
		}
		r := w.WorldEventLog[i]
		if r.Effects == nil {
			r.Effects = make([]ScenarioEffect, 0)
		}
		out.Items = append(out.Items, r)
	}
	return out
}
