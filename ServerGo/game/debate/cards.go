// Package debate — 辩论比赛内置辩题卡池。
//
// 2026-08-31 §20260831-01 — 30+ 内置辩题,覆盖:
//   - classic (10): 经典哲学/价值辩题
//   - new (10): 2025-2026 最新热门辩题
//   - policy (5): 政策型辩题
//   - divergent (5): 发散性议题
//   - tech (若干): 科技伦理题
//   - education (若干): 教育类
//   - custom: 用户自定义(通过创建房间时传入)
//
// 详见 docs/辩论比赛/03-辩论比赛房间创建与配置设计.md §2.3。
package debate

// BuiltInTopics 返回所有内置辩题的副本(避免外部修改)。
//
// 设计:每次调用都返回新切片,避免调用方误改全局状态。
func BuiltInTopics() []DebateTopic {
	out := make([]DebateTopic, len(builtinTopics))
	copy(out, builtinTopics)
	return out
}

// builtinTopics 内置辩题池(30+ 条,按设计文档 §2.3 完整收录)。
var builtinTopics = []DebateTopic{
	// === 经典辩题(10 条) ===
	{
		ID: "classic_001", Text: "人性本善 / 人性本恶",
		Type: "classic", Category: "classic",
		ProPosition: "人性本善", ConPosition: "人性本恶",
		Keywords:    []string{"哲学", "人性", "善恶", "孟荀"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "classic_002", Text: "温饱是 / 不是谈道德的必要条件",
		Type: "classic", Category: "classic",
		ProPosition: "温饱是谈道德的必要条件", ConPosition: "温饱不是谈道德的必要条件",
		Keywords:    []string{"道德", "经济基础", "物质"},
		Difficulty:  4, IsOfficial: true,
	},
	{
		ID: "classic_003", Text: "知难行易 / 知易行难",
		Type: "classic", Category: "classic",
		ProPosition: "知难行易", ConPosition: "知易行难",
		Keywords:    []string{"认知", "实践", "知行合一"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "classic_004", Text: "真理越辩越明 / 不会越辩越明",
		Type: "classic", Category: "classic",
		ProPosition: "真理越辩越明", ConPosition: "真理不会越辩越明",
		Keywords:    []string{"真理", "辩论", "认知"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "classic_005", Text: "网络拉近 / 疏远了人与人之间的距离",
		Type: "classic", Category: "classic",
		ProPosition: "网络拉近了人与人之间的距离", ConPosition: "网络疏远了人与人之间的距离",
		Keywords:    []string{"网络", "社交", "距离"},
		Difficulty:  2, IsOfficial: true,
	},
	{
		ID: "classic_006", Text: "金钱是 / 不是万恶之源",
		Type: "classic", Category: "classic",
		ProPosition: "金钱是万恶之源", ConPosition: "金钱不是万恶之源",
		Keywords:    []string{"金钱", "道德", "资本"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "classic_007", Text: "顺境 / 逆境更有利于人的成长",
		Type: "classic", Category: "classic",
		ProPosition: "顺境更有利于人的成长", ConPosition: "逆境更有利于人的成长",
		Keywords:    []string{"成长", "环境"},
		Difficulty:  2, IsOfficial: true,
	},
	{
		ID: "classic_008", Text: "现代社会更需要通才 / 专才",
		Type: "value", Category: "classic",
		ProPosition: "现代社会更需要通才", ConPosition: "现代社会更需要专才",
		Keywords:    []string{"通才", "专才", "教育"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "classic_009", Text: "集体利益 vs 个体自由,何者更重要",
		Type: "value", Category: "classic",
		ProPosition: "集体利益比个体自由更重要", ConPosition: "个体自由比集体利益更重要",
		Keywords:    []string{"集体", "个人", "自由"},
		Difficulty:  4, IsOfficial: true,
	},
	{
		ID: "classic_010", Text: "以成败论英雄是否可取",
		Type: "value", Category: "classic",
		ProPosition: "以成败论英雄可取", ConPosition: "以成败论英雄不可取",
		Keywords:    []string{"成败", "英雄", "评价"},
		Difficulty:  3, IsOfficial: true,
	},

	// === 最新辩题(2025-2026,10 条) ===
	{
		ID: "new_001", Text: "\"勇敢的人先享受世界\"是 / 不是一个陷阱",
		Type: "value", Category: "new",
		ProPosition: "勇敢的人先享受世界不是一个陷阱", ConPosition: "勇敢的人先享受世界是一个陷阱",
		Keywords:    []string{"勇气", "消费主义", "Z世代"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "new_002", Text: "\"向内求\"是 / 不是年轻人自我治愈的有效手段",
		Type: "value", Category: "new",
		ProPosition: "向内求是年轻人自我治愈的有效手段", ConPosition: "向内求不是年轻人自我治愈的有效手段",
		Keywords:    []string{"心理健康", "自省", "年轻"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "new_003", Text: "如果 AI 能写出更好的情书,我还要 / 不要自己写",
		Type: "tech", Category: "new",
		ProPosition: "我还要自己写", ConPosition: "我不要自己写",
		Keywords:    []string{"AI", "情感", "原创"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "new_004", Text: "身处\"奥德赛时期\"的年轻人应该 / 不应该追求确定性",
		Type: "value", Category: "new",
		ProPosition: "年轻人应该追求确定性", ConPosition: "年轻人不应该追求确定性",
		Keywords:    []string{"奥德赛时期", "确定性", "年轻人"},
		Difficulty:  4, IsOfficial: true,
	},
	{
		ID: "new_005", Text: "\"人工智能+\"背景下青年就业更具机遇 / 更具挑战",
		Type: "tech", Category: "new",
		ProPosition: "青年就业更具机遇", ConPosition: "青年就业更具挑战",
		Keywords:    []string{"AI", "就业", "青年"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "new_006", Text: "AI 提升了 / 降低了人类创作者存在的意义",
		Type: "tech", Category: "new",
		ProPosition: "AI 提升了人类创作者的意义", ConPosition: "AI 降低了人类创作者的意义",
		Keywords:    []string{"AI", "创作", "意义"},
		Difficulty:  4, IsOfficial: true,
	},
	{
		ID: "new_007", Text: "年轻人应该先攒钱再享受 / 先享受不必过度攒钱",
		Type: "value", Category: "new",
		ProPosition: "年轻人应该先攒钱再享受", ConPosition: "年轻人应该先享受不必过度攒钱",
		Keywords:    []string{"消费", "储蓄", "年轻人"},
		Difficulty:  2, IsOfficial: true,
	},
	{
		ID: "new_008", Text: "大学高中化让青春更值得 / 更不值得",
		Type: "education", Category: "new",
		ProPosition: "大学高中化让青春更值得", ConPosition: "大学高中化让青春更不值得",
		Keywords:    []string{"大学", "高中化", "青春"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "new_009", Text: "人工智能的规范发展更靠法律 / 伦理",
		Type: "tech", Category: "new",
		ProPosition: "AI 规范发展更靠法律", ConPosition: "AI 规范发展更靠伦理",
		Keywords:    []string{"AI", "法律", "伦理"},
		Difficulty:  4, IsOfficial: true,
	},
	{
		ID: "new_010", Text: "低能量人群应该逼自己一把 / 放自己一马",
		Type: "value", Category: "new",
		ProPosition: "低能量人群应该逼自己一把", ConPosition: "低能量人群应该放自己一马",
		Keywords:    []string{"自我驱动", "心理健康", "低能量"},
		Difficulty:  3, IsOfficial: true,
	},

	// === 政策型辩题(5 条) ===
	{
		ID: "policy_001", Text: "安乐死应该 / 不应该合法化",
		Type: "policy", Category: "policy",
		ProPosition: "安乐死应该合法化", ConPosition: "安乐死不应该合法化",
		Keywords:    []string{"安乐死", "伦理", "法律"},
		Difficulty:  4, IsOfficial: true,
	},
	{
		ID: "policy_002", Text: "碳排放税应该 / 不应该征收",
		Type: "policy", Category: "policy",
		ProPosition: "碳排放税应该征收", ConPosition: "碳排放税不应该征收",
		Keywords:    []string{"碳排放", "税收", "环保"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "policy_003", Text: "无人驾驶电车难题应优先保护乘客 / 行人",
		Type: "policy", Category: "policy",
		ProPosition: "无人驾驶电车难题应优先保护乘客", ConPosition: "无人驾驶电车难题应优先保护行人",
		Keywords:    []string{"自动驾驶", "伦理", "电车难题"},
		Difficulty:  4, IsOfficial: true,
	},
	{
		ID: "policy_004", Text: "基层「工作留痕」利大于弊 / 弊大于利",
		Type: "policy", Category: "policy",
		ProPosition: "工作留痕利大于弊", ConPosition: "工作留痕弊大于利",
		Keywords:    []string{"基层", "工作留痕", "形式主义"},
		Difficulty:  3, IsOfficial: true,
	},
	{
		ID: "policy_005", Text: "优先推动减工时 / 加工资更有助于打工人福祉",
		Type: "policy", Category: "policy",
		ProPosition: "优先推动减工时", ConPosition: "优先推动加工资",
		Keywords:    []string{"工时", "工资", "打工人"},
		Difficulty:  3, IsOfficial: true,
	},

	// === 发散性议题(5 条) ===
	{
		ID: "divergent_001", Text: "如何平衡效率与公平",
		Type: "divergent", Category: "divergent",
		ProPosition: "效率优先", ConPosition: "公平优先",
		Keywords:    []string{"效率", "公平", "社会"},
		Difficulty:  4, IsOfficial: true,
	},
	{
		ID: "divergent_002", Text: "未来 10 年人类社会最大的挑战是什么",
		Type: "divergent", Category: "divergent",
		ProPosition: "AI 带来的社会变革", ConPosition: "气候变化与资源",
		Keywords:    []string{"未来", "挑战", "趋势"},
		Difficulty:  5, IsOfficial: true,
	},
	{
		ID: "divergent_003", Text: "科技让人更自由还是更不自由",
		Type: "divergent", Category: "divergent",
		ProPosition: "科技让人更自由", ConPosition: "科技让人更不自由",
		Keywords:    []string{"科技", "自由", "依赖"},
		Difficulty:  4, IsOfficial: true,
	},
	{
		ID: "divergent_004", Text: "全球化 vs 本土化:未来的发展方向",
		Type: "divergent", Category: "divergent",
		ProPosition: "全球化是未来方向", ConPosition: "本土化是未来方向",
		Keywords:    []string{"全球化", "本土化", "文化"},
		Difficulty:  4, IsOfficial: true,
	},
	{
		ID: "divergent_005", Text: "如何构建一个更包容的社会",
		Type: "divergent", Category: "divergent",
		ProPosition: "以制度保障为主", ConPosition: "以文化教育为主",
		Keywords:    []string{"包容", "社会", "制度"},
		Difficulty:  4, IsOfficial: true,
	},
}

// FindTopicByID 根据 ID 查内置辩题;找不到返回 (nil, false)。
func FindTopicByID(id string) (*DebateTopic, bool) {
	for i := range builtinTopics {
		if builtinTopics[i].ID == id {
			t := builtinTopics[i] // 拷贝
			return &t, true
		}
	}
	return nil, false
}

// TopicsByType 按类型筛选辩题。
func TopicsByType(t string) []DebateTopic {
	out := []DebateTopic{}
	for _, tpc := range builtinTopics {
		if tpc.Type == t {
			out = append(out, tpc)
		}
	}
	return out
}

// TopicsByCategory 按细分分类筛选辩题。
func TopicsByCategory(cat string) []DebateTopic {
	out := []DebateTopic{}
	for _, tpc := range builtinTopics {
		if tpc.Category == cat {
			out = append(out, tpc)
		}
	}
	return out
}

// SearchTopics 简单关键词搜索(text / keywords 包含 substr)。
func SearchTopics(q string) []DebateTopic {
	if q == "" {
		return BuiltInTopics()
	}
	out := []DebateTopic{}
	for _, tpc := range builtinTopics {
		// 命中 text
		if containsFold(tpc.Text, q) {
			out = append(out, tpc)
			continue
		}
		// 命中 keywords
		for _, kw := range tpc.Keywords {
			if containsFold(kw, q) {
				out = append(out, tpc)
				break
			}
		}
	}
	return out
}

// containsFold 简单大小写不敏感子串匹配(中文按字符匹配,英文 ASCII 大小写折叠)。
func containsFold(s, sub string) bool {
	if sub == "" {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}