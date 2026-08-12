// Package wwplayer — phase_config.go: 阶段指令注册表。
// 灵感来源: PI Agent 的 Skills 系统 (.pi/skills/ 目录)。
//
// 将分散在 BuildTools / SkipPhaseAction / BuildSystemPrompt 中的阶段相关配置
// 统一到 PhaseConfig 注册表,新增阶段只需添加一个条目。
//
// 设计原则:
//   - 单一事实来源: 新增阶段只需修改 phaseConfigs map
//   - 自动同步: BuildTools / SkipPhaseAction / PromptHint 从同一注册表读取
//   - 向后兼容: 未注册的 phase 降级到原有 switch-case 逻辑
package wwplayer

// PhaseConfig 描述一个游戏阶段的完整配置。
type PhaseConfig struct {
	// Phase 是阶段标识符 (与 GameState.Phase 一致)。
	Phase string

	// ToolKeys 是该阶段 LLM 可用的工具名列表。
	// BuildTools 从这里读取,而非硬编码 switch-case。
	ToolKeys []string

	// SkipAction 是 quarantine/watchdog 强制跳过时的安全动作。
	// 对应 SkipPhaseAction 的返回值。
	SkipAction string

	// SkipActionTarget 是 SkipAction 的默认目标参数 (如 -1 表空过)。
	SkipActionTarget int

	// DeadlineSec 是该阶段的 deadline 秒数 (watchdog 用)。
	// 0 = 使用全局默认值。
	DeadlineSec int

	// PromptHint 是该阶段追加到 system prompt 的指令提示。
	// 空串 = 不追加。
	PromptHint string

	// IsSecretPhase 是否是秘密阶段 (法官不公开信息)。
	IsSecretPhase bool

	// AllowSpeech 是否允许发言工具。
	AllowSpeech bool

	// AllowWhisper 是否允许私聊工具。
	AllowWhisper bool
}

// phaseConfigs 是所有活跃阶段的配置注册表。
// 新增阶段只需在此 map 中添加条目。
var phaseConfigs = map[string]*PhaseConfig{
	// ─── 夜间阶段 ─────────────────────────────────────
	"pre_wolves": {
		Phase:            "pre_wolves",
		ToolKeys:         []string{"speak"},
		SkipAction:       "idle_silent",
		SkipActionTarget: 0,
		DeadlineSec:      60,
		PromptHint:       "天黑前的最后发言机会。你可以发表看法或保持沉默。",
		IsSecretPhase:    false,
		AllowSpeech:      true,
	},
	"night_guard": {
		Phase:            "night_guard",
		ToolKeys:         []string{"guard_protect"},
		SkipAction:       "guard_protect",
		SkipActionTarget: -1, // 空守
		DeadlineSec:      60,
		PromptHint:       "你是守卫，请选择今晚要守护的玩家。不可连续守护同一人，不可守自己。选择 -1 表示空守。",
		IsSecretPhase:    true,
	},
	"night_wolves": {
		Phase:            "night_wolves",
		ToolKeys:         []string{"wolf_kill", "wolf_whisper"},
		SkipAction:       "wolf_kill",
		SkipActionTarget: -1, // 弃刀
		DeadlineSec:      120,
		PromptHint:       "天黑了，请狼人小队选择今晚的袭击目标。选择 -1 表示弃刀。",
		IsSecretPhase:    true,
	},
	"night_seer": {
		Phase:            "night_seer",
		ToolKeys:         []string{"seer_check"},
		SkipAction:       "seer_check",
		SkipActionTarget: -1, // 弃验
		DeadlineSec:      60,
		PromptHint:       "你是预言家，请选择今晚要查验的玩家。选择 -1 表示弃验。",
		IsSecretPhase:    true,
	},
	"night_witch": {
		Phase:            "night_witch",
		ToolKeys:         []string{"witch_act"},
		SkipAction:       "witch_act",
		SkipActionTarget: -1, // 不用药
		DeadlineSec:      60,
		PromptHint:       "你是女巫，今晚有人被袭击。你可以使用解药救人或毒药杀人，也可以不用药。",
		IsSecretPhase:    true,
	},
	"night_demon_hunter": {
		Phase:            "night_demon_hunter",
		ToolKeys:         []string{"demon_hunter_hunt"},
		SkipAction:       "demon_hunter_hunt",
		SkipActionTarget: -1, // 空过
		DeadlineSec:      60,
		PromptHint:       "你是猎魔人，可以选择今晚狩猎一名玩家。选择 -1 表示空过。",
		IsSecretPhase:    true,
	},

	// ─── 白天阶段 ─────────────────────────────────────
	"dawn": {
		Phase:         "dawn",
		ToolKeys:      []string{"speak", "speak_with_thought", "interject"},
		SkipAction:    "idle_silent",
		DeadlineSec:   30,
		PromptHint:    "天亮了。你可以发表看法。",
		AllowSpeech:   true,
	},
	"speak": {
		Phase:       "speak",
		ToolKeys:    []string{"speak", "speak_with_thought", "interject", "whisper", "emotion_switch_speak", "vote", "finish_vote", "sheriff_candidate", "sheriff_elect", "sheriff_stream", "knight_duel", "propose_vote", "restart_vote", "use_prop", "prop_inspect", "prop_status", "prop_history"},
		SkipAction:  "idle_silent",
		DeadlineSec: 120,
		PromptHint:  "白天发言阶段。你可以发言、私聊、投票或使用道具。",
		AllowSpeech: true,
		AllowWhisper: true,
	},
	"vote": {
		Phase:       "vote",
		ToolKeys:    []string{"vote", "finish_vote"},
		SkipAction:  "vote",
		SkipActionTarget: -1, // 弃票
		DeadlineSec: 90,
		PromptHint:  "投票阶段。请投出你认为应该被放逐的玩家。选择 -1 表示弃票。",
	},
	"sheriff": {
		Phase:       "sheriff",
		ToolKeys:    []string{"sheriff_candidate", "sheriff_elect", "sheriff_stream", "speak", "speak_with_thought"},
		SkipAction:  "idle_silent",
		DeadlineSec: 60,
		PromptHint:  "警长竞选阶段。你可以竞选、投票或发言。",
		AllowSpeech: true,
	},

	// ─── 特殊阶段 ─────────────────────────────────────
	"hunter_shoot": {
		Phase:            "hunter_shoot",
		ToolKeys:         []string{"hunter_shoot"},
		SkipAction:       "hunter_shoot",
		SkipActionTarget: -1, // 不开枪
		DeadlineSec:      60,
		PromptHint:       "你是猎人，可以选择开枪带走一名玩家。选择 -1 表示不开枪。",
	},
	"death_lyric": {
		Phase:         "death_lyric",
		ToolKeys:      []string{"last_words", "last_words_skip"},
		SkipAction:    "last_words_skip",
		DeadlineSec:   60,
		PromptHint:    "你即将死亡，可以留下遗言。",
		AllowSpeech:   true,
	},
	"idiot_reveal": {
		Phase:            "idiot_reveal",
		ToolKeys:         []string{"idiot_reveal"},
		SkipAction:       "idiot_reveal",
		SkipActionTarget: 0, // 默认翻牌
		DeadlineSec:      30,
		PromptHint:       "你是白痴，被投票放逐。你可以选择翻牌免死。",
	},
	"restart_vote": {
		Phase:       "restart_vote",
		ToolKeys:    []string{"restart_vote"},
		SkipAction:  "restart_vote",
		DeadlineSec: 300, // 5 分钟投票窗口
		PromptHint:  "游戏已结束。请投票是否重开一局。",
	},
}

