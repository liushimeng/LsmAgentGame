// Package wealth — districts.go: 8 城区静态表(2026-09-14 §财商流P0)。
//
// 世界地图 = 一座城市。DistrictDefs 顺序即数组下标(view 层 game.state.market.districts
// 按 this 顺序输出)。数值出处: 后端架构文档 §4(基准价/beta/租售比为 P0 新定,
// 区间校准《规则》§6.2.2 一线 300–1000 万)。
package wealth

// DistrictDef 是单个城区的静态定义。
type DistrictDef struct {
	ID          string  // "finance" …
	NameCN      string  // 中文名
	X           float64 // 前端 2.5D 地图坐标(40×40 平面)
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

// DistrictDefs 8 城区静态表(顺序即 view 输出顺序,不可调整)。
var DistrictDefs = []DistrictDef{
	{ID: "finance", NameCN: "金融CBD", X: 0, Z: 0, Beta: 1.3, BasePriceWan: 800, Color: "#1d4ed8"},
	{ID: "tech", NameCN: "科技园", X: -10, Z: 4, Beta: 1.15, BasePriceWan: 500, Color: "#0e7490"},
	{ID: "industry", NameCN: "工业区", X: -12, Z: -8, Beta: 0.85, BasePriceWan: 200, Color: "#57534e"},
	{ID: "oldtown", NameCN: "老城区", X: 2, Z: -12, Beta: 0.8, BasePriceWan: 180, Color: "#92400e"},
	{ID: "commerce", NameCN: "商业中心", X: 10, Z: -2, Beta: 1.1, BasePriceWan: 400, Color: "#b91c1c"},
	{ID: "residential", NameCN: "居住区", X: 0, Z: 12, Beta: 1.0, BasePriceWan: 300, Color: "#15803d"},
	{ID: "suburb", NameCN: "郊区", X: -14, Z: 14, Beta: 0.7, BasePriceWan: 120, Color: "#65a30d"},
	{ID: "riverside", NameCN: "滨河新区", X: 14, Z: 10, Beta: 1.25, BasePriceWan: 450, Color: "#7c3aed"},
}

// DistrictCount 城区总数。
const DistrictCount = 8

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
