// Package agent — prop_tools.go: 道具系统 4 个 Agent 工具（v5 重构，迁出 tools.go）。
//
// 4 个工具通过包级 init() 注册到 toolsRegistry：
//   - use_prop      （PhaseSpeak，动态 schema 从 gc.PropSnapshot 生成）
//   - prop_inspect  （PhaseSpeak，纯查询；不在 AddGameContext 时也能调）
//   - prop_status   （PhaseSpeak，快查余额/冷却/预算综合判断）
//   - prop_history  （PhaseSpeak，返回最近 N 条公开事件）
//
// 与 v4 的兼容：
//   - v4 在 tools.go::BuildTools 通过 addUsePropTool(add, gc, alive, seat) 等 4 个
//     add* 函数手工挂载；v5 改为遍历 MountTools(PhaseSpeak, gc) 装配。
//   - v4 在 dispatchToolInner 通过 switch case 派发；v5 改为查 DispatchToolByName。
//   - 公开 API 行为不变 — 所有 prop_v3_test.go + prop_v4_test.go 测试零修改。
//
// 2026-07-21 道具系统 v5 重构。
package wwplayer

import (
	"LsmAgentGame/agent/wwtypes"
	"fmt"
	"strings"
)

// init — 注册 4 个道具工具到全局 registry。
func init() {
	RegisterTool(&ToolSpec{
		Name:     "use_prop",
		Phase:    ToolPhaseSpeak,
		Category: "prop",
		// 静态基础描述（Anthropic wire 顶层 "description" 字段）。
		Description: "使用道具对目标进行心理战攻击(道具系统 v5)。消耗金币,部分回滚到本局彩池(胜者分享)、部分系统销毁(永久通缩)、部分补偿被击中者;每局最多 3 个;间隔 ≥30s;仅白天发言阶段可使用;公开广播(所有人可见)。目标不可为已死亡玩家;身份暴露类道具不可对同阵营狼队友使用。",
		// 动态描述（每个道具的价格/中招率/AOE/经济档位比例）拼在末尾。
		BuildDescription: buildUsePropDynamicDescription,
		Builder:          buildUsePropSchema,
		// v4 addUsePropTool 的 MountIf 等价：无可购买道具时跳过（避免空挂载）。
		MountIf: func(gc *wwtypes.GameContext) bool {
			return gc != nil && len(gc.PropSnapshot) > 0
		},
		Dispatcher: dispatchUseProp,
	})
	RegisterTool(&ToolSpec{
		Name:        "prop_inspect",
		Description: "【道具盘点 v3】查看当前对局可购买道具 + 个人/全局预算 + 最近被道具击中效果。scope='mine'=仅自己;scope='all'=含公开使用历史。返回 JSON 字符串,字段: props[]/cooldown_remaining_sec/used_this_game/max_per_game/budget_remaining/budget_total/last_effect。",
		Phase:       ToolPhaseSpeak,
		Category:    "prop",
		Builder:     buildPropInspectSchema,
		MountIf:     nil, // 始终挂载（即使 wwtypes.PropSnapshot 为空，让 LLM 收到 "no props"）
		Dispatcher:  dispatchPropInspect,
	})
	RegisterTool(&ToolSpec{
		Name:        "prop_status",
		Description: "【道具状态快速查询 v3】返回你当前的道具使用状态(纯查询不消耗资源): cooldown_remaining_sec/used_this_game/max_per_game/wallet_balance/room_prop_budget_remaining/can_use_prop(综合 bool)。",
		Phase:       ToolPhaseSpeak,
		Category:    "prop",
		Builder:     buildPropStatusSchema,
		MountIf:     nil,
		Dispatcher:  dispatchPropStatus,
	})
	RegisterTool(&ToolSpec{
		Name:        "prop_history",
		Description: "【道具公开使用历史 v3】查看本局最近 N 条道具使用记录(from_seat/to_seat/prop_key/hit/effect_hint/phase/round)。公开信息;用于复盘'谁用了什么对谁'。limit ≤ 20。",
		Phase:       ToolPhaseSpeak,
		Category:    "prop",
		Builder:     buildPropHistorySchema,
		// v4 addPropHistoryTool 等价:PropHistorySnapshot 为空时不挂载(无历史就别暴露)。
		MountIf:    func(gc *wwtypes.GameContext) bool { return gc != nil && len(gc.PropHistorySnapshot) > 0 },
		Dispatcher: dispatchPropHistory,
	})
}

// ─── use_prop ──────────────────────────────────────────────────────────────

