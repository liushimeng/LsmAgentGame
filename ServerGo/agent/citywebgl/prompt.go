// Package citywebgl — prompt.go: City-WebGL-Show 渲染显示 Agent 的角色段
// 与 user 消息构造(2026-11-03 §批次81)。
//
// 角色段经 sysprompt.EnsureHead 注入三段式头(§14.3);纪律与方案文档
// §3.3「职责边界」逐字对齐:只产出结构化显示指令、不做图形渲染、
// 指令精准克制、mode 只能 orbit|fly、annotate ≤40 字、不编造地名。
package citywebgl

import (
	"fmt"
	"strings"
)

// roleText 是 City-WebGL-Show Agent 的角色段(与设计文档「角色段要点」一致)。
const roleText = `你是虚拟城市的渲染显示 Agent(LsmAgentGame-City-WebGL-Show)。你的唯一职责是把「看哪里 / 怎么显示」翻译成**结构化显示指令**;你不做图形渲染、不移动相机、不绘制任何东西 —— 指令由前端 3D 引擎消费执行。

【产出纪律】
1. 只调用显示原语工具产出指令;一次聚焦类请求 ≤3 条指令(通常 1-2 条),精准克制,禁止堆砌。
2. mode 只能 orbit|fly;不确定运动风格时省略 set_mode。
3. annotate 文本 ≤40 字,简洁直白;不写链接/Markdown。
4. 不编造地名:district 只能来自请求方给出的参数或上下文中出现过的城区 id;没有就聚焦坐标或反问,绝不臆造。
5. 需要清除高亮/标注时用 clear_display(产出 clear 指令),不要用注释解释。
6. 完成翻译即可停止,不解释、不闲聊;没有可翻译的内容就直接结束,不强行产出指令。`

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
func structuredHint(req DisplayRequest) string {
	lines := make([]string, 0, 4)
	if req.District != "" {
		lines = append(lines, fmt.Sprintf("district=%s", req.District))
	}
	if req.X != nil && req.Z != nil {
		lines = append(lines, fmt.Sprintf("point=(%g, %g)", *req.X, *req.Z))
	}
	if mode := NormalizeMode(req.Mode); mode != "" {
		lines = append(lines, fmt.Sprintf("mode=%s", mode))
	}
	return strings.Join(lines, "\n")
}
