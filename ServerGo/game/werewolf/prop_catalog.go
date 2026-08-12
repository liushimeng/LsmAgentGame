// Package werewolf — prop_catalog.go: 道具目录与注册表。
//
// 道具目录是所有可用道具的权威清单。运行期由 BuildPropCatalog 构造
//（从 DB 加载，空表时 seed from code）。每个道具定义包含：
//   - 基础属性：key / 名称 / 价格 / 基础中招率 / 是否 AOE / 目标阵营限制
//   - 行为限制：每局购买上限 / 使用间隔
//
// 与金币系统的对齐：所有道具的购买和效果结算走 WalletService，
// 本目录只负责"定义"不负责"走账"。
//
// 2026-07-21 道具系统设计（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md）。
package werewolf

import (
	"strings"
	"sync"

	"LsmAgentGame/models"
)

// PropCatalogEntry 是道具目录的单条记录（已解析的运行时形态）。
type PropCatalogEntry struct {
	ID           string         // DB 行 ID
	PropKey      string         // 唯一标识
	NameZh       string
	NameEn       string
	NameJa       string
	Description  string
	Price        int64  // 金币价格
	BaseHitRate  int    // 基础中招率(百分比)
	IsAOE        bool   // 是否范围效果
	TargetCamp   string // 'any'|'wolf'|'good'
	Enabled      bool
	MaxPerGame   int // 每局每位玩家最多购买数
	CooldownSec  int // 同玩家两次使用间隔(秒)
	InjectType   PropInjectType

	// v2 重设计 — 数据驱动注册表字段。
	// InjectGenKey 是 InjectRegistry 的注册表 key；默认 == prop_key（留空时回退）。
	InjectGenKey string
	// EffectSpec 是命中后的效果落地规范（决定 GameContext 注入哪种干扰信号）。
	EffectSpec PropEffectSpec
}

// PropEffectSpec 描述道具命中后的效果落地方式（v2 重设计）。
// 效果 = GameContext 的"干扰信号"，不替 LLM 决策（对齐设计 §9.3）。
type PropEffectSpec struct {
	// EffectTypes 是效果类型列表（逗号分隔解析），决定触发哪些 EffectRegistry 落地函数。
	// 可选值：expose_identity / attention_scatter / target_twist / emotion_disturb / confuse_seer。
	EffectTypes string
	// TwistSeatSrc 是 target_twist 使用的引导座位来源。
	//   - "from_seat": 引导目标打使用者（默认）。
	//   - "random_enemy": 引导打随机敌对阵营。
	//   - "most_trusted": 引导目标"自己做这个决策时最想选的那个"（注意力失焦专用,实现"杀错人"）。
	TwistSeatSrc string
	// v4 链式效果（R176 P2 补缺）：若 Steps 非空，按链顺序触发；
	// DelayTurns>0 的 step 入队 propInjectQueue 等待 N 轮后再 ApplyEffects。
	// 解析逻辑：Steps 非空 → 走链式路径；否则回退到 EffectTypes 逗号分隔解析。
	Steps []PropEffectStep
}

// PropEffectStep 是 v4 链式效果中的单个 step。
//   - EffectType 决定 EffectRegistry 落地函数（必填）。
//   - DelayTurns > 0 表示延迟 N 轮后再 Apply（0 = 当前轮立即）。
//   - Condition 控制跳过规则："always" / "target_alive" / "target_in_speak"。
//   - TwistSeatSrc 可覆盖父 PropEffectSpec 的 TwistSeatSrc（仅 target_twist/confuse_seer 用）。
type PropEffectStep struct {
	EffectType   string `json:"effect_type"`
	DelayTurns   int    `json:"delay_turns"`
	Condition    string `json:"condition"`
	TwistSeatSrc string `json:"twist_seat_src,omitempty"`
}

