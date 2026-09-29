// 批次 33 真实城市时间系统单测:表完整性 / 键解析 / random 确定性 /
// 日出日落插值端点与回绕(方案 33 §4)。
package virtual_city

import (
	"testing"
	"time"
)

// TestCityGeoTableComplete 20 城 × 12 月表完整性:键唯一、日出∈(0,720)、
// 日落∈(720,1440)、日落>日出(数据文档照抄校验,防录入笔误)。
func TestCityGeoTableComplete(t *testing.T) {
	if len(cityGeoTable) != 20 {
		t.Fatalf("cityGeoTable 应为 20 城,实际 %d", len(cityGeoTable))
	}
	seen := map[string]bool{}
	for _, g := range cityGeoTable {
		if g.Key == "" || seen[g.Key] {
			t.Fatalf("城市键重复或为空: %q", g.Key)
		}
		seen[g.Key] = true
		if g.NameZH == "" || g.NameEN == "" || g.NameJA == "" {
			t.Fatalf("%s 三语名缺失", g.Key)
		}
		if g.Lat < -90 || g.Lat > 90 || g.Lng < -180 || g.Lng > 180 {
			t.Fatalf("%s 经纬度非法: %v,%v", g.Key, g.Lat, g.Lng)
		}
		for m := 0; m < 12; m++ {
			r, s := g.SunriseMin[m], g.SunsetMin[m]
			if r <= 0 || r >= 720 || s <= 720 || s >= 1440 || s <= r {
				t.Fatalf("%s 月%d 日出日落非法: rise=%d set=%d", g.Key, m+1, r, s)
			}
		}
	}
}

// TestResolveCityGeo 键解析:空=nil、random=确定性且落在表内、未知键=nil。
func TestResolveCityGeo(t *testing.T) {
	if g := resolveCityGeo("", 0); g != nil {
		t.Fatalf("空键应返回 nil(默认城市)")
	}
	if g := resolveCityGeo("no_such_city", 7); g != nil {
		t.Fatalf("未知键应返回 nil")
	}
	tokyo := resolveCityGeo("tokyo", 7)
	if tokyo == nil || tokyo.NameZH != "东京" || tokyo.TZMin != 540 {
		t.Fatalf("tokyo 解析失败: %+v", tokyo)
	}
	// random 确定性:同 seed 同城、不同 seed 大概率不同(抽 100 seed 至少见 5 城)。
	first := resolveCityGeo("random", 42)
	if first == nil {
		t.Fatalf("random 不应返回 nil")
	}
	for i := 0; i < 20; i++ {
		if g := resolveCityGeo("random", 42); g.Key != first.Key {
			t.Fatalf("random 同 seed 应确定: %s != %s", g.Key, first.Key)
		}
	}
	distinct := map[string]bool{}
	for seed := int64(0); seed < 100; seed++ {
		distinct[resolveCityGeo("random", seed).Key] = true
	}
	if len(distinct) < 5 {
		t.Fatalf("random 分布异常,100 seed 仅 %d 座城", len(distinct))
	}
}

// cityMsAt 构造某城市当地时间的城市时钟 ms(UTC 口径 + 偏移反推)。
func cityMsAt(g *CityGeo, year int, month time.Month, day, hour, min int) int64 {
	local := time.Date(year, month, day, hour, min, 0, 0, time.UTC)
	return local.UnixMilli() - int64(g.TZMin)*60_000
}

// TestSunTimesAtMonthStart 每月第 1 天 = 表值原样(插值 frac=0)。
func TestSunTimesAtMonthStart(t *testing.T) {
	g := resolveCityGeo("tokyo", 0)
	for m := time.January; m <= time.December; m++ {
		ms := cityMsAt(g, 2026, m, 1, 12, 0)
		rise, set := SunTimesAt(g, ms)
		if rise != g.SunriseMin[m-1] || set != g.SunsetMin[m-1] {
			t.Fatalf("东京 %d 月 1 日应等于表值: got %d/%d want %d/%d",
				m, rise, set, g.SunriseMin[m-1], g.SunsetMin[m-1])
		}
	}
}

// TestSunTimesAtInterpolation 月内插值:1 月 16 日(frac=15/31)落在 1 月与
// 2 月表值之间;12 月插值向 1 月回绕。
func TestSunTimesAtInterpolation(t *testing.T) {
	g := resolveCityGeo("new_york", 0)
	r1, s1 := g.SunriseMin[0], g.SunsetMin[0] // 1 月
	r2, s2 := g.SunriseMin[1], g.SunsetMin[1] // 2 月
	rise, set := SunTimesAt(g, cityMsAt(g, 2026, time.January, 16, 12, 0))
	if rise <= min(r1, r2) || rise >= max(r1, r2) {
		t.Fatalf("1/16 日出应介于两月表值间: %d (%d..%d)", rise, r1, r2)
	}
	if set <= min(s1, s2) || set >= max(s1, s2) {
		t.Fatalf("1/16 日落应介于两月表值间: %d (%d..%d)", set, s1, s2)
	}
	// 12 月 31 日:frac=30/31,接近次年 1 月表值(回绕插值)。
	riseDec, _ := SunTimesAt(g, cityMsAt(g, 2026, time.December, 31, 12, 0))
	if diff := absInt(riseDec - g.SunriseMin[0]); diff > 30 {
		t.Fatalf("12/31 日出应接近 1 月表值: %d vs %d", riseDec, g.SunriseMin[0])
	}
}

// TestSunTimesAtNilGeo 默认城市(nil)= 6:00/18:00(批次 27 语义零回归)。
func TestSunTimesAtNilGeo(t *testing.T) {
	rise, set := SunTimesAt(nil, time.Now().UnixMilli())
	if rise != 360 || set != 1080 {
		t.Fatalf("默认城市应 6:00/18:00: %d/%d", rise, set)
	}
}

// TestCityGeoJSONAt 下发快照:字段齐、HH:MM 格式化正确;nil → nil。
func TestCityGeoJSONAt(t *testing.T) {
	g := resolveCityGeo("delhi", 0)
	j := CityGeoJSONAt(g, cityMsAt(g, 2026, time.January, 1, 12, 0))
	if j == nil || j.Key != "delhi" || j.TZOffsetMin != 330 {
		t.Fatalf("快照字段错误: %+v", j)
	}
	if j.Sunrise != "07:09" || j.Sunset != "17:37" {
		t.Fatalf("1/1 应等于表值原文: %s/%s", j.Sunrise, j.Sunset)
	}
	if CityGeoJSONAt(nil, 0) != nil {
		t.Fatalf("nil 城市应返回 nil 快照")
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
