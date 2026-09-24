// Package wealth — districts.go: 32 城区静态表(2026-09-14 §财商流P0;
// v2.12 阶段 2 由 8 城区扩展到 16 城区,地图 40×40 → 80×80;
// 批次 20 由 16 城区扩展到 32 城区,地图 80×80 → 120×120,前 16 区
// id/顺序/坐标全部冻结,16 新区仅追加于尾部下标 16–31)。
//
// 世界地图 = 一座城市。DistrictDefs 顺序即数组下标(view 层 game.state.market.districts
// 按 this 顺序输出)。数值出处: 后端架构文档 §4(基准价/beta/租售比为 P0 新定,
// 区间校准《规则》§6.2.2 一线 300–1000 万)。
package wealth

// DistrictDef 是单个城区的静态定义。
type DistrictDef struct {
	ID          string  // "finance" …
	NameCN      string  // 中文名
	X           float64 // 前端 2.5D 地图坐标(120×120 平面,纯渲染坐标)
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

// DistrictDefs 32 城区静态表(顺序即 view 输出顺序,不可调整)。
// 前 8 区为 P0 原有城区(id/顺序不可修改);下标 8–15 为 v2.12 阶段 2 扩展
// 城区;下标 16–31 为批次 20 扩展城区(16→32,只追加、前 16 区全部冻结,
// id/顺序/坐标/经济参数逐字对齐契约
// 《虚拟城市-批次20-32城区地图扩展与渲染性能-设计v1.md》§2)。
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
	// ── 批次 20 扩展城区(120×120 地图外圈,下标 16–31,顺序冻结) ──
	{ID: "fin_sub_center", NameCN: "金融副中心", X: 24, Z: 30, Beta: 1.3, BasePriceWan: 650, Color: "#1e40af"},
	{ID: "software_park", NameCN: "软件园", X: 38, Z: 14, Beta: 1.2, BasePriceWan: 520, Color: "#0d9488"},
	{ID: "airport_town", NameCN: "空港小镇", X: 44, Z: -20, Beta: 1.05, BasePriceWan: 300, Color: "#0369a1"},
	{ID: "air_logistics", NameCN: "航空物流园", X: 36, Z: -38, Beta: 0.95, BasePriceWan: 260, Color: "#334155"},
	{ID: "auto_city", NameCN: "汽车城", X: 8, Z: -40, Beta: 1.0, BasePriceWan: 300, Color: "#a16207"},
	{ID: "mountain_resort", NameCN: "山居民宿区", X: -8, Z: -44, Beta: 0.9, BasePriceWan: 180, Color: "#4d7c0f"},
	{ID: "chem_park", NameCN: "化工园区", X: -20, Z: -44, Beta: 0.7, BasePriceWan: 130, Color: "#52525b"},
	{ID: "agri_park", NameCN: "现代农业园", X: -40, Z: -36, Beta: 0.8, BasePriceWan: 160, Color: "#ca8a04"},
	{ID: "health_town", NameCN: "康养小镇", X: -44, Z: -10, Beta: 0.8, BasePriceWan: 200, Color: "#fb7185"},
	{ID: "steel_town", NameCN: "特钢镇", X: -46, Z: 2, Beta: 0.75, BasePriceWan: 150, Color: "#44403c"},
	{ID: "old_city_culture", NameCN: "古城文化区", X: -44, Z: 22, Beta: 0.85, BasePriceWan: 240, Color: "#9a3412"},
	{ID: "university_town", NameCN: "大学城", X: -32, Z: 34, Beta: 0.95, BasePriceWan: 300, Color: "#6366f1"},
	{ID: "wetland_park", NameCN: "湿地公园", X: -12, Z: 40, Beta: 0.9, BasePriceWan: 280, Color: "#14b8a6"},
	{ID: "sports_new_city", NameCN: "体育新城", X: 10, Z: 40, Beta: 1.05, BasePriceWan: 340, Color: "#facc15"},
	{ID: "bay_new_town", NameCN: "湾区新城", X: 40, Z: 28, Beta: 1.25, BasePriceWan: 580, Color: "#7e22ce"},
	{ID: "highspeed_rail_town", NameCN: "高铁新城", X: 46, Z: 2, Beta: 1.15, BasePriceWan: 380, Color: "#c2410c"},
}

// DistrictCount 城区总数。
const DistrictCount = 32

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

// ── 城区感官基底表(2026-09-22 §CityHuman重构,设计文档 1 §3.2) ──
//
// 每个城区定义 2~4 个气味标签 + 1~2 个环境声标签,作为 smell/hear 感知工具的
// 确定性基底;当月动态事件气味/声响由 events.go ambianceOverlay 叠加。

// AmbianceBase 单城区感官基底。
type AmbianceBase struct {
	Smells []string // 气味基底(嗅觉 ≈50m)
	Sounds []string // 环境声基底(听觉 ≈100m)
}