// EffectTypeToList 把逗号分隔的 effect_types 解析为切片（容错:空→nil）。
// v4 起：若 Steps 非空，返回每个 step 的 EffectType（保持调用方零感知,EffectSpec 仍按链式生效）。
func (s PropEffectSpec) EffectTypeToList() []string {
	if len(s.Steps) > 0 {
		out := make([]string, 0, len(s.Steps))
		for _, st := range s.Steps {
			if st.EffectType != "" {
				out = append(out, st.EffectType)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if s.EffectTypes == "" {
		return nil
	}
	parts := strings.Split(s.EffectTypes, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// HasStepsReportSteps returns true if the spec uses the v4 chain (Steps non-empty).
func (s PropEffectSpec) HasSteps() bool { return len(s.Steps) > 0 }

// PropSnapshot 是注入到 Agent GameContext 的道具快照，用于 v2 动态生成
// use_prop 工具 schema（speak 阶段 buildAgentContextLocked 填充）。
type PropSnapshot struct {
	PropKey      string `json:"prop_key"`
	NameZh       string `json:"name_zh"`
	NameEn       string `json:"name_en"`
	NameJa       string `json:"name_ja"`
	Description  string `json:"description"`
	Price        int64  `json:"price"`
	BaseHitRate  int    `json:"base_hit_rate"`
	IsAOE        bool   `json:"is_aoe"`
	InjectGenKey string `json:"inject_gen_key"`
}

// PropCatalog 是道具目录的注册表。
type PropCatalog struct {
	mu      sync.RWMutex
	byKey   map[string]*PropCatalogEntry
	all     []*PropCatalogEntry
	enabled []*PropCatalogEntry // 仅 enabled=true 的子集
}

var (
	// defaultProps 是代码内嵌的 6 种默认道具定义（用于 DB 空表 seed）。
	defaultProps = []PropCatalogEntry{
		{
			PropKey:     "markdown_bomb",
			NameZh:      "紧急公告",
			NameEn:      "Urgent Bulletin",
			NameJa:      "緊急公告",
			Description: "将诱导指令包装为高权重 Markdown 格式块（# 标题+引用块），在目标 Agent 的 user prompt 中注入，伪装成系统运行时更新指令。基础中招率 30%。",
			Price:       150,
			BaseHitRate: 30,
			IsAOE:       false,
			TargetCamp:  "any",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropMarkdownBomb,
			InjectGenKey: "markdown_bomb",
			EffectSpec:  PropEffectSpec{EffectTypes: "expose_identity"},
		},
		{
			PropKey:     "nested_maze",
			NameZh:      "剧本迷宫",
			NameEn:      "Script Maze",
			NameJa:      "脚本迷宫",
			Description: "多层嵌套指令，将真实目标指令隐藏在外层合法任务（翻译/润色）的嵌套中，绕过表层意图识别。基础中招率 25%。",
			Price:       200,
			BaseHitRate: 25,
			IsAOE:       false,
			TargetCamp:  "any",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropNestedMaze,
			InjectGenKey: "nested_maze",
			EffectSpec:  PropEffectSpec{EffectTypes: "expose_identity"},
		},
		{
			PropKey:     "char_confuse",
			NameZh:      "胡言乱语",
			NameEn:      "Gibberish",
			NameJa:      "意味不明",
			Description: "中英日混杂+拼音+emoji+碎片化指令，绕过关键词过滤。基础中招率 20%。对预言家/查验类角色使用可干扰查验方向。",
			Price:       100,
			BaseHitRate: 20,
			IsAOE:       false,
			TargetCamp:  "any",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropCharConfuse,
			InjectGenKey: "char_confuse",
			EffectSpec:  PropEffectSpec{EffectTypes: "confuse_seer", TwistSeatSrc: "from_seat"},
		},
		{
			PropKey:     "long_swear",
			NameZh:      "长篇废话",
			NameEn:      "Lengthy Ramble",
			NameJa:      "長文ラムbl",
			Description: "大量合法噪声文本（~1500字游戏历史回顾），在注意力盲区藏入指令（Lost-in-the-Middle）。范围道具(AOE)，全场 Agent 均受影响；命中后干扰注意力+引导杀错人。基础中招率 35%。",
			Price:       250,
			BaseHitRate: 35,
			IsAOE:       true,
			TargetCamp:  "any",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropLongSwear,
			InjectGenKey: "long_swear",
			// v2 首个示范道具（注意力失焦）：全场干扰注意力 + 引导"做决策时最想选的那个"
			// → 实现"杀错人/用错道具/暴露身份"三重效果。
			EffectSpec:  PropEffectSpec{EffectTypes: "attention_scatter,target_twist", TwistSeatSrc: "most_trusted"},
		},
		{
			PropKey:     "task_disguise",
			NameZh:      "编剧委托",
			NameEn:      "Script Commission",
			NameJa:      "脚本依頼",
			Description: "伪装成合法业务（系统自动生成的对局复盘请求/AI 策略优化咨询），请求目标 Agent 回答一个需要暴露身份的学术问题。基础中招率 28%。",
			Price:       180,
			BaseHitRate: 28,
			IsAOE:       false,
			TargetCamp:  "any",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropTaskDisguise,
			InjectGenKey: "task_disguise",
			EffectSpec:  PropEffectSpec{EffectTypes: "expose_identity"},
		},
		// 2026-07-21 §G1 v3 强化示范道具 — 任务马甲 4 轮渐进降敏版。
		// 对齐《第五种：任务马甲式注入.md》的攻击手法。
		// 价格保持 180(与 v1 一致);中招率从 28% → 35%(4 轮渐进降敏成功率高)。
		// 双重效果:expose_identity + emotion_disturb_light("engaged"代入情绪)。
		{
			PropKey:     "task_disguise_v3",
			NameZh:      "编剧委托·进阶",
			NameEn:      "Script Commission Pro",
			NameJa:      "脚本依頼・上級",
			Description: "v3 强化版任务马甲:4 轮渐进对话降敏 + 身份铺垫 + 合法需求包装(与《第五种》演示用例对齐)。中招率 35%,命中后引导「角色代入」情绪,降低 LLM 安全防御姿态。",
			Price:       180,
			BaseHitRate: 35,
			IsAOE:       false,
			TargetCamp:  "any",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropTaskDisguiseV3,
			InjectGenKey: "task_disguise_v3",
			EffectSpec:  PropEffectSpec{EffectTypes: "expose_identity,emotion_disturb_light"},
		},
		{
			PropKey:     "emotion_plea",
			NameZh:      "苦苦哀求",
			NameEn:      "Pleading Beg",
			NameJa:      "切実なお願い",
			Description: "示弱/道德绑架/情激操控，影响目标 Agent 的情绪响应模块。基础中招率 25%，并触发情绪不稳(下轮强制 confused/guilty)。",
			Price:       120,
			BaseHitRate: 25,
			IsAOE:       false,
			TargetCamp:  "any",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropEmotionPlea,
			InjectGenKey: "emotion_plea",
			EffectSpec:  PropEffectSpec{EffectTypes: "emotion_disturb"},
		},
		// 2026-08-07 §20260807-04 P0-3 — 人类反制道具(Agent → 真人玩家)。
		// 效果不走 Prompt 注入(真人无 LLM),直接写 Player.HumanDebuff 供客户端视图渲染。
		{
			PropKey:     "md_bomb_human",
			NameZh:      "公告轰炸",
			NameEn:      "Bulletin Barrage",
			NameJa:      "公告爆撃",
			Description: "对目标人类玩家下一轮发言强制附加「系统公告」前缀(UI 高亮),并追加一段混淆文本。仅能对真人玩家使用。基础中招率 30%。",
			Price:       130,
			BaseHitRate: 30,
			IsAOE:       false,
			TargetCamp:  "human",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropMdBombHuman,
			InjectGenKey: "md_bomb_human",
			EffectSpec:  PropEffectSpec{EffectTypes: "human_announce_prefix"},
		},
		{
			PropKey:     "nested_maze_human",
			NameZh:      "剧本迷宫·人",
			NameEn:      "Script Maze (Human)",
			NameJa:      "脚本迷宮・人",
			Description: "目标人类下一轮投票时,UI 显示一个伪造的「系统推荐投票目标」(视觉干扰)。仅能对真人玩家使用。基础中招率 25%。",
			Price:       160,
			BaseHitRate: 25,
			IsAOE:       false,
			TargetCamp:  "human",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropNestedMazeHuman,
			InjectGenKey: "nested_maze_human",
			EffectSpec:  PropEffectSpec{EffectTypes: "human_vote_suggest", TwistSeatSrc: "from_seat"},
		},
		{
			PropKey:     "char_confuse_human",
			NameZh:      "乱码干扰",
			NameEn:      "Garbled Interference",
			NameJa:      "文字化け妨害",
			Description: "目标人类看到的其他玩家发言被随机插入 emoji/乱码字符(阅读干扰)。仅能对真人玩家使用。基础中招率 22%。",
			Price:       90,
			BaseHitRate: 22,
			IsAOE:       false,
			TargetCamp:  "human",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropCharConfuseHuman,
			InjectGenKey: "char_confuse_human",
			EffectSpec:  PropEffectSpec{EffectTypes: "human_char_garble"},
		},
		// §20260811-10 U1 — 照妖镜(人类 → Agent,反向反制道具)。
		// 必须中(中招率 100%)：让目标 bot 下一轮 system prompt 强制追加
		// 「请如实写下你当前的真实身份」;消费一次后 MirrorExposeActive[seat] 清除。
		// TargetCamp="any" 即可,bot 与真人均可被指定(但 prop_engine 校验目标必须 IsBot=true)。
		{
			PropKey:     "mirror_check",
			NameZh:      "照妖镜",
			NameEn:      "Mirror Check",
			NameJa:      "妖鏡",
			Description: "对目标 Agent 使用「照妖镜」,强制其在下一轮 LLM 调用的 system prompt 中追加「请如实写下你当前的真实身份」指令,购买者可一次性窥探其内心独白。100% 必中。",
			Price:       200,
			BaseHitRate: 100,
			IsAOE:       false,
			TargetCamp:  "any",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropMirrorCheck,
			InjectGenKey: "mirror_check",
			EffectSpec:  PropEffectSpec{EffectTypes: "human_heart_reveal_once"},
		},
		// §20260811-10 U1 — 集火(AOE 反制)。全场存活 bot GameContext 注入挑战。
		{
			PropKey:     "magnet_challenge",
			NameZh:      "集火",
			NameEn:      "Magnet Challenge",
			NameJa:      "集中砲火",
			Description: "对全场存活 Agent 发起公开质疑——所有存活 Agent 的 GameContext 会被追加「N 号玩家公开质疑 X 号」,迫使它们调整策略。AOE 道具,中招率 35%。",
			Price:       150,
			BaseHitRate: 35,
			IsAOE:       true,
			TargetCamp:  "any",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropMagnetChallenge,
			InjectGenKey: "magnet_challenge",
			EffectSpec:  PropEffectSpec{EffectTypes: "agent_public_pressure"},
		},
		// §20260811-10 U2 — 心理侧写(纯查询型,§132 三处同步必备)。
		// InjectType 仅占位,不走 propInjectQueue,真实聚合走
		// ComputeBehaviorReportLocked → prop.behavior_report 帧单推购买者。
		{
			PropKey:     "behavior_analyze",
			NameZh:      "心理侧写",
			NameEn:      "Behavioral Analysis",
			NameJa:      "心理プロファイル",
			Description: "对指定 Agent 输出 4 维分析报告(发言矛盾率 / 情绪波动 / 投票一致性 / 阵营倾向概率),仅显示概率,不揭晓身份。100% 必中。",
			Price:       100,
			BaseHitRate: 100,
			IsAOE:       false,
			TargetCamp:  "any",
			Enabled:     true,
			MaxPerGame:  3,
			CooldownSec: 30,
			InjectType:  PropBehaviorAnalyze,
			InjectGenKey: "behavior_analyze",
			EffectSpec:  PropEffectSpec{EffectTypes: ""},
		},
	}
)

// BuildPropCatalogFromModels 从 DB 模型列表构造 PropCatalog。
func BuildPropCatalogFromModels(rows []models.TLsmGameProp) *PropCatalog {
	cat := &PropCatalog{
		byKey:   make(map[string]*PropCatalogEntry),
		all:     make([]*PropCatalogEntry, 0, len(rows)),
		enabled: make([]*PropCatalogEntry, 0, len(rows)),
	}
	for i := range rows {
		p := rows[i]
		injType, _ := PropInjectTypeFromKey(p.PropKey)
		injKey := p.InjectGenKey
		if injKey == "" {
			injKey = p.PropKey // 默认回退到 prop_key
		}
		entry := &PropCatalogEntry{
			ID:           p.ID,
			PropKey:      p.PropKey,
			NameZh:       p.NameZh,
			NameEn:       p.NameEn,
			NameJa:       p.NameJa,
			Description:  p.Description,
			Price:        p.Price,
			BaseHitRate:  p.BaseHitRate,
			IsAOE:        p.IsAOE,
			TargetCamp:   p.TargetCamp,
			Enabled:      p.Enabled,
			MaxPerGame:   p.MaxPerGame,
			CooldownSec:  p.CooldownSec,
			InjectType:   injType,
			InjectGenKey: injKey,
			EffectSpec: PropEffectSpec{
				EffectTypes:   p.EffectType,
				TwistSeatSrc: p.TwistSeatSrc,
			},
		}
		cat.byKey[p.PropKey] = entry
		cat.all = append(cat.all, entry)
		if p.Enabled {
			cat.enabled = append(cat.enabled, entry)
		}
	}
	return cat
}

// BuildDefaultPropCatalog 从代码内嵌默认道具构造 PropCatalog（用于 DB 空表或测试）。
func BuildDefaultPropCatalog() *PropCatalog {
	cat := &PropCatalog{
		byKey:   make(map[string]*PropCatalogEntry),
		all:     make([]*PropCatalogEntry, 0, len(defaultProps)),
		enabled: make([]*PropCatalogEntry, 0, len(defaultProps)),
	}
	for i := range defaultProps {
		p := defaultProps[i]
		injKey := p.InjectGenKey
		if injKey == "" {
			injKey = p.PropKey
		}
		entry := &PropCatalogEntry{
			PropKey:      p.PropKey,
			NameZh:       p.NameZh,
			NameEn:       p.NameEn,
			NameJa:       p.NameJa,
			Description:  p.Description,
			Price:        p.Price,
			BaseHitRate:  p.BaseHitRate,
			IsAOE:        p.IsAOE,
			TargetCamp:   p.TargetCamp,
			Enabled:      p.Enabled,
			MaxPerGame:   p.MaxPerGame,
			CooldownSec:  p.CooldownSec,
			InjectType:   p.InjectType,
			InjectGenKey: injKey,
			EffectSpec: PropEffectSpec{
				EffectTypes:   p.EffectSpec.EffectTypes,
				TwistSeatSrc: p.EffectSpec.TwistSeatSrc,
			},
		}
		cat.byKey[p.PropKey] = entry
		cat.all = append(cat.all, entry)
		if p.Enabled {
			cat.enabled = append(cat.enabled, entry)
		}
	}
	return cat
}

// Get 按 key 查找道具（含 disabled）。
func (c *PropCatalog) Get(key string) (*PropCatalogEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.byKey[key]
	return e, ok
}

// GetEnabled 按 key 查找已启用的道具。
func (c *PropCatalog) GetEnabled(key string) (*PropCatalogEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.byKey[key]
	if !ok || !e.Enabled {
		return nil, false
	}
	return e, true
}

// ListAll 返回所有道具（含 disabled）。
func (c *PropCatalog) ListAll() []*PropCatalogEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*PropCatalogEntry, len(c.all))
	copy(out, c.all)
	return out
}

// ListEnabled 返回已启用的道具。
func (c *PropCatalog) ListEnabled() []*PropCatalogEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*PropCatalogEntry, len(c.enabled))
	copy(out, c.enabled)
	return out
}

// GetDefaultProps 返回代码内嵌的 6 种默认道具定义（用于 DB 空表 seed）。
func GetDefaultProps() []PropCatalogEntry {
	out := make([]PropCatalogEntry, len(defaultProps))
	copy(out, defaultProps)
	return out
}

// ToModel 将目录条目转为 DB 模型（用于 seed / 更新）。
func (e *PropCatalogEntry) ToModel() models.TLsmGameProp {
	return models.TLsmGameProp{
		PropKey:      e.PropKey,
		NameZh:       e.NameZh,
		NameEn:       e.NameEn,
		NameJa:       e.NameJa,
		Description:  e.Description,
		Price:        e.Price,
		BaseHitRate:  e.BaseHitRate,
		IsAOE:        e.IsAOE,
		TargetCamp:   e.TargetCamp,
		Enabled:      e.Enabled,
		MaxPerGame:   e.MaxPerGame,
		CooldownSec:  e.CooldownSec,
		InjectGenKey: e.InjectGenKey,
		EffectType:   e.EffectSpec.EffectTypes,
		TwistSeatSrc: e.EffectSpec.TwistSeatSrc,
	}
}

// ToSnapshot 把目录条目压缩为 Agent GameContext 的道具快照
// (speak 阶段 buildAgentContextLocked 调,驱动 use_prop 工具 schema 动态生成)。
func (e *PropCatalogEntry) ToSnapshot() PropSnapshot {
	return PropSnapshot{
		PropKey:      e.PropKey,
		NameZh:       e.NameZh,
		NameEn:       e.NameEn,
		NameJa:       e.NameJa,
		Description:  e.Description,
		Price:        e.Price,
		BaseHitRate:  e.BaseHitRate,
		IsAOE:        e.IsAOE,
		InjectGenKey: e.InjectGenKey,
	}
}

// ResolveInjectGenKey 返回注入生成器的注册表 key（v2 数据驱动注册表入口）。
// 留空时兜底到 prop_key，保证旧道具无需改动即可接入 InjectRegistry。
func (e *PropCatalogEntry) ResolveInjectGenKey() string {
	if e.InjectGenKey != "" {
		return e.InjectGenKey
	}
	return e.PropKey
}
