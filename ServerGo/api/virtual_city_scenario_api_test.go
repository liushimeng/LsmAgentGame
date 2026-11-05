// Package api — virtual_city_scenario_api_test.go: 虚拟城市场景目录检索端点
// 契约测试(2026-11-05 §批次84)。
//
// 覆盖:
//
//	① 无参返回:含 scenarios / dimensions / total 三个键;
//	② q 过滤:关键词命中收窄结果;
//	③ group 过滤:只命中该组;
//	④ limit 上限:≤200;
//	⑤ 非法 limit 归一:非数字 ⇒ 走缺省 30;
//	⑥ 未知 group ⇒ code 35064。
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"

	"github.com/gin-gonic/gin"
)

// scenarioCatalogRouter 挂载场景目录端点(绕过鉴权中间件 —— 与其它
// virtual_city 端点同口径:鉴权由 router.go 的 AuthRequired 统一保证)。
func scenarioCatalogRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	a := NewVirtualCityScenarioAPI(nil)
	r := gin.New()
	r.GET("/api/games/virtual_city/scenario_catalog", a.Catalog)
	return r
}

// scenarioCatalogResp 是 data 层载荷(兼容新旧两种形状)。
type scenarioCatalogResp struct {
	Scenarios  []virtual_city.ScenarioSpec      `json:"scenarios"`
	Items      []virtual_city.ScenarioSpec      `json:"items"`
	Dimensions []virtual_city.ScenarioDimension `json:"dimensions"`
	Total      int                              `json:"total"`
}

