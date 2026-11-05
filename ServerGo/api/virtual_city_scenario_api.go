// Package api — virtual_city_scenario_api.go: 虚拟城市场景推演 / 预测运行 HTTP 入口
// (2026-10-21 §场景推演)。
//
// 契约: tmpPlan/虚拟城市-模拟程序化与预测运行优化_20261021_095114.md §5.1。
//
//	GET  /api/games/virtual_city/scenario_catalog        场景目录(10 个)
//	POST /api/games/virtual_city/rooms/:id/scenario      注入场景(条件触发者)
//	GET  /api/games/virtual_city/rooms/:id/scenarios     最近 20 条注入记录
//
// 权限:任意登录用户(观察者 = 条件触发者;AuthRequired 中间件保证登录态),
// 与 social survey 同口径。房间不存在 → 35001;业务校验全部在
// virtual_city.VirtualCityRoom.ApplyScenario(持锁)。
package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"

	"github.com/gin-gonic/gin"
)

// VirtualCityScenarioAPI 是场景推演三端点的处理器。
type VirtualCityScenarioAPI struct {
	rooms VirtualCityRoomSource // 复用 survey API 的窄接口(main.go 注入 vcMgr)
}

// NewVirtualCityScenarioAPI 构造(rooms 由 main.go 注入 vcMgr)。
func NewVirtualCityScenarioAPI(rooms VirtualCityRoomSource) *VirtualCityScenarioAPI {
	return &VirtualCityScenarioAPI{rooms: rooms}
}

// scenarioCatalogConfigOnce 目录配置只应用一次(装配层缓存,重复调用无意义)。
var scenarioCatalogConfigOnce sync.Once

// applyScenarioCatalogConfig 把 config.scenario_catalog{} 的 max_entries /
// search_limit 应用到目录装配层(缺失 ⇒ 装配层缺省 10000 / 30)。
//
// config.Load() 在配置文件缺失时可能 panic(测试/早期 bootstrap)⇒ recover
// 兜底走装配层缺省(照抄 game/werewolf 的安全读取模式)。
func applyScenarioCatalogConfig() {
	scenarioCatalogConfigOnce.Do(func() {
		defer func() { _ = recover() }()
		cfg := config.Load().ScenarioCatalog.Resolved()
		virtual_city.SetScenarioCatalogLimits(cfg.MaxEntries, cfg.SearchLimit)
	})
}

// injectScenarioReq POST body(契约 §5.1)。
//
// magnitude 缺省/0 = 场景目录 default_magnitude;duration_months 缺省/0 =
// 场景目录默认持续月数。target 仅 goods_price_shock(goods id)与
// policy_shock(tight|loose)消费,其余场景宽松忽略。
type injectScenarioReq struct {
	Kind           string `json:"kind"`
	Magnitude      int    `json:"magnitude"`
	DurationMonths int    `json:"duration_months"`
	Target         string `json:"target"`
	Note           string `json:"note"`
}

// Catalog 处理 GET /api/games/virtual_city/scenario_catalog。
// 与房间无关(全局目录),登录即可读。
//
// 批次84(2026-11-05 §批次84 §7.2)扩展:支持 group / q / limit 检索参数,
// 响应在既有 `scenarios` 之上追加 `dimensions` 与 `total`:
//
//	scenarios  受 group/q/limit 过滤后的条目(**保留既有字段名**,向后兼容)
//	items      同 scenarios 的别名(批次84 §5.1 前端 normalizeScenarioCatalog
//	           在新形状分支读 data.items;两个键内容逐字相同,兼容新旧两种消费方)
//	dimensions 维度索引(13 组,每组 count + ≤3 个代表 kind;恒定体积)
//	total      匹配 group/q 的条目总数(**limit 截断前**;不受 limit 影响),
//	           用于分页 —— 无参时 = 全目录条目数(受 scenario_catalog.max_entries
//	           截断后的真实值);单组规模另见 dimensions[].count(契约 §7.2)
//
// 参数语义与 virtual_city.ScenarioCatalogSearch 一致:
//   - group:空 = 全部;未知分组 ⇒ code 35064;
//   - q:kind/name/description/group 子串匹配(大小写不敏感);
//   - limit:<=0 ⇒ 缺省 30(config.scenario_catalog.search_limit);上限 200。
func (a *VirtualCityScenarioAPI) Catalog(c *gin.Context) {
	// 批次84:首次访问把 config.scenario_catalog{} 的上限/默认条数落到装配层。
	applyScenarioCatalogConfig()
	group := c.Query("group")
	q := c.Query("q")
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			// 非法 limit(非数字)⇒ 归一为缺省(0 ⇒ 下游走缺省 30)。
			n = 0
		}
		limit = n
	}
	// total = 命中条数(group/q 过滤后、limit 截断前);顺带完成分组名校验。
	total, terr := virtual_city.ScenarioCatalogMatchCount(q, group)
	if terr != nil {
		ee := errcode.AsError(terr)
		c.JSON(http.StatusOK, gin.H{"code": ee.Code, "message": ee.Message})
		return
	}
	items, e := virtual_city.ScenarioCatalogSearchErr(q, group, limit)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	if items == nil {
		items = []virtual_city.ScenarioSpec{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"scenarios":  items,
			"items":      items,
			"dimensions": virtual_city.ScenarioDimensionIndex(),
			"total":      total,
		},
	})
}

// Inject 处理 POST /api/games/virtual_city/rooms/:id/scenario。
func (a *VirtualCityScenarioAPI) Inject(c *gin.Context) {
	roomID := c.Param("id")
	if a.rooms == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "virtual_city manager not wired"})
		return
	}
	r := a.rooms.Get(roomID)
	if r == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrVirtualCityRoomNotFound,
			"message": errcode.DefaultMessages[errcode.ErrVirtualCityRoomNotFound],
		})
		return
	}
	var req injectScenarioReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrValidationFailed, "message": "invalid scenario payload: " + err.Error()})
		return
	}
	rec, effects, e := r.ApplyScenario(req.Kind, req.Magnitude, req.DurationMonths, req.Target, req.Note)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	if effects == nil {
		effects = []virtual_city.ScenarioEffect{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"scenario": rec, "effects": effects},
	})
}

// List 处理 GET /api/games/virtual_city/rooms/:id/scenarios(最近 20 条,
// 旧→新)。
func (a *VirtualCityScenarioAPI) List(c *gin.Context) {
	roomID := c.Param("id")
	if a.rooms == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "virtual_city manager not wired"})
		return
	}
	r := a.rooms.Get(roomID)
	if r == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrVirtualCityRoomNotFound,
			"message": errcode.DefaultMessages[errcode.ErrVirtualCityRoomNotFound],
		})
		return
	}
	list := r.ListScenarios()
	if list == nil {
		list = []virtual_city.ScenarioAppliedRecord{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"scenarios": list},
	})
}
