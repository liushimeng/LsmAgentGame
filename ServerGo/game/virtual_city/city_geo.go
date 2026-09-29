// Package virtual_city — city_geo.go: 批次 33「真实城市时间系统」
// 世界前 20 大城市静态表 + 逐月日出日落插值(方案 33 §2.1)。
//
//	城市表     数据文档逐值照抄(00-数据来源-世界前20大城市经纬度与逐月日出日落.md)
//	时区      手工标准时区表(TZMin)——不走经度推算(如 Delhi +330 而 Karachi +300,
//	          经度线性推会撞车;与数据文档的当地口径保持一致)
//	SunTimesAt 城市当日日出/日落(当地分钟):月内线性插值,确定性纯函数
//	resolveCityGeo  city_key → *CityGeo;"" = 默认城市(nil), "random" = 按
//	          房间 seed 确定性抽取(同 seed 复现同一座城)
package virtual_city

import (
	"fmt"
	"time"
)

// cityGeoSeedSalt random 城市抽取的 rng 派生盐(ASCII "CITYGEO33";与
// weatherSeedSalt / citySeedSalt 无碰撞即可)。
const cityGeoSeedSalt int64 = 0x4349545947454F33

// CityGeo 一座真实城市的静态档案(20 城表行;方案 33 §2.1)。
type CityGeo struct {
	Key       string // tokyo / delhi / ... / new_york(建房 city_key)
	NameZH    string // 中文名(主显示)
	NameEN    string
	NameJA    string
	CountryZH string
	CountryEN string
	CountryJA string
	Lat       float64
	Lng       float64
	// TZMin 标准时区偏移(分钟,东正西负;与日出日落表的当地口径一致)。
	TZMin int
	// SunriseMin / SunsetMin 逐月日出/日落(当地 0 点起分钟数;
	// 数据文档逐值照抄;月内按 (day-1)/daysInMonth 线性插值)。
	SunriseMin [12]int
	SunsetMin  [12]int
}

// hhmm "HH:MM" → 当地分钟(数据表录入用;非法值返回 0,录入期可发现)。
func hhmm(s string) int {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0
	}
	return h*60 + m
}