// getScenarioCatalog 打一次目录端点并解析(code + data)。
func getScenarioCatalog(t *testing.T, query string) (int, scenarioCatalogResp) {
	t.Helper()
	r := scenarioCatalogRouter()
	url := "/api/games/virtual_city/scenario_catalog"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("http status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Code    int                 `json:"code"`
		Message string              `json:"message"`
		Data    scenarioCatalogResp `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json %q: %v", w.Body.String(), err)
	}
	return body.Code, body.Data
}

// TestVirtualCityScenarioCatalog_Default ①:无参返回含 scenarios/dimensions/total。
func TestVirtualCityScenarioCatalog_Default(t *testing.T) {
	code, data := getScenarioCatalog(t, "")
	if code != errcode.OK {
		t.Fatalf("code = %d, want 0", code)
	}
	if data.Scenarios == nil {
		t.Fatal("data.scenarios missing")
	}
	if data.Items == nil {
		t.Fatal("data.items missing")
	}
	if data.Dimensions == nil {
		t.Fatal("data.dimensions missing")
	}
	if data.Total <= 0 {
		t.Fatalf("data.total = %d, want > 0", data.Total)
	}
	// total = 目录实际条目数(受 max_entries 截断后的真实值)。
	if data.Total != virtual_city.ScenarioCatalogCount() {
		t.Fatalf("total = %d, want %d", data.Total, virtual_city.ScenarioCatalogCount())
	}
	// scenarios 与 items 内容一致(新旧形状兼容)。
	if len(data.Scenarios) != len(data.Items) {
		t.Fatalf("scenarios=%d items=%d, must be identical", len(data.Scenarios), len(data.Items))
	}
	// 默认 limit 30。
	if len(data.Scenarios) != 30 {
		t.Fatalf("default scenarios len = %d, want 30", len(data.Scenarios))
	}
	// dimensions = 13 组。
	if len(data.Dimensions) != 13 {
		t.Fatalf("dimensions len = %d, want 13", len(data.Dimensions))
	}
}

// TestVirtualCityScenarioCatalog_QueryFilter ②:q 过滤收窄结果。
func TestVirtualCityScenarioCatalog_QueryFilter(t *testing.T) {
	_, all := getScenarioCatalog(t, "limit=200")
	_, hit := getScenarioCatalog(t, "q="+g("地震")+"&limit=200")
	if len(hit.Scenarios) == 0 {
		t.Fatal("q=地震 returned no hits")
	}
	if len(hit.Scenarios) >= len(all.Scenarios) {
		t.Fatalf("q filter did not narrow: %d >= %d", len(hit.Scenarios), len(all.Scenarios))
	}
	for _, s := range hit.Scenarios {
		blob := s.Kind + s.Name + s.Description + s.Group
		if !containsFold(blob, "地震") {
			t.Fatalf("q=地震 hit %q which lacks the term", s.Kind)
		}
	}
}

// TestVirtualCityScenarioCatalog_GroupFilter ③:group 过滤只命中该组。
func TestVirtualCityScenarioCatalog_GroupFilter(t *testing.T) {
	code, data := getScenarioCatalog(t, "group="+g(virtual_city.ScenarioGroupPublicHealth)+"&limit=200")
	if code != errcode.OK {
		t.Fatalf("code = %d, want 0", code)
	}
	if len(data.Scenarios) == 0 {
		t.Fatal("group=公共卫生 returned no hits")
	}
	for _, s := range data.Scenarios {
		if s.Group != virtual_city.ScenarioGroupPublicHealth {
			t.Fatalf("group filter leaked %q (group %q)", s.Kind, s.Group)
		}
	}
}

// TestVirtualCityScenarioCatalog_LimitCap ④+⑤:limit 上限 200;非法 limit 归一。
func TestVirtualCityScenarioCatalog_LimitCap(t *testing.T) {
	_, capped := getScenarioCatalog(t, "limit=9999")
	if len(capped.Scenarios) > 200 {
		t.Fatalf("limit cap failed: %d > 200", len(capped.Scenarios))
	}
	// 非法 limit(非数字)⇒ 归一为缺省 30。
	_, bad := getScenarioCatalog(t, "limit=abc")
	if len(bad.Scenarios) != 30 {
		t.Fatalf("invalid limit len = %d, want 30", len(bad.Scenarios))
	}
	// 显式小 limit 生效。
	_, small := getScenarioCatalog(t, "limit=5")
	if len(small.Scenarios) != 5 {
		t.Fatalf("limit=5 len = %d, want 5", len(small.Scenarios))
	}
}

// TestVirtualCityScenarioCatalog_UnknownGroup ⑥:未知 group ⇒ code 35064。
func TestVirtualCityScenarioCatalog_UnknownGroup(t *testing.T) {
	code, data := getScenarioCatalog(t, "group=不存在的分组")
	if code != errcode.ErrVirtualCityScenarioGroupUnknown {
		t.Fatalf("code = %d, want %d", code, errcode.ErrVirtualCityScenarioGroupUnknown)
	}
	if len(data.Scenarios) != 0 {
		t.Fatalf("unknown group scenarios = %d, want 0", len(data.Scenarios))
	}
}

// g 是 query 参数编码(中文组名需转义)。
func g(s string) string { return url.QueryEscape(s) }

// containsFold 大小写不敏感子串匹配。
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// TestVirtualCityScenarioCatalog_LimitParse ⑤ 补充:负数 limit 归一为缺省。
func TestVirtualCityScenarioCatalog_LimitParse(t *testing.T) {
	_, neg := getScenarioCatalog(t, "limit=-5")
	if len(neg.Scenarios) != 30 {
		t.Fatalf("negative limit len = %d, want 30", len(neg.Scenarios))
	}
	// strconv 可解析的极限值也不会越界(服务端 clamp)。
	_, max := getScenarioCatalog(t, "limit="+strconv.Itoa(1<<31-1))
	if len(max.Scenarios) > 200 {
		t.Fatalf("huge limit len = %d, want <= 200", len(max.Scenarios))
	}
}

// dimensionCount 返回维度索引中指定组的 count(未命中返回 -1)。
func dimensionCount(dims []virtual_city.ScenarioDimension, group string) int {
	for _, d := range dims {
		if d.Group == group {
			return d.Count
		}
	}
	return -1
}

// TestVirtualCityScenarioCatalog_TotalFiltered ⑦ total = 匹配 group/q 的
// 条目总数(limit 截断前);跟随过滤、不受 limit 影响(契约 §7.2)。
func TestVirtualCityScenarioCatalog_TotalFiltered(t *testing.T) {
	// group 过滤:total == 该组命中数 == dimensions[该组].count;limit=5 只截列表。
	code, data := getScenarioCatalog(t, "group="+g(virtual_city.ScenarioGroupPublicHealth)+"&limit=5")
	if code != errcode.OK {
		t.Fatalf("code = %d, want 0", code)
	}
	if data.Total != 264 {
		t.Fatalf("group=公共卫生 total = %d, want 264(该组命中数)", data.Total)
	}
	if dim := dimensionCount(data.Dimensions, virtual_city.ScenarioGroupPublicHealth); data.Total != dim {
		t.Fatalf("group=公共卫生 total = %d != dimensions count %d", data.Total, dim)
	}
	if len(data.Scenarios) != 5 {
		t.Fatalf("limit=5 scenarios = %d, want 5(total 不受 limit 影响)", len(data.Scenarios))
	}

	// q 过滤:total == 命中条数(> 0);scenarios == min(total, limit)。
	_, hit := getScenarioCatalog(t, "q="+g("地震")+"&limit=200")
	if hit.Total <= 0 {
		t.Fatal("q=地震 total must be > 0")
	}
	wantItems := hit.Total
	if wantItems > 200 {
		wantItems = 200
	}
	if len(hit.Scenarios) != wantItems {
		t.Fatalf("q=地震 scenarios = %d, want min(total,200) = %d",
			len(hit.Scenarios), wantItems)
	}

	// limit 不影响 total:limit=5 与 limit=9999(钳 200)的 total 同为全量。
	_, small := getScenarioCatalog(t, "limit=5")
	_, big := getScenarioCatalog(t, "limit=9999")
	if small.Total != 1203 {
		t.Fatalf("limit=5 total = %d, want 1203(limit 不得缩减 total)", small.Total)
	}
	if big.Total != 1203 {
		t.Fatalf("limit=9999 total = %d, want 1203", big.Total)
	}
	if len(small.Scenarios) != 5 {
		t.Fatalf("limit=5 scenarios = %d, want 5", len(small.Scenarios))
	}

	// 无参 ⇒ total = 全目录条目数。
	_, def := getScenarioCatalog(t, "")
	if def.Total != 1203 {
		t.Fatalf("no-param total = %d, want 1203", def.Total)
	}
}

// TestVirtualCityScenarioCatalog_TotalPerGroup ⑧ 13 个组逐个断言
// total == dimensions[group].count(group 过滤命中数与维度索引同口径)。
func TestVirtualCityScenarioCatalog_TotalPerGroup(t *testing.T) {
	_, base := getScenarioCatalog(t, "limit=1")
	if len(base.Dimensions) != 13 {
		t.Fatalf("dimensions len = %d, want 13", len(base.Dimensions))
	}
	for _, dim := range base.Dimensions {
		_, data := getScenarioCatalog(t, "group="+g(dim.Group)+"&limit=1")
		if data.Total != dim.Count {
			t.Errorf("group %s total = %d, want dimensions count %d",
				dim.Group, data.Total, dim.Count)
		}
		if len(data.Scenarios) != 1 {
			t.Errorf("group %s limit=1 scenarios = %d, want 1",
				dim.Group, len(data.Scenarios))
		}
	}
}
