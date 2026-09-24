// Package virtual_city — agent_sense.go: City-Human 感知与行动工具桥
// (2026-09-22 §CityHuman重构)。
//
// 契约: lag_docs/虚拟城市/已实现/12-CityHuman重构/虚拟城市-CityHuman-Agent合并与感知系统设计-v1.md §3/§4。
// 实现 vcplayer.ToolRunner 的 See/Hear/Smell/Move/SpeakTo 五方法:
// 感知结果全部确定性构造(不调 LLM)——人 = 同区座位居民 + Backdrop 抽样真实档案;
// 物 = 城区地标 + 本区挂单;事 = 当月本区事件;声/味 = 城区基底表 + 事件叠加。
package virtual_city

import (
	"fmt"
	"time"

	"LsmAgentGame/agent/vctypes"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/city"
)

// speakMonthlyLimit 发言月度限额(2026-09-22 §CityHuman重构:1 → 2,
// speak area+private 合计)。
const speakMonthlyLimit = 2

// sensePeopleCap / senseEventCap / senseThingCap 感知结果条数上限(设计 §3.3:≤8 条邻居)。
const (
	sensePeopleCap = 8
	senseEventCap  = 6
	senseThingCap  = 6
	senseUtterCap  = 5
	senseKeepLast  = 3 // bot_contexts[].last_senses 保留条数
)

// districtLandmarks 城区地标(see 的「物」静态基底;16 区各 2~3 个)。
var districtLandmarks = map[string][]string{
	"finance":            {"环球金融中心", "证券大厦", "银行总部裙楼"},
	"tech":               {"创业咖啡街区", "孵化器大楼", "程序员食堂"},
	"industry":           {"标准厂房群", "货运堆场", "职工宿舍"},
	"oldtown":            {"骑楼老街", "菜市场", "社区棋牌室"},
	"commerce":           {"购物中心", "步行街", "电影院"},
	"residential":        {"社区花园", "菜鸟驿站", "便民超市"},
	"suburb":             {"农家乐", "城乡结合部集市", "自建房村落"},
	"riverside":          {"滨江步道", "观景平台", "水上巴士码头"},
	"logistics_port":     {"集装箱码头", "保税仓库", "货运站"},
	"hightech_park":      {"研发中心", "路演大厅", "人才公寓"},
	"edu_district":       {"大学城", "图书馆", "学生商业街"},
	"medical_city":       {"三甲医院", "康复中心", "药房一条街"},
	"industrial_park":    {"产业园标准厂房", "员工食堂", "通勤班车站"},
	"central_park":       {"中央草坪", "湖心亭", "健身步道"},
	"transport_hub":      {"高铁南站", "长途客运站", "地铁换乘厅"},
	"cultural_creative":  {"文创市集", "Livehouse", "独立书店"},
}

// moodOfPlayer 由座位居民状态映射一词情绪提示(感知结果用)。
func moodOfPlayer(p *Player) string {
	switch {
	case p == nil || !p.Alive:
		return "沉寂"
	case p.UnemployedMonths > 0:
		return "焦虑"
	case p.Energy <= 2:
		return "疲惫"
	case p.Cash < 0:
		return "拮据"
	default:
		return "如常"
	}
}

// moodOfNeighbor 背景居民情绪提示。
func moodOfNeighbor(n city.DistrictNeighbor) string {
	if n.Stressed {
		return "焦虑"
	}
	if !n.Employed {
		return "找活中"
	}
	return "如常"
}

