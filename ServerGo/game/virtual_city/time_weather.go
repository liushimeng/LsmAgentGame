// Package virtual_city — time_weather.go: 批次 27「时间比例与昼夜季节天气」
// 服务端确定性纯函数(方案 27 §3.1-3.3)。
//
//	SeasonAt        城市日历月 → 季节(§3.2 北半球温带映射)
//	WeatherAt       (seed ^ weatherSeedSalt, CityDayIdx) → 天气类型 + 强度
//	MonthMsForRatio time_ratio → 经济月节拍(§3.1 推导公式,后端权威)
//	TimeEnv         BuildClientState 时间环境参数(ws 广播路径使用)
//
// 无新增定时器/状态:每次 game.state 构造时按当前 city_clock_ms 现算,
// 同 seed 同城市日恒等(可复现、可测试);季节/天气不进存档(重启重建,
// 与 pendingOpts 现状一致,方案 §9 不做清单)。
package virtual_city

import (
	"math"
	"time"
)

// weatherSeedSalt 天气 rng 派生盐:取 ASCII "WX2027" 的 int64 编码
// (沿用 room_city.go citySeedSalt / cityProfileSeedSalt 派生盐模式,
// 与两者无碰撞即可)。
const weatherSeedSalt int64 = 0x575832303237

// cityEpochBaseMsCached cityEpochBaseMillis() 的包级缓存(CityDayIdx 高频
// 调用,避免每次 time.Date 构造;值恒为 1735689600000)。
var cityEpochBaseMsCached = cityEpochBaseMillis()

// hashU64 splitmix64 finalizer(golden ratio 常数;确定性纯函数,不依赖
// math/rand,重放/跨进程恒等)。
func hashU64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// hashUnit01 把 64 位哈希折叠为 [0,1) 均匀量(取高 53 位保浮点精度)。
func hashUnit01(x uint64) float64 {
	return float64(hashU64(x)>>11) / (1 << 53)
}

// CityDayIdx 城市日序号:floor((cityClockMs − 纪元)/86_400_000)。
// 纪元 2025-01-01 08:00 +0800(批次 25)⇒ 开局当日为第 0 日。
// 整数除法向零取整;开局前(cityClockMs < 纪元)为负值,同样确定性。
func CityDayIdx(cityClockMs int64) int64 {
	return (cityClockMs - cityEpochBaseMsCached) / 86_400_000
}

// SeasonAt 城市日历月 → 季节(方案 §3.2;时区沿用 cityEpochTZ 固定东八区):
// 3/4/5 月 spring、6/7/8 summer、9/10/11 autumn、12/1/2 winter。
// 纪元 2025-01-01 ⇒ 默认档开局即冬季。
func SeasonAt(cityClockMs int64) string {
	switch time.UnixMilli(cityClockMs).In(cityEpochTZ).Month() {
	case time.March, time.April, time.May:
		return "spring"
	case time.June, time.July, time.August:
		return "summer"
	case time.September, time.October, time.November:
		return "autumn"
	default:
		return "winter"
	}
}

// weatherKinds 权重表列序(与 weatherWeights 行对齐;协议 8 类型 §3.3)。
var weatherKinds = []string{"clear", "cloudy", "fog", "drizzle", "rain", "storm", "snow", "blizzard"}

// weatherWeights 季节 × 类型权重表(温带;方案 §3.3 逐值照抄,每季合计 = 1;
// 行序与 weatherKinds 对齐)。改表须同步 city/time_weather.go 显示侧副本。
var weatherWeights = map[string][8]float64{
	"spring": {0.38, 0.24, 0.06, 0.12, 0.12, 0.04, 0.03, 0.01},
	"summer": {0.40, 0.20, 0.04, 0.08, 0.14, 0.12, 0.01, 0.01},
	"autumn": {0.45, 0.26, 0.09, 0.08, 0.09, 0.02, 0.005, 0.005},
	"winter": {0.30, 0.22, 0.10, 0.04, 0.06, 0.02, 0.20, 0.06},
}

// WeatherAt 天气(确定性纯函数)= f(seed ^ weatherSeedSalt, CityDayIdx):
// splitmix64 派生双均匀量 u1/u2 —— u1 按当季权重表选类型,u2 定强度。
// 强度口径(§3.3):clear=0、cloudy=0.35、其余 0.25+0.75×u2(四舍五入
// 两位小数,∈ [0.25, 1.00])。
func WeatherAt(seed int64, cityClockMs int64) (kind string, intensity float64) {
	base := uint64(seed ^ weatherSeedSalt)
	day := uint64(CityDayIdx(cityClockMs))
	u1 := hashUnit01(base ^ (day+1)*0x9E3779B97F4A7C15)
	u2 := hashUnit01(base ^ (day+2)*0xBF58476D1CE4E5B9)
	weights := weatherWeights[SeasonAt(cityClockMs)]
	// 权重兜底:行缺失(理论不可达)时退 blizzard,保证恒有类型。
	kind = weatherKinds[len(weatherKinds)-1]
	acc := 0.0
	for i, w := range weights {
		acc += w
		if u1 < acc {
			kind = weatherKinds[i]
			break
		}
	}
	switch kind {
	case "clear":
		return kind, 0
	case "cloudy":
		return kind, 0.35
	default:
		return kind, math.Round((0.25+0.75*u2)*100) / 100
	}
}

// weatherKindAt 天气类型单值便捷封装(Agent 感知/播报文案用)。
func weatherKindAt(seed, cityClockMs int64) string {
	k, _ := WeatherAt(seed, cityClockMs)
	return k
}

// MonthMsForRatio 时间比例 → 经济月节拍毫秒(方案 §3.1 推导公式,后端权威):
//
//	month_ms = clamp(round(2_592_000_000 / time_ratio), 3000, 60000)   // 30 天城市月换算
//
// 13 档预设:60~43200 → 60000(慢于 1分钟比1个月 的档位经济月封顶 60s,
// 居民 Agent 令牌桶与月窗对齐);86400/172800/345600/525600 → 30000/15000/
// 7500/4932(快档经济月 = 城市日历月,精确同步)。除法四舍五入 —— 仅档 13
// 非整除(4931.5 → 4932,与方案 §3.1 预设表一致),其余档位整除不受影响。
// ratio ≤ 0 防御按 60。
func MonthMsForRatio(ratio int) int {
	if ratio <= 0 {
		ratio = 60
	}
	r64 := int64(ratio)
	ms := (2_592_000_000 + r64/2) / r64
	if ms < 3000 {
		ms = 3000
	}
	if ms > 60000 {
		ms = 60000
	}
	return int(ms)
}

// TimeEnv 批次 27:BuildClientState 的时间环境参数(seed 是房间私有小写
// 字段,ws 层经 VirtualCityRoom.TimeEnv() 锁内快照透出)。
type TimeEnv struct {
	Ratio int   // 城市秒/现实秒(clamp [60,864000];缺省 60)
	Seed  int64 // 房间 seed(天气派生)
}

// weatherLabelZH / seasonLabelZH 中文标签(月结 city_weather 播报与 Agent
// 感知上下文共用;方案 §3.3 八类型 + §3.2 四季)。
var weatherLabelZH = map[string]string{
	"clear":    "晴",
	"cloudy":   "多云",
	"fog":      "雾",
	"drizzle":  "小雨",
	"rain":     "雨",
	"storm":    "暴雨雷暴",
	"snow":     "雪",
	"blizzard": "暴雪",
}

var seasonLabelZH = map[string]string{
	"spring": "春季",
	"summer": "夏季",
	"autumn": "秋季",
	"winter": "冬季",
}
