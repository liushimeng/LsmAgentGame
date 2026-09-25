// Package api — room_api_llm_lines_test.go: 建房 llm_lines 请求体校验
// (2026-09-25 §LLM线路池配额)。
//
// 负数 → 400;0(缺省)= 未指定(不进配置分支,零回归);正数并入
// VirtualCity.LLMLines 透传(顶层优先,virtual_city 子对象亦可显式携带);
// 其他 kind 静默忽略。[1,64] clamp 与池总量取 min 在 game 层覆盖(见
// game/virtual_city/room_city_test.go TestManager_ApplyRoomOptions_LLMLines)。
//
// 正数透传的 HTTP 级 200 断言不可行:postCreateRoom helper 的 svc 为 nil
// (与 room_api_resident_test.go 同款限制 —— 通过校验分支后在
// CreateRoomWithAgents 处 panic),故透传语义经纯函数
// mergeVirtualCityBodyFields 钉死(handler 同路径)。
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"LsmAgentGame/service"
)

// 负数 llm_lines → HTTP 400 且 message 指明字段(与 resident_count 同款)。
func TestCreateRoom_LLMLinesNegativeRejected(t *testing.T) {
	w := postCreateRoom(t, "virtual_city", `{"llm_lines":-1}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("negative llm_lines must 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "llm_lines") {
		t.Fatalf("400 message must name llm_lines, body=%s", w.Body.String())
	}
}

// 绑定层(与 handler 相同的 DisallowUnknownFields 解码):顶层 llm_lines 与
// virtual_city 子对象 llm_lines 均可解码;未知顶层字段仍 400(语义不放松)。
func TestCreateRoom_LLMLinesBindingTolerances(t *testing.T) {
	for _, body := range []string{
		`{"llm_lines":3}`,
		`{"llm_lines":0}`,
		`{"virtual_city":{"month_ms":5000,"llm_lines":5}}`,
	} {
		var req createRoomRequest
		dec := json.NewDecoder(bytes.NewBufferString(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			t.Fatalf("llm_lines payload must bind, body=%s err=%v", body, err)
		}
	}
	// 子对象路径直解到 VirtualCity.LLMLines(json tag 透传)。
	var nested createRoomRequest
	dec := json.NewDecoder(bytes.NewBufferString(`{"virtual_city":{"llm_lines":5}}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&nested); err != nil || nested.VirtualCity == nil || nested.VirtualCity.LLMLines != 5 {
		t.Fatalf("nested virtual_city.llm_lines must decode to LLMLines=5, req=%+v err=%v", nested, err)
	}
	// 未知顶层字段仍 400。
	var req createRoomRequest
	dec2 := json.NewDecoder(bytes.NewBufferString(`{"no_such_field":1}`))
	dec2.DisallowUnknownFields()
	if err := dec2.Decode(&req); err == nil {
		t.Fatal("unknown top-level field must still be rejected")
	}
}

// 并入语义(纯函数,handler 同路径):正数透传 cfg.LLMLines(顶层优先);
// 0/缺省不构造配置对象;其他 kind(如 werewolf)带任意 llm_lines 静默忽略
// 不 400;负数(virtual_city)返回 400 原因字符串。
func TestMergeVirtualCityBodyFields_LLMLines(t *testing.T) {
	// 正数 → 并入(nil cfg 就地构造)。
	cfg, bad := mergeVirtualCityBodyFields(createRoomRequest{LLMLines: 3}, "virtual_city", nil)
	if bad != "" || cfg == nil || cfg.LLMLines != 3 {
		t.Fatalf("positive llm_lines must merge into cfg, cfg=%+v bad=%q", cfg, bad)
	}
	// 顶层优先于子对象已携带值。
	sub := &service.VirtualCityRoomOptions{LLMLines: 7}
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{LLMLines: 3}, "virtual_city", sub)
	if bad != "" || cfg.LLMLines != 3 {
		t.Fatalf("top-level llm_lines must win over nested, cfg=%+v bad=%q", cfg, bad)
	}
	// 0/缺省 → 不动作(cfg 保持 nil,零回归)。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{}, "virtual_city", nil)
	if bad != "" || cfg != nil {
		t.Fatalf("absent llm_lines must not construct cfg, cfg=%+v bad=%q", cfg, bad)
	}
	// 其他 kind(如 werewolf)负数也静默忽略(不 400)。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{LLMLines: -1}, "werewolf", nil)
	if bad != "" || cfg != nil {
		t.Fatalf("non-virtual_city kind must silently ignore llm_lines, cfg=%+v bad=%q", cfg, bad)
	}
	// 负数(virtual_city)→ handler 400 原因。
	if _, bad = mergeVirtualCityBodyFields(createRoomRequest{LLMLines: -1}, "virtual_city", nil); bad != "llm_lines must be >= 0" {
		t.Fatalf("negative llm_lines error = %q, want llm_lines must be >= 0", bad)
	}
}
