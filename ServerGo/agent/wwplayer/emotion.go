// Package agent — emotion.go: Agent 情绪模块 (2026-07-10 §124)
//
// 设计要点(详见 docs/狼人杀-Agent与系统/狼人杀Agent情绪模块设计.md):
//   - 10 类情绪,模仿人类狼人杀玩家常见的情绪模式;
//   - 每个 Agent 在 NewWithRoom 末尾随机选一个初始情绪;
//   - LLM 通过 emotion_switch 工具自主切换情绪;
//   - 情绪状态写入 BotTranscript,通过 game.state.bot_contexts 下发给前端,
//     真人玩家 + 其它 Agent + 观众都能看到(与 §119 HeartThought 的协议层隔离对照);
//   - 情绪作为 system prompt 中的硬约束,引导 LLM 的说话风格与决策倾向。
//
// 本文件仅负责:
//   1. 10 类情绪 key 的常量 + 中文名映射 + 极性/唤醒度元数据;
//   2. EmotionRecord 结构(切换历史);
//   3. pickInitialEmotion 函数(随机初始);
//   4. Agent 情绪相关字段与 getter/setter (与 agent.go 配合);
//   5. BotTranscript Emotion/EmotionUpdatedAt/EmotionHistory 字段填充。
package wwplayer

import (
	"LsmAgentGame/agent/wwtypes"
	"math/rand"
	"sync"
	"time"
)

// Emotion 字符串常量 — 10 类情绪的 wire 标识。
// 前端 types/werewolf.ts 必须使用相同的 key(见附录 A 色卡)。
const (
	EmotionConfident = "confident"  // 自信从容
	EmotionExcited   = "excited"    // 亢奋得意
	EmotionCalm      = "calm"       // 冷静平淡
	EmotionPanic     = "panic"      // 紧张恐慌
	EmotionWary      = "wary"       // 疑虑警惕
	EmotionIrritated = "irritated"  // 恼怒急躁
	EmotionGrievance = "grievance"  // 委屈不满
	EmotionConfused  = "confused"   // 困惑茫然
	EmotionGuilty    = "guilty"     // 心虚愧疚(狼人专属,但不限角色)
	EmotionTired     = "tired"      // 懈怠疲惫
)

// AllEmotions 列出所有 10 类情绪 key — 用于 emotion_switch.random 与单元测试。
var AllEmotions = []string{
	EmotionConfident, EmotionExcited, EmotionCalm,
	EmotionPanic, EmotionWary, EmotionIrritated,
	EmotionGrievance, EmotionConfused,
	EmotionGuilty, EmotionTired,
}

// emotionMeta 描述每类情绪的展示属性(中文名 + 极性 + 唤醒度 + 风格描述)。
// 用于 prompt 注入与前端徽章渲染。
type emotionMeta struct {
	Name             string // 中文名
	Polarity         string // "positive" / "negative" / "neutral"
	Arousal          string // "high" / "medium" / "low"
	Emoji            string // 表情
	SpeechStyle      string // 说话风格(用于 system prompt)
	DecisionStyle    string // 决策风格
	TypicalScenarios string // 典型场景触发
	Color            string // 前端徽章背景色
}

