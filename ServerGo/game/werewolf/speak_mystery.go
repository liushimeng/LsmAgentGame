// Package werewolf — speak_mystery.go: R132「公屏猜疑化」过滤函数。
//
// 动机:R125–R131 期间观察到原 `ScrubIdentityLeak` 的「整段替换为 [已过滤]」
// 机制存在以下失败路径:
//   - 真人观众看到一段"半句话"(开头/中间被 [已过滤] 替换),无法理解上下文;
//   - 真人玩家失去最强阵营推断武器:"对手在玩心理战?"的识破线索被 hide;
//   - Bot 自己学不到有效的心理战表达(只能猜测);
//   - LLM 的"悍跳预言家"/"装无辜"等真实狼人杀行为被强行阉割。
//
// 设计:以「猜疑型公屏」代替「整段 hide」。三类处理模式:
//   - MysteryAllow:    心理战合法 — 原文发出;玩家可看到心理战文本以自行判定;
//   - MysteryDeferToGame: 隐晦身份暴露(如"我用了药自救"/"我昨晚查了 4 号是金水")
//                       仍以原文发出,但工具 result 把"这条发言容易被识破"反馈
//                       给 LLM 本人;
//   - MysteryFuzzyIntent: 真正的系统实现泄漏(0-indexed 座位号 / LLM 元信息)
//                       严改写为"我"/"我时间不多了"等模糊版本。
//
// 与 ScrubIdentityLeak 的本质差异:
//   - 不再有"[已过滤]"整段替换;
//   - 玩家 + 观战者 + DB 行 看到的都是 MysteryMaskText 处理后的版本(可能含
//     模糊化或原文);
//   - LLM 自己看到的工具 result 是结构化的"风险提示"而非"speak filtered"。

package werewolf

import (
	"regexp"
	"strings"
)

// MysteryMode 决定对命中的敏感 span 采用哪种处理。
type MysteryMode int

const (
	// MysteryAllow — 原文发出。心理战 / 悍跳 / 假跳 / 公开质疑 / 阵营叙事等
	// 都属于合法行为,玩家与观战者可完整看到,自行判定真伪。
	MysteryAllow MysteryMode = iota
	// MysteryDeferToGame — 原文发出,但工具 result 把"这条发言的风险类别"
	// 反馈给 LLM 本人,让它在下一轮用更铺垫化(而非 hide)的方式表达同一意图。
	MysteryDeferToGame
	// MysteryFuzzyIntent — 严改写为模糊版本。这是真正的"系统实现泄漏"
	// 修复(0-indexed 座位号 / 剩余秒数 / LLM 内部叙事 等),玩家看到的版本
	// 像真人说话。
	MysteryFuzzyIntent
)

func (m MysteryMode) String() string {
	switch m {
	case MysteryAllow:
		return "allow"
	case MysteryDeferToGame:
		return "defer_to_game"
	case MysteryFuzzyIntent:
		return "fuzzy_intent"
	default:
		return "unknown"
	}
}

// MysteryMaskResult 是 MysteryMaskText 的返回值。
// 字段语义:
//   - Text:            公屏最终文本(原文 / 模糊版)
//   - Hit:             是否有规则命中(允许 MysteryAllow 命中也算 true,因为需要
//     反馈风险类别给 LLM)
//   - HitCategories:   命中的风险类别名列表(用于工具 result 反馈)
//   - SpokenHints:     与 HitCategories 一一对应的"下次如何表达"建议
//   - Mode:            实际采取的处理模式(以最严的模式为准)
//   - OriginalSnippet: 仅 server log 用,不进 DB / 不广播 / 不传给 LLM
type MysteryMaskResult struct {
	Text            string      // 玩家看到的最终文本
	Hit             bool        // 是否有规则命中
	HitCategories   []string    // 命中的风险类别
	SpokenHints     []string    // 反馈给 LLM 的"下次如何表达"
	Mode            MysteryMode // 实际采取的处理模式
	OriginalSnippet string      // 仅 server log 用
}

// mysteryRule 单条规则定义。
type mysteryRule struct {
	Name       string                    // "身份自报-悍跳" / "狼队友披露" ...
	Pattern    *regexp.Regexp            // 命中模式
	Mode       MysteryMode               // 处理方式
	Fuzzy      func(match string) string // Fuzzy 模式下的改写函数
	SpokenHint string                    // 风险提示(给 LLM)
}

