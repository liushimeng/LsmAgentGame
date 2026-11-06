// Package citychrome — market_hours.go: 交易时段感知(批次88 · 交易时段感知
// 的采集分级调度)。
//
// 契约出处:《虚拟城市模拟系统·自迭代优化规范》
//   - §三.2「刷新分级:行情分钟级、新闻小时级、宏观日级」;
//   - §五.6「交易时段高频抓取行情,新闻与突发消息走分钟级轮询,宏观数据
//     日/周级更新,非交易时段自动降频」。
//
// 纪律(参照 game/virtual_city/world_event.go 文件头):
//   - **纯函数**:零 IO、零 RNG、零全局态;时刻一律作入参(now time.Time);
//   - 判定一律换算 **UTC** 进行;时间区间语义统一为**左闭右开 [start, end)**;
//   - 同输入同输出,可全量单测、可复现。
//
// 已知限制:**不做节假日日历**(工作日近似)——各市场节假日一律按普通交易日
// 处理,仅周末与固定休市窗口生效;如需节假日感知须接入独立日历数据源(另行批次)。
package citychrome

import "time"

// ── 交易时段表(分钟粒度;区间 [startMin,endMin) 左闭右开) ──

// dayWindow 是市场本地(或 UTC)一个交易日内的交易窗口 [startMin,endMin)
// (当日分钟数 0..1440;endMin=1440 表示到当日 24:00)。
type dayWindow struct{ startMin, endMin int }

// weekSessions 是一周交易时段表:星期缺省键 = 全日休市。
type weekSessions map[time.Weekday][]dayWindow

// weekdays 帮助函数:把 [from,to] 星期区间的每一天都填入同一窗口表。
func weekdays(from, to time.Weekday, wins ...dayWindow) weekSessions {
	s := weekSessions{}
	for d := from; ; d = (d + 1) % 7 {
		s[d] = wins
		if d == to {
			break
		}
	}
	return s
}

// mergeSessions 合并多张星期表(后者覆盖前者的同键日)。
func mergeSessions(tables ...weekSessions) weekSessions {
	out := weekSessions{}
	for _, t := range tables {
		for d, w := range t {
			out[d] = w
		}
	}
	return out
}

// 时段表(市场本地时间口径;metal/futures 直接用 UTC):
//   - a_share(上交所/深交所,UTC+8):周一~五 09:30-11:30 / 13:00-15:00;
//   - hk(港交所,UTC+8):周一~五 09:30-12:00 / 13:00-16:00;
//   - us(纽交所/纳斯达克,美东):周一~五 09:30-16:00(EDT=UTC-4 / EST=UTC-5);
//   - metal/futures(外盘 CME Globex 近 24 小时口径,UTC):周日 22:00 起开市
//     至周五 21:00 收盘(周六全天休市、周日 22:00 前休市);两值同一时段表;
//   - crypto:7×24 恒开市(无时段表)。
var (
	aShareSessions = weekdays(time.Monday, time.Friday,
		dayWindow{9*60 + 30, 11*60 + 30}, // 09:30-11:30
		dayWindow{13 * 60, 15 * 60})      // 13:00-15:00
	hkSessions = weekdays(time.Monday, time.Friday,
		dayWindow{9*60 + 30, 12 * 60}, // 09:30-12:00
		dayWindow{13 * 60, 16 * 60})   // 13:00-16:00
	usSessions = weekdays(time.Monday, time.Friday,
		dayWindow{9*60 + 30, 16 * 60}) // 09:30-16:00
	globexSessions = mergeSessions(
		weekdays(time.Sunday, time.Sunday, dayWindow{22 * 60, 24 * 60}), // 周日 22:00 起
		weekdays(time.Monday, time.Thursday, dayWindow{0, 24 * 60}),     // 全日
		weekdays(time.Friday, time.Friday, dayWindow{0, 21 * 60}))       // 至周五 21:00
)

// sessionsOpen 报告 (wd,minute) 是否落在窗口表内(左闭右开;缺省日 = 休市)。
func sessionsOpen(s weekSessions, wd time.Weekday, minute int) bool {
	for _, w := range s[wd] {
		if minute >= w.startMin && minute < w.endMin {
			return true
		}
	}
	return false
}

// fixedOffsetOpen 把 now 换算为固定偏移(分钟,东正西负)的市场本地时刻后
// 查窗口表。
func fixedOffsetOpen(now time.Time, offsetMin int, s weekSessions) bool {
	l := now.UTC().Add(time.Duration(offsetMin) * time.Minute)
	return sessionsOpen(s, l.Weekday(), l.Hour()*60+l.Minute())
}

// usEasternDaylight 报告 now 时刻美东是否处于夏令时(EDT,UTC-4)。
// 美国夏令时规则:每年 3 月第二个周日 02:00 EST(=07:00 UTC)起 ~ 11 月第一个
// 周日 02:00 EDT(=06:00 UTC)止;边界按左闭右开 [start,end) 判定。
// 纯函数:第 N 个星期日按 UTC 日历推算(切换瞬间本地 02:00/03:00,UTC 日期与
// 本地日期同日,推算无歧义)。
func usEasternDaylight(now time.Time) bool {
	u := now.UTC()
	y := u.Year()
	start := nthWeekdayUTC(y, time.March, time.Sunday, 2).Add(7 * time.Hour)  // 02:00 EST
	end := nthWeekdayUTC(y, time.November, time.Sunday, 1).Add(6 * time.Hour) // 02:00 EDT
	return !u.Before(start) && u.Before(end)
}

// nthWeekdayUTC 返回 UTC 日历中某月第 n 个星期 wd 的 00:00 UTC 时刻(n≥1)。
func nthWeekdayUTC(year int, month time.Month, wd time.Weekday, n int) time.Time {
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	gap := (int(wd) - int(first.Weekday()) + 7) % 7
	return time.Date(year, month, 1+gap+(n-1)*7, 0, 0, 0, 0, time.UTC)
}

// CategoryMarketOpen 报告类目对应市场在 now 时刻是否开市(纯函数)。
// news/macro 不参与时段判定(恒 true = 不受时段约束);未知类目同此处理。
func CategoryMarketOpen(cat string, now time.Time) bool {
	switch cat {
	case CategoryAShare:
		return fixedOffsetOpen(now, 8*60, aShareSessions) // UTC+8
	case CategoryHK:
		return fixedOffsetOpen(now, 8*60, hkSessions) // UTC+8
	case CategoryUS:
		off := -5 * 60 // EST = UTC-5
		if usEasternDaylight(now) {
			off = -4 * 60 // EDT = UTC-4
		}
		return fixedOffsetOpen(now, off, usSessions)
	case CategoryMetal, CategoryFutures:
		u := now.UTC() // CME Globex 口径直接用 UTC
		return sessionsOpen(globexSessions, u.Weekday(), u.Hour()*60+u.Minute())
	case CategoryCrypto:
		return true // 7×24 恒开市
	}
	return true // news/macro/未知类目:不受时段约束
}

// QuoteRefreshSeconds 返回类目在 now 时刻的刷新间隔档位(秒,纯函数):
// 行情 6 类目开市 defaultQuoteOpenRefreshSeconds(600,分钟级)/休市
// defaultQuoteClosedRefreshSeconds(7200,降频 12×);news/macro 固定
// 600/86400(不受时段约束)。档位常量的唯一事实来源在 scheduler.go 常量块。
func QuoteRefreshSeconds(cat string, now time.Time) int64 {
	switch cat {
	case CategoryNews:
		return defaultNewsRefreshSeconds
	case CategoryMacro:
		return defaultMacroRefreshSeconds
	}
	if CategoryMarketOpen(cat, now) {
		return defaultQuoteOpenRefreshSeconds
	}
	return defaultQuoteClosedRefreshSeconds
}