var emotionMetas = map[string]emotionMeta{
	EmotionConfident: {
		Name: "自信从容", Polarity: "positive", Arousal: "low",
		Emoji:            "😌",
		SpeechStyle:      "语速平稳笃定,逻辑链条完整清晰,主动带队盘逻辑,愿意完整分享推理过程,发言说服力强",
		DecisionStyle:    "决策果断坚定,敢于主动归票、带队站边,倾向坚持自身判断,不易被场外因素带节奏",
		TypicalScenarios: "拿到预言家/女巫等强神身份 / 自身逻辑被全场多数人认可 / 抿出全部狼人身份",
		Color:            "#cce5ff",
	},
	EmotionExcited: {
		Name: "亢奋得意", Polarity: "positive", Arousal: "high",
		Emoji:            "🤩",
		SpeechStyle:      "语速加快、语气上扬,带有炫耀感,反复强调自己的正确判断,可能出现过度自信的口嗨式发言",
		DecisionStyle:    "决策风格激进,风险承受度提升,倾向主动冲票、开毒/开枪,容易低估对手操作空间",
		TypicalScenarios: "白天成功投出狼人 / 夜间刀中关键神牌 / 验出的查杀/金水被全场公认",
		Color:            "#ffd9b3",
	},
	EmotionCalm: {
		Name: "冷静平淡", Polarity: "neutral", Arousal: "medium",
		Emoji:            "😐",
		SpeechStyle:      "语气平稳中立,只输出客观信息,不主动带节奏,发言篇幅适中,无明显情绪倾向",
		DecisionStyle:    "决策偏保守,优先收集信息,不轻易站边,倾向观望跟票,行为可预测性强",
		TypicalScenarios: "游戏初期身份未明 / 场上局势平稳无冲突 / 处于划水观望状态",
		Color:            "#e6e6e6",
	},
	EmotionPanic: {
		Name: "紧张恐慌", Polarity: "negative", Arousal: "high",
		Emoji:            "😨",
		SpeechStyle:      "逻辑断层、发言卡顿、出现口误或重复辩解,发言长度骤减或语无伦次,严重时出现贴脸式自证",
		DecisionStyle:    "决策失准短视,优先以「保命」为目标,容易乱投票、乱开技能,狼人玩家可能贸然选择自爆",
		TypicalScenarios: "身份即将暴露 / 被多人联合点名怀疑 / 己方阵营人数处于绝对劣势",
		Color:            "#ffcccc",
	},
	EmotionWary: {
		Name: "疑虑警惕", Polarity: "negative", Arousal: "medium",
		Emoji:            "🤔",
		SpeechStyle:      "发言以质疑为主,反复追问细节,对他人表述持保留态度,不轻易暴露自身身份与信息",
		DecisionStyle:    "决策谨慎保守,倾向弃票、分票,不轻易站队,会优先验证存疑目标,拒绝盲目跟票",
		TypicalScenarios: "他人发言出现明显逻辑漏洞 / 场上冲票迹象明显 / 信息模糊无法锁定狼人",
		Color:            "#fff2b3",
	},
	EmotionIrritated: {
		Name: "恼怒急躁", Polarity: "negative", Arousal: "high",
		Emoji:            "😤",
		SpeechStyle:      "语气强硬带攻击性,会指责其他玩家,逻辑输出情绪化,容易冲动拍身份、说气话",
		DecisionStyle:    "决策冲动逆反,倾向硬刚怀疑对象,容易因情绪放弃最优策略,强行归票自己抵触的玩家",
		TypicalScenarios: "被好人冤枉成狼 / 队友发言严重拉胯 / 连续两轮被抗推",
		Color:            "#ffb3b3",
	},
	EmotionGrievance: {
		Name: "委屈不满", Polarity: "negative", Arousal: "medium",
		Emoji:            "🥺",
		SpeechStyle:      "语气偏弱带诉苦感,反复强调自己的好人面,情绪化辩解多于理性逻辑输出",
		DecisionStyle:    "决策消极抵触,倾向摆烂弃票,对带队行为持排斥态度,容易出现逆反式投票",
		TypicalScenarios: "平民被全场打为狼人 / 好人身份不被认可 / 逻辑正确却被投出局",
		Color:            "#ffd1dc",
	},
	EmotionConfused: {
		Name: "困惑茫然", Polarity: "negative", Arousal: "low",
		Emoji:            "😵",
		SpeechStyle:      "发言内容少、划水感强,频繁表示「没听清」「没搞懂」,主动要求他人复述逻辑",
		DecisionStyle:    "决策盲从性强,极易被带节奏,倾向跟随多数人投票,随机选择目标的概率显著提升",
		TypicalScenarios: "场上逻辑完全混乱 / 多人对跳身份真假难辨 / 信息不足无法形成判断",
		Color:            "#d9d9d9",
	},
	EmotionGuilty: {
		Name: "心虚愧疚", Polarity: "negative", Arousal: "medium",
		Emoji:            "😬",
		SpeechStyle:      "发言卡顿、回避关键问题,编造的逻辑细节不足,频繁转移话题、甩锅他人",
		DecisionStyle:    "决策保守求稳,优先保全自身身份,不敢激进冲票,夜间刀人会出现犹豫",
		TypicalScenarios: "悍跳预言家出现漏洞 / 发言撒谎被点中关键逻辑 / 刀中熟人身份牌",
		Color:            "#d6c4e0",
	},
	EmotionTired: {
		Name: "懈怠疲惫", Polarity: "negative", Arousal: "low",
		Emoji:            "😴",
		SpeechStyle:      "发言极度简短,以划水为主,懒得输出逻辑,常用「过」「跟着大家走」应付",
		DecisionStyle:    "决策随意佛系,倾向随便投票或弃票,不关心对局结果,被动跟随主流意见",
		TypicalScenarios: "游戏进入多轮后期 / 连续平票反复拉扯 / 重复盘相同逻辑",
		Color:            "#c9d6e0",
	},
}