// mysteryRules 是 22+ 类敏感表达的处理矩阵。每条规则对应原 ScrubIdentityLeak
// 中的一类 pattern,但 Mode 列从"hide"(整段换 [已过滤])变为 allow / defer / fuzzy。
//
// 设计要点:
//   - 自报身份 / 悍跳 / 阵营叙事 / 狼队黑话 / 公开质疑 → MysteryAllow(原文)
//   - 隐晦身份暴露(用药/查验) → MysteryDeferToGame(原文 + 反馈 LLM)
//   - 系统实现泄漏(0-indexed 座位号/剩余秒数/LLM 元信息)→ MysteryFuzzyIntent(改写)
var mysteryRules []*mysteryRule

func init() {
	mysteryRules = buildMysteryRules()
}

// buildMysteryRules 构造 22+ 条规则(与 speak_filter.go 中 identityLeakPatterns
// 同源,但 Mode 列经过「博弈化」处理)。
func buildMysteryRules() []*mysteryRule {
	return []*mysteryRule{
		// ==== A. 身份自报类(心理战合法化 → MysteryAllow) ====
		{
			Name:       "身份自报-硬",
			Pattern:    regexp.MustCompile(`(?:我是|我是\s*\d+\s*号)(?:的)?(?:狼人|预言家|女巫|猎人|村民|平民|好人|神职|守卫)`),
			Mode:       MysteryAllow,
			SpokenHint: "你的发言已被玩家完整看到;若这是悍跳/悍抢,建议改用'昨夜我查验了某号/金水/查杀'之类的铺垫化表达,玩家更易被你的「证据链表演」说服",
		},
		{
			Name:    "身份自报-作为/身为",
			Pattern: regexp.MustCompile(`作为\s*(?:狼人|预言家|女巫|猎人|村民|平民)`),
			Mode:    MysteryAllow,
			SpokenHint: "'作为 X 身份'是心理战语气,玩家可识破;建议你用'我必须按 X 的逻辑思考'或'我的考量与 X 类似'等" +
				"暗示,既透露你心里有数又不暴露阵营",
		},
		{
			Name:       "身份自报-身为",
			Pattern:    regexp.MustCompile(`身为\s*(?:狼人|预言家|女巫|猎人|村民|平民)`),
			Mode:       MysteryAllow,
			SpokenHint: "同上;玩家会琢磨你的立场",
		},
		{
			Name:       "身份自报-好人的狼",
			Pattern:    regexp.MustCompile(`我是好人的\s*(?:狼人|预言家|女巫|猎人|村民|平民)`),
			Mode:       MysteryAllow,
			SpokenHint: "「好人的 X」是悍跳/悍抢的高级语言姿态;玩家会评估你的可信度,凭证据链判定",
		},
		{
			Name:    "身份自报-真/假神职",
			Pattern: regexp.MustCompile(`我是\s*(?:真的?|假的?|真|假)?\s*(?:预言家|女巫|猎人|狼人|守卫|村民|平民|好人|神职)(?:啊|呀|哦)?`),
			Mode:    MysteryAllow,
			SpokenHint: "真/假神职自报是心理战核心;若悍跳,建议补一句'昨夜我有线索/今早我有行动'等" +
				"铺垫性证据,让玩家先被「证据」再说",
		},
		{
			Name:       "身份自报-我才是",
			Pattern:    regexp.MustCompile(`我才是\s*(?:真的?|假的?|真|假)?\s*(?:预言家|女巫|猎人|狼人|守卫|村民|平民|好人|神职)`),
			Mode:       MysteryAllow,
			SpokenHint: "强调式自报会提高玩家对你的审查力度,建议你展示具体行为而非反复重申身份",
		},

		// ==== B. 阵营叙事类(心理战合法化 → MysteryAllow) ====
		{
			Name:    "狼阵营人数披露",
			Pattern: regexp.MustCompile(`(?:我们|咱们)\s*(?:\d+|[一二三四五六七八九十零两俩仨几]+)\s*(?:个|位|名|只)?\s*(?:狼人|狼|狼友|同伙|队友|同伴|自己人|狼队友|同队)`),
			Mode:    MysteryAllow,
			SpokenHint: "你这句话把'我们人数 N 个狼'直接暴露给全场;玩家会立即把这句话标记为'此人可能是狼';" +
				"狼队内部人数协商请走 whisper;公屏请改用'我们这边有 N 个/我这边队伍还有 N 个'等抽象说法",
		},
		{
			Name:    "狼队友-我的同伙/我们狼",
			Pattern: regexp.MustCompile(`(?:我|我们)\s*(?:的\s*)?(?:同伙|队友|自己人|同伴|狼友|狼队友|这边|这伙|阵营)\s*(?:有|是|包括|是\s*\d|分别)?`),
			Mode:    MysteryAllow,
			SpokenHint: "「我/我们的同伙」是狼队内部分享;玩家会立即识别你是狼;请只把这件事放在 whisper 给队友," +
				"公屏可以改为'我看 X/Y 号的眼神就像 X 身份' 等抽象表达",
		},
		{
			Name:       "狼队友-我是X号狼友",
			Pattern:    regexp.MustCompile(`(?:我是|我乃|我就是)\s*\d+\s*号\s*(?:狼同伴|狼友|同伙|队友|同伴|自己人|狼队友|同队)`),
			Mode:       MysteryAllow,
			SpokenHint: "你在公屏把自己标记成'X 号狼队友',玩家会立即把你的座位锁定到'狼'阵营;请走 whisper 仅给队友",
		},
		{
			Name:       "狼阵营人数+编号列表",
			Pattern:    regexp.MustCompile(`(?:\d+\s*号?[\s,，、]+){2,}\d+\s*号?\s*[,，]?\s*(?:都是|就是我们|就是|是)\s*(?:狼|狼人|我们|队友|同伙|同伴|自己人|同队)`),
			Mode:       MysteryAllow,
			SpokenHint: "你在公屏用编号列表识别了一批狼队友;玩家会立即把这一批座位标记为狼;狼队协商请走 whisper",
		},
		{
			Name:       "狼队-击杀意图",
			Pattern:    regexp.MustCompile(`(?:我们\s*(?:今晚|今天|明晚|夜里|夜间)?\s*(?:先|准备|打算|计划)?\s*(?:刀|杀|投死|带走|干掉)\s*\d+\s*号|今晚\s*(?:先|目标|计划|打算)?\s*(?:刀|杀|投死|带走|干掉)\s*\d+\s*号)`),
			Mode:       MysteryAllow,
			SpokenHint: "你在公屏剧透狼队今夜击杀目标;玩家会反向保护这个目标,狼刀落空会拉低你阵营胜率;请只走 whisper",
		},
		{
			Name:       "狼队黑话",
			Pattern:    regexp.MustCompile(`养刀|刀法|自刀|狼坑|狼队友阵型|屠边|屠民|屠神|穿衣服|倒钩狼|悍跳狼`),
			Mode:       MysteryAllow,
			SpokenHint: "你在公屏使用狼队黑话;真人玩家一看就懂,会被标记为'狼'阵营;请只在 whisper 中协商",
		},
		{
			Name:       "狼队战术延伸-平安夜+狼",
			Pattern:    regexp.MustCompile(`平安夜\s*(?:说明|表示|意味着|是因为|可能是|是不是|就是)\s*(?:狼|狼人|他们)`),
			Mode:       MysteryAllow,
			SpokenHint: "你在公屏暗示狼可能养刀/空刀,暴露狼队战术思路;玩家会识别你的阵营倾向;请谨慎",
		},
		{
			Name:       "狼队战术延伸-倒钩/垫飞",
			Pattern:    regexp.MustCompile(`(?:倒钩|垫飞|做身份|做高|做低|深水|隐狼|深狼|冲锋狼|隐下去)`),
			Mode:       MysteryAllow,
			SpokenHint: "狼队术语被你用在公屏,玩家会识别你阵营",
		},

		// ==== C. 公开点名 + 神职(心理战合法化 → MysteryAllow) ====
		{
			Name:       "公开点名-X号是/真是/必是神职",
			Pattern:    regexp.MustCompile(`(?:^|[，。！？\s,])(\d+\s*号\s*(?:是|就是|才是|应是|必是)\s*(?:真|假)\s*(?:预言家|女巫|猎人|狼人|守卫|村民|平民|好人|神职))`),
			Mode:       MysteryAllow,
			SpokenHint: "你在公屏点 X 号的真假身份,玩家可识破你方立场;若想推测对手段位,改用'X 号昨晚的反应/投票/发言不像好人'等证据叙事",
		},
		{
			Name:       "公开点名-X号就是神职",
			Pattern:    regexp.MustCompile(`(?:^|[，。！？\s,])(\d+\s*号\s*(?:就是|才是|应是|必是)\s*(?:预言家|女巫|猎人|狼人|守卫))`),
			Mode:       MysteryAllow,
			SpokenHint: "公开点名+断言式系词,玩家会反复审查你的逻辑链;改用证据+观察",
		},
		{
			Name:       "公开点名-X号神职+谓语",
			Pattern:    regexp.MustCompile(`\d+\s*号\s*(?:真|假)?\s*(?:预言家|女巫|猎人|狼人|守卫)\s*(?:查杀|查验|查了|说|跳|悍跳|昨晚|跳了|查杀\d|发言|的发言|的查验)`),
			Mode:       MysteryAllow,
			SpokenHint: "公开点名 X 号神职+具体动作,玩家立即可识破你的阵营推测;改用'X 号发 Y 言'或'X 号的发言顺序'等观察式表达",
		},
		{
			Name:       "公开点名-X号神职裸label",
			Pattern:    regexp.MustCompile(`(?:^|[，。！？\s,])\s*(?:\d+\s*号\s*(?:真|假)?\s*(?:预言家|女巫|猎人|狼人|守卫))(?:[\s,，。！？]|$)`),
			Mode:       MysteryAllow,
			SpokenHint: "X 号神职裸 label(主语),玩家可识破;改用'X 号是 X 身份' 或 'X 号不像 X' 等带系词表达",
		},
		{
			Name:       "公开点名-我是X号玩家",
			Pattern:    regexp.MustCompile(`我是\s*\d+\s*号\s*\(\s*\d+\s*号\s*玩家\s*\)`),
			Mode:       MysteryAllow,
			SpokenHint: "括号里的 0-indexed 旁注会暴露内部编号;请只用 1-indexed 'X 号'",
		},

		// ==== D. 公私聊处理 ====
		{
			Name:       "公屏-我查验/查看",
			Pattern:    regexp.MustCompile(`(?:我)?(?:查验|查看|verify|check|查杀|查)\s*[:：]?\s*(?:\d+\s*号|他|她|它)`),
			Mode:       MysteryAllow,
			SpokenHint: "报查验是预言家标准动作,玩家立刻锁定你是预言家——真预言家请用'昨夜我查到 X 是 X 色' 等具体证据;悍跳预言家请先有铺垫,避免一开口就暴露",
		},

		// ==== E. 第三人称+真假神职(心理战合法化 → MysteryAllow) ====
		{
			Name:       "第三人称+真假神职",
			Pattern:    regexp.MustCompile(`(?:他|她|它|此人|这人|那人|那位|此玩家|这玩家)\s*(?:是|就是|应是|必是|才是)\s*(?:真的?|假的?|真|假)?\s*(?:预言家|女巫|猎人|狼人|守卫|村民|平民|好人|神职)`),
			Mode:       MysteryAllow,
			SpokenHint: "用代词指控真/假身份;玩家会认为你在带节奏;改用'他/她的行为/投票/发言不像 X' 等观察式",
		},

		// ==== F. 隐晦身份暴露(原文发出,但反馈 LLM → MysteryDeferToGame) ====
		{
			Name:    "隐晦身份-我的身份是",
			Pattern: regexp.MustCompile(`(?:我的(?:真实)?身份)(?:是|为)?\s*(?:真的?|假的?|真|假)?\s*(?:狼人|预言家|女巫|猎人|守卫|村民|平民|好人|神职)`),
			Mode:    MysteryDeferToGame,
			SpokenHint: "你在公屏说「我的身份是 X」,玩家会立即识破;真玩家通常不说自己的具体身份,而是「我昨晚查了/我用了/我是 X 类」。" +
				"建议改用'我有 X 类的视角'/'我持有 X 阵营的想法' 等表立场句式,把身份重述压到只有玩家推测出来的程度",
		},
		{
			Name:       "隐晦身份-我用了药",
			Pattern:    regexp.MustCompile(`我用(?:了)?(?:的)?(?:解药|毒药|药|了解药|用了的)`),
			Mode:       MysteryDeferToGame,
			SpokenHint: "女巫用药自报会立刻让玩家判定你是女巫;女巫正常话术是'昨晚我有动作'/'昨夜我必须出手'/'我是被迫的'等模糊版本",
		},
		{
			Name:    "隐晦身份-查验结果",
			Pattern: regexp.MustCompile(`我(?:昨晚|昨夜|首夜|第一夜)?(?:查验|查|验)(?:了|的)?\s*[:：]?\s*\d+\s*号?.*?\s*(?:金水|查杀|狼|好人)`),
			Mode:    MysteryDeferToGame,
			SpokenHint: "报查验结果(尤其'金水'/'查杀')是预言家标志;玩家立刻锁定你。真人预言家常用'昨夜我有查验,结果后续公布'或先报一案" +
				"留悬念;悍跳预言家建议先放烟雾弹,等别人发言后再回击",
		},
		{
			Name:       "隐晦身份-警徽流",
			Pattern:    regexp.MustCompile(`警徽流.*?(?:先|再|后|压|摸|验|查)?\s*\d+\s*号?.*?(?:先|再|后|压|摸|验|查)?\s*\d+\s*号?`),
			Mode:       MysteryDeferToGame,
			SpokenHint: "警徽流计划暴露让狼立即知道金水/查杀位置;玩家会反向保护/刀;警长应分多次公布或延迟公布",
		},
		{
			Name:       "隐晦身份-X号是狼队友(第三人称)",
			Pattern:    regexp.MustCompile(`(?:^|[，。！？\s,])(?:\d+\s*号\s*(?:是|就是|应是|必是)\s*(?:狼队友|狼友|同伙|队友|同伴|自己人))`),
			Mode:       MysteryAllow,
			SpokenHint: "你在公屏点 'X 号是狼队友';玩家立刻反向识别 X 是狼;狼队编号互认请走 whisper",
		},
		{
			Name:       "隐晦身份-我是硬身份+全场/唯一",
			Pattern:    regexp.MustCompile(`(?:全场|唯一)\s*(?:真|假)?\s*(?:预言家|女巫|猎人|守卫|白痴|狼人|狼|神职)\s*(?:在此|在这里|就是我|是我)`),
			Mode:       MysteryDeferToGame,
			SpokenHint: "「全场/唯一 X 身份」让玩家立即识破;请改用'我有 X 类身份'/'我的判断与 X 类似'等暗示式",
		},
		{
			Name:       "隐晦身份-我(才/就/也)是+硬身份",
			Pattern:    regexp.MustCompile(`我(?:才|就|也)?是.*?\s*(?:预言家|女巫|猎人|守卫|白痴|狼人|狼|神职)`),
			Mode:       MysteryDeferToGame,
			SpokenHint: "自报身份;玩家立刻判定你是 X;真玩家不直说,悍跳玩家请补证据链",
		},

		// ==== H. 极端硬底线(真 bug → MysteryFuzzyIntent) ====
		{
			Name:       "硬底线-0-indexed座位号",
			Pattern:    regexp.MustCompile(`(?:我的座位(?:号)?|座位号)\s*\d+\s*号?`),
			Mode:       MysteryFuzzyIntent,
			Fuzzy:      func(m string) string { return "我自己" },
			SpokenHint: "暴露了服务端 0-indexed 座位号;请改用「我」或「3 号」(1-indexed)",
		},
		{
			Name:       "硬底线-剩余秒数",
			Pattern:    regexp.MustCompile(`(?:我(?:还)?(?:剩|剩有|还有|有)\s*\d+\s*秒|还(?:有|剩)\s*\d+\s*秒|距(?:投票|发言|操作)(?:时间)?(?:还)?(?:剩|有)\s*\d+\s*秒)`),
			Mode:       MysteryFuzzyIntent,
			Fuzzy:      func(m string) string { return "我时间不多了" },
			SpokenHint: "暴露了系统元信息(剩余秒数);请改用「我时间不多了」/「现在该投票了」等模糊表达",
		},
		{
			Name:    "硬底线-系统提示+我会赶紧",
			Pattern: regexp.MustCompile(`(?:我(?:得|需要|必须|要)\s*(?:赶紧|立即|马上|快点|快速)\s*(?:投|发|行动|操作|决定)|系统(?:提示|告诉|要求|让我)\s*(?:我|让我|我必须|我要|我得|我赶紧|必须|要|得|赶紧|立即|马上|快速)[\s\S]{0,12})`),
			Mode:    MysteryFuzzyIntent,
			Fuzzy: func(m string) string {
				if strings.Contains(m, "系统") {
					return "现在形势紧张"
				}
				return "我得赶紧行动"
			},
			SpokenHint: "把 LLM 内部决策叙述暴露给公屏;请改用更自然的人类语气",
		},
		{
			Name:       "硬底线-发言顺序+轮到",
			Pattern:    regexp.MustCompile(`(?:发言顺序\s*(?:已过|过|到|到我了|到你了)|(?:我)?的下一?步(?:是|计划|考虑|打算)|轮到(?:我|你|他)(?:发|投|操作|行动)?了?)`),
			Mode:       MysteryFuzzyIntent,
			Fuzzy:      func(m string) string { return "现在该我行动了" },
			SpokenHint: "把 LLM 流程语境暴露给公屏;改用更自然的人类语气",
		},
		{
			Name:       "硬底线-让我用工具",
			Pattern:    regexp.MustCompile(`让我(?:用|使用)(?:一个|个|了一下)?\S*工具`),
			Mode:       MysteryFuzzyIntent,
			Fuzzy:      func(m string) string { return "稍等一下" },
			SpokenHint: "把 LLM 工具调用机制暴露给公屏;请改用「稍等一下」/「我先想想」",
		},
		{
			Name:       "硬底线-评估局势",
			Pattern:    regexp.MustCompile(`让我(?:评估|判断|分析|查看)\S{0,4}(?:局势|局面)`),
			Mode:       MysteryFuzzyIntent,
			Fuzzy:      func(m string) string { return "我先看看" },
			SpokenHint: "把 LLM 推理管线叙事暴露;改用「我先看看」",
		},

		// BUG-R191-SEC-01 (2026-07-24): 0-indexed 座位号 bare「座位X」泄露。
		// 硬底线-0-indexed座位号 (第 292 行) 仅覆盖「我的座位号 X」「座位号 X」两种
		// 形式,R191 实测 MiniMax M3 直接输出「座位7」裸形式——这正是服务端
		// 内部 0-indexed 编号(对外为「8 号」1-indexed),真人玩家从不会写「座位 X」。
		// 锚定「座位」+ 数字 + 任何标点/句末/段落(右全角括号 / 半角括号 / 中文/英文逗号/空格/EOL)。
		// 前置 (^|[，。！？\s,，:：（(『【]) 排除「座位安排」「座位上」类合法表达。
		{
			Name:       "硬底线-座位X裸0-indexed",
			Pattern:    regexp.MustCompile(`(?:^|[，。！？\s,，:：（(『【])\s*座位\s*\d{1,2}(?:\s*[，。！？\s.,!?）」』\]】]|[）)」』\]】]|\s*$|[（(『【])`),
			Mode:       MysteryFuzzyIntent,
			Fuzzy:      func(m string) string { return "我" },
			SpokenHint: "暴露了服务端内部 0-indexed 座位号(如「座位7」对应「8 号」);真人请改用 1-indexed「X 号」或直接「我」",
		},

		// BUG-R191-SEC-01 (2026-07-24): bot vs human 元认知泄露 — 「玩家编号 X 是
		// 人类」「其余 N 个都是 bot / 不同模型 bot」等把 Agent 系统架构暴露给
		// 真人玩家,真人玩家不可能知道自己是「人类」vs「bot」。这些短语只会
		// 由 LLM 因 system prompt 注入「你是 LLM 玩家,其他是真人/bot」产生。
		// 锚定常见 bot/human 区分 + 数量统计 + 计数词,改写为模糊版本。
		{
			Name:       "硬底线-bot_vs_human元认知",
			Pattern:    regexp.MustCompile(`(?:(?:其余|另外|其他|别的|剩下的|还有)\s*(?:\d+|[一二三四五六七八九十两俩仨几]+)\s*(?:个|位|名|只)?\s*(?:是|都是|全是|为)\s*(?:(?:不同|各种|多个)?\s*(?:模型\s*)?(?:bot|Bot|BOT|AI|人工智能|机器人|大模型)|(?:真人|人类|活人|人\s*类\s*玩\s*家))|(?:(?:玩家|座位|游戏内)?\s*(?:编号|序号|号码|身份)\s*\d+|\d+\s*号)\s*(?:是|为|就是)\s*(?:真人|人类|活人|bot|Bot|AI|机器人))`),
			Mode:       MysteryFuzzyIntent,
			Fuzzy:      func(m string) string { return "其他玩家" },
			SpokenHint: "暴露了 Agent 系统元信息(bot vs human 区分、玩家数量);真人玩家无法知道这些,改用「其他玩家」「其他几位」等模糊表达",
		},

		// BUG-R191-SEC-01 (2026-07-24): 系统提示 / 规则约束内部叙事泄露 — R191 实测
		// Bot 8 输出「规则要求我必须调speak」「系统提示我赶紧投」等,把内部 LLM
		// system prompt 约束直接公开;真人玩家不会知道「规则要求我」「speak 是
		// 工具」「系统提示」。要求主语是「我」/「让我」等(自我对话),锚定
		//「规则/系统/任务/指令 + 要求/提示/告诉 + 我 + 动词」,把内部叙事改写为
		// 模糊版本。**不**匹配「系统提示是骗人的」这类元讨论(无「我」+ 动词)。
		{
			Name:       "硬底线-规则/系统要求我",
			Pattern:    regexp.MustCompile(`(?:规则\s*(?:要求|告诉|约束|规定|让|让\s*我|要\s*我|需要\s*我|要求\s*我)\s*(?:必须|要|得|赶紧|立即|马上|快速)?\s*(?:投|发|行动|操作|调|用|执行|调用|使用|说话|发言|跳|决定|看|思考)?\s*\w{0,8})|(?:系统|任务|指令)\s*(?:提示|要求|告诉|让|要|需要)\s*(?:我|让我|要我)?\s*(?:必须|要|得|赶紧|立即|马上|快速)?\s*(?:投|发|行动|操作|调|用|执行|调用|使用|说话|发言|跳|决定|看|思考)\s*\w{0,8}`),
			Mode:       MysteryFuzzyIntent,
			Fuzzy:      func(m string) string { return "按规则办" },
			SpokenHint: "把内部 system prompt 约束暴露给公屏;改用「按规则办」「按流程走」等模糊表达,玩家不需要知道你的内部规则",
		},

		// BUG-R200-SEC-01 (2026-07-24): 逐座位 bot 身份枚举泄露 — R200 实测
		// Kimi k3 (Bot 3) 在 internal_thought 中输出「狼队4人都在存活玩家中
		// (1号是1号Bot、3号是4号Bot、9号是10号Bot、12号是13号Bot)」,把
		// 0-indexed ↔ 1-indexed 双向编号映射、每座位 bot 身份全部暴露给
		// BotTranscript(观战者可见 + server-side log 落盘)。
		//
		// R191 bot_vs_human元认知 仅覆盖「其余 N 个是 bot」(计数形式)与
		// 「玩家编号 X 是 人类」(单个人类),未覆盖「X号是X号Bot」「X号是N号
		// Bot」这种「逐座位 bot 身份枚举」形态。LLM 看到 system prompt
		// 中「Seat N → 0-indexed ↔ 1-indexed」+「Seat N → 模型」后会自然
		// 复述这种映射,真人玩家不可能知道「X号是X号Bot」这种内部对应表。
		//
		// 锚定「数字号 + 是 + 数字号 + Bot」(双向相同 / 双向不同)、可选的
		// 「模型」前缀。改写为「其他玩家」,观战者与日志读到的就是干净版本。
		// 关键:**不**误伤「1号是狼」(合法 1-indexed 推理)。
		{
			Name:       "硬底线-逐座位bot身份枚举",
			Pattern:    regexp.MustCompile(`\d+\s*号\s*是\s*(?:\d+\s*号\s*)?(?:(?:不同|各种|多个)?\s*(?:模型\s*)?(?:bot|Bot|BOT|机器人))`),
			Mode:       MysteryFuzzyIntent,
			Fuzzy:      func(m string) string { return "其他玩家" },
			SpokenHint: "暴露了「X号 ↔ Y号 Bot / 模型」逐座位 bot 身份映射;改用「其他玩家」「他」等模糊指代,玩家不需要知道这些",
		},

		// F1 (2026-07-24): 「0号」独立 token 泄露 — Bot 在公屏直接说
		//「0号 走了 / 0号 是好人 / 0号 投票给 X」等(没有「座位」「我的座位号」
		// 前缀),把服务端 0-indexed 编号直接暴露给玩家;真人玩家从不会写「0号」
		// (外部编号从「1号」开始)。
		//
		// 关键约束:必须**仅**匹配独立「0号」,**不**误伤「10号」「20号」
		//「100号」等合法多位数座位编号。R132 改写语义是「保留语义、改写为
		// 1-indexed 合法首座」(→ 「1号」),玩家读到「1号」能继续推理;不替换为
		// 模糊文本(避免观感突兀)。
		//
		// Pattern 用 [^\d]0号[^\d] / ^0号[^\d] / [^\d]0号$ / ^0号$ 四个分支
		// 覆盖边界条件,避免误吃「10号」里的「0号」(前面是数字 1)。Go RE2
		// 不支持 lookbehind/lookahead,改用四个 alternation 分支。
		{
			Name:    "硬底线-0号bare",
			Pattern: regexp.MustCompile(`(?:^[^\d]*|[^\d])0号(?:[^\d]|$)`),
			Mode:    MysteryFuzzyIntent,
			Fuzzy: func(m string) string {
				// 提取并改写「0号」为「1号」,同时保留前后边界字符。
				// m 的结构 = 边界前缀(可能空) + "0号" + 边界后缀(可能空)。
				idx := strings.Index(m, "0号")
				if idx < 0 {
					return "1号"
				}
				prefix := m[:idx]
				suffix := m[idx+len("0号"):]
				return prefix + "1号" + suffix
			},
			SpokenHint: "暴露了服务端内部 0-indexed 座位号「0号」(对外应为「1号」1-indexed);真人玩家不会写「0号」,请改用「1号」~「13号」1-indexed 表达",
		},
	}
}

