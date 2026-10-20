// Package virtual_city — districts_grid_test.go: 批次 61「全城连续正交方格路网」
// 的**方格不变量**守卫。
//
// 背景(§130「声明了却从不接线」的反面:声明了就必须有东西守住):
// 批次 61 之所以要做「街区方格化」,是因为旧城区坐标是散点 —— x 集合里任意相邻
// 两值之差都 < 9,而街区半宽 4.0 + 路廊半宽 0.7 = 4.7,于是**不存在任何一条能
// 连续贯穿、又不切进任何街区的南北向街道线**(用户反馈:「街道还是不连续,
// 一块块还是斜向的」)。修法是把 32 个城区全部落到 12 的整数倍上。
//
// 本文件把这个「必须落在 12 的整数倍 + 两两不重合」的不变量写成单测,
// 防止日后有人手改坐标把方格网破坏掉(而且是**无声**破坏:编译与渲染都不会报错,
// 只有「街道又变回斜的」能被肉眼发现)。
//
// 前端对应物:`ClientWeb/src/components/virtualCity/cityGrid.ts` + `verify_city_grid.mjs`。
package virtual_city

import "testing"

// gridCellSize 方格网街区边长(世界单位;1u = 10m ⇒ 120m 街区)。
// 与前端 `cityGrid.ts::GRID_CELL` 必须同值。
const gridCellSize = 12.0

// TestDistricts_SnapToGrid 断言 32 个城区坐标全部是 12 的整数倍。
func TestDistricts_SnapToGrid(t *testing.T) {
	if len(DistrictDefs) != 32 {
		t.Fatalf("城区数量 = %d, 期望 32", len(DistrictDefs))
	}
	for _, d := range DistrictDefs {
		if d.X != float64(int(d.X)) {
			t.Errorf("城区 %s 的 X=%v 不是整数", d.ID, d.X)
		}
		if d.Z != float64(int(d.Z)) {
			t.Errorf("城区 %s 的 Z=%v 不是整数", d.ID, d.Z)
		}
		if mod12(d.X) != 0 {
			t.Errorf("城区 %s 的 X=%v 不是 %v 的整数倍(方格网不变量被破坏)", d.ID, d.X, gridCellSize)
		}
		if mod12(d.Z) != 0 {
			t.Errorf("城区 %s 的 Z=%v 不是 %v 的整数倍(方格网不变量被破坏)", d.ID, d.Z, gridCellSize)
		}
	}
}

// TestDistricts_NoOverlap 断言两两城区不重合(否则两区共用一个街区 ⇒ 建筑互相穿模)。
func TestDistricts_NoOverlap(t *testing.T) {
	seen := make(map[[2]int64]string, len(DistrictDefs))
	for _, d := range DistrictDefs {
		key := [2]int64{int64(d.X), int64(d.Z)}
		if prev, ok := seen[key]; ok {
			t.Errorf("城区 %s 与 %s 落在同一街区单元 (%v,%v)", d.ID, prev, d.X, d.Z)
			continue
		}
		seen[key] = d.ID
	}
}

// TestDistricts_FinanceAtOrigin 断言金融 CBD 固定在原点 ——
// CBD 环/一环/相机初始朝向/地图中心都以此为锚(批次 61 删环后仍保留该锚)。
func TestDistricts_FinanceAtOrigin(t *testing.T) {
	d := districtByID("finance")
	if d == nil {
		t.Fatal("缺少 finance 城区")
	}
	if d.X != 0 || d.Z != 0 {
		t.Errorf("金融 CBD 坐标 = (%v,%v), 期望 (0,0)", d.X, d.Z)
	}
}

// TestDistricts_WithinCityExtent 断言全部城区落在 [-36,36]² 内
// (7×7 方格的中心范围),避免有人把某区拖到方格网之外导致「孤岛街区」。
func TestDistricts_WithinCityExtent(t *testing.T) {
	const half = 3 * gridCellSize // 36
	for _, d := range DistrictDefs {
		if d.X < -half || d.X > half || d.Z < -half || d.Z > half {
			t.Errorf("城区 %s 坐标 (%v,%v) 越出方格网范围 [-%v,%v]", d.ID, d.X, d.Z, half, half)
		}
	}
}

// mod12 返回 v 对 12 取模的绝对值(浮点容差内)。v 必为 12 的整数倍时返回 0。
func mod12(v float64) float64 {
	m := v / gridCellSize
	r := m - float64(int(m))
	if r < 0 {
		r = -r
	}
	if r > 0.999999 {
		r = 0 // 负数方向的浮点余数(如 -1 → r=1)
	}
	return r
}
