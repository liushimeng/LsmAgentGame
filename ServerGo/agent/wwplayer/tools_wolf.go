// Package agent — wolf_tools.go: 狼人 Agent 工具（v5 重构，迁出 tools.go）。
//
// 当前注册 1 个工具：
//   - wolf_whisper（§20260810-04 U1：speak + night_wolves 两阶段均挂载；
//     MountIf 去除 WolfTeammateSeat>=0 硬依赖,与 30% 互知率解耦;
//     仅 faction=="wolf" 仍是挂载前置）
//
// 协议层隔离（v4 §13.1 + §119 HeartThought 设计）：
//   - 留言**不**入 chat_message 表 / chat_history 队列 / BotTranscript.HeartThought
//   - 仅小队存活狼 Agent 在 user prompt 中可见
//   - 死亡时由 EmitPlayerDied 调 WolfPackRoom.PurgeByDeath 清理
//
// 与 v4 兼容：addWolfWhisperTool add 函数签名保留为 thin wrapper（向后兼容；
// tools.go 中的旧 addWolfWhisperTool 函数保留但内部委托到 registry）。
//
// 2026-07-21 道具系统 v5 重构。
package wwplayer

import "LsmAgentGame/agent/wwtypes"

// init — 注册狼人小队工具。
func init() {
	RegisterTool(&ToolSpec{
		Name:        "wolf_whisper",
		Description: "【狼小队内部广播 v4 §13.1 + §20260810-04 U1】向所有狼队友推送一条留言(≤80字)。仅狼队友可见；不入公屏、不入 HeartThought。\n典型用法:**夜间刀人前协调目标**(night_wolves 阶段可挂载,U1 修复后可用)、首日互证身份、伪装策略同步。留言必须可执行(刀谁/悍跳顺序/弃票策略)。",
		// §20260810-04 U1 — 多阶段挂载:speak + night_wolves(K3-F1 修复)。
		Phase:  ToolPhaseSpeak,
		Phases: []ToolPhase{ToolPhaseSpeak, ToolPhaseNight},
		Category: "wolf",
		Builder:  buildWolfWhisperSchema,
		// §20260810-04 U1 — MountIf 仅看阵营(通道本身一直存在);
		// 双重防御:tool 层 MountIf + 服务端 WolfWhisper 校验
		// State.Roles[seat]==RoleWerewolf(防止身份未确认狼 Agent 误用)。
		// WolfTeammateSeat>=0 仅控制「是否互知身份」,不再门控通道挂载。
		MountIf: func(gc *wwtypes.GameContext) bool {
			return gc != nil && gc.Faction == "wolf"
		},
		Dispatcher: dispatchWolfWhisper,
	})

	// §20260810-10 U1 — wolfpack_assign:轮值狼王重排自己的战术分工。
	// MountIf 门控:仅 faction=="wolf" 且 gc.WolfKingSeat==gc.MySeat(当前狼王)
	// 才挂载;非狼王狼 bot 根本看不到此工具,服务端 WolfpackAssign 再做双重校验。
	RegisterTool(&ToolSpec{
		Name:        "wolfpack_assign",
		Description: "【狼队分工重排 §20260810-10 U1 — 仅轮值狼王可用】把你自己的战术分工改为指定值(hype=悍跳位/charger=冲锋位/hook=倒钩位/deep=深水位)。若目标分工已被队友占用则与其互换。变更会以系统留言通知全狼。典型用法:首夜确定悍跳人选、悍跳位死亡后重新指定接班人。",
		Phase:       ToolPhaseSpeak,
		Phases:      []ToolPhase{ToolPhaseSpeak, ToolPhaseNight},
		Category:    "wolf",
		Builder:     buildWolfpackAssignSchema,
		MountIf: func(gc *wwtypes.GameContext) bool {
			return gc != nil && gc.Faction == "wolf" &&
				gc.WolfKingSeat >= 0 && gc.WolfKingSeat == gc.MySeat
		},
		Dispatcher: dispatchWolfpackAssign,
	})
}

// WolfWhisperRunner 接口已声明在 tools.go（v4 §13.1），这里不再重复声明，
// 避免同一 package 内重定义（Go 不允许）。wolf_whisper Dispatcher 直接使用
// tools.go 中定义的 WolfWhisperRunner 接口。

// buildWolfWhisperSchema 生成 wolf_whisper schema。
func buildWolfWhisperSchema(_ *wwtypes.GameContext) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "狼小队留言内容(≤80字,仅你和配对狼队友可见)",
			},
		},
		"required": []string{"text"},
	}
}

// dispatchWolfWhisper 派发。
func dispatchWolfWhisper(args map[string]any, gc *wwtypes.GameContext, runner ToolRunner) (string, error) {
	text, _ := args["text"].(string)
	if text == "" {
		return "wolf_whisper rejected: text required", nil
	}
	if r, ok := runner.(WolfWhisperRunner); ok {
		return r.WolfWhisper(text)
	}
	return "wolf_whisper rejected: runner does not support wolfpack", nil
}

// ─── §20260810-10 U1 wolfpack_assign ───

// WolfpackAssignRunner 是 ToolRunner 的可选扩展接口(§20260810-10 U1),
// 提供狼队分工重排能力。实现位置:werewolf.agentRunner。
//
// §20260811-04 U1 — 追加 cipherMode 参数(starter/advanced/"")
//   - "starter"  → 装 2 模板(target_position + fake_seer_posture)
//   - "advanced" → 装 4 模板(全部)
//   - ""         → 关闭暗号(默认,零回归)
type WolfpackAssignRunner interface {
	WolfpackAssign(newRole string, cipherMode string) (string, error)
}

// buildWolfpackAssignSchema 生成 wolfpack_assign schema。
// enum 服务端收敛 4 种合法分工(§134 教训:enum 剔除优于事后报错)。
// §20260811-04 U1 — 追加 cipher_mode 可选参数。
func buildWolfpackAssignSchema(_ *wwtypes.GameContext) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"role": map[string]any{
				"type":        "string",
				"enum":        []string{"hype", "charger", "hook", "deep"},
				"description": "新分工:hype=悍跳位(假冒预言家)/charger=冲锋位(造势攻击)/hook=倒钩位(混入好人)/deep=深水位(低调划水)",
			},
			"cipher_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"", "starter", "advanced"},
				"description": "§20260811-04 U1 暗号模式(可选):空=关闭(默认);starter=装2模板(target_position+fake_seer_posture);advanced=装4模板(全部)。仅狼队可见,不入公屏/HeartThought。",
			},
		},
		"required": []string{"role"},
	}
}

// dispatchWolfpackAssign 派发。
// §20260811-04 U1 — 追加 cipher_mode 解析(可选,空 = 关闭暗号)。
func dispatchWolfpackAssign(args map[string]any, gc *wwtypes.GameContext, runner ToolRunner) (string, error) {
	role, _ := args["role"].(string)
	if role == "" {
		return "wolfpack_assign rejected: role required", nil
	}
	cipherMode, _ := args["cipher_mode"].(string)
	if r, ok := runner.(WolfpackAssignRunner); ok {
		return r.WolfpackAssign(role, cipherMode)
	}
	return "wolfpack_assign rejected: runner does not support role assignment", nil
}