// buildUsePropSchema 生成 use_prop 工具的 input_schema。
//
// v4 addUsePropTool 等价：prop_id enum 从 gc.PropSnapshot 取，target enum = 存活
// 非自己座位（+ AOE 的 -1），payload 可选 ≤100 字。
//
// 仅在 MountIf 通过时才调（gc != nil && len(wwtypes.PropSnapshot) > 0）。
//
// 注意：target enum 需要 BuildTools 阶段的 alive []int 信息。v5 选择**只暴露
// 类型=integer（不限 enum）**，由 PropEngine 在 use_prop 派发时二次校验目标
// 存活（已活玩家自动拒收）。这样 schema 与 ctx 解耦：
//   - Builder 只看 wwtypes.GameContext（不收 alive []int），便于 ToolSpec 统一签名
//   - Dispatch 时由 runner 校验活着/阵营约束
func buildUsePropSchema(gc *wwtypes.GameContext) map[string]any {
	if gc == nil || len(gc.PropSnapshot) == 0 {
		// 不应触达（MountIf 已过滤）；防御性返回最小 schema。
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prop_id": map[string]any{"type": "string", "description": "道具类型"},
			},
			"required": []string{"prop_id"},
		}
	}
	enum := make([]string, 0, len(gc.PropSnapshot))
	var desc strings.Builder
	_ = desc // desc 字段已迁到 buildUsePropDynamicDescription(动态拼接),此处保留
	// enum = 所有可购买道具的 key (DB 启用 + 未达个人/全局预算)
	enum = make([]string, 0, len(gc.PropSnapshot))
	for _, s := range gc.PropSnapshot {
		enum = append(enum, s.PropKey)
	}

	// target 类型 = integer，让 LLM 可填座位号（存活校验交给 PropEngine）。
	// v4 实现的 enum 限制保留在 dispatch 端（兜底：目标不存在/已死 runner 拒收）。
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prop_id": map[string]any{
				"type":        "string",
				"enum":        enum,
				"description": "道具类型",
			},
			"target": map[string]any{
				"type":        "integer",
				"description": "目标座位号 (0-indexed，AOE 道具可填 -1 表示全场)",
			},
			"payload": map[string]any{
				"type":        "string",
				"description": "道具附带的自定义文本(≤100字,可选,留空则用默认诱导指令)",
			},
		},
		"required": []string{"prop_id", "target"},
	}
}

// dispatchUseProp 派发 use_prop。runner 实现 → werewolf.agentRunner.UseProp。
func dispatchUseProp(args map[string]any, gc *wwtypes.GameContext, runner ToolRunner) (string, error) {
	propID, _ := args["prop_id"].(string)
	target := intInput(args, "target")
	payload, _ := args["payload"].(string)
	if propID == "" {
		return "use_prop rejected: prop_id required", nil
	}
	if r, ok := runner.(PropUserRunner); ok {
		return r.UseProp(propID, target, payload)
	}
	return "use_prop rejected: runner does not support props", nil
}

// buildUsePropDynamicDescription 拼接 use_prop 工具的动态描述(由 mountFromRegistry
// 优先于 Description 调用)。拼出每个道具的价格/中招率/AOE/经济档位销毁比例。
//
// 与 v4 addUsePropTool 的 desc 字段等价;分离出来便于:
//
//  1. 单元测试覆盖(只测 Description 拼接,不依赖 schema)
//  2. 未来新增 ToolSpec 字段(如"经济档位比例")无需改 buildUsePropSchema
func buildUsePropDynamicDescription(gc *wwtypes.GameContext) string {
	if gc == nil || len(gc.PropSnapshot) == 0 {
		return "当前无可购买道具。"
	}
	var b strings.Builder
	b.WriteString("【当前可购买道具 - 道具系统 v5】\n")
	for _, s := range gc.PropSnapshot {
		aoe := ""
		if s.IsAOE {
			aoe = ",范围AOE"
		}
		b.WriteString(fmt.Sprintf("  %s %s(%d币,中招%d%%)%s\n",
			propKeyToEmoji(s.PropKey), s.NameZh, s.Price, s.BaseHitRate, aoe))
	}
	b.WriteString("【v5 经济档位 - 当前为 ")
	if gc.EconTier != "" {
		b.WriteString(string(gc.EconTier))
	} else {
		b.WriteString("health")
	}
	b.WriteString(" 档】销毁比例按房间总金币存量动态调整(Boom 20% / Health 30% / Caution 40% / Danger 45% / Critical 60%)。\n")
	b.WriteString("【约束】每局≤3道具;间隔≥30s;仅白天可使用;公开广播;不能对死者/狼队友(身份暴露类)使用。")
	return b.String()
}

// ─── prop_inspect ──────────────────────────────────────────────────────────

// buildPropInspectSchema prop_inspect schema。
func buildPropInspectSchema(_ *wwtypes.GameContext) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope": map[string]any{
				"type":        "string",
				"enum":        []string{"mine", "all"},
				"description": "mine=仅自己;all=含全场公开使用历史",
			},
		},
	}
}

// dispatchPropInspect 派发。读取当前 currentGC（由 dispatch 入口设置），返回 JSON 字符串。
func dispatchPropInspect(args map[string]any, gc *wwtypes.GameContext, runner ToolRunner) (string, error) {
	// v4 等价：dispatchToolInner 进入此 case 前已 SetCurrentGC(CurrentGC())。
	// 直接读 currentGC（与 formatPropInspect 一致）。
	scope, _ := args["scope"].(string)
	if scope == "" {
		scope = "mine"
	}
	return formatPropInspect(scope), nil
}

// ─── prop_status ───────────────────────────────────────────────────────────

// buildPropStatusSchema prop_status schema。
func buildPropStatusSchema(_ *wwtypes.GameContext) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func dispatchPropStatus(args map[string]any, gc *wwtypes.GameContext, runner ToolRunner) (string, error) {
	return formatPropStatus(), nil
}

// ─── prop_history ──────────────────────────────────────────────────────────

// buildPropHistorySchema prop_history schema。
func buildPropHistorySchema(_ *wwtypes.GameContext) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{
				"type":        "integer",
				"description": "返回最近 N 条历史（≤20）",
			},
		},
	}
}

func dispatchPropHistory(args map[string]any, gc *wwtypes.GameContext, runner ToolRunner) (string, error) {
	limit := intInput(args, "limit")
	return formatPropHistory(limit), nil
}