// cityGeoTable 世界前 20 大城市(数据文档排名序;方案 33 §2.1 唯一事实来源)。
var cityGeoTable = []CityGeo{
	{
		Key: "tokyo", NameZH: "东京", NameEN: "Tokyo", NameJA: "東京",
		CountryZH: "日本", CountryEN: "Japan", CountryJA: "日本",
		Lat: 35.6762, Lng: 139.6503, TZMin: 540,
		SunriseMin: [12]int{hhmm("06:50"), hhmm("06:24"), hhmm("05:45"), hhmm("05:07"), hhmm("04:39"), hhmm("04:25"), hhmm("04:36"), hhmm("05:00"), hhmm("05:28"), hhmm("05:56"), hhmm("06:24"), hhmm("06:44")},
		SunsetMin:  [12]int{hhmm("16:46"), hhmm("17:21"), hhmm("17:54"), hhmm("18:27"), hhmm("18:55"), hhmm("19:18"), hhmm("19:14"), hhmm("18:48"), hhmm("18:07"), hhmm("17:26"), hhmm("16:53"), hhmm("16:40")},
	},
	{
		Key: "delhi", NameZH: "德里", NameEN: "Delhi", NameJA: "デリー",
		CountryZH: "印度", CountryEN: "India", CountryJA: "インド",
		Lat: 28.6139, Lng: 77.2090, TZMin: 330,
		SunriseMin: [12]int{hhmm("07:09"), hhmm("06:51"), hhmm("06:20"), hhmm("05:48"), hhmm("05:24"), hhmm("05:18"), hhmm("05:32"), hhmm("05:53"), hhmm("06:11"), hhmm("06:31"), hhmm("06:52"), hhmm("07:07")},
		SunsetMin:  [12]int{hhmm("17:37"), hhmm("18:04"), hhmm("18:28"), hhmm("18:52"), hhmm("19:14"), hhmm("19:32"), hhmm("19:30"), hhmm("19:08"), hhmm("18:34"), hhmm("18:00"), hhmm("17:38"), hhmm("17:31")},
	},
	{
		Key: "shanghai", NameZH: "上海", NameEN: "Shanghai", NameJA: "上海",
		CountryZH: "中国", CountryEN: "China", CountryJA: "中国",
		Lat: 31.2304, Lng: 121.4737, TZMin: 480,
		SunriseMin: [12]int{hhmm("06:52"), hhmm("06:30"), hhmm("05:52"), hhmm("05:14"), hhmm("04:49"), hhmm("04:45"), hhmm("05:01"), hhmm("05:24"), hhmm("05:48"), hhmm("06:12"), hhmm("06:39"), hhmm("06:56")},
		SunsetMin:  [12]int{hhmm("17:10"), hhmm("17:40"), hhmm("18:08"), hhmm("18:36"), hhmm("19:01"), hhmm("19:14"), hhmm("19:06"), hhmm("18:42"), hhmm("18:04"), hhmm("17:27"), hhmm("16:59"), hhmm("16:52")},
	},
	{
		Key: "dhaka", NameZH: "达卡", NameEN: "Dhaka", NameJA: "ダッカ",
		CountryZH: "孟加拉国", CountryEN: "Bangladesh", CountryJA: "バングラデシュ",
		Lat: 23.8103, Lng: 90.4125, TZMin: 360,
		SunriseMin: [12]int{hhmm("06:45"), hhmm("06:31"), hhmm("06:08"), hhmm("05:44"), hhmm("05:27"), hhmm("05:22"), hhmm("05:32"), hhmm("05:47"), hhmm("05:59"), hhmm("06:14"), hhmm("06:31"), hhmm("06:43")},
		SunsetMin:  [12]int{hhmm("17:42"), hhmm("18:02"), hhmm("18:20"), hhmm("18:38"), hhmm("18:56"), hhmm("19:10"), hhmm("19:08"), hhmm("18:52"), hhmm("18:27"), hhmm("18:03"), hhmm("17:45"), hhmm("17:39")},
	},
	{
		Key: "sao_paulo", NameZH: "圣保罗", NameEN: "São Paulo", NameJA: "サンパウロ",
		CountryZH: "巴西", CountryEN: "Brazil", CountryJA: "ブラジル",
		Lat: -23.5505, Lng: -46.6333, TZMin: -180,
		SunriseMin: [12]int{hhmm("05:23"), hhmm("05:38"), hhmm("05:53"), hhmm("06:07"), hhmm("06:22"), hhmm("06:31"), hhmm("06:26"), hhmm("06:08"), hhmm("05:44"), hhmm("05:24"), hhmm("05:12"), hhmm("05:16")},
		SunsetMin:  [12]int{hhmm("18:57"), hhmm("18:42"), hhmm("18:17"), hhmm("17:48"), hhmm("17:24"), hhmm("17:10"), hhmm("17:17"), hhmm("17:40"), hhmm("18:06"), hhmm("18:31"), hhmm("18:51"), hhmm("18:59")},
	},
	{
		Key: "mexico_city", NameZH: "墨西哥城", NameEN: "Mexico City", NameJA: "メキシコシティ",
		CountryZH: "墨西哥", CountryEN: "Mexico", CountryJA: "メキシコ",
		Lat: 19.4326, Lng: -99.1332, TZMin: -360,
		SunriseMin: [12]int{hhmm("07:14"), hhmm("07:07"), hhmm("06:50"), hhmm("06:29"), hhmm("06:14"), hhmm("06:11"), hhmm("06:20"), hhmm("06:32"), hhmm("06:40"), hhmm("06:49"), hhmm("07:02"), hhmm("07:11")},
		SunsetMin:  [12]int{hhmm("18:12"), hhmm("18:28"), hhmm("18:40"), hhmm("18:52"), hhmm("19:04"), hhmm("19:14"), hhmm("19:12"), hhmm("18:58"), hhmm("18:34"), hhmm("18:11"), hhmm("18:00"), hhmm("18:04")},
	},
	{
		Key: "cairo", NameZH: "开罗", NameEN: "Cairo", NameJA: "カイロ",
		CountryZH: "埃及", CountryEN: "Egypt", CountryJA: "エジプト",
		Lat: 30.0444, Lng: 31.2357, TZMin: 120,
		SunriseMin: [12]int{hhmm("06:48"), hhmm("06:29"), hhmm("06:00"), hhmm("05:30"), hhmm("05:09"), hhmm("05:03"), hhmm("05:14"), hhmm("05:34"), hhmm("05:55"), hhmm("06:17"), hhmm("06:40"), hhmm("06:54")},
		SunsetMin:  [12]int{hhmm("17:47"), hhmm("18:11"), hhmm("18:33"), hhmm("18:56"), hhmm("19:17"), hhmm("19:34"), hhmm("19:32"), hhmm("19:12"), hhmm("18:40"), hhmm("18:09"), hhmm("17:48"), hhmm("17:42")},
	},
	{
		Key: "mumbai", NameZH: "孟买", NameEN: "Mumbai", NameJA: "ムンバイ",
		CountryZH: "印度", CountryEN: "India", CountryJA: "インド",
		Lat: 19.0760, Lng: 72.8777, TZMin: 330,
		SunriseMin: [12]int{hhmm("07:07"), hhmm("06:56"), hhmm("06:38"), hhmm("06:18"), hhmm("06:02"), hhmm("05:58"), hhmm("06:05"), hhmm("06:17"), hhmm("06:26"), hhmm("06:37"), hhmm("06:51"), hhmm("07:03")},
		SunsetMin:  [12]int{hhmm("18:11"), hhmm("18:26"), hhmm("18:39"), hhmm("18:53"), hhmm("19:06"), hhmm("19:17"), hhmm("19:15"), hhmm("19:02"), hhmm("18:41"), hhmm("18:20"), hhmm("18:06"), hhmm("18:07")},
	},
	{
		Key: "beijing", NameZH: "北京", NameEN: "Beijing", NameJA: "北京",
		CountryZH: "中国", CountryEN: "China", CountryJA: "中国",
		Lat: 39.9042, Lng: 116.4074, TZMin: 480,
		SunriseMin: [12]int{hhmm("07:32"), hhmm("07:04"), hhmm("06:20"), hhmm("05:34"), hhmm("05:02"), hhmm("04:46"), hhmm("04:59"), hhmm("05:29"), hhmm("05:58"), hhmm("06:28"), hhmm("07:00"), hhmm("07:24")},
		SunsetMin:  [12]int{hhmm("17:00"), hhmm("17:37"), hhmm("18:10"), hhmm("18:45"), hhmm("19:17"), hhmm("19:42"), hhmm("19:37"), hhmm("19:09"), hhmm("18:24"), hhmm("17:40"), hhmm("17:09"), hhmm("16:59")},
	},
	{
		Key: "dar_es_salaam", NameZH: "达累斯萨拉姆", NameEN: "Dar es Salaam", NameJA: "ダルエスサラーム",
		CountryZH: "坦桑尼亚", CountryEN: "Tanzania", CountryJA: "タンザニア",
		Lat: -6.7924, Lng: 39.2083, TZMin: 180,
		SunriseMin: [12]int{hhmm("06:10"), hhmm("06:18"), hhmm("06:20"), hhmm("06:18"), hhmm("06:11"), hhmm("06:04"), hhmm("06:02"), hhmm("06:05"), hhmm("06:07"), hhmm("06:06"), hhmm("06:07"), hhmm("06:09")},
		SunsetMin:  [12]int{hhmm("18:35"), hhmm("18:31"), hhmm("18:18"), hhmm("18:01"), hhmm("17:50"), hhmm("17:46"), hhmm("17:49"), hhmm("17:57"), hhmm("18:04"), hhmm("18:16"), hhmm("18:27"), hhmm("18:33")},
	},
	{
		Key: "osaka", NameZH: "大阪", NameEN: "Osaka", NameJA: "大阪",
		CountryZH: "日本", CountryEN: "Japan", CountryJA: "日本",
		Lat: 34.6762, Lng: 135.5019, TZMin: 540,
		SunriseMin: [12]int{hhmm("06:54"), hhmm("06:30"), hhmm("05:54"), hhmm("05:18"), hhmm("04:51"), hhmm("04:38"), hhmm("04:48"), hhmm("05:11"), hhmm("05:37"), hhmm("06:03"), hhmm("06:29"), hhmm("06:48")},
		SunsetMin:  [12]int{hhmm("17:03"), hhmm("17:34"), hhmm("18:04"), hhmm("18:34"), hhmm("19:00"), hhmm("19:21"), hhmm("19:17"), hhmm("18:52"), hhmm("18:14"), hhmm("17:34"), hhmm("17:02"), hhmm("16:50")},
	},
	{
		Key: "kolkata", NameZH: "加尔各答", NameEN: "Kolkata", NameJA: "コルカタ",
		CountryZH: "印度", CountryEN: "India", CountryJA: "インド",
		Lat: 22.5726, Lng: 88.3639, TZMin: 330,
		SunriseMin: [12]int{hhmm("06:40"), hhmm("06:26"), hhmm("06:04"), hhmm("05:41"), hhmm("05:25"), hhmm("05:20"), hhmm("05:29"), hhmm("05:44"), hhmm("05:57"), hhmm("06:12"), hhmm("06:29"), hhmm("06:41")},
		SunsetMin:  [12]int{hhmm("17:45"), hhmm("18:04"), hhmm("18:21"), hhmm("18:39"), hhmm("18:55"), hhmm("19:08"), hhmm("19:07"), hhmm("18:52"), hhmm("18:29"), hhmm("18:05"), hhmm("17:47"), hhmm("17:40")},
	},
	{
		Key: "buenos_aires", NameZH: "布宜诺斯艾利斯", NameEN: "Buenos Aires", NameJA: "ブエノスアイレス",
		CountryZH: "阿根廷", CountryEN: "Argentina", CountryJA: "アルゼンチン",
		Lat: -34.6037, Lng: -58.3816, TZMin: -180,
		SunriseMin: [12]int{hhmm("05:42"), hhmm("06:01"), hhmm("06:20"), hhmm("06:38"), hhmm("06:56"), hhmm("07:07"), hhmm("07:01"), hhmm("06:40"), hhmm("06:10"), hhmm("05:43"), hhmm("05:24"), hhmm("05:30")},
		SunsetMin:  [12]int{hhmm("19:52"), hhmm("19:28"), hhmm("18:52"), hhmm("18:11"), hhmm("17:39"), hhmm("17:16"), hhmm("17:24"), hhmm("17:54"), hhmm("18:30"), hhmm("19:06"), hhmm("19:40"), hhmm("19:53")},
	},
	{
		Key: "karachi", NameZH: "卡拉奇", NameEN: "Karachi", NameJA: "カラチ",
		CountryZH: "巴基斯坦", CountryEN: "Pakistan", CountryJA: "パキスタン",
		Lat: 24.8607, Lng: 67.0110, TZMin: 300,
		SunriseMin: [12]int{hhmm("07:12"), hhmm("06:56"), hhmm("06:30"), hhmm("06:03"), hhmm("05:42"), hhmm("05:36"), hhmm("05:47"), hhmm("06:05"), hhmm("06:22"), hhmm("06:40"), hhmm("06:59"), hhmm("07:11")},
		SunsetMin:  [12]int{hhmm("17:57"), hhmm("18:20"), hhmm("18:41"), hhmm("19:02"), hhmm("19:21"), hhmm("19:36"), hhmm("19:34"), hhmm("19:15"), hhmm("18:46"), hhmm("18:17"), hhmm("17:58"), hhmm("17:53")},
	},
	{
		Key: "istanbul", NameZH: "伊斯坦布尔", NameEN: "Istanbul", NameJA: "イスタンブール",
		CountryZH: "土耳其", CountryEN: "Turkey", CountryJA: "トルコ",
		Lat: 41.0082, Lng: 28.9784, TZMin: 180,
		SunriseMin: [12]int{hhmm("08:22"), hhmm("07:54"), hhmm("07:10"), hhmm("06:23"), hhmm("05:48"), hhmm("05:30"), hhmm("05:42"), hhmm("06:10"), hhmm("06:40"), hhmm("07:10"), hhmm("07:43"), hhmm("08:09")},
		SunsetMin:  [12]int{hhmm("17:44"), hhmm("18:18"), hhmm("18:48"), hhmm("19:19"), hhmm("19:49"), hhmm("20:14"), hhmm("20:10"), hhmm("19:41"), hhmm("19:00"), hhmm("18:20"), hhmm("17:50"), hhmm("17:39")},
	},
	{
		Key: "lagos", NameZH: "拉各斯", NameEN: "Lagos", NameJA: "ラゴス",
		CountryZH: "尼日利亚", CountryEN: "Nigeria", CountryJA: "ナイジェリア",
		Lat: 6.5244, Lng: 3.3792, TZMin: 60,
		SunriseMin: [12]int{hhmm("06:46"), hhmm("06:50"), hhmm("06:47"), hhmm("06:40"), hhmm("06:34"), hhmm("06:32"), hhmm("06:34"), hhmm("06:38"), hhmm("06:41"), hhmm("06:44"), hhmm("06:45"), hhmm("06:46")},
		SunsetMin:  [12]int{hhmm("18:34"), hhmm("18:37"), hhmm("18:34"), hhmm("18:26"), hhmm("18:19"), hhmm("18:16"), hhmm("18:18"), hhmm("18:23"), hhmm("18:28"), hhmm("18:31"), hhmm("18:33"), hhmm("18:34")},
	},
	{
		Key: "rio_de_janeiro", NameZH: "里约热内卢", NameEN: "Rio de Janeiro", NameJA: "リオデジャネイロ",
		CountryZH: "巴西", CountryEN: "Brazil", CountryJA: "ブラジル",
		Lat: -22.9068, Lng: -43.1729, TZMin: -180,
		SunriseMin: [12]int{hhmm("05:17"), hhmm("05:32"), hhmm("05:47"), hhmm("06:02"), hhmm("06:17"), hhmm("06:26"), hhmm("06:22"), hhmm("06:04"), hhmm("05:41"), hhmm("05:21"), hhmm("05:10"), hhmm("05:14")},
		SunsetMin:  [12]int{hhmm("18:49"), hhmm("18:34"), hhmm("18:10"), hhmm("17:42"), hhmm("17:19"), hhmm("17:05"), hhmm("17:12"), hhmm("17:34"), hhmm("18:00"), hhmm("18:25"), hhmm("18:45"), hhmm("18:53")},
	},
	{
		Key: "chennai", NameZH: "金奈", NameEN: "Chennai", NameJA: "チェンナイ",
		CountryZH: "印度", CountryEN: "India", CountryJA: "インド",
		Lat: 13.0827, Lng: 80.2707, TZMin: 330,
		SunriseMin: [12]int{hhmm("06:34"), hhmm("06:31"), hhmm("06:21"), hhmm("06:08"), hhmm("05:57"), hhmm("05:54"), hhmm("05:58"), hhmm("06:04"), hhmm("06:07"), hhmm("06:11"), hhmm("06:21"), hhmm("06:30")},
		SunsetMin:  [12]int{hhmm("17:44"), hhmm("17:56"), hhmm("18:06"), hhmm("18:17"), hhmm("18:29"), hhmm("18:39"), hhmm("18:38"), hhmm("18:29"), hhmm("18:14"), hhmm("17:59"), hhmm("17:48"), hhmm("17:43")},
	},
	{
		Key: "bangkok", NameZH: "曼谷", NameEN: "Bangkok", NameJA: "バンコク",
		CountryZH: "泰国", CountryEN: "Thailand", CountryJA: "タイ",
		Lat: 13.7563, Lng: 100.5018, TZMin: 420,
		SunriseMin: [12]int{hhmm("06:37"), hhmm("06:32"), hhmm("06:20"), hhmm("06:05"), hhmm("05:54"), hhmm("05:51"), hhmm("05:55"), hhmm("06:01"), hhmm("06:05"), hhmm("06:10"), hhmm("06:20"), hhmm("06:30")},
		SunsetMin:  [12]int{hhmm("17:59"), hhmm("18:11"), hhmm("18:21"), hhmm("18:32"), hhmm("18:44"), hhmm("18:54"), hhmm("18:53"), hhmm("18:43"), hhmm("18:27"), hhmm("18:11"), hhmm("18:00"), hhmm("17:56")},
	},
	{
		Key: "new_york", NameZH: "纽约", NameEN: "New York", NameJA: "ニューヨーク",
		CountryZH: "美国", CountryEN: "United States", CountryJA: "アメリカ",
		Lat: 40.7128, Lng: -74.0060, TZMin: -300,
		SunriseMin: [12]int{hhmm("07:20"), hhmm("07:04"), hhmm("06:23"), hhmm("06:47"), hhmm("06:13"), hhmm("05:53"), hhmm("06:01"), hhmm("06:26"), hhmm("06:54"), hhmm("06:23"), hhmm("06:53"), hhmm("07:19")},
		SunsetMin:  [12]int{hhmm("16:47"), hhmm("17:21"), hhmm("17:54"), hhmm("19:24"), hhmm("19:57"), hhmm("20:20"), hhmm("20:16"), hhmm("19:47"), hhmm("18:59"), hhmm("18:22"), hhmm("17:04"), hhmm("16:38")},
	},
}