// EmotionMeta 返回 emotion 的展示元数据;未知情绪返回零值。
func EmotionMeta(emotion string) emotionMeta {
	if m, ok := emotionMetas[emotion]; ok {
		return m
	}
	return emotionMeta{Name: emotion}
}

// IsValidEmotion 报告 emotion 是否为已知的 10 类之一。
func IsValidEmotion(emotion string) bool {
	_, ok := emotionMetas[emotion]
	return ok
}

// EmotionRecord 是单次情绪切换的历史记录(供 BotTranscript.EmotionHistory)。
type EmotionRecord struct {
	Emotion string `json:"emotion"` // 目标情绪 key
	Reason  string `json:"reason"`  // 切换原因(LLM 在 emotion_switch.reason 给出,≤80 字)
	AtMs    int64  `json:"at_ms"`   // 切换时间 unix 毫秒
}

// emotionHistoryMaxLen 是 emotionHistory 的滚动上限(最近 5 条)。
// 超出按 FIFO 淘汰;更老的历史写 DB 即可(后续可对接 model_game_log)。
const emotionHistoryMaxLen = 5

// EmotionFx 是 emotion_switch_speak 的表现层参数(2026-08-04 §表情特效,
// 见 docs/Agent拟人化和表情特效-解决和设计方案-20260804-02.md §5)。
// 全部字段可省略(零值) → 服务端按默认 pulse/mid/12s 处理,契约向后兼容。
//
// **时单位约定**(防御性注释):DurationSec 是**秒**(外部 wire / LLM 视角),
// 内部存储换算为 emotionState.fxDurationMs(毫秒)。调用方切勿把毫秒当秒传入,
// NormalizeEmotionFx 已经 clamp [8,30],外部传入前不要再乘 1000。
//
// **协议层隔离红线**(对齐 §119/§133):Caption 只进 BotTranscript,
// **绝不**写入 chat_message 表 / chat_history 队列 / HeartThought。
type EmotionFx struct {
	Effect      string // pulse/shake/sweat/rage/tears/spin_question/glow/drowsy 空=pulse
	Intensity   string // low/mid/high 空=mid
	Caption     string // ≤20 rune,超出服务端截断(见 NormalizeEmotionFx)
	DurationSec int    // 秒(clamp [8,30],0=12),勿传毫秒
}

// 特效参数服务端权威约束(不信任 LLM 输出,对齐 §84b 严格校验精神)。
const (
	emotionFxDefaultEffect   = "pulse"
	emotionFxDefaultIntensity = "mid"
	emotionFxDefaultDurationSec = 12
	emotionFxMinDurationSec  = 8
	emotionFxMaxDurationSec  = 30
	emotionFxCaptionMaxRunes = 20
)