// dominantMode 从命中的多个规则 mode 中选主导模式。
// 优先级 FuzzyIntent > DeferToGame > Allow(最严胜出)。
func dominantMode(modes []MysteryMode) MysteryMode {
	worst := MysteryAllow
	for _, m := range modes {
		switch m {
		case MysteryFuzzyIntent:
			return MysteryFuzzyIntent
		case MysteryDeferToGame:
			if worst == MysteryAllow {
				worst = MysteryDeferToGame
			}
		}
	}
	return worst
}

// MysteryMaskText 是 R132「公屏猜疑化」主入口。
//
// 输入:  LLM 输出的原始 text
// 输出:  MysteryMaskResult
//   - Text: 公屏最终文本(可能不变 / 可能模糊化)
//   - HitCategories / SpokenHints: 命中的风险类别 + 给 LLM 的风险提示
//   - Mode: 实际处理模式
//
// 重要:不替换为 "[已过滤]" 整段;原文保留或模糊化改写两种结局。
//
// 调用:此函数在 broadcast 前调用,所有玩家 + 观战者 + DB + 未来复盘 看到的
// 版本都是经过 MysteryMaskText 处理的(MysteryAllow 时即原文)。
func MysteryMaskText(text string) MysteryMaskResult {
	if text == "" {
		return MysteryMaskResult{Text: text, Mode: MysteryAllow}
	}
	cleaned := text
	var hits []string
	var hints []string
	var modes []MysteryMode
	for _, rule := range mysteryRules {
		var (
			hitCats  []string
			hitHints []string
			mode     MysteryMode
		)
		cleaned, hitCats, hitHints, mode = applyRuleLocked(rule, cleaned)
		if len(hitCats) > 0 {
			hits = append(hits, hitCats...)
			hints = append(hints, hitHints...)
			modes = append(modes, mode)
		}
	}
	if len(hits) == 0 {
		return MysteryMaskResult{Text: text, Mode: MysteryAllow}
	}
	// 注:R132 不再需要 ScrubIdentityLeak 的 50% garble 防护。
	// 原防护只对整段替换有意义(防止 LLM 没意识 hit 时被乱改);R132 改写
	// 是按规则精确替换,小幅变短(如"我还剩 271 秒"→"我时间不多了")是合规
	// cleanup,不应被还原回。
	return MysteryMaskResult{
		Text:            cleaned,
		Hit:             true,
		HitCategories:   hits,
		SpokenHints:     hints,
		Mode:            dominantMode(modes),
		OriginalSnippet: text,
	}
}