// districtAmbianceBase 32 城区感官基底(键 = DistrictDefs ID;批次 20 补齐
// 下标 16–31 新区基底,契约 §2.2)。
var districtAmbianceBase = map[string]AmbianceBase{
	"finance":            {Smells: []string{"咖啡", "打印机墨粉", "空调新风"}, Sounds: []string{"键盘声", "电梯提示音"}},
	"tech":               {Smells: []string{"咖啡", "外卖盒饭", "新机箱塑料味"}, Sounds: []string{"服务器风扇", "讨论声"}},
	"industry":           {Smells: []string{"油烟", "尾气", "金属切削液"}, Sounds: []string{"机器轰鸣", "货车倒车提示"}},
	"oldtown":            {Smells: []string{"老汤卤味", "煤炉烟火气", "旧木家具"}, Sounds: []string{"收音机戏曲", "邻里寒暄"}},
	"commerce":           {Smells: []string{"食物香气", "香水", "爆米花"}, Sounds: []string{"促销广播", "人群嘈杂"}},
	"residential":        {Smells: []string{"饭菜香", "洗衣液", "绿化泥土"}, Sounds: []string{"广场舞音乐", "儿童嬉闹"}},
	"suburb":             {Smells: []string{"青草", "农田土腥", "炊烟"}, Sounds: []string{"犬吠", "风声"}},
	"riverside":          {Smells: []string{"河水湿气", "水草腥", "夜摊烧烤"}, Sounds: []string{"游船汽笛", "水声"}},
	"logistics_port":     {Smells: []string{"柴油", "海腥", "纸箱"}, Sounds: []string{"吊机作业", "集卡鸣笛"}},
	"hightech_park":      {Smells: []string{"咖啡", "无尘车间清洗剂", "新打印纸"}, Sounds: []string{"通勤班车", "路演掌声"}},
	"edu_district":       {Smells: []string{"食堂饭菜", "粉笔灰", "旧书页"}, Sounds: []string{"上下课铃", "操场哨声"}},
	"medical_city":       {Smells: []string{"消毒水", "药味"}, Sounds: []string{"救护车笛", "叫号广播"}},
	"industrial_park":    {Smells: []string{"机油", "焊接烟尘", "食堂大锅菜"}, Sounds: []string{"流水线节拍", "叉车提示音"}},
	"central_park":       {Smells: []string{"青草", "花香", "湖水湿气"}, Sounds: []string{"鸟鸣", "风声"}},
	"transport_hub":      {Smells: []string{"尾气", "快餐油烟", "行李箱橡胶轮"}, Sounds: []string{"列车广播", "人流脚步声"}},
	"cultural_creative":  {Smells: []string{"咖啡", "油墨", "旧书页"}, Sounds: []string{"街头艺人", "轻声交谈"}},
	// ── 批次 20 新区基底(契约 §2.2,2–3 气味 + 1–2 环境声) ──
	"fin_sub_center":      {Smells: []string{"咖啡", "空调新风", "大理石清洁剂"}, Sounds: []string{"电梯提示音", "键盘声"}},
	"software_park":       {Smells: []string{"咖啡", "外卖盒饭", "新机箱塑料味"}, Sounds: []string{"服务器风扇", "讨论声"}},
	"airport_town":        {Smells: []string{"航油", "免税店香氛", "快餐油烟"}, Sounds: []string{"航班广播", "行李轮声"}},
	"air_logistics":       {Smells: []string{"纸箱", "缠膜塑料", "柴油"}, Sounds: []string{"叉车提示音", "扫描枪"}},
	"auto_city":           {Smells: []string{"车漆", "橡胶", "焊接烟尘"}, Sounds: []string{"总装线节拍", "试车轰鸣"}},
	"mountain_resort":     {Smells: []string{"松针", "柴火饭", "山泉水汽"}, Sounds: []string{"虫鸣", "溪水"}},
	"chem_park":           {Smells: []string{"硫化物", "氨味", "防护服胶皮"}, Sounds: []string{"管道泄压声", "罐车装卸"}},
	"agri_park":           {Smells: []string{"泥土腥", "肥料氨", "果蔬甜腐"}, Sounds: []string{"无人机植保嗡鸣", "鸡鸣"}},
	"health_town":         {Smells: []string{"艾草", "药膳汤", "樟木"}, Sounds: []string{"太极广播", "鸟雀"}},
	"steel_town":          {Smells: []string{"铁锈", "焦炭", "机油"}, Sounds: []string{"轧机声", "火车汽笛"}},
	"old_city_culture":    {Smells: []string{"香灰", "糖炒栗子", "旧木"}, Sounds: []string{"锣鼓", "叫卖"}},
	"university_town":     {Smells: []string{"食堂饭菜", "打印纸", "草坪"}, Sounds: []string{"铃声", "球场哨"}},
	"wetland_park":        {Smells: []string{"水草腥", "泥炭", "荷花气"}, Sounds: []string{"蛙鸣", "水鸟"}},
	"sports_new_city":     {Smells: []string{"塑胶跑道", "爆米花", "汗味"}, Sounds: []string{"哨声", "观众欢呼"}},
	"bay_new_town":        {Smells: []string{"海风味", "游艇蜡", "精品咖啡"}, Sounds: []string{"游艇汽笛", "露天爵士"}},
	"highspeed_rail_town": {Smells: []string{"快餐油烟", "行李箱橡胶", "消毒水"}, Sounds: []string{"进站广播", "闸机滴声"}},
}

// DistrictAmbiance 返回城区感官基底(未命中返回空)。
func DistrictAmbiance(id string) AmbianceBase {
	return districtAmbianceBase[id]
}
