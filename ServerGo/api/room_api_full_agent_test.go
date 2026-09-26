// Package api — room_api_full_agent_test.go: 建房 body.full_agent 三态接线单测
// (2026-09-26 §批次25 §3.2;原 createRoomRequest.FullAgent 声明后从不被读取,
// §130 修复)。下游:service.VirtualCityRoomOptions.FullAgent →
// wealth.applyOpts(FullAgentMode)+ prepareVirtualCityAgentRoom(SetFullAgentMode)。
package api

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestCreateRoom_FullAgentBodyBinding full_agent 三态绑定与并入:
// 显式 false → cfg.FullAgent=&false(允许人类入座);true/nil(缺省)→ 全 Agent。
func TestCreateRoom_FullAgentBodyBinding(t *testing.T) {
	falseVal := false
	trueVal := true
	cases := []struct {
		name string
		body string
		want *bool // cfg.FullAgent 期望(nil = 未构造)
	}{
		{"explicit false allows humans", `{"full_agent":false}`, &falseVal},
		{"explicit true stays full agent", `{"full_agent":true}`, &trueVal},
		{"absent keeps default (nil)", `{}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req createRoomRequest
			dec := json.NewDecoder(bytes.NewBufferString(tc.body))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				t.Fatalf("decode %s: %v", tc.body, err)
			}
			cfg, bad := mergeVirtualCityBodyFields(req, "virtual_city", nil)
			if bad != "" {
				t.Fatalf("merge: %s", bad)
			}
			if tc.want == nil {
				if cfg != nil && cfg.FullAgent != nil {
					t.Fatalf("cfg.FullAgent = %v, want nil (未传不构造)", *cfg.FullAgent)
				}
				return
			}
			if cfg == nil || cfg.FullAgent == nil || *cfg.FullAgent != *tc.want {
				t.Fatalf("cfg.FullAgent = %+v, want %v", cfg, *tc.want)
			}
		})
	}
	// 非 wealth kind 静默忽略。
	cfg, bad := mergeVirtualCityBodyFields(createRoomRequest{FullAgent: &falseVal}, "werewolf", nil)
	if bad != "" || cfg != nil {
		t.Fatalf("non-wealth kind must ignore full_agent: cfg=%+v bad=%q", cfg, bad)
	}
}