// resolveCityGeo 城市键解析(方案 33 §2.1):
//   - ""(未选)→ nil = 默认城市,全部城市化逻辑旁路,批次 27 行为逐分不差;
//   - "random" → 按房间 seed 确定性抽取(hashU64 派生,与 rng 纪律一致);
//   - 其余按键查表,未命中 nil(防御:旧客户端乱传不炸)。
func resolveCityGeo(key string, seed int64) *CityGeo {
	switch key {
	case "":
		return nil
	case "random":
		return &cityGeoTable[int(hashU64(uint64(seed^cityGeoSeedSalt))%uint64(len(cityGeoTable)))]
	}
	for i := range cityGeoTable {
		if cityGeoTable[i].Key == key {
			return &cityGeoTable[i]
		}
	}
	return nil
}

// daysInMonth 平年各月天数(日出日落插值粒度用;闰年 2 月差 1 天的插值误差
// <1 分钟,可忽略 —— 数据表本身就是月均值)。
var daysInMonth = [12]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

// SunTimesAt 城市当日日出/日落(当地分钟,自当地 0 点;方案 33 §2.1):
// 月内线性插值 —— 当月值视为当月第 1 天,(day-1)/daysInMonth 向下一月推进
// (12 月向次年 1 月回绕)。确定性纯函数:同 cityMs 恒同结果。
func SunTimesAt(g *CityGeo, cityClockMs int64) (rise, set int) {
	if g == nil {
		return 6 * 60, 18 * 60 // 默认城市 6:00/18:00(批次 27 语义)
	}
	local := time.UnixMilli(cityClockMs + int64(g.TZMin)*60_000).UTC()
	m := int(local.Month()) - 1 // 0-based
	d := local.Day()
	next := (m + 1) % 12
	frac := float64(d-1) / float64(daysInMonth[m])
	lerp := func(a, b int) int {
		return int(float64(a) + (float64(b)-float64(a))*frac + 0.5)
	}
	return lerp(g.SunriseMin[m], g.SunriseMin[next]), lerp(g.SunsetMin[m], g.SunsetMin[next])
}

