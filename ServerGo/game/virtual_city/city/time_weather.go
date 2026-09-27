// Package city — time_weather.go: 驱动层 contextText 的城市时间/季节/天气
// 显示推导(批次 27 §3.3-3.4)。
//
// ⚠️ 本文件是 game/virtual_city/time_weather.go(协议唯一事实来源)的
// **显示侧副本**:city 是 virtual_city 的子包,不能反向 import(循环依赖),
// 而驱动层 contextText 需要在 city 包内把 (cityClockMs, seed) 渲染成
// 「2006-01-02 15:04 · 冬季 · 小雨」。两份实现的口径一致性由
// game/virtual_city/time_weather_test.go 的跨包断言钉死(同 (seed, ms)
// 必须产出相同季节/天气)—— 改权重表/盐/纪元请两处同步改。
package city

import (
	"math"
	"time"
)

// cityEpochTZ / cityEpochBaseMsCached 与 room.go cityEpochTZ /
// cityEpochBaseMillis 同值(2025-01-01 08:00 +0800,批次 25 城市时钟纪元)。
var cityEpochTZ = time.FixedZone("Asia/Shanghai", 8*3600)

var cityEpochBaseMsCached = time.Date(2025, 1, 1, 8, 0, 0, 0, cityEpochTZ).UnixMilli()

// weatherSeedSalt 与 virtual_city.weatherSeedSalt 同值(ASCII "WX2027")。
const weatherSeedSalt int64 = 0x575832303237

// hashU64 splitmix64 finalizer(与 virtual_city.hashU64 逐字节一致)。
func hashU64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

func hashUnit01(x uint64) float64 {
	return float64(hashU64(x)>>11) / (1 << 53)
}

// cityDayIdx 城市日序号(与 virtual_city.CityDayIdx 同口径)。
func cityDayIdx(cityClockMs int64) int64 {
	return (cityClockMs - cityEpochBaseMsCached) / 86_400_000
}

// SeasonAt 城市日历月 → 季节(与 virtual_city.SeasonAt 同口径;导出供
// time_weather_test.go 跨包一致性断言)。
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

// weatherKinds / weatherWeights 与 virtual_city 同名表逐值一致(§3.3)。
var weatherKinds = []string{"clear", "cloudy", "fog", "drizzle", "rain", "storm", "snow", "blizzard"}

var weatherWeights = map[string][8]float64{
	"spring": {0.38, 0.24, 0.06, 0.12, 0.12, 0.04, 0.03, 0.01},
	"summer": {0.40, 0.20, 0.04, 0.08, 0.14, 0.12, 0.01, 0.01},
	"autumn": {0.45, 0.26, 0.09, 0.08, 0.09, 0.02, 0.005, 0.005},
	"winter": {0.30, 0.22, 0.10, 0.04, 0.06, 0.02, 0.20, 0.06},
}

// WeatherAt 天气(与 virtual_city.WeatherAt 同口径;导出供跨包一致性断言)。
func WeatherAt(seed int64, cityClockMs int64) (kind string, intensity float64) {
	base := uint64(seed ^ weatherSeedSalt)
	day := uint64(cityDayIdx(cityClockMs))
	u1 := hashUnit01(base ^ (day+1)*0x9E3779B97F4A7C15)
	u2 := hashUnit01(base ^ (day+2)*0xBF58476D1CE4E5B9)
	weights := weatherWeights[SeasonAt(cityClockMs)]
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

// weatherLabelZH / seasonLabelZH 中文标签(与 virtual_city 同名表一致)。
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