// GetPhaseConfig 获取阶段配置。未注册的 phase 返回 nil。
func GetPhaseConfig(phase string) *PhaseConfig {
	return phaseConfigs[phase]
}

// GetAllPhaseConfigs 返回所有注册的阶段配置 (用于测试/调试)。
func GetAllPhaseConfigs() map[string]*PhaseConfig {
	return phaseConfigs
}

// PhaseToolKeys 返回指定阶段的工具名列表。未注册返回 nil。
func PhaseToolKeys(phase string) []string {
	if cfg, ok := phaseConfigs[phase]; ok {
		return cfg.ToolKeys
	}
	return nil
}

// PhaseSkipAction 返回指定阶段的安全跳过动作和目标。未注册返回 ("", 0)。
func PhaseSkipAction(phase string) (string, int) {
	if cfg, ok := phaseConfigs[phase]; ok {
		return cfg.SkipAction, cfg.SkipActionTarget
	}
	return "", 0
}

// PhaseDeadlineSec 返回指定阶段的 deadline 秒数。未注册返回 0。
func PhaseDeadlineSec(phase string) int {
	if cfg, ok := phaseConfigs[phase]; ok {
		return cfg.DeadlineSec
	}
	return 0
}

// PhasePromptHint 返回指定阶段的 prompt 提示。未注册返回空串。
func PhasePromptHint(phase string) string {
	if cfg, ok := phaseConfigs[phase]; ok {
		return cfg.PromptHint
	}
	return ""
}

// IsSecretPhase 检查指定阶段是否是秘密阶段。
func IsSecretPhase(phase string) bool {
	if cfg, ok := phaseConfigs[phase]; ok {
		return cfg.IsSecretPhase
	}
	return false
}

// PhaseAllowSpeech 检查指定阶段是否允许发言。
func PhaseAllowSpeech(phase string) bool {
	if cfg, ok := phaseConfigs[phase]; ok {
		return cfg.AllowSpeech
	}
	return false
}

// PhaseAllowWhisper 检查指定阶段是否允许私聊。
func PhaseAllowWhisper(phase string) bool {
	if cfg, ok := phaseConfigs[phase]; ok {
		return cfg.AllowWhisper
	}
	return false
}
