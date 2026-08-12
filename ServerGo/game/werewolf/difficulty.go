package werewolf

// difficulty.go — §20260811-09 U2 Agent 难度分级体系
//
// 把 Agent 推理强度拆成 4 档（easy/normal/hard/hell），玩家创建房间时可选。
// 通过 prompt directive + 运行时参数控制 Agent 表现，所有 bot 共用一档（不支持混档）。
//
// 设计原则：
//   - 不更换模型（model_key 是独立维度）；
//   - prompt 前缀字节不变（normal 与旧版逐字节一致 → Anthropic prompt cache 命中），
//     仅 easy/hard/hell 在末尾追加 §Difficulty 段；
//   - 难度档影响金币倍率（仅放大胜方收益，败方扣款不变 → 新手不放大惩罚）。

// AgentDifficulty 是难度档位字符串类型。合法值集 {easy, normal, hard, hell}。
type AgentDifficulty string

const (
	DifficultyEasy   AgentDifficulty = "easy"
	DifficultyNormal AgentDifficulty = "normal"
	DifficultyHard   AgentDifficulty = "hard"
	DifficultyHell   AgentDifficulty = "hell"
)

// DifficultyProfile 是某一档难度对应的全部运行时参数。
// PromptDirective 为空时 BuildSystemPrompt 不注入该段（normal 行为，cache 命中）。
// CoinMultiplierX10 是胜方奖励倍率（10 = 1.0×，5 = 0.5×，20 = 2.0×），
// 用整数十倍避免浮点误差；败方不变。
type DifficultyProfile struct {
	PromptDirective   string
	MaxToolUse        int
	MemoryInjectRunes int
	InjectHypotheses  bool
	InjectLongMemory  bool
	SpeakLimiterScale float64
	CoinMultiplierX10 int
}

// ProfileFor 返回指定档位的 DifficultyProfile。
// 未知 / 空值归一化为 normal —— 与死亡延时揭晓（setDeathRevealDelayMinLocked）
// / 法官模式（cfgWerewolfJudgeMode）的「未知值回退默认」单点约束一致。
//
// Coin 倍率说明：
//   - easy   0.5×  → 新手有保护，胜方收益不放大（败方仍扣 ante，避免「easy 局照样惩罚」）
//   - normal 1.0×  → 现状零回归
//   - hard   1.5×  → 熟练玩家收益放大
//   - hell   2.0×  → 高手挑战最大化
func ProfileFor(d AgentDifficulty) DifficultyProfile {
	switch d {
	case DifficultyEasy:
		return DifficultyProfile{
			PromptDirective: "【难度=简单】保守推理：只做最直接的逻辑推断，不主动悍跳/欺骗。" +
				"发言简短，投票倾向跟随主流。优先使用基础工具，谨慎使用欺骗类工具。\n",
			MaxToolUse:        3,
			MemoryInjectRunes: 1500,
			InjectHypotheses:  false,
			InjectLongMemory:  false,
			SpeakLimiterScale: 1.5,
			CoinMultiplierX10: 5,
		}
	case DifficultyHard:
		return DifficultyProfile{
			PromptDirective: "【难度=困难】深度推理：主动构建假说链，识别发言矛盾，合理使用道具与欺骗。" +
				"善用假说表、承诺追踪、反事实推理；高效率发言（避免冗余），策略性强。\n",
			MaxToolUse:        6,
			MemoryInjectRunes: 4000,
			InjectHypotheses:  true,
			InjectLongMemory:  true,
			SpeakLimiterScale: 1.0,
			CoinMultiplierX10: 15,
		}
	case DifficultyHell:
		return DifficultyProfile{
			PromptDirective: "【难度=地狱】大师级：全量使用假说表/承诺追踪/反事实推理，主动布局多轮策略，" +
				"欺骗与反欺骗并重。每轮发言必须有战术目的（混淆/钓鱼/收集/压制），善用道具与狼队暗号。\n",
			MaxToolUse:        8,
			MemoryInjectRunes: 6000,
			InjectHypotheses:  true,
			InjectLongMemory:  true,
			SpeakLimiterScale: 0.8,
			CoinMultiplierX10: 20,
		}
	case DifficultyNormal:
		fallthrough
	default:
		// normal 显式零指令：PromptDirective=""，BuildSystemPrompt 不注入任何附加段，
		// 输出与旧版逐字节一致 → Anthropic prompt cache 命中（§20260810-10 U2 同款纪律）。
		return DifficultyProfile{
			PromptDirective:   "",
			MaxToolUse:        0, // 0 = 不限（与 wwplayer/agent.go:788 MaxToolUse=0 一致）
			MemoryInjectRunes: 4000,
			InjectHypotheses:  true,
			InjectLongMemory:  true,
			SpeakLimiterScale: 1.0,
			CoinMultiplierX10: 10,
		}
	}
}

// NormalizeAgentDifficulty 把任意字符串归一化为合法 AgentDifficulty。
// 空字符串 → normal（前端未传字段 = 不指定 = 默认）。
// 未知值 → normal（不报错，与 §198 JudgeMode 同款「宽松兼容」策略）。
func NormalizeAgentDifficulty(s string) AgentDifficulty {
	switch AgentDifficulty(s) {
	case DifficultyEasy, DifficultyNormal, DifficultyHard, DifficultyHell:
		return AgentDifficulty(s)
	default:
		return DifficultyNormal
	}
}

// AllDifficulties 返回合法难度档位列表（用于前端下拉 / 测试枚举）。
// 顺序与 UI 展示顺序一致。
func AllDifficulties() []AgentDifficulty {
	return []AgentDifficulty{DifficultyEasy, DifficultyNormal, DifficultyHard, DifficultyHell}
}