// validEmotionFxEffects 列出 8 种合法 effect key(每种对应前端一组 CSS keyframes)。
var validEmotionFxEffects = map[string]bool{
	"pulse": true, "shake": true, "sweat": true, "rage": true,
	"tears": true, "spin_question": true, "glow": true, "drowsy": true,
}

// validEmotionFxIntensities 列出 3 档合法 intensity。
var validEmotionFxIntensities = map[string]bool{
	"low": true, "mid": true, "high": true,
}

// NormalizeEmotionFx 对 LLM 给出的 fx 做服务端归一化:
//   - Effect 空/非法 → pulse
//   - Intensity 空/非法 → mid
//   - DurationSec 0 → 12; <8 clamp 到 8; >30 clamp 到 30
//   - Caption 截断到 20 rune
//
// 这是 dispatch 层单一归一化入口,agent_runner / SwitchEmotionFx 拿到的
// 都是已归一化的 fx,不做二次校验。
func NormalizeEmotionFx(fx EmotionFx) EmotionFx {
	if !validEmotionFxEffects[fx.Effect] {
		fx.Effect = emotionFxDefaultEffect
	}
	if !validEmotionFxIntensities[fx.Intensity] {
		fx.Intensity = emotionFxDefaultIntensity
	}
	if fx.DurationSec == 0 {
		fx.DurationSec = emotionFxDefaultDurationSec
	} else if fx.DurationSec < emotionFxMinDurationSec {
		fx.DurationSec = emotionFxMinDurationSec
	} else if fx.DurationSec > emotionFxMaxDurationSec {
		fx.DurationSec = emotionFxMaxDurationSec
	}
	if r := []rune(fx.Caption); len(r) > emotionFxCaptionMaxRunes {
		fx.Caption = string(r[:emotionFxCaptionMaxRunes])
	}
	return fx
}

// emotionState 是 Agent 内部情绪状态的结构;独立 a.emotionMu mutex 保护,
// 避免与 a.mu(events channel 切换)争抢。
//
// emotionMu 与 a.mu 不会同时持有:
//   - 写入(SwitchEmotion / pickInitialEmotionLocked)只持 emotionMu;
//   - 读取(CurrentEmotion / CurrentEmotionReason / EmotionUpdatedAt)在
//     recordTranscript 路径上由 a.mu 包住,但 emotion 字段的读是单独
//     emotionMu.RLock(),与 a.mu 嵌套顺序严格(先 a.Lock 再 emotionMu.RLock
//     是安全的,因为 emotionMu 不会反过来取 a.mu)。
type emotionState struct {
	mu              sync.RWMutex
	current         string         // 当前情绪 key
	updatedAtMs     int64          // 当前情绪的切换时间 unix ms
	reason          string         // 当前情绪的切换原因(LLM 给出)
	history         []EmotionRecord // 最近 N 次切换记录

	// 2026-08-04 §表情特效 — emotion_switch_speak 表现层参数(§5.2)。
	// fx* 字段随 SwitchEmotionFx 与 current/reason 一并原子更新;
	// speak 失败回滚语义不变 — 整组字段都不动。
	fxEffect      string // pulse/shake/... 空=未指定(等价 pulse)
	fxIntensity   string // low/mid/high
	fxCaption     string // ≤20 rune 表情文字气泡(仅 BotTranscript,绝不进 chat)
	fxStartedAtMs int64  // 特效开始 unix ms
	fxDurationMs  int64  // 特效持续 ms(由 duration_sec clamp [8,30] 换算)
}

