// Package citysystem — dispatch_display.go: apply_display 工具的派发实现
// (2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/
//       01-方案设计.md §4.2 #13 / §4.5.1(显示原语 6 → 14) / §4.5.2(wire)。
//
// apply_display 与 focus_camera 同构:构造**中性 displayRequest** 经总线
// `run_display` 转发给 City-WebGL-Show,由渲染 Agent 翻译成 DisplayCommand,
// 本侧 parseDisplayCommands 解析后经 emitCommands 统一下发(show_command
// 单一发射点,批次81 纪律)。
//
// ⚠ 本侧**严格校验**枚举与边界(非法 ⇒ isErr 失败文本,不转发):渲染侧的
// Structured 路径对非法取值是「静默忽略」(无错误回传通道),若不在此拦截,
// 「未知 phase」会变成「什么都没发生」—— 对话即控制面必须给出确定反馈。
//
// 已知边界(跨线依赖,不在本包工作面):
//   - WS 层对 City-WebGL-Show 的结构化路径判据仍是内联的
//     `req.District != "" || req.X != nil || req.Z != nil`,未覆盖批次84 新参数
//     —— 仅带 phase/weather 等新参数的请求会落到 LLM 路径(空 text 报错)。
//     citywebgl 侧已提供 `HasStructuredParams()` 供 WS 层改用,见
//     CitySystem-Agent设计.md「已知边界」。
package citysystem

import (
	"context"
	"fmt"
	"strings"

	agentroot "LsmAgentGame/agent"
)

// applyDisplay 高级显示控制(批次84 §4.2 #13):校验 → 中性请求 → 经总线
// 转发给 City-WebGL-Show("run_display") → 解析返回的显示指令并统一下发。
//
// 校验失败一律 isErr=true + 失败文本(不转发);成功返回指令类型摘要。
func (a *Agent) applyDisplay(ctx context.Context, input map[string]any) (string, bool) {
	if a.hub == nil {
		return "失败:Agent 总线未绑定", true
	}
	req, err := buildDisplayRequest(input)
	if err != nil {
		return "失败:" + err.Error(), true
	}
	res, err := a.hub.Send(ctx, a.RoomID, string(agentroot.AgentClassCitySystem),
		agentClassCityWebGLShow, "run_display", req)
	if err != nil {
		return busFailure(err), true
	}
	cmds, err := parseDisplayCommands(res)
	if err != nil {
		return "失败:" + err.Error(), true
	}
	if len(cmds) == 0 {
		return "显示 Agent 未返回任何指令(请检查参数或稍后再试)。", false
	}
	a.emitCommands(cmds)
	return "已下发显示指令:" + describeDisplayCommands(cmds), false
}

