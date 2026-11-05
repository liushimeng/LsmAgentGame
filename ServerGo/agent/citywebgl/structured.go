// Package citywebgl — structured.go: 结构化快路径的指令构造
// (2026-11-05 §批次84 扩展:支持新增的 11 个结构化参数)。
//
// Structured 路径 = 参数齐备时**不过 LLM** 的零延迟翻译通道。
// 批次81 只有 district / (x,z) / mode 三个参数;批次84 追加
// phase / hour / weather / speed / entity_id / kind / layer / visible /
// category / preset / points —— 每个新参数各产出一条指令。
//
// 与 LLM 路径的语义差异(仅在**非法取值**上):
//   - LLM 路径:非法枚举 ⇒ tool_result is_error,不产指令(让模型自纠);
//   - Structured 路径:非法枚举 ⇒ **静默忽略该参数**(无错误回传通道,
//     宁可少一条指令,不可产生一条非法指令)。
package citywebgl

import (
	"strings"
)

// maxStructuredCommands 是单次 Structured 请求的指令条数上限
// (防「指令风暴」;既有 6 原语时代上限为 2,批次84 放宽到 5)。
const maxStructuredCommands = 5

// buildStructuredCommands 按固定优先级构造结构化指令序列(≤5 条)。
//
// 优先级(截断时保留靠前的):
//
//  1. focus(district 优先于 (x,z))
//  2. set_mode(相机运动风格)
//  3. set_time_of_day → set_weather → set_time_speed
//  4. focus_entity → set_layer_visibility → set_filter
//  5. camera_preset → fly_path
func buildStructuredCommands(req DisplayRequest) []DisplayCommand {
	cmds := make([]DisplayCommand, 0, maxStructuredCommands)

	// 1) 聚焦:district 优先于 (x,z)(focus_camera 契约二选一)。
	if d := strings.TrimSpace(req.District); d != "" {
		cmds = append(cmds, DisplayCommand{Type: DisplayFocusDistrict, District: d})
	} else if req.X != nil && req.Z != nil && isFinite(*req.X) && isFinite(*req.Z) {
		cmds = append(cmds, DisplayCommand{Type: DisplayFocusPoint, X: *req.X, Z: *req.Z})
	}

	// 2) 相机运动风格(mode ∈ orbit|fly)。
	//    ⚠ mode 是上下文相关的扁平字段:当它取 highlight|dim 时属于
	//    set_filter 的呈现方式(见 §4),此时不产 set_mode。
	cameraMode := NormalizeMode(req.Mode)
	filterMode := ""
	switch cameraMode {
	case "":
		if m := NormalizeEnum(req.Mode, DisplayFilterModes); m != "" {
			filterMode = m
		}
	default:
		cmds = append(cmds, DisplayCommand{Type: DisplaySetMode, Mode: cameraMode})
	}

	// 3) 时间 / 天气 / 流速。
	if phase := NormalizeEnum(req.Phase, DisplayPhases); phase != "" {
		cmd := DisplayCommand{Type: DisplaySetTimeOfDay, Phase: phase}
		if req.Hour != nil && isFinite(*req.Hour) && *req.Hour >= hourMin && *req.Hour <= hourMax {
			h := *req.Hour
			cmd.Hour = &h
		}
		cmds = append(cmds, cmd)
	}
	if weather := NormalizeEnum(req.Weather, DisplayWeathers); weather != "" {
		cmds = append(cmds, DisplayCommand{Type: DisplaySetWeather, Weather: weather})
	}
	if speed := NormalizeEnum(req.Speed, DisplaySpeeds); speed != "" {
		cmds = append(cmds, DisplayCommand{Type: DisplaySetTimeSpeed, Speed: speed})
	}

	// 4) 实体 / 图层 / 筛选。
	if entityID := strings.TrimSpace(req.EntityID); entityID != "" {
		cmd := DisplayCommand{Type: DisplayFocusEntity, EntityID: entityID}
		if kind := NormalizeEnum(req.Kind, DisplayEntityKinds); kind != "" {
			cmd.Kind = kind
		}
		cmds = append(cmds, cmd)
	}
	if layer := NormalizeEnum(req.Layer, DisplayLayers); layer != "" && req.Visible != nil {
		v := *req.Visible
		cmds = append(cmds, DisplayCommand{Type: DisplaySetLayerVisibility, Layer: layer, Visible: &v})
	}
	if category := strings.TrimSpace(req.Category); category != "" {
		mode := filterMode
		if mode == "" {
			mode = DisplayFilterModes[0] // 默认 highlight
		}
		cmds = append(cmds, DisplayCommand{Type: DisplaySetFilter, Category: category, Mode: mode})
	}

	// 5) 机位预设 / 飞行路径。
	if preset := NormalizeEnum(req.Preset, DisplayCameraPresets); preset != "" {
		cmds = append(cmds, DisplayCommand{Type: DisplayCameraPreset, Preset: preset})
	}
	if points, ok := parsePoints(req.Points); ok {
		cmds = append(cmds, DisplayCommand{Type: DisplayFlyPath, Points: points})
	}

	if len(cmds) > maxStructuredCommands {
		cmds = cmds[:maxStructuredCommands]
	}
	return cmds
}