// CityGeoJSON 下发前端的城市档案(BuildClientState 现算填充;方案 33 §2.3)。
type CityGeoJSON struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"` // 中文名
	NameEN      string  `json:"name_en"`
	NameJA      string  `json:"name_ja"`
	Country     string  `json:"country"`
	CountryEN   string  `json:"country_en"`
	CountryJA   string  `json:"country_ja"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	TZOffsetMin int     `json:"tz_offset_min"`
	Sunrise     string  `json:"sunrise"` // HH:MM(城市当日,插值)
	Sunset      string  `json:"sunset"`
	SunriseMin  int     `json:"sunrise_min"`
	SunsetMin   int     `json:"sunset_min"`
}

// fmtHHMM 分钟 → "HH:MM"(前端徽章直显)。
func fmtHHMM(min int) string {
	return fmt.Sprintf("%02d:%02d", min/60, min%60)
}

// CityGeoJSONAt 构造下发快照(每次 BuildClientState 按当前城市时钟现算日出日落)。
func CityGeoJSONAt(g *CityGeo, cityClockMs int64) *CityGeoJSON {
	if g == nil {
		return nil
	}
	rise, set := SunTimesAt(g, cityClockMs)
	return &CityGeoJSON{
		Key:         g.Key,
		Name:        g.NameZH,
		NameEN:      g.NameEN,
		NameJA:      g.NameJA,
		Country:     g.CountryZH,
		CountryEN:   g.CountryEN,
		CountryJA:   g.CountryJA,
		Lat:         g.Lat,
		Lng:         g.Lng,
		TZOffsetMin: g.TZMin,
		Sunrise:     fmtHHMM(rise),
		Sunset:      fmtHHMM(set),
		SunriseMin:  rise,
		SunsetMin:   set,
	}
}