// buildDisplayRequest 把工具入参归一化为中性 displayRequest,并做**严格校验**。
//
// 校验规则(契约 §4.5.1;非法取值一律拒绝,不静默忽略):
//   - phase / weather / speed / layer / kind / preset / mode 必须命中枚举;
//   - hour ∈ [0,24] 且为有限数;
//   - layer 与 visible 必须同给(图层显隐是一个动作的两半);
//   - entity_id 与 kind:kind 可空;给了 kind 必须命中枚举;
//   - points ∈ [2,8] 个有限 {x,z} 点;
//   - 至少给一个显示参数(全空 ⇒ 失败,防误发空指令)。
func buildDisplayRequest(input map[string]any) (displayRequest, error) {
	req := displayRequest{}
	got := false

	if v, ok := input["phase"]; ok && v != nil {
		p := normalizeEnum(toString(v), DisplayPhases)
		if p == "" {
			return req, fmt.Errorf("phase 只能 dawn|day|dusk|night(收到 %q)", toString(v))
		}
		req.Phase, got = p, true
	}
	if v, ok := input["hour"]; ok && v != nil {
		h, ok := toFloat64(v)
		if !ok || !isFinite(h) || h < hourMin || h > hourMax {
			return req, fmt.Errorf("hour 必须是 %v-%v 之间的有限数", hourMin, hourMax)
		}
		req.Hour, got = &h, true
	}
	if v, ok := input["weather"]; ok && v != nil {
		w := normalizeEnum(toString(v), DisplayWeathers)
		if w == "" {
			return req, fmt.Errorf("weather 只能 clear|rain|snow|fog|haze|sandstorm|storm(收到 %q)", toString(v))
		}
		req.Weather, got = w, true
	}
	if v, ok := input["speed"]; ok && v != nil {
		s := normalizeEnum(toString(v), DisplaySpeeds)
		if s == "" {
			return req, fmt.Errorf("speed 只能 pause|slow|normal|fast|xfast(收到 %q)", toString(v))
		}
		req.Speed, got = s, true
	}
	if v, ok := input["layer"]; ok && v != nil {
		l := normalizeEnum(toString(v), DisplayLayers)
		if l == "" {
			return req, fmt.Errorf("layer 只能 buildings|trees|vehicles|pedestrians|props|labels|water(收到 %q)", toString(v))
		}
		req.Layer = l
		got = true
	}
	if v, ok := input["visible"]; ok && v != nil {
		b, ok := toBool(v)
		if !ok {
			return req, fmt.Errorf("visible 必须是布尔值(true 显示 / false 隐藏)")
		}
		req.Visible = &b
		got = true
	}
	if (req.Layer == "") != (req.Visible == nil) {
		return req, fmt.Errorf("layer 与 visible 必须同时给出(图层显隐是一个动作的两半)")
	}
	if v, ok := input["entity_id"]; ok && v != nil {
		e := strings.TrimSpace(toString(v))
		if e == "" {
			return req, fmt.Errorf("entity_id 不能为空")
		}
		req.EntityID, got = e, true
	}
	if v, ok := input["kind"]; ok && v != nil {
		k := normalizeEnum(toString(v), DisplayEntityKinds)
		if k == "" {
			return req, fmt.Errorf("kind 只能 building|resident|vehicle(收到 %q)", toString(v))
		}
		req.Kind, got = k, true
	}
	if v, ok := input["category"]; ok && v != nil {
		c := strings.TrimSpace(toString(v))
		if c != "" {
			req.Category, got = c, true
		}
	}
	if v, ok := input["preset"]; ok && v != nil {
		p := normalizeEnum(toString(v), DisplayCameraPresets)
		if p == "" {
			return req, fmt.Errorf("preset 只能 bird|eye|street|top|free(收到 %q)", toString(v))
		}
		req.Preset, got = p, true
	}
	if v, ok := input["points"]; ok && v != nil {
		pts, err := parsePoints(v)
		if err != nil {
			return req, err
		}
		req.Points, got = pts, true
	}
	if !got {
		return req, fmt.Errorf("至少给出一个显示参数(phase/hour/weather/speed/layer+visible/entity_id/kind/category/preset/points)")
	}
	return req, nil
}

// parsePoints 解析 fly_path 的 points:2-8 个有限 {x,z} 点。
func parsePoints(raw any) ([]displayPoint, error) {
	list, ok := parsePointList(raw)
	if !ok {
		return nil, fmt.Errorf("points 必须是 %d-%d 个 {x,z} 路径点(坐标须为有限数)",
			flyPathMinPoints, flyPathMaxPoints)
	}
	if len(list) < flyPathMinPoints || len(list) > flyPathMaxPoints {
		return nil, fmt.Errorf("points 点数须在 %d-%d 之间(收到 %d)",
			flyPathMinPoints, flyPathMaxPoints, len(list))
	}
	return list, nil
}

// parsePointList 宽松解析 points 数组(兼容 []any / []map[string]any /
// []displayPoint;JSON 往返后通常是 []any[map[string]any])。任一点非法 ⇒ false。
func parsePointList(raw any) ([]displayPoint, bool) {
	var out []displayPoint
	add := func(m map[string]any) bool {
		x, xOK := toFloat64(m["x"])
		z, zOK := toFloat64(m["z"])
		if !xOK || !zOK || !isFinite(x) || !isFinite(z) {
			return false
		}
		out = append(out, displayPoint{X: x, Z: z})
		return true
	}
	switch v := raw.(type) {
	case nil:
		return nil, false
	case []displayPoint:
		for _, p := range v {
			if !isFinite(p.X) || !isFinite(p.Z) {
				return nil, false
			}
		}
		return append([]displayPoint(nil), v...), true
	case []map[string]any:
		for _, m := range v {
			if !add(m) {
				return nil, false
			}
		}
		return out, true
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			if !add(m) {
				return nil, false
			}
		}
		return out, true
	default:
		return nil, false
	}
}

// toString 宽松字符串转换(数字等非字符串转空串)。
func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// toBool 宽松布尔转换(bool → 值;JSON 往返后可能为 bool)。
func toBool(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}
