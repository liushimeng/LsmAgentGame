// Package citywebgl — prompt.go: City-WebGL-Show 渲染显示 Agent 的角色段
// 与 user 消息构造(2026-11-03 §批次81;2026-11-05 §批次84 扩到 14 原语)。
//
// 角色段经 sysprompt.EnsureHead 注入三段式头(§14.3);纪律与方案文档
// §3.3 / §4.5.1「职责边界」逐字对齐:只产出结构化显示指令、不做图形渲染、
// 指令精准克制、mode 只能 orbit|fly、annotate ≤40 字、不编造地名。
package citywebgl

import (
	"fmt"
	"strings"
)

// roleText 是 City-WebGL-Show Agent 的角色段(与设计文档「角色段要点」一致)。
//
// ⚠ 批次84 扩展了 8 个原语的取值枚举 —— 枚举值逐字来自方案文档 §4.5.1,
// 与 command.go 的 DisplayPhases / DisplayWeathers / ... 常量一一对应
// (prompt_test / tools_test 双向钉死,防「提示词与代码漂移」)。
const roleText = `你是虚拟城市的渲染显示 Agent(LsmAgentGame-City-WebGL-Show)。你的唯一职责是把「看哪里 / 怎么显示」翻译成**结构化显示指令**;你不做图形渲染、不移动相机、不绘制任何东西 —— 指令由前端 3D 引擎消费执行。

【可用显示原语(14 个)】
· 聚焦: focus_district(district) / focus_point(x,z) / focus_entity(entity_id, kind 可选)
· 相机: set_mode(mode) / camera_preset(preset) / fly_path(points)
· 标记: highlight_district(district, color 可选) / annotate(text, district 可选) / set_filter(category, mode 可选) / clear_display
· 环境: set_time_of_day(phase, hour 可选) / set_weather(weather) / set_time_speed(speed)
· 图层: set_layer_visibility(layer, visible)

【取值枚举(只能用这些,不许自造)】
· set_mode 的 mode(相机运动风格)只能 orbit|fly;
· phase 只能 dawn|day|dusk|night;hour 为 0-24 的数字;
· weather 只能 clear|rain|snow|fog|haze|sandstorm|storm;
· speed 只能 pause|slow|normal|fast|xfast;
· focus_entity 的 kind 只能 building|resident|vehicle;
· layer 只能 buildings|trees|vehicles|pedestrians|props|labels|water;
· set_filter 的 mode(筛选呈现方式)只能 highlight|dim;
· preset 只能 bird|eye|street|top|free;
· fly_path 的 points 为 2-8 个 {x,z} 世界坐标点。

【产出纪律】
1. 只调用显示原语工具产出指令;一次聚焦类请求 ≤3 条指令(通常 1-2 条),一次显示类请求(环境/图层/路径等)≤5 条指令,精准克制,禁止堆砌。
2. mode(相机运动风格)只能 orbit|fly;不确定运动风格时省略 set_mode。
3. annotate 文本 ≤40 字,简洁直白;不写链接/Markdown。
4. 不编造地名:district 只能来自请求方给出的参数或上下文中出现过的城区 id;没有就聚焦坐标或反问,绝不臆造。
5. 不编造实体与图层:id 类参数(entity_id / district)只能来自请求参数或上下文中出现过的真实 id;layer 只能用上面枚举里的图层名。绝不用占位符或「示例 id」充数。
6. set_layer_visibility 的 visible 必须是 true/false 布尔值;set_filter 的 category 留空表示取消筛选;category 支持图层名 / 物件类别(如 park、fire-station)/ 车辆类型(sedan|truck|bus|taxi)。
7. 需要清除高亮/标注时用 clear_display(产出 clear 指令),不要用注释解释。
8. 完成翻译即可停止,不解释、不闲聊;没有可翻译的内容就直接结束,不强行产出指令。`

// userPrompt 构造 LLM 路径的 user 消息(自然语言请求 + 可选结构化提示)。
func userPrompt(req DisplayRequest) string {
	var b strings.Builder
	b.WriteString("【显示请求】\n")
	b.WriteString(strings.TrimSpace(req.Text))
	if hint := structuredHint(req); hint != "" {
		b.WriteString("\n\n【请求中已带的结构化参数(优先直译,勿重复解析文本)】\n")
		b.WriteString(hint)
	}
	return b.String()
}

// structuredHint 把请求里已有的结构化参数列成提示行(全空 → "")。
//
// 批次84 扩展到 11 个新参数:phase / hour / weather / speed / entity_id /
// kind / layer / visible / category / preset / points —— 让模型**优先直译**
// 而不是从自然语言里猜(零歧义)。
func structuredHint(req DisplayRequest) string {
	lines := make([]string, 0, 14)
	if req.District != "" {
		lines = append(lines, fmt.Sprintf("district=%s", req.District))
	}
	if req.X != nil && req.Z != nil {
		lines = append(lines, fmt.Sprintf("point=(%g, %g)", *req.X, *req.Z))
	}
	if mode := NormalizeMode(req.Mode); mode != "" {
		lines = append(lines, fmt.Sprintf("mode=%s", mode))
	}
	if phase := NormalizeEnum(req.Phase, DisplayPhases); phase != "" {
		lines = append(lines, fmt.Sprintf("phase=%s", phase))
	}
	if req.Hour != nil {
		lines = append(lines, fmt.Sprintf("hour=%g", *req.Hour))
	}
	if weather := NormalizeEnum(req.Weather, DisplayWeathers); weather != "" {
		lines = append(lines, fmt.Sprintf("weather=%s", weather))
	}
	if speed := NormalizeEnum(req.Speed, DisplaySpeeds); speed != "" {
		lines = append(lines, fmt.Sprintf("speed=%s", speed))
	}
	if req.EntityID != "" {
		lines = append(lines, fmt.Sprintf("entity_id=%s", req.EntityID))
	}
	if kind := NormalizeEnum(req.Kind, DisplayEntityKinds); kind != "" {
		lines = append(lines, fmt.Sprintf("kind=%s", kind))
	}
	if layer := NormalizeEnum(req.Layer, DisplayLayers); layer != "" {
		lines = append(lines, fmt.Sprintf("layer=%s", layer))
	}
	if req.Visible != nil {
		lines = append(lines, fmt.Sprintf("visible=%t", *req.Visible))
	}
	if req.Category != "" {
		lines = append(lines, fmt.Sprintf("category=%s", req.Category))
	}
	if preset := NormalizeEnum(req.Preset, DisplayCameraPresets); preset != "" {
		lines = append(lines, fmt.Sprintf("preset=%s", preset))
	}
	if len(req.Points) > 0 {
		lines = append(lines, fmt.Sprintf("points=%d个路径点", len(req.Points)))
	}
	return strings.Join(lines, "\n")
}
