// Package wealth — districts.go: 16 城区静态表(2026-09-14 §财商流P0;
// v2.12 阶段 2 由 8 城区扩展到 16 城区,地图 40×40 → 80×80)。
//
// 世界地图 = 一座城市。DistrictDefs 顺序即数组下标(view 层 game.state.market.districts
// 按 this 顺序输出)。数值出处: 后端架构文档 §4(基准价/beta/租售比为 P0 新定,
// 区间校准《规则》§6.2.2 一线 300–1000 万)。
package wealth

// DistrictDef 是单个城区的静态定义。
type DistrictDef struct {
	ID          string  // "finance" …
	NameCN      string  // 中文名
	X           float64 // 前端 2.5D 地图坐标(80×80 平面)
	Z           float64
	Beta        float64 // 周期敏感度(价格水平与收益率同时乘 beta,后端架构 §4 ⚠️)
	BasePriceWan float64 // 基准房价(万元/套)
	Color       string  // 主色(前端楼群)
}

// 租售比(月租 = 当前房价 × 因子;后端架构 §4):
//   - 住宅 0.16%/月 ≈ 年租 2%
//   - 商铺 0.35%/月 ≈ 年租 4.2%(《规则》§6.2.1 商业地产 4–6% 区间)
const (
	HouseRentFactor = 0.0016
	ShopRentFactor  = 0.0035
)

// DistrictDefs 16 城区静态表(顺序即 view 输出顺序,不可调整)。
// 前 8 区为 P0 原有城区(id/顺序不可修改);后 8 区为 v2.12 阶段 2 扩展城区
// (顺序与前端 types/wealth.ts WEALTH_DISTRICTS 完全一致,§130 契约对齐)。
var DistrictDefs = []DistrictDef{
	{ID: "finance", NameCN: "金融CBD", X: 0, Z: 0, Beta: 1.3, BasePriceWan: 800, Color: "#1d4ed8"},
	{ID: "tech", NameCN: "科技园", X: -10, Z: 4, Beta: 1.15, BasePriceWan: 500, Color: "#0e7490"},
	{ID: "industry", NameCN: "工业区", X: -12, Z: -8, Beta: 0.85, BasePriceWan: 200, Color: "#57534e"},
	{ID: "oldtown", NameCN: "老城区", X: 2, Z: -12, Beta: 0.8, BasePriceWan: 180, Color: "#92400e"},
	{ID: "commerce", NameCN: "商业中心", X: 10, Z: -2, Beta: 1.1, BasePriceWan: 400, Color: "#b91c1c"},
	{ID: "residential", NameCN: "居住区", X: 0, Z: 12, Beta: 1.0, BasePriceWan: 300, Color: "#15803d"},
	{ID: "suburb", NameCN: "郊区", X: -14, Z: 14, Beta: 0.7, BasePriceWan: 120, Color: "#65a30d"},
	{ID: "riverside", NameCN: "滨河新区", X: 14, Z: 10, Beta: 1.25, BasePriceWan: 450, Color: "#7c3aed"},
	// ── v2.12 阶段 2 扩展城区(80×80 地图外圈,位置 ±30 单位内) ──
	{ID: "logistics_port", NameCN: "物流港", X: -22, Z: -4, Beta: 0.9, BasePriceWan: 250, Color: "#475569"},
	{ID: "hightech_park", NameCN: "高新园区", X: -22, Z: 12, Beta: 1.2, BasePriceWan: 600, Color: "#0891b2"},
	{ID: "edu_district", NameCN: "教育园区", X: -8, Z: 22, Beta: 0.95, BasePriceWan: 350, Color: "#7c3aed"},
	{ID: "medical_city", NameCN: "医疗城", X: 8, Z: 22, Beta: 1.05, BasePriceWan: 450, Color: "#db2777"},
	{ID: "industrial_park", NameCN: "产业基地", X: -24, Z: -20, Beta: 0.7, BasePriceWan: 150, Color: "#78716c"},
	{ID: "central_park", NameCN: "中央公园", X: 0, Z: -22, Beta: 1.0, BasePriceWan: 500, Color: "#16a34a"},
	{ID: "transport_hub", NameCN: "交通枢纽", X: 22, Z: -14, Beta: 0.85, BasePriceWan: 280, Color: "#ea580c"},
	{ID: "cultural_creative", NameCN: "文创区", X: 24, Z: 8, Beta: 1.1, BasePriceWan: 380, Color: "#e11d48"},
}

// DistrictCount 城区总数。
const DistrictCount = 16

// districtByID 从 id 查静态定义;未命中返回 nil。
func districtByID(id string) *DistrictDef {
	for i := range DistrictDefs {
		if DistrictDefs[i].ID == id {
			return &DistrictDefs[i]
		}
	}
	return nil
}

// ValidDistrict 报告 district id 是否合法。
func ValidDistrict(id string) bool {
	return districtByID(id) != nil
}

// DistrictCN 返回城区中文名(未命中返回原 id)。
func DistrictCN(id string) string {
	if d := districtByID(id); d != nil {
		return d.NameCN
	}
	return id
}
