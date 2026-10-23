// Package api — room_api_city_chrome_line_test.go: 建房 city_chrome_line
// (2026-10-23 §批次79 — LsmAgentGame-City-Chrome 的 LLM 线路选择)。
//
// 语义:""/缺省 = 沿用服务端配置 model_key(旧行为,零回归);"random" =
// 每个 mission 从线路池随机取线(建房弹窗默认);其余 = 固定 model key。
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
func TestCreateRoom_CityChromeLineBinding(t *testing.T) {
	for _, body := range []string{
		`{"city_chrome_line":"random"}`,
		`{"city_chrome_line":"DeepSeek-model"}`,
		`{"virtual_city":{"month_ms":5000,"city_chrome_line":"random"}}`,
	} {
		var req createRoomRequest
		dec := json.NewDecoder(bytes.NewBufferString(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			t.Fatalf("city_chrome_line payload must bind, body=%s err=%v", body, err)
		}
	}
	var nested createRoomRequest
	dec := json.NewDecoder(bytes.NewBufferString(`{"virtual_city":{"city_chrome_line":"random"}}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&nested); err != nil || nested.VirtualCity == nil ||
		nested.VirtualCity.CityChromeLine != "random" {
		t.Fatalf("nested city_chrome_line must decode, req=%+v err=%v", nested, err)
	}
}

// 并入语义(纯函数,handler 同路径)。
func TestMergeVirtualCityBodyFields_CityChromeLine(t *testing.T) {
	// 顶层 random → 并入。
	cfg, bad := mergeVirtualCityBodyFields(createRoomRequest{CityChromeLine: "random"}, "virtual_city", nil)
	if bad != "" || cfg == nil || cfg.CityChromeLine != "random" {
		t.Fatalf("top-level city_chrome_line must merge, cfg=%+v bad=%q", cfg, bad)
	}
	// 顶层优先于子对象已携带值。
	sub := &service.VirtualCityRoomOptions{CityChromeLine: "Kimi-model"}
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{CityChromeLine: "random"}, "virtual_city", sub)
	if bad != "" || cfg.CityChromeLine != "random" {
		t.Fatalf("top-level must win over nested, cfg=%+v bad=%q", cfg, bad)
	}
	// 顶层缺省 → 保留子对象值。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{}, "virtual_city",
		&service.VirtualCityRoomOptions{CityChromeLine: "Kimi-model"})
	if bad != "" || cfg.CityChromeLine != "Kimi-model" {
		t.Fatalf("nested-only value must survive, cfg=%+v bad=%q", cfg, bad)
	}
	// 缺省 → 空串(沿用服务端配置,旧行为)。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{}, "virtual_city", nil)
	if bad != "" || cfg == nil || cfg.CityChromeLine != "" {
		t.Fatalf("absent city_chrome_line must stay empty, cfg=%+v bad=%q", cfg, bad)
	}
	// 其他 kind 静默忽略。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{CityChromeLine: "random"}, "werewolf", nil)
	if bad != "" || cfg != nil {
		t.Fatalf("non-virtual_city kind must ignore city_chrome_line, cfg=%+v bad=%q", cfg, bad)
	}
	// 超长 → 400 原因。
	long := make([]rune, 129)
	for i := range long {
		long[i] = 'x'
	}
	if _, bad = mergeVirtualCityBodyFields(createRoomRequest{CityChromeLine: string(long)}, "virtual_city", nil); bad != "city_chrome_line too long" {
		t.Fatalf("overlong city_chrome_line error = %q", bad)
	}
}