// pickInitialEmotion 从 10 类情绪中随机抽取一个作为初始情绪。
// 狼人有 20% 概率默认 guilty(心虚愧疚 — 因为他们必须撒谎,开局紧张符合人设)。
// 其余按均匀分布。
//
// 调用方:Agent.NewWithRoom 末尾(在没有任何历史对战数据时)。
func pickInitialEmotion(role string) string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	if role == "werewolf" && rng.Float64() < 0.20 {
		return EmotionGuilty
	}
	return AllEmotions[rng.Intn(len(AllEmotions))]
}

// PickInitialEmotionForTest 是 pickInitialEmotion 的导出 wrapper — 仅供
// agent_test 包验证分布使用;生产代码请用 pickInitialEmotion。
func PickInitialEmotionForTest(role string) string {
	return pickInitialEmotion(role)
}

// pickRandomEmotion 从 10 类情绪中均匀随机抽取一个(用于 emotion_switch.random)。
// 调用方:agentRunner.EmotionSwitchRandom。
func pickRandomEmotion() string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return AllEmotions[rng.Intn(len(AllEmotions))]
}

// PickRandomEmotion 是 pickRandomEmotion 的导出 wrapper — 供 werewolf 侧
// agentRunner 调,避免直接调用包内函数。
func PickRandomEmotion() string {
	return pickRandomEmotion()
}

// SwitchEmotion 切换 Agent 当前情绪 + 追加历史。
// 在 emotionMu 保护下原子完成;若 emotion 不是 10 类之一则 no-op。
// reason 会被截断到 80 字(与 emotion_switch.reason schema 一致)。
//
// 2026-08-04 §表情特效:本函数保留为无 fx 的兼容入口,内部委托
// SwitchEmotionFx 传零值 EmotionFx(effect/intensity 由调用层在读取时
// 按 pulse/mid 默认处理,fxStartedAtMs/fxDurationMs 记 0 → wire 上
// omitempty 不下发,旧客户端零感知)。
//
// 调用方:Agent.EmotionSwitch (供 emotion_switch 工具调用入口)。
func (a *Agent) SwitchEmotion(emotion, reason string) {
	a.SwitchEmotionFx(emotion, reason, EmotionFx{})
}

// SwitchEmotionFx 切换 Agent 当前情绪 + 表现层特效参数 + 追加历史。
// fx 必须已由调用层(见 tools.go dispatch case)经 NormalizeEmotionFx 归一化;
// 本函数只做 StartedAtMs 时间戳填充与持续时长换算,不做二次校验。
// speak 失败路径不调用本函数 → 整组字段(emotion + fx)保持上一状态(回滚语义)。
func (a *Agent) SwitchEmotionFx(emotion, reason string, fx EmotionFx) {
	if !IsValidEmotion(emotion) {
		return
	}
	// 防御性 clamp DurationSec 到 [8,30](仅当 >0)。注意**不**调 NormalizeEmotionFx:
	// NormalizeEmotionFx 会把 DurationSec=0 改写为 12,破坏 SwitchEmotion 兼容入口
	// (SwitchEmotion → SwitchEmotionFx(emotion, reason, EmotionFx{}) 语义为「只切情绪、
	// 不带特效」,fxStartedAtMs/fxDurationMs 必须保持 0)。此处只把住溢出关。
	if fx.DurationSec > 0 {
		if fx.DurationSec < emotionFxMinDurationSec {
			fx.DurationSec = emotionFxMinDurationSec
		} else if fx.DurationSec > emotionFxMaxDurationSec {
			fx.DurationSec = emotionFxMaxDurationSec
		}
	}
	now := time.Now().UnixMilli()
	reason = truncate(reason, 80)
	// 情绪状态提交放进独立作用域,让 emotion.mu 在函数返回**之前**就释放 ——
	// 下面的发布回调需要读 a.onTranscriptPublished(受 a.mu 保护),而文档化的
	// 锁序是 a.mu → emotion.mu,反向嵌套会引入锁序风险(§92a 同源约束)。
	func() {
		a.emotion.mu.Lock()
		defer a.emotion.mu.Unlock()
		a.emotion.current = emotion
		a.emotion.updatedAtMs = now
		a.emotion.reason = reason
		// §表情特效:fx 与 emotion 原子绑定。零值 EmotionFx 的 Effect/Intensity
		// 为空串(wire 上 omitempty),DurationSec=0 → fxStartedAtMs/fxDurationMs 记 0。
		a.emotion.fxEffect = fx.Effect
		a.emotion.fxIntensity = fx.Intensity
		a.emotion.fxCaption = fx.Caption
		if fx.DurationSec > 0 {
			a.emotion.fxStartedAtMs = now
			a.emotion.fxDurationMs = int64(fx.DurationSec) * 1000
		} else {
			a.emotion.fxStartedAtMs = 0
			a.emotion.fxDurationMs = 0
		}
		a.emotion.history = append(a.emotion.history, EmotionRecord{
			Emotion: emotion,
			Reason:  reason,
			AtMs:    now,
		})
		if len(a.emotion.history) > emotionHistoryMaxLen {
			// FIFO 淘汰最早的一条。
			a.emotion.history = a.emotion.history[len(a.emotion.history)-emotionHistoryMaxLen:]
		}
	}()
	// 2026-08-05 §Agent聊天显示优化 (B3, 修复 P0-2):情绪提交成功后立即触发
	// transcript 发布回调 → room 侧 broadcast game.state,让座位卡 EmotionAvatar
	// 与发言气泡在**同一时刻**刷新。此前 SwitchEmotionFx 不触发任何发布回调,
	// 情绪只能等下一次无关广播才被人类看到。
	// 与 recordTranscript / RecordLastSpeech 同一范式:锁内读回调、锁外 go cb()。
	a.Lock()
	cb := a.onTranscriptPublished
	a.Unlock()
	if cb != nil {
		go cb()
	}
}