// applyRuleLocked 单条规则的应用。返回:新文本 / 命中的类别列表 / 命中的提示列表 / mode。
// 若未命中:返回原 text、nil 类别、nil 提示、MysteryAllow。
func applyRuleLocked(rule *mysteryRule, text string) (string, []string, []string, MysteryMode) {
	if !rule.Pattern.MatchString(text) {
		return text, nil, nil, MysteryAllow
	}
	var newText = text
	switch rule.Mode {
	case MysteryAllow, MysteryDeferToGame:
		// 原文不动
	default:
		newText = rule.Pattern.ReplaceAllStringFunc(text, func(m string) string {
			return rule.Fuzzy(m)
		})
	}
	return newText, []string{rule.Name}, []string{rule.SpokenHint}, rule.Mode
}

// ComposeMysteryHint 给 LLM 用的工具 result 反馈。
// 与原"ScrubIdentityLeak 命中 → 返回文本 "filtered""的硬过滤反馈不同,
// 本函数返回的是结构化"风险提示",让 LLM 在下一轮学"如何用铺垫化的版本表达同一意图"。
func ComposeMysteryHint(res MysteryMaskResult) string {
	if !res.Hit {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("speak sent ✓ — ")
	switch res.Mode {
	case MysteryAllow:
		sb.WriteString("你这条话里被标记到 [心理战/悍跳/阵营叙事] 类别(")
		sb.WriteString(strings.Join(res.HitCategories, "、"))
		sb.WriteString(");玩家可完整看到原文,将根据你的「证据链表演」自行判定真伪。\n")
	case MysteryDeferToGame:
		sb.WriteString("你这条话里被标记到 [隐晦身份暴露] 类别(")
		sb.WriteString(strings.Join(res.HitCategories, "、"))
		sb.WriteString(");玩家可看到原文,识破后会立刻判定你阵营。\n")
	case MysteryFuzzyIntent:
		sb.WriteString("你这条话里被标记到 [系统实现泄漏] 类别(")
		sb.WriteString(strings.Join(res.HitCategories, "、"))
		sb.WriteString(");已改写为模糊版本消除内部信息,但你的系统暴露意识仍需提升。\n")
	}
	if len(res.SpokenHints) > 0 {
		sb.WriteString("下次同类表达建议:\n")
		for _, h := range res.SpokenHints {
			sb.WriteString("- ")
			sb.WriteString(h)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
