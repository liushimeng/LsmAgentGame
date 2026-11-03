// Package api — room_api_city_system_line_test.go: 建房 city_system_line
// (2026-11-03 §批次81 — LsmAgentGame-City-System 主 Agent 的 LLM 线路选择)。
//
// 语义:""/缺省 = 沿用服务端配置 model_key(旧行为,零回归);"random" =
// 每轮对话从线路池随机取线(建房弹窗默认);其余 = 固定 model key。
// 顶层优先,virtual_city 子对象亦可显式携带;其他 kind 静默忽略。
// 超长(>128 rune)→ 400 原因。
package api

import (
	"bytes"
	"encoding/json"
	"testing"

	"LsmAgentGame/service"
)

// 绑定层:顶层 / 子对象两种写法均可解码(DisallowUnknownFields 不放松)。
func TestCreateRoom_CitySystemLineBinding(t *testing.T) {
	for _, body := range []string{
		`{"city_system_line":"random"}`,
		`{"city_system_line":"DeepSeek-model"}`,
		`{"virtual_city":{"month_ms":5000,"city_system_line":"random"}}`,
	} {
		var req createRoomRequest
		dec := json.NewDecoder(bytes.NewBufferString(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			t.Fatalf("city_system_line payload must bind, body=%s err=%v", body, err)
		}
	}
	var nested createRoomRequest
	dec := json.NewDecoder(bytes.NewBufferString(`{"virtual_city":{"city_system_line":"random"}}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&nested); err != nil || nested.VirtualCity == nil ||
		nested.VirtualCity.CitySystemLine != "random" {
		t.Fatalf("nested city_system_line must decode, req=%+v err=%v", nested, err)
	}
}

// 并入语义(纯函数,handler 同路径)。
func TestMergeVirtualCityBodyFields_CitySystemLine(t *testing.T) {
	// 顶层 random → 并入。
	cfg, bad := mergeVirtualCityBodyFields(createRoomRequest{CitySystemLine: "random"}, "virtual_city", nil)
	if bad != "" || cfg == nil || cfg.CitySystemLine != "random" {
		t.Fatalf("top-level city_system_line must merge, cfg=%+v bad=%q", cfg, bad)
	}
	// 顶层优先于子对象已携带值。
	sub := &service.VirtualCityRoomOptions{CitySystemLine: "Kimi-model"}
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{CitySystemLine: "random"}, "virtual_city", sub)
	if bad != "" || cfg.CitySystemLine != "random" {
		t.Fatalf("top-level must win over nested, cfg=%+v bad=%q", cfg, bad)
	}
	// 顶层缺省 → 保留子对象值。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{}, "virtual_city",
		&service.VirtualCityRoomOptions{CitySystemLine: "Kimi-model"})
	if bad != "" || cfg.CitySystemLine != "Kimi-model" {
		t.Fatalf("nested-only value must survive, cfg=%+v bad=%q", cfg, bad)
	}
	// 缺省 → 空串(沿用服务端配置,旧行为)。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{}, "virtual_city", nil)
	if bad != "" || cfg == nil || cfg.CitySystemLine != "" {
		t.Fatalf("absent city_system_line must stay empty, cfg=%+v bad=%q", cfg, bad)
	}
	// 其他 kind 静默忽略。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{CitySystemLine: "random"}, "werewolf", nil)
	if bad != "" || cfg != nil {
		t.Fatalf("non-virtual_city kind must ignore city_system_line, cfg=%+v bad=%q", cfg, bad)
	}
	// 超长 → 400 原因。
	long := make([]rune, 129)
	for i := range long {
		long[i] = 'x'
	}
	if _, bad = mergeVirtualCityBodyFields(createRoomRequest{CitySystemLine: string(long)}, "virtual_city", nil); bad != "city_system_line too long" {
		t.Fatalf("overlong city_system_line error = %q", bad)
	}
}