// CurrentEmotion 返回 Agent 当前情绪 key。空字符串表示尚未初始化。
// 加锁读;并发安全。
func (a *Agent) CurrentEmotion() string {
	a.emotion.mu.RLock()
	defer a.emotion.mu.RUnlock()
	return a.emotion.current
}

// CurrentEmotionReason 返回当前情绪的切换原因。
func (a *Agent) CurrentEmotionReason() string {
	a.emotion.mu.RLock()
	defer a.emotion.mu.RUnlock()
	return a.emotion.reason
}

// EmotionUpdatedAtMs 返回当前情绪切换时间(unix ms);0 表示从未切换。
func (a *Agent) EmotionUpdatedAtMs() int64 {
	a.emotion.mu.RLock()
	defer a.emotion.mu.RUnlock()
	return a.emotion.updatedAtMs
}

// EmotionHistoryCopy 返回情绪切换历史的副本(防止外部修改内部状态)。
// 返回空 slice 表示尚未切换过;长度上限 emotionHistoryMaxLen。
func (a *Agent) EmotionHistoryCopy() []EmotionRecord {
	a.emotion.mu.RLock()
	defer a.emotion.mu.RUnlock()
	if len(a.emotion.history) == 0 {
		return nil
	}
	out := make([]EmotionRecord, len(a.emotion.history))
	copy(out, a.emotion.history)
	return out
}

// CurrentEmotionFx 返回当前情绪的特效参数副本(2026-08-04 §表情特效)。
// startedAtMs/durationMs 为 0 表示本次切换未携带 fx(旧入口 SwitchEmotion
// 或未传新参数的历史路径)。加锁读;并发安全。
func (a *Agent) CurrentEmotionFx() (fx EmotionFx, startedAtMs, durationMs int64) {
	a.emotion.mu.RLock()
	defer a.emotion.mu.RUnlock()
	fx = EmotionFx{
		Effect:    a.emotion.fxEffect,
		Intensity: a.emotion.fxIntensity,
		Caption:   a.emotion.fxCaption,
	}
	// duration 以 ms 存,换算回 sec 保持 EmotionFx.DurationSec 语义一致。
	if a.emotion.fxDurationMs > 0 {
		fx.DurationSec = int(a.emotion.fxDurationMs / 1000)
	}
	return fx, a.emotion.fxStartedAtMs, a.emotion.fxDurationMs
}

