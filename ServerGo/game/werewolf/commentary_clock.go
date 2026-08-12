package werewolf

import "time"

// commentary_clock.go — 把 time.Now() 抽成可注入的 var,便于单元测试
// 构造 fixed 时刻而不引入 testbed 复杂 mock。

var timeNowFunc = func() time.Time { return time.Now() }