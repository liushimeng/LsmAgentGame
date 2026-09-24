// Package api — room_api_election_test.go: 建房 body.civic_election_enabled
// 参数链单测(2026-09-24 §批次20 文档3 A2)。下游:
// service.VirtualCityRoomOptions → wealth.applyOpts → World.Election.Enabled
// (接线断言见 game/virtual_city/civic_election_test.go)。
package api

import (
	"bytes"
	"encoding/json"
	"testing"

	"LsmAgentGame/service"
)

// TestCreateRoom_ElectionBodyBinding 顶层与嵌套两种载荷均绑定成功
// (DisallowUnknownFields 下新字段必须登记;false 缺省兼容旧客户端)。
func TestCreateRoom_ElectionBodyBinding(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{`{"civic_election_enabled":true}`, true},
		{`{}`, false},
		{`{"civic_election_enabled":false}`, false},
		{`{"resident_count":200,"civic_election_enabled":true}`, true},
	}
	for _, c := range cases {
		var req createRoomRequest
		dec := json.NewDecoder(bytes.NewBufferString(c.body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			t.Fatalf("body %s must bind: %v", c.body, err)
		}
		if req.CivicElectionEnabled != c.want {
			t.Errorf("body %s: got %v, want %v", c.body, req.CivicElectionEnabled, c.want)
		}
	}
	// 嵌套 wealth 子对象亦可携带。
	var req createRoomRequest
	if err := json.Unmarshal([]byte(`{"virtual_city":{"civic_election_enabled":true,"month_ms":3000}}`), &req); err != nil {
		t.Fatalf("nested payload: %v", err)
	}
	if req.VirtualCity == nil || !req.VirtualCity.CivicElectionEnabled {
		t.Fatalf("nested wealth.civic_election_enabled lost: %+v", req.VirtualCity)
	}
}

// TestMergeVirtualCityBodyFields 并入语义表驱动(纯函数,覆盖 handler 分支)。
func TestMergeVirtualCityBodyFields(t *testing.T) {
	// 顶层 true → 构造 cfg 并置位。
	cfg, bad := mergeVirtualCityBodyFields(createRoomRequest{CivicElectionEnabled: true}, "virtual_city", nil)
	if bad != "" || cfg == nil || !cfg.CivicElectionEnabled {
		t.Fatalf("top-level true: cfg=%+v bad=%q", cfg, bad)
	}
	// 缺省 false + 无 wealth → 仍 nil(不构造;旧行为零改动)。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{}, "virtual_city", nil)
	if bad != "" || cfg != nil {
		t.Fatalf("all absent must keep nil: %+v %q", cfg, bad)
	}
	// 嵌套显式携带不受影响。
	nested := &service.VirtualCityRoomOptions{MonthMs: 3000, CivicElectionEnabled: true}
	cfg, _ = mergeVirtualCityBodyFields(createRoomRequest{}, "virtual_city", nested)
	if cfg != nested || !cfg.CivicElectionEnabled {
		t.Fatalf("nested must pass through: %+v", cfg)
	}
	// resident_count 与 election 同段共存(契约验收 E2E 载荷形态)。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{ResidentCount: 200, CivicElectionEnabled: true}, "virtual_city", nil)
	if bad != "" || cfg == nil || cfg.ResidentCount != 200 || !cfg.CivicElectionEnabled {
		t.Fatalf("combined fields: %+v %q", cfg, bad)
	}
	// 负数 resident 400 分支保留(既有契约 04 §1.1)。
	if _, bad = mergeVirtualCityBodyFields(createRoomRequest{ResidentCount: -1}, "virtual_city", nil); bad == "" {
		t.Error("negative resident_count must still yield 400 reason")
	}
	// 非 wealth kind 静默忽略。
	cfg, bad = mergeVirtualCityBodyFields(createRoomRequest{CivicElectionEnabled: true, ResidentCount: 5}, "werewolf", nil)
	if bad != "" || cfg != nil {
		t.Errorf("non-wealth kind must ignore wealth fields: %+v %q", cfg, bad)
	}
}