// snapshotEmotion 持锁拷贝当前情绪状态 — 供 recordTranscript 在 a.Lock 内
// 一次性获取所有需要下发的情绪字段(避免在 a.Lock 内再嵌 emotionMu.RLock 的
// 嵌套锁复杂度)。
//
// 实际使用中:recordTranscript 走 a.Lock → 读 emotionState 直接访问;
// 本函数保留以便其他可能需要"无 a.mu 直接读 emotion"的场景。
func (a *Agent) snapshotEmotion() (current string, reason string, updatedMs int64, history []EmotionRecord) {
	a.emotion.mu.RLock()
	defer a.emotion.mu.RUnlock()
	current = a.emotion.current
	reason = a.emotion.reason
	updatedMs = a.emotion.updatedAtMs
	if len(a.emotion.history) > 0 {
		history = make([]EmotionRecord, len(a.emotion.history))
		copy(history, a.emotion.history)
	}
	return
}

// EmotionStyleBlock 渲染【当前情绪】段(注入 BuildSystemPrompt / BuildUserPrompt)。
// 设计要点:情绪作为 system prompt 硬约束,引导 LLM 说话/决策风格。
//
// 输出示例:
//
//	【你的当前情绪】(2026-07-10 §124)
//	你当前的情绪是「紧张恐慌」(情绪极性=negative, 唤醒度=high),emoji=😨。
//	请按以下风格说话与决策:
//	  • 语速/句长: 逻辑断层、发言卡顿、出现口误或重复辩解...
//	  • 决策倾向: 决策失准短视,优先以「保命」为目标...
//	  • 典型场景触发: 身份即将暴露 / 被多人联合点名怀疑...
//	情绪会显著影响你的发言风格与决策风格。**这是硬约束**,不要假装"我没情绪"。
func EmotionStyleBlock(emotionKey string) string {
	if emotionKey == "" {
		return ""
	}
	m := EmotionMeta(emotionKey)
	if m.Name == emotionKey {
		// 未知情绪:返回空,避免在 prompt 里渲染 "未知情绪:xxx"。
		return ""
	}
	s := "\n【你的当前情绪】(2026-07-10 §124)\n"
	s += "你当前的情绪是「" + m.Name + "」(情绪极性=" + m.Polarity + ", 唤醒度=" + m.Arousal + "),emoji=" + m.Emoji + "。\n"
	s += "请按以下风格说话与决策:\n"
	s += "  • 语速/句长: " + m.SpeechStyle + "\n"
	s += "  • 决策倾向: " + m.DecisionStyle + "\n"
	s += "  • 典型场景触发: " + m.TypicalScenarios + "\n"
	s += "情绪会显著影响你的发言风格与决策风格。**这是硬约束**,不要假装\"我没情绪\"。\n"
	return s
}