// monthEventsRawLocked 当月本区事件原始记录(锁内;含 Seat=-1 全城事件, cap 6)。
func (a *AgentRunner) monthEventsRawLocked(district string) []EventRecord {
	w := a.room.World
	out := make([]EventRecord, 0, senseEventCap)
	for i := len(w.Events) - 1; i >= 0 && len(out) < senseEventCap; i-- {
		ev := w.Events[i]
		if ev.Month != w.Month {
			if ev.Month < w.Month {
				break // Events 按时间升序
			}
			continue
		}
		if ev.Seat >= 0 {
			ep := w.Players[ev.Seat]
			if ep == nil || ep.District != district {
				continue
			}
		}
		out = append(out, ev)
	}
	// 恢复时间升序。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// monthEventsForDistrictLocked 当月本区事件文本(锁内, cap 6)。
func (a *AgentRunner) monthEventsForDistrictLocked(district string) []string {
	raw := a.monthEventsRawLocked(district)
	out := make([]string, 0, len(raw))
	for _, ev := range raw {
		out = append(out, ev.Text)
	}
	return out
}

// ambianceForLocked 构造单城区当月感官画像(基底表 + 事件叠加;锁内)。
func (a *AgentRunner) ambianceForLocked(district string) vctypes.AmbianceBrief {
	base := DistrictAmbiance(district)
	smells := append([]string(nil), base.Smells...)
	sounds := append([]string(nil), base.Sounds...)
	ovS, ovH := ambianceOverlay(a.monthEventsRawLocked(district))
	smells = append(smells, ovS...)
	sounds = append(sounds, ovH...)
	return vctypes.AmbianceBrief{District: district, Smells: smells, Sounds: sounds}
}

// cityAmbianceLocked 全城 16 区当月感官画像(锁内;CitySnapshotView 填充
// city.Snapshot.Ambiance 用)。
func (r *VirtualCityRoom) cityAmbianceLocked() map[string]city.AmbianceTags {
	if r.World == nil || r.City == nil {
		return nil
	}
	ar := &AgentRunner{room: r}
	out := make(map[string]city.AmbianceTags, DistrictCount)
	for _, d := range DistrictDefs {
		ab := ar.ambianceForLocked(d.ID)
		out[d.ID] = city.AmbianceTags{Smells: ab.Smells, Sounds: ab.Sounds}
	}
	return out
}

// recordSenseLocked 把一条感知摘要写入 bot_contexts[].last_senses(锁内, cap 3)。
func (a *AgentRunner) recordSenseLocked(seat int, kind, text string) {
	t := a.room.Transcripts[seat]
	t.Month = a.room.World.Month
	t.UpdatedAt = time.Now().UnixMilli()
	t.LastSenses = append([]SenseEntry{{Month: a.room.World.Month, Kind: kind, Text: clip(text, 160)}}, t.LastSenses...)
	if len(t.LastSenses) > senseKeepLast {
		t.LastSenses = t.LastSenses[:senseKeepLast]
	}
	a.room.Transcripts[seat] = t
}

// appendUtteranceLocked 追加公开发言记录(锁内, cap 50;hear 数据源)。
func (a *AgentRunner) appendUtteranceLocked(u UtteranceRecord) {
	r := a.room
	r.utterances = append(r.utterances, u)
	if len(r.utterances) > 50 {
		r.utterances = r.utterances[len(r.utterances)-50:]
	}
}

// ── ToolRunner 感知三件套(不耗动作预算,限次由 Agent 侧月度计数控制) ──

// See 视觉(≈500m,同城区):人(座位居民 + 抽样背景居民真实档案)/物(地标+挂牌)/事(本月事件)。
func (a *AgentRunner) See(seat int) (*vctypes.SenseResult, error) {
	a.room.mu.Lock()
	if err := a.senseGateLocked(seat); err != nil {
		a.room.mu.Unlock()
		return nil, err
	}
	w := a.room.World
	p := w.Players[seat]
	district := p.District
	res := &vctypes.SenseResult{District: district}

	// 人:同区座位居民优先。
	for s, pp := range w.Players {
		if len(res.People) >= sensePeopleCap {
			break
		}
		if pp == nil || s == seat || pp.District != district {
			continue
		}
		name := a.room.Nicknames[s]
		if name == "" {
			name = pp.Card.Name
		}
		res.People = append(res.People, vctypes.NeighborBrief{
			Kind: "seat", Seat: s, Name: name, Occupation: pp.Card.Title,
			District: district, MoodHint: moodOfPlayer(pp),
		})
	}
	// 人:背景居民抽样(真实锚定档案优先,§5 人物卡驱动)。
	if a.room.City != nil && len(res.People) < sensePeopleCap {
		for _, n := range a.room.City.SampleDistrictNeighbors(district, sensePeopleCap-len(res.People)) {
			res.People = append(res.People, vctypes.NeighborBrief{
				Kind: "resident", CardID: n.CardID, Name: n.Name, Occupation: n.Occupation,
				District: n.DistrictName, MoodHint: moodOfNeighbor(n),
			})
		}
	}

	// 物:城区地标 + 本区在售挂单。
	res.Things = append(res.Things, districtLandmarks[district]...)
	for _, l := range w.GetListings(ListingAsset) {
		if len(res.Things) >= senseThingCap+len(districtLandmarks[district]) {
			break
		}
		if l == nil || l.Status != "open" || l.Type != ListingAsset || l.Payload.Asset == nil {
			continue
		}
		asset := l.Payload.Asset.Asset
		if asset.AssetDistrict() == district {
			res.Things = append(res.Things,
				fmt.Sprintf("挂牌出售:%s 要价¥%d", assetNameCN(&asset), l.AskCNY))
		}
	}

	// 事:本月本区事件。
	res.Events = a.monthEventsForDistrictLocked(district)

	text := fmt.Sprintf("环顾%s:看见 %d 人(%s 等),%d 处景物/挂牌,%d 起本月事件",
		DistrictCN(district), len(res.People), firstNeighborName(res.People), len(res.Things), len(res.Events))
	a.recordSenseLocked(seat, "see", text)
	hooks := a.room.hooks
	roomID := a.room.RoomID
	a.room.mu.Unlock()
	if hooks.OnState != nil {
		hooks.OnState(roomID)
	}
	return res, nil
}

// Hear 听觉(≈100m,同城区近处):公开发言摘录 + 城市之声 + 环境声 + 事件动静。
func (a *AgentRunner) Hear(seat int) (*vctypes.SenseResult, error) {
	a.room.mu.Lock()
	if err := a.senseGateLocked(seat); err != nil {
		a.room.mu.Unlock()
		return nil, err
	}
	p := a.room.World.Players[seat]
	district := p.District
	res := &vctypes.SenseResult{District: district}

	// 公开发言摘录:同区座位发言 + 全城城市之声(新→旧,cap 5)。
	for i := len(a.room.utterances) - 1; i >= 0 && len(res.Utterances) < senseUtterCap; i-- {
		u := a.room.utterances[i]
		if u.District != "" && u.District != district {
			continue
		}
		res.Utterances = append(res.Utterances, u.Text)
	}
	res.Events = a.monthEventsForDistrictLocked(district)
	ab := a.ambianceForLocked(district)
	res.Sounds = ab.Sounds

	text := fmt.Sprintf("在%s听见 %d 条议论,%d 起动静;环境声:%v",
		DistrictCN(district), len(res.Utterances), len(res.Events), res.Sounds)
	a.recordSenseLocked(seat, "hear", text)
	hooks := a.room.hooks
	roomID := a.room.RoomID
	a.room.mu.Unlock()
	if hooks.OnState != nil {
		hooks.OnState(roomID)
	}
	return res, nil
}

// Smell 嗅觉(≈50m):所在城区气味画像(基底 + 当月事件叠加)。
func (a *AgentRunner) Smell(seat int) (*vctypes.SenseResult, error) {
	a.room.mu.Lock()
	if err := a.senseGateLocked(seat); err != nil {
		a.room.mu.Unlock()
		return nil, err
	}
	p := a.room.World.Players[seat]
	district := p.District
	ab := a.ambianceForLocked(district)
	res := &vctypes.SenseResult{District: district, Smells: ab.Smells, Sounds: ab.Sounds}

	text := fmt.Sprintf("%s的空气里飘着:%v", DistrictCN(district), res.Smells)
	a.recordSenseLocked(seat, "smell", text)
	hooks := a.room.hooks
	roomID := a.room.RoomID
	a.room.mu.Unlock()
	if hooks.OnState != nil {
		hooks.OnState(roomID)
	}
	return res, nil
}

// senseGateLocked 感知工具公共门控(锁内):进行中 + acting + 本月决策槽 + 存活。
func (a *AgentRunner) senseGateLocked(seat int) error {
	if a.room.closed || a.room.Status != StatusPlaying || a.room.Phase != PhaseActing {
		return errcode.Code(errcode.ErrVirtualCityWrongPhase)
	}
	if err := a.checkAgentDecisionLocked(seat); err != nil {
		return err
	}
	p := a.room.World.Players[seat]
	if p == nil || !p.Alive {
		return errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	return nil
}

// firstNeighborName 取首个邻居名(摘要渲染用)。
func firstNeighborName(people []vctypes.NeighborBrief) string {
	if len(people) == 0 {
		return "附近无人"
	}
	return people[0].Name
}

// ── ToolRunner 行动两件套 ──

// Move 统一移动(耗 1 次动作预算):walk/run 区内移动;bus/metro/taxi 跨城区。
// 结算走 ApplyAction(ActMove)单一代码路径,与人类 WS 动作无分叉。
func (a *AgentRunner) Move(seat int, destination string, mode string) error {
	return a.apply(seat, "move", "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActMove, District: destination, Mode: mode})
	})
}

// SpeakTo 私聊指定座位(每月与 area 合计 ≤2;仅目标与观战者可见,
// 走 ChatService 耳语通道,参考狼人杀 WhisperFromBot 路径)。
func (a *AgentRunner) SpeakTo(seat int, targetSeat int, text string) error {
	a.room.mu.Lock()
	if a.room.closed || a.room.Status != StatusPlaying || a.room.Phase != PhaseActing {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrVirtualCityWrongPhase)
	}
	if err := a.checkAgentDecisionLocked(seat); err != nil {
		a.room.mu.Unlock()
		return err
	}
	p := a.room.World.Players[seat]
	if p == nil || !p.Alive || p.SpeakCountThisMonth >= speakMonthlyLimit {
		a.room.mu.Unlock()
		return errcode.CodeMsg(errcode.ErrVirtualCitySenseLimit, "speak monthly limit reached (2 per month)")
	}
	if targetSeat == seat || targetSeat < 0 || targetSeat >= MaxSeats {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrVirtualCityWhisperTarget)
	}
	tp := a.room.World.Players[targetSeat]
	toUserID := a.room.Seats[targetSeat]
	if tp == nil || !tp.Alive || toUserID == "" {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrVirtualCityWhisperTarget)
	}
	wsp, ok := a.room.chatSender.(ChatWhisperer)
	if !ok || wsp == nil {
		a.room.mu.Unlock()
		return errcode.CodeMsg(errcode.ErrVirtualCityWhisperTarget, "whisper channel unavailable")
	}
	p.SpeakCountThisMonth++
	roomID := a.room.RoomID
	userID := a.room.Seats[seat]
	modelKey := a.room.SeatModelKeys[seat]
	toAccount := a.room.Nicknames[targetSeat]
	a.room.mu.Unlock()

	return wsp.WhisperFromBot(roomID, userID, modelKey, modelKey, toUserID, toAccount, clip(text, 100))
}