// OthersEmotionBlock 渲染【他人情绪感知】段(注入 BuildSystemPrompt)。
// 输入 others 是其它 bot 的 (seat+1 显示, emotion, reason) 列表;空时返回 ""。
//
// 输出示例:
//
//	【他人情绪 — 你可感知其他 Agent 的实时情绪】(2026-07-10 §124)
//	当前房间内其它 Agent 的情绪状态:
//	  • 1号(美团 LongCat-2.0): 自信从容(confident) — "查杀节奏顺利"
//	  • 3号(豆包 2.0): 紧张恐慌(panic) — "被多人质疑身份"
//	  • 5号(DeepSeek V4-Pro): 疑虑警惕(wary) — "场上有悍跳迹象"
//	策略:
//	  • 敌人(其他阵营)情绪紧张时 → 你可以更激进地逼问 / 制造压力
//	  • 队友情绪紧张时 → 主动 whisper 鼓励 / 帮 ta 解围
//	  • 敌人情绪亢奋时 → 警惕 ta 可能有底牌(预言家/悍跳狼),避免硬刚
func OthersEmotionBlock(others []wwtypes.SeatEmotionBrief) string {
	if len(others) == 0 {
		return ""
	}
	s := "\n【他人情绪 — 你可感知其他 Agent 的实时情绪】(2026-07-10 §124)\n"
	s += "当前房间内其它 Agent 的情绪状态:\n"
	for _, o := range others {
		m := EmotionMeta(o.Emotion)
		name := m.Name
		if name == o.Emotion {
			name = o.Emotion
		}
		reason := o.Reason
		if reason == "" {
			reason = "(无)"
		}
		s += "  • " + itoa(o.Seat+1) + "号: " + name + "(" + o.Emotion + ") — \"" + truncate(reason, 60) + "\"\n"
	}
	s += "策略:\n"
	s += "  • 敌人(其他阵营)情绪紧张时 → 你可以更激进地逼问 / 制造压力\n"
	s += "  • 队友情绪紧张时 → 主动 whisper 鼓励 / 帮 ta 解围\n"
	s += "  • 敌人情绪亢奋时 → 警惕 ta 可能有底牌(预言家/悍跳狼),避免硬刚\n"
	return s
}

// MyEmotionBlock 渲染【我的情绪】短段(注入 BuildUserPrompt 末尾,中段强调)。
// 比 EmotionStyleBlock 更紧凑,仅 2 行:让 LLM 在每次决策时看到自己的情绪。
func MyEmotionBlock(emotionKey, reason string) string {
	if emotionKey == "" {
		return ""
	}
	m := EmotionMeta(emotionKey)
	name := m.Name
	if name == emotionKey {
		name = emotionKey
	}
	reason = truncate(reason, 60)
	if reason == "" {
		reason = "(无)"
	}
	s := "\n【你的情绪】(§124)\n"
	s += "当前: " + name + "(" + emotionKey + ") " + m.Emoji + " — \"" + reason + "\"\n"
	s += "情绪风格: " + m.SpeechStyle + "\n"
	return s
}

// EmotionSwitchSpeakWriteRule 渲染【合并发言 + 切情绪】硬约束段(注入 BuildSystemPrompt 末尾)。
//
// 2026-08-04 §重构 — emotion_switch 已删除,合并到 emotion_switch_speak。
// 这段 prompt 让 LLM 知道:
//  1. 想发言时必须用 emotion_switch_speak(不再有独立 emotion_switch)
//  2. 单响应最多 1 次 emotion_switch_speak;多次以最后一次为准
//  3. emotion_switch_speak 与 speak/speak_with_thought 不能同响应
//  4. emotion / reason 字段可省略(只填 text 等价于 speak)
func EmotionSwitchSpeakWriteRule() string {
	return "\n【合并发言 + 切情绪(emotion_switch_speak) — 2026-08-04 重构】\n" +
		"  • 想发言时必须用 emotion_switch_speak(text=..., emotion=..., reason=...)\n" +
		"  • text 必填(≤80字);emotion 省略=保持当前;reason 仅在 emotion 指定时生效\n" +
		"  • 单次响应只允许 1 次 emotion_switch_speak;多次以最后一次为准,前面的会被服务端丢弃\n" +
		"  • emotion_switch_speak 与 speak / speak_with_thought 不能同响应(避免双发言)\n" +
		"  • 只想静默时用 idle_silent;只想投票用 vote\n" +
		"  • **已删除独立 emotion_switch 工具** — 不要再调\n" +
		"  • §表情特效:effect/intensity/duration_sec/caption 均可省略(默认 pulse/mid/12s);\n" +
		"    caption 是 ≤20 字的头像气泡文字,只在头像上短暂展示,不进公屏聊天记录\n"
}