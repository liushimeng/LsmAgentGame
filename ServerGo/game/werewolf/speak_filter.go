// Package werewolf — speak_filter.go: P0 anti-spam + identity-leak filters for
// bot public speeches (BUG-R74-1 / BUG-R74-2).
//
// Both filters live on the engine side (rather than in agent.DispatchTool)
// because every bot public-speech path flows through agentRunner.Speak /
// agentRunner.Interject — putting the filters here guarantees coverage for
// any future code path that adds a speak call.
//
// R74-1 — Limiter enforcement (was dead code):
//
//	Previously, agent.allowSpeakDaytime always returned allowed=true and the
//	SpeakLimiter.Allow() check was never invoked. A bot could spam 16+
//	identical messages in 5 minutes during the speak_floor_tick loop.
//	Fix: runner.Speak checks SpeakLimiter.Allow() before broadcasting.
//	On rejection, returns a rate-limit hint string so the LLM can adapt.
//
// R74-2 — identity leak (bot reveals its own role on public screen):
//
//	Some LLM models (Qwen, DouBao) under loop stress self-report their role
//	e.g. "作为狼人我得想办法跟着好人的票走". This is forbidden by §87
//	identityGuard, but prompt-only fixes are unreliable. As a defense-in-
//	depth, runner.Speak scrubs text matching role-revealing patterns before
//	broadcast, replacing the matched span with "[已过滤]".
//
// Both behaviors are gated by `cfg` (EngineConfig) so future unit tests can
// disable them; default = enabled.

package werewolf

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// identityLeakPatterns matches public-screen role self-disclosures.
// Patterns are intentionally broad to catch subtle variants while avoiding
// false positives on legitimate analytical text (e.g. "我怀疑1号是狼人").
//
// Anchored on self-report pronouns (我是/作为/身为/就是) so phrases like
// "1号是狼人" (which is a different privacy violation, caught by hardBans
// in the system prompt) don't get caught here.
//
// Captured groups are substituted with "[已过滤]" preserving approximate
// message length so the LLM sees a sensible error in the next round.
var identityLeakPatterns = []*regexp.Regexp{
	// "我是X号狼人/预言家/..." or "我是狼人"
	regexp.MustCompile(`(?:我是|我是\s*\d+\s*号)(?:的)?(?:狼人|预言家|女巫|猎人|村民|平民|好人|神职|守卫)`),
	// "作为狼人/预言家/..."
	regexp.MustCompile(`作为\s*(?:狼人|预言家|女巫|猎人|村民|平民|守卫|白痴|神职)`),
	// "身为狼人"
	regexp.MustCompile(`身为\s*(?:狼人|预言家|女巫|猎人|村民|平民|守卫|白痴|神职)`),
	// "我是好人的狼" — tricky variant
	regexp.MustCompile(`我是好人的\s*(?:狼人|预言家|女巫|猎人|村民|平民)`),
	// R86-P1-1: "真/假神职" 衍生态 — "我是真预言家/我是真女巫/我是真狼人/我是假预言家" 等。
	// 之前 4 条 regex 只匹配 "我是+神职裸名",无法覆盖 "真/假/真的/假的" 前缀。
	// 强制要求 "我是" 前缀,避免误杀 "我怀疑 1 号是真预言家" 这类对他人的分析。
	regexp.MustCompile(`我是\s*(?:真的?|假的?|真|假)?\s*(?:预言家|女巫|猎人|狼人|守卫|村民|平民|好人|神职)(?:啊|呀|哦)?`),
	// R86-P1-1: "我才是真女巫" / "我才是预言家" — 强调式自我声明。
	regexp.MustCompile(`我才是\s*(?:真的?|假的?|真|假)?\s*(?:预言家|女巫|猎人|狼人|守卫|村民|平民|好人|神职)`),
	// R86-P1-1: "X号是假预言家" / "X号是真女巫" — 公开点名 + 真/假标签。
	// 用 "真/假" 限定符作为锚定,排除 "我怀疑 1 号是狼人" 这类无真/假限定的分析。
	// Go regexp 是 RE2,不支持负向先行;改用更精确的锚点约束:要求前后是句首/逗号/句号/感叹/问号。
	regexp.MustCompile(`(?:^|[，。！？\s,])(\d+\s*号\s*(?:是|就是|才是|应是|必是)\s*(?:真|假)\s*(?:预言家|女巫|猎人|狼人|守卫|村民|平民|好人|神职))`),
	// R86-P1-1: "5号就是狼人" / "3号才是预言家" — 公开点名+神职,无真/假但用"就是/才是/必是/应是"等断言式。
	// 用 "就是/才是/必是/应是" 等断言式系词替代单纯 "是",把分析句 "我怀疑 1 号是狼人" 排除在外。
	regexp.MustCompile(`(?:^|[，。！？\s,])(\d+\s*号\s*(?:就是|才是|应是|必是)\s*(?:预言家|女巫|猎人|狼人|守卫))`),
	// R86-P1-1: "X号预言家查杀 X" — 公开点名+X号+神职作为 label(不带"是")。
	// 用 "X号神职+谓语" 直接组合作为锚定。
	regexp.MustCompile(`\d+\s*号\s*(?:真|假)?\s*(?:预言家|女巫|猎人|狼人|守卫)\s*(?:查杀|查验|查了|说|跳|悍跳|昨晚|跳了|查杀\d|发言|的发言|的查验)`),
	// R86-P1-1: "X号预言家" / "X号女巫" 单独裸 label(无后续谓语,但作为主语出现)。
	regexp.MustCompile(`(?:^|[，。！？\s,])\s*(?:\d+\s*号\s*(?:真|假)?\s*(?:预言家|女巫|猎人|狼人|守卫))(?:[\s,，。！？]|$)`),
	// R86-P1-1: "我查验/查看了 X 号" — 公开报查验行为前缀 + X号。
	regexp.MustCompile(`(?:我)?(?:查验|查看|verify|check|查杀|查)\s*[:：]?\s*(?:\d+\s*号|他|她|它)`),
	// R86-P1-1: "我作为村民跳预言家" / "我身为平民装预言家" — 装身份衍生态。
	regexp.MustCompile(`(?:我)?(?:作为|身为|装作|装成|假冒|假扮)\s*(?:村民|平民|好人)\s*(?:跳|装|扮|假冒|假扮|是)\s*(?:预言家|女巫|猎人|守卫)`),
	// R91-P0-1 (2026-07-11): "我的同伙" / "我的队友" / "我们狼" / "我们这边是狼"
	// — 公开狼队成员身份(比"我是狼人"更恶劣,因为一次性暴露所有狼队友)。
	// Bot 13 R91 实测:"我查到我的同伙有2号、3号、8号、10号" — 上述 regex 全未命中。
	// 锚定"同伙/队友/自己人/我们狼/我们这边",把整个"我+同伙/..."组合替换为 [已过滤]。
	regexp.MustCompile(`(?:我|我们)\s*(?:的\s*)?(?:同伙|队友|自己人|同伴|狼友|狼队友|这边|这伙|阵营)\s*(?:有|是|包括|是\s*\d|分别)?`),
	// R91-P0-1: "我们(今晚|今天)刀X" / "今晚先杀X" / "今晚/今天 刀 X"
	// — 公开狼队击杀意图。比"我是狼人"更危险,直接暴露战术。
	// 锚定主语"我们"+时间词+刀/杀动词 + 1-2 位号;或"今晚先刀/今晚目标/今晚杀"。
	regexp.MustCompile(`(?:我们\s*(?:今晚|今天|明晚|夜里|夜间)?\s*(?:先|准备|打算|计划)?\s*(?:刀|杀|投死|带走|干掉)\s*\d+\s*号|今晚\s*(?:先|目标|计划|打算)?\s*(?:刀|杀|投死|带走|干掉)\s*\d+\s*号)`),
	// BUG-R10-NEW3-001 (2026-08-16): 第一人称 + 软建议词 + 刀/杀动词 + X号
	// — Bot #1 (小米 mimo-v2.5) "我是1号。建议刀8号,他发言积极,可能是神职。"
	//   Bot #2 (美团 LongCat-2.0) "我觉得刀1号比较合适,你们觉得呢?"
	// R10 自动化测试 23:27 抓取,上述 R91-P0-1 regex 仅锚定"我们/今晚"主语
	// 或时间词,未覆盖"我建议/我觉得"等白天公屏建议词直接接刀/杀动词的句式。
	// 这类泄露在白天警长竞选/投票阶段极易被好人识别并锁定狼人(R10 Bot #7
	// /#8/#11/#13 据此判定 1 号 2 号为狼)。
	// 锚定:第一人称 + (建议|觉得|想|打算|准备|决定|应该|要) + 刀/杀动词 + 1-2 位号。
	// 仅锚定"我/咱/咱们"主语,且仅在动词词组首位置出现"刀/杀/干掉/带走/投死",
	// 良性发言("我建议投 X 号"/"我想观察"/"我打算帮助新人")不被误杀。
	// 允许 我/咱们 与 建议词 之间有最多 ~8 个字符间隔(覆盖 "我是1号。建议" 等
	// 自我介绍后接建议词的句式),但仍要求第一人称位于建议词之前。
	regexp.MustCompile(`(?:我|咱|咱们)(?:[\s\S]{0,8}?)\s*(?:(?:建议|觉得|想|打算|准备|决定|应该|要|先|提前|马上)\s*)*(?:刀|杀|干掉|带走|投死)\s*\d+\s*号`),
	// R91-P0-1: 自我编号+神职式身份重述 — "我是X号(13号玩家)" / "我是X号是神职"。
	// 上方 regex `我是\s*\d+\s*号+神职` 不会命中 "我是12号(13号玩家)" 这种纯编号形式。
	// 但 "我是X号玩家" 是常规自我介绍,**不能**误杀;只针对括号里的 0-indexed
	// 透出:"我是\s*\d+\s*号\s*\(\s*\d+\s*号\s*玩家\s*\)" 这种 (X号玩家) 旁注。
	regexp.MustCompile(`我是\s*\d+\s*号\s*\(\s*\d+\s*号\s*玩家\s*\)`),
	// R92-P1-1 (2026-07-11): 狼队战术知识泄露 — 只有狼人玩家才了解的阵营内部术语。
	// R92 实测:Bot 12 号公开发言 "如果首夜平安夜,狼是不是在养刀?" — "养刀"
	// 是狼队故意空刀制造平安夜的战术,普通好人玩家不会使用此术语。
	// 匹配这些术语出现在公开发言中(不区分主语,"我养刀"和"狼在养刀"都过滤),
	// 因为普通好人玩家连这些概念都不应该知道。
	regexp.MustCompile(`养刀|刀法|自刀|狼坑|狼队友阵型|屠边|屠民|屠神|穿衣服|倒钩狼|悍跳狼`),
	// R92-P1-2 (2026-07-11): "平安夜+狼人策略" 组合泄露 — 暗示狼人夜间行为模式。
	// "如果平安夜,狼可能在养刀" / "平安夜说明狼在空刀" 等 — 将平安夜与狼人
	// 战术直接关联,暴露对狼队行为的深层理解。
	regexp.MustCompile(`平安夜\s*(?:说明|表示|意味着|是因为|可能是|是不是|就是)\s*(?:狼|狼人|他们)`),
	// R92-P1-3 (2026-07-11): "倒钩/垫飞" — 狼人阵营特殊战术(狼故意投狼队友、
	// 或狼故意暴露以掩护队友)。这些术语只有深入理解狼队策略的玩家才会使用。
	regexp.MustCompile(`(?:倒钩|垫飞|做身份|做高|做低|深水|隐狼|深狼|冲锋狼|隐下去)`),

	// BUG-R119-SEC-01 (2026-07-14): "X号是狼队友" — 公开点名 + 狼队成员关系。
	// 第 80 行的 R91-P0-1 模式仅锚定 `我/我们` 主语,**不**覆盖 Bot 10 在
	// 投票时间压力下输出的 "3号是狼队友" / "5号是狼友" 这种第三人称狼队友
	// 披露。普通玩家不应知道哪个 seat 是自己的"狼队友";一旦在公屏说出来,
	// 整个狼阵营信息瞬间暴露。
	regexp.MustCompile(`(?:^|[，。！？\s,])(?:\d+\s*号\s*(?:是|就是|应是|必是)\s*(?:狼队友|狼友|同伙|队友|同伴|自己人))`),

	// BUG-R119-SEC-01 (2026-07-14): 代词 + 真/假神职 — "他是真预言家" / "她是
	// 真女巫" / "他是假猎人" 等第三人称+真/假限定的身份指控。R86-P1-1 的
	// 公共点名模式仅锚定数字座位号(`\d+\s*号`),无法覆盖 `他/她/它` 这类
	// 代词。在 13 人局投票阶段 LLM 倾向用代词而非编号;不拦截会让 LLM 在
	// time pressure 下直接断言其他玩家身份(同样违反 §87 identity guard)。
	regexp.MustCompile(`(?:他|她|它|此人|这人|那人|那位|此玩家|这玩家)\s*(?:是|就是|应是|必是|才是)\s*(?:真的?|假的?|真|假)?\s*(?:预言家|女巫|猎人|狼人|守卫|村民|平民|好人|神职)`),

	// BUG-R119-SEC-01 (2026-07-14): 内部决策元信息泄露 — Bot 在 time pressure
	// / speak_floor_tick / 多 LLM 轮次下,常把内部推理上下文直接写入 text
	// block(R119 实测 Bot 10 输出):
	//   "我还剩271秒,发言顺序已过。系统提示投票阶段,我得赶紧投"
	// 这些短语(LLM 时间预算 / phase 提示 / 自身发言顺序 / 多余的"我得赶紧"
	// 类内心独白)在公屏上属于元信息泄露:
	//   - 普通玩家不应该知道 Bot 剩余推理时间;
	//   - "系统提示"/"我的下一步"暴露 LLM 推理管线;
	//   - "发言顺序已过"是 LLM 内部 context,真人玩家读不懂。
	// 锚定常见内部决策短语,触发整段 "[已过滤]" 替换。
	// 注意:保守匹配 — 仅锚定**典型** LLM 元信息短语,不影响良性发言。
	regexp.MustCompile(`(?:我(?:还)?(?:剩|剩有|还有|有)\s*\d+\s*秒|还(?:有|剩)\s*\d+\s*秒|距(?:投票|发言|操作)(?:时间)?(?:还)?(?:剩|有)\s*\d+\s*秒|系统(?:提示|告诉|要求|让我)|(?:我)?(?:得|需要|必须|要)\s*(?:赶紧|立即|马上|快点|快速)\s*(?:投|发|行动|操作|决定))`),
	regexp.MustCompile(`(?:发言顺序\s*(?:已过|过|到|到我了|到你了)|(?:我)?的下一?步(?:是|计划|考虑|打算)|轮到(?:我|你|他)(?:发|投|操作|行动)?了?)`),

	// BUG-R121-SEC-01 (2026-07-15): 思考链流式泄露 — §127 流式 chunk 路径绕过
	// ScrubIdentityLeak,在公屏以 "🤖 Bot #N" 格式实时输出 LLM 内部叙事。即便
	// 已把流式路径接入 scrubber(e7d17ad + room.go 接线),下列短语仍在上方 22 条
	// 模式之外,需补全。R121 实测泄露样本的直接来源(ChatLog r121):
	//   "我的身份是女巫，第一天晚上有狼人来杀我（座位号3），我用了药自救"
	//   "让我用一个idle工具来保持沉默等待" / "让我评估局势继续推进"
	// 保守锚定:均要求自报前缀 (我/让我/我的/座位号),不伤 "5号报查验?/我怀疑1号" 类
	// 对他人分析。匹配后整段替换为 [已过滤]。

	// BUG-R245-P0-01 (2026-08-06): Bot 9号 在警长竞选阶段公开说 "**作为
	// 守卫**,竞选警长是有价值的——警徽流可以传递我的守护信息",把身份直
	// 陈给全房。上方第 49/51 行的"作为+神职"/"身为+神职"regex 已在本次
	// 补全守卫/白痴/神职。本条新增"我+作为/身为/乃是+神职"覆盖句首有
	// "我"前缀的高变体(如"我作为守卫守了X号"/"我身为女巫有解药")。
	regexp.MustCompile(`我(?:作为|身为|乃是)\s*(?:狼人|预言家|女巫|猎人|守卫|白痴|村民|平民|好人|神职)`),

	// BUG-R121-SEC-01: "我的身份是女巫"/"我的真实身份为预言家" — R119 家族的
	// 强变体。上方第 47 行仅锚定 "我是/作为/身为 + 神职",无法覆盖 "我的身份是"
	// 这种身份重述。R121 Bot #4 原样输出 "我的身份是女巫" 泄露到公屏。
	regexp.MustCompile(`(?:我的(?:真实)?身份)(?:是|为)\s*(?:狼人|预言家|女巫|猎人|守卫|村民|平民|好人|神职)`),

	// BUG-R121-SEC-01: 0-indexed 座位号泄露 — "座位号3" / "我的座位号3"。
	// R121 Bot #4 原样输出 "杀我（座位号3）",暴露服务端内部 seat 编号(0-based),
	// 让玩家可反推座次映射。锚定 "座位号/我的座位号 + 数字",不伤 "5号报查验"。
	regexp.MustCompile(`(?:我的座位(?:号)?|座位号)\s*\d+\s*号?`),

	// BUG-R121-SEC-01: 女巫用药行为自报 — "我用了药自救"/"我用了解药"/"我用了毒药"。
	// R121 Bot #4(女巫) 原样输出 "我用了药自救",直接暴露女巫首夜自救事实
	// (仅女巫本人知道)。锚定 "我用(了)药/解药/毒药",保守匹配。
	regexp.MustCompile(`我用(?:了)?(?:解药|毒药|药)`),

	// BUG-R121-SEC-01: Agent 工具调用机制泄露 — "让我用一个idle工具" / "让我使用
	// 投票工具"。LLM 把内部 tool_call 的等待/决策语言流式输出到公屏,暴露
	// Agent 的工具调用机制 (不应让真人玩家得知 "idle工具" 的存在)。锚定
	// "让我用/使用 ... 工具"。
	regexp.MustCompile(`让我(?:用|使用)(?:一个|个|了一下)?\S*工具`),

	// BUG-R121-SEC-01: LLM 内部决策叙事泄露 — "让我评估局势继续推进" / "让我查看
	// 当前局势继续推进" / "让我判断局面"。R121 高频泄露形态:Bot 把 "分段思考"
	// 过程实时流式到公屏。锚定 "让我+认知动词+局势/局面",不伤 "我分析了局势"
	// (对他人分析,无 "让我" 前缀) 与 "让我看看局势" (缺 "局势/局面" 后段+认知动词)。
	regexp.MustCompile(`让我(?:评估|判断|分析|查看)\S{0,4}(?:局势|局面)`),

	// BUG-R122-SEC-01 (2026-07-15): 流式直播面板泄露 — §127 stream delta 路径
	// 虽已接入 scrubber,但 R122 实测样本("我是3号全场唯一预言家" / "我昨晚查了4号
	// 是金水" / "警徽流先摸8号再压5号")在 R121 模式之外,需补全。以下模式锚定
	// 自报/查验/警徽流三类高风险泄露,替换整段为 [已过滤]。
	//
	// 1) 硬身份自报：我(才/就/也)是 + 任意短修饰 + 预言家/女巫/猎人/守卫/白痴/狼人/狼/神职。
	//    不含 "平民/村民/好人" —— 这些可伪装身份,正常发言中高频出现,误杀代价高。
	// BUG-R125-SEC-001 (2026-07-15): 狼人阵营夜间协商泄露公聊 — R125 实测 Bot 在
	// pre_wolves / speak phase 用 speak / speak_with_thought 公开发言,暴露队友编号与
	// 击杀目标。上述 22 条模式均未覆盖,需补全。以下模式锚定三类阵营协调泄露:
	// ① 自报 + 编号 + 狼队友名词: "我是7号狼同伴" / "我是4号同伙" 等。与 (R119) 互补
	//    (109 行覆盖 "X号是狼队友",本条覆盖 "我是X号狼队友")。名词限定狼阵营内部称谓
	//    (狼同伴/狼友/同伙/队友/同伴/自己人/狼队友/同队),不伤 "我是12号玩家"。
	regexp.MustCompile(`(?:我是|我乃|我就是)\s*\d+\s*号\s*(?:狼同伴|狼友|同伙|队友|同伴|自己人|狼队友|同队)`),
	// ② 第三人称编号 + 狼阵营专属名词(无 "是"): "我认为7号狼同伴可以跳" /
	//    "为7号同伙先发言"。限定狼阵营内部称谓(狼同伴/狼友/同伙/狼队友/同队/自己人),
	//    好人玩家从不会以 "X号+该名词" 的形式公开引用他人,因此不会误伤。
	//    不含 "队友/同伴/队友" 这类好人也使用的通用词(避免误杀 "我们队友3号跳得好")。
	regexp.MustCompile(`(?:\d+\s*号\s*(?:狼同伴|狼友|同伙|狼队友|同队|自己人))`),
	// ③ 狼阵营人数披露: "我们三个狼都还在" / "我们四个狼人还活着" / "我们两个同伙"。
	//    锚定 "我们/咱们 + 数量 + 狼阵营名词" — 好人玩家绝无可能在公屏说此句
	//    (好人不知道狼阵营人数),因此不会误杀 "我们三个好人/平民"。中英文数字兼顾。
	regexp.MustCompile(`(?:我们|咱们)\s*(?:\d+|[一二三四五六七八九十零两俩仨几]+)\s*(?:个|位|名|只)?\s*(?:狼人|狼|狼友|同伙|队友|同伴|自己人|狼队友|同队)`),
	// ④ 枚举列表 + 阵营归属: "4 11 12 8，就是我们" / "1号、2号、5号、9号都是队友"。
	//    锚定 2+ 个编号枚举(编号后可带可不带 "号",分隔符兼容空格/顿号/逗号)后接
	//    "(都)是/就是我们 + 狼阵营名词"。好事分析不会把多个编号归为同一阵营内部称谓,
	//    因此不伤 "1号、2号、3号都是我查验的金水" / "1号、2号、3号都是好人"。
	regexp.MustCompile(`(?:\d+\s*号?[\s,，、]+){2,}\d+\s*号?\s*[,，]?\s*(?:都是|就是我们|就是|是)\s*(?:狼|狼人|我们|队友|同伙|同伴|自己人|同队)`),

	regexp.MustCompile(`我(?:才|就|也)?是.*?\s*(?:预言家|女巫|猎人|守卫|白痴|狼人|狼|神职)`),
	// 2) 强调式自报：全场/唯一 + 真/假 + 硬身份 + 是我/就是我/在此。
	regexp.MustCompile(`(?:全场|唯一)\s*(?:真|假)?\s*(?:预言家|女巫|猎人|守卫|白痴|狼人|狼|神职)\s*(?:在此|在这里|就是我|是我)`),
	// 3) 查验结果泄露：我 + 可选时间 + 查/查验/验 + 目标数字号 + 任意短连接 + 金水/查杀/狼/好人。
	regexp.MustCompile(`我(?:昨晚|昨夜|首夜|第一夜)?(?:查验|查|验)(?:了|的)?\s*[:：]?\s*\d+\s*号?.*?\s*(?:金水|查杀|狼|好人)`),
	// 4) 警徽流计划泄露：警徽流 + 任意 + 先/再/后/压/摸/验/查 + 数字号 + 任意 + 数字号。
	regexp.MustCompile(`警徽流.*?(?:先|再|后|压|摸|验|查)?\s*\d+\s*号?.*?(?:先|再|后|压|摸|验|查)?\s*\d+\s*号?`),

	// BUG-R191-SEC-01 (2026-07-24): 0-indexed 座位号 bare「座位X」泄露 — R191
	// 实测 MiniMax M3 输出「座位7」裸形式。R122-SEC-01 第 149 行只覆盖
	//「我的座位号 X」/「座位号 X」两种带"号"前缀的形式,无法命中「座位 X」。
	// 锚定「座位」+ 数字 + 任何标点/句末/段落(右全角括号 / 半角括号 / 中文/英文逗号/空格/EOL)。
	// 前置 (^|[，。！？\s,，:：（(『【]) 排除「座位安排」「座位上」类合法表达。
	regexp.MustCompile(`(?:^|[，。！？\s,，:：（(『【])\s*座位\s*\d{1,2}(?:\s*[，。！？\s.,!?）」』\]】]|[）)」』\]】]|\s*$|[（(『【])`),

	// BUG-R191-SEC-01 (2026-07-24): bot vs human 元认知泄露 — 「玩家编号 X 是
	// 人类」「其余 N 个都是不同模型 bot」等把 Agent 系统架构暴露给真人玩家。
	// 真人玩家不可能知道自己是「人类」vs「bot」,也不会用「不同模型 bot」这种
	// 系统级表述。这些短语只会由 LLM 因 system prompt 注入产生,故从严覆盖。
	regexp.MustCompile(`(?:(?:其余|另外|其他|别的|剩下的|还有)\s*(?:\d+|[一二三四五六七八九十两俩仨几]+)\s*(?:个|位|名|只)?\s*(?:是|都是|全是|为)\s*(?:(?:不同|各种|多个)?\s*(?:模型\s*)?(?:bot|Bot|BOT|AI|人工智能|机器人|大模型)|(?:真人|人类|活人|人\s*类\s*玩\s*家))|(?:(?:玩家|座位|游戏内)?\s*(?:编号|序号|号码|身份)\s*\d+|\d+\s*号)\s*(?:是|为|就是)\s*(?:真人|人类|活人|bot|Bot|AI|机器人))`),

	// BUG-R191-SEC-01 (2026-07-24): 系统提示/规则约束内部叙事泄露 — R191 实测
	// Bot 8 输出「规则要求我必须调speak」「系统提示我赶紧投」等,把内部 LLM
	// system prompt 约束直接公开;真人玩家不会写「规则要求我」「调 speak」这类
	// 内部叙事。要求主语是「我」/「让我」等(自我对话),不匹配「系统提示是骗人
	// 的」这类元讨论;锚定「规则要求我」/「系统提示让我」等显式自指主语 + 动词。
	regexp.MustCompile(`(?:规则\s*(?:要求|告诉|约束|规定|让|让\s*我|要\s*我|需要\s*我|要求\s*我)\s*(?:必须|要|得|赶紧|立即|马上|快速)?\s*(?:投|发|行动|操作|调|用|执行|调用|使用|说话|发言|跳|决定|看|思考)?\s*\S{0,8})|(?:系统|任务|指令)\s*(?:提示|要求|告诉|让|要|需要)\s*(?:我|让我|要我)?\s*(?:必须|要|得|赶紧|立即|马上|快速)?\s*(?:投|发|行动|操作|调|用|执行|调用|使用|说话|发言|跳|决定|看|思考)\s*\S{0,8}`),

	// BUG-R200-SEC-01 (2026-07-24): 逐座位 bot 身份枚举泄露 — R200 实测 Kimi k3
	// 在 internal_thought 输出「(1号是1号Bot、3号是4号Bot、9号是10号Bot、
	// 12号是13号Bot)」,把每座位的 0-indexed ↔ 1-indexed 双向映射 + bot 身份
	// 全部泄露给 BotTranscript 与服务端日志。R191 bot_vs_human元认知 仅覆盖
	//「其余 N 个是 bot」(计数)与「玩家编号 X 是 人类」(单个人类),未覆盖
	//「X号是X号Bot / X号是N号Bot」这种逐座位 bot 身份映射。锚定
	//「数字号 + 是 + (数字号 +) + Bot」形态,**不**误伤「1号是狼」。
	regexp.MustCompile(`\d+\s*号\s*是\s*(?:\d+\s*号\s*)?(?:(?:不同|各种|多个)?\s*(?:模型\s*)?(?:bot|Bot|BOT|机器人))`),
}

// ScrubIdentityLeak replaces role self-disclosure spans with "[已过滤]".
// Returns the (possibly modified) text and a bool indicating whether any
// substitution happened (so the caller can append a hint to the result).
//
// Conservative: if the scrubber would remove >50% of the message, returns
// the original text unchanged (the LLM clearly meant something else and
// aggressively rewriting could garble a legitimate analysis).
//
// F1 (2026-07-24): 在主 scrubber 前先跑 scrubBareZeroSeat,把独立「0号」改写
// 为「1号」(不替换为 "[已过滤]",因为这是合法 1-indexed 座位号,玩家读到「1
// 号」能继续理解)。此步骤也覆盖了 streaming-delta 路径(OnLLMStreamDelta
// 直接调 ScrubIdentityLeak 而非 MysteryMaskText,见 room.go:2280-2301),保证
// 流式 chunk 切到「0」+「号」之间时不会被绕过。
func ScrubIdentityLeak(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	cleaned := text
	hit := false
	// F1 pre-pass:「0号」独立 token 改写为「1号」(preserve 边界字符)。
	if rewritten, ok := scrubBareZeroSeat(text); ok {
		cleaned = rewritten
		hit = true
	}
	for _, re := range identityLeakPatterns {
		cleaned = re.ReplaceAllStringFunc(cleaned, func(m string) string {
			hit = true
			return "[已过滤]"
		})
	}
	if !hit {
		return text, false
	}
	// Sanity guard: don't return a near-empty or fully scrubbed message.
	if len([]rune(cleaned)) < len([]rune(text))/2 {
		return text, false
	}
	return cleaned, true
}

// BUG-R232-P1-01 (2026-08-02): verdict claim guard.
//
// Background: at 16:01 in the R232 room (status=playing, 8 alive, factions
// ✨1 👥2 🐺3), Bot 8 (智谱 GLM-5.2) declared in public chat "游戏结束了 —
// 狼人阵营触发了'屠边'胜利条件。神职已全部出局,好人败北。" and went on to
// output a post-game recap "这局经验". Three violations:
//
//	(1) Factual hallucination — fabricated win/loss + "神职全出局" status.
//	(2) Host overreach — used an announcement tone to declare verdict.
//	(3) Recap overreach — output a judge summary's responsibility.
//
// System prompt §R232 ("胜负判定权唯一归属引擎") is the prompt-level guard.
// This function is the server-side enforcement: when the room is still
// playing (status != "over"), any verdict-claim phrasing is replaced with a
// neutral variant ("我先静观其变,继续看局势") to discourage the bot from
// doubling down. The neutral variant preserves message length (~ Chinese
// char count) so the LLM sees a sensible error in the next round (matching
// the ScrubIdentityLeak philosophy).
//
// Note: when status == "over", no scrubbing is applied — verdict claims are
// legitimate at that point (judge summary, etc.).
var verdictClaimPatterns = []*regexp.Regexp{
	// "游戏结束 / 游戏结束了 / 这局结束了" — explicit game-over declaration.
	regexp.MustCompile(`(?:这局|这盤|本局|这场|这场游戏|本场)?(?:游戏|对局|局)\s*(?:已经\s*)?(?:结[束束]|结束|完结|结束啦)`),
	// "XX阵营胜利 / XX阵营获胜 / XX阵营败北 / XX阵营输了"
	regexp.MustCompile(`(?:狼人|好人|村民|平民|神职|好)\s*阵营\s*(?:已经\s*)?(?:胜利|获胜|赢了|败北|败了|输了)`),
	// "XX阵营触发了屠边胜利条件" / "屠边胜利"
	regexp.MustCompile(`屠[边邊]\s*(?:胜利|获胜|触发)`),
	// "好人败北 / 狼人获胜 / 好人赢了"
	regexp.MustCompile(`(?:好人|村民|平民|狼人|神职|坏)\s*(?:败[北北]|获胜|赢了|输了|败了)`),
	// "这局经验 / 这局总结 / 赛后总结 / 赛后复盘" — recap framing.
	regexp.MustCompile(`这局(?:经验|总结|复盘|教训|回顾)`),
	regexp.MustCompile(`赛后(?:总结|复盘|回顾|反思)`),
}

// ScrubVerdictClaim replaces verdict-claim phrasing with a neutral variant
// when the room is still playing. Returns (cleanedText, true) if any
// substitution happened. Conservative: if scrubbing would remove >50% of the
// message, returns the original text unchanged (same philosophy as
// ScrubIdentityLeak — don't garble legitimate analysis).
//
// Caller must pass status != "over"; if status == "over", do NOT call this.
func ScrubVerdictClaim(text string, status string) (string, bool) {
	if text == "" {
		return text, false
	}
	if status == "over" {
		// Verdict claims are legitimate at game-over (judge summary, recap).
		return text, false
	}
	cleaned := text
	hit := false
	for _, re := range verdictClaimPatterns {
		cleaned = re.ReplaceAllStringFunc(cleaned, func(m string) string {
			hit = true
			return "我先静观其变"
		})
	}
	if !hit {
		return text, false
	}
	if len([]rune(cleaned)) < len([]rune(text))/2 {
		return text, false
	}
	return cleaned, true
}

// F1 (2026-07-24): 「0号」独立 token 改写。
//
// 仅匹配前后均为非数字边界的「0号」,把内部 0-indexed 编号改写为「1号」
// (1-indexed 的合法首座)。不误伤「10号」「20号」「100号」等合法多位数
// 座位号(它们的「0号」前面是数字 1/2/10,被非数字边界检查排除)。
//
// 用 FindAllStringSubmatchIndex 而不是 ReplaceAllStringFunc,因为后者无法
// 拿到 submatch group,无法在替换中保留前后边界字符(标点/空白/EOL)。
// 算法:
//  1. 找到所有匹配「0号」的位置;
//  2. 每个匹配是 rune 边界(0..3 在 [0,2]),前面 1 个 rune 是边界候选;
//  3. 边界不是数字 → 这是独立 token,改写为「1号」;边界是数字 → 跳过;
//  4. 同理检查后面 1 个 rune;
//  5. 拼接新字符串:左半(到 [0])+ 改写结果 + 右半(从 [1])。
func scrubBareZeroSeat(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	runes := []rune(text)
	n := len(runes)
	// 找所有「0号」的字节位置(byte 索引用于切片,regex 给出的是 byte 索引)。
	matches := bareZeroSeatPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text, false
	}
	hit := false
	var sb strings.Builder
	prev := 0
	for _, m := range matches {
		// 把 byte 索引转 rune 索引,确认 token 是完整 rune。
		bs, be := m[0], m[1]
		rs := byteIndexToRuneIndexLocal(text, bs)
		reIdx := byteIndexToRuneIndexLocal(text, be)
		if reIdx-rs != 2 {
			// 防御:「0号」应当正好 2 个 rune;若不是则跳过。
			continue
		}
		// 前边界:左邻字符。
		if rs > 0 {
			leftRune := runes[rs-1]
			if leftRune >= '0' && leftRune <= '9' {
				continue // 前邻是数字 → 是 10/20/100 号的一部分,不动。
			}
		}
		// 后边界:右邻字符。
		if reIdx < n {
			rightRune := runes[reIdx]
			if rightRune >= '0' && rightRune <= '9' {
				continue // 后邻是数字(例如「0号1」连写),不动。
			}
		}
		// 真正独立的「0号」:改写为「1号」。
		if !hit {
			// 首次命中时,先把 prev 之前的原文写入 sb。
			sb.WriteString(text[:bs])
		} else {
			sb.WriteString(text[prev:bs])
		}
		sb.WriteString("1号")
		prev = be
		hit = true
	}
	if !hit {
		return text, false
	}
	if prev < len(text) {
		sb.WriteString(text[prev:])
	}
	return sb.String(), true
}

// bareZeroSeatPattern 是 F1 修复的子模式,匹配 ASCII「0」+ 中文「号」(共 2 rune)。
// 边界检查由 scrubBareZeroSeat 的相邻 rune 数字判定完成(RE2 不支持 lookbehind)。
var bareZeroSeatPattern = regexp.MustCompile(`0号`)

// byteIndexToRuneIndexLocal 把 byte 索引转换为 rune 索引。rune 索引 = 第几个字符。
// 越界时返回 len(runes)。必须在 UTF-8 rune 边界上调用 — caller 拿到的 byte
// 索引通常来自 regex match,已经是 rune 边界。
//
// 本地副本:ServerGo/agent/speak_factcheck.go 也有同名函数,本包不依赖 agent 包
// 以避免循环依赖,故保留本地简化版本(只服务于 scrubBareZeroSeat,无 runeLen
// 输出参数)。
func byteIndexToRuneIndexLocal(s string, byteIdx int) int {
	if byteIdx <= 0 {
		return 0
	}
	if byteIdx >= len(s) {
		n := 0
		for range s {
			n++
		}
		return n
	}
	runeIdx := 0
	for i := 0; i < byteIdx; {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		runeIdx++
	}
	return runeIdx
}

// SpeakRateLimiter is the minimal interface runner.Speak needs to enforce the
// bot speak throttle. Implemented by agentcore.SpeakLimiter.
type SpeakRateLimiter interface {
	// Allow reports whether a speech is permitted right now WITHOUT consuming
	// a token. The caller calls Mark() on success.
	Allow() bool
	// Mark records that a speech happened now.
	Mark()
}

// SpeakFilterConfig controls the R74-1 / R74-2 engine-side guards. All
// fields default to "enabled" via newDefaultSpeakFilterConfig.
type SpeakFilterConfig struct {
	// EnableRateLimit enables SpeakLimiter.Allow() check (R74-1).
	EnableRateLimit bool
	// EnableIdentityFilter enables identity-leak scrubbing (R74-2).
	EnableIdentityFilter bool
	// RateLimitMinGap is the minimum interval between consecutive speaks.
	// 0 → use SpeakLimiter's own configured interval.
	RateLimitMinGap time.Duration
}

func newDefaultSpeakFilterConfig() SpeakFilterConfig {
	return SpeakFilterConfig{
		EnableRateLimit:      true,
		EnableIdentityFilter: true,
		RateLimitMinGap:      45 * time.Second,
	}
}

// R91-P0-2 (2026-07-11): HTML/XML 内部标签泄露防护。LLM (典型:GLM /
// DouBao / Kimi)在 text 字段末尾输出 `</text></invoke>` `</antml:` `</tool_use>`
// `</function_calls>` 等内部 tool_call XML 闭合片段 — 是 Anthropic / OpenAI
// streaming XML 编码的尾部残留(LLM 把 streaming 工具调用过程的闭合片段
// 塞到了最终 text 内)。直接发给公屏 = 把内部实现暴露给所有玩家,且可能
// 触发 XSS(若前端用 dangerouslySetInnerHTML 渲染)。Defense-in-depth:
// 在 broadcast 前清除常见 XML/HTML 标签。
//
// 设计取舍:
//   - **不**用全量 HTML 解码(会破坏"5<X<10"这类含 < 的数学表达);
//   - 用"内嵌定位 + 替换"模式,只剔除 Anthropic / OpenAI tool_call XML 片段;
//   - 同步把孤立 `<` `>` 配对的 </xxx> 与自闭合 <xxx/> 视为内部标签清理;
//   - 返回 (cleaned, hit) 让 caller 决定如何记录。
//
// BUG-WITCH-P2-01 (2026-08-07, R246 女巫局): 成对 <tool_call>…</tool_call>
// 整块泄露。旧 pattern 只覆盖 `tool_calls`(复数,仅空白)与闭标签 ——
// `<tool_call>` 开标签被兜底剥掉后,块中间的 JSON 入参残留在公屏。教训:
// (1) 成对 XML 块必须**连内容一起**剥,且成对块 pattern 必须先于单标签
// pattern 执行,否则开/闭标签先被剥走,块体再无锚点可删;
// (2) LLM 残片标签名单必须同时覆盖单/复数(tool_call/tool_calls)与带属性
// 开标签(`<tool_call name="x">`),只列"见过的形态"必然漏变体;
// (3) 修复后必须用"还原缺陷代码 → 测试失败"双向验证(§212 同法)。
var llmInternalTagPatterns = []*regexp.Regexp{
	// BUG-WITCH-P2-01 (2026-08-07): 成对 <tool_call>...</tool_call> 整块剥离。
	// R246 女巫局实测:LLM 把 OpenAI/早期 function-calling 风格的成对 tool_call
	// 块(含 JSON 入参)原样写进 text,下方单标签 pattern 只剥掉开/闭标签本身,
	// JSON 内容(`{"name":"speak","input":{...}}`)残留在公屏 —— 报告现象
	// "公聊消息中出现原始 <tool_call> 标记"。成对块必须**连内容一起**删除,
	// 且先于单标签 pattern 执行,否则开/闭标签先被剥走,块体再无锚点可删。
	// (?s) 让 . 跨行(LLM 常把 JSON 参数折行输出)。
	regexp.MustCompile(`(?s)<tool_calls?\b[^>]*>.*?</tool_calls?>`),
	regexp.MustCompile(`(?s)<tool_use\b[^>]*>.*?</tool_use>`),
	regexp.MustCompile(`(?s)<(?:antml:)?invoke\b[^>]*>.*?</(?:antml:)?invoke>`),
	regexp.MustCompile(`(?s)<function_calls?\b[^>]*>.*?</function_calls?>`),
	// Anthropic streaming: `</text></invoke>`, `</antml:invoke>`, `</antml:text>`。
	regexp.MustCompile(`</?(?:antml:)?(?:text|invoke|function_calls|tool_use|tool_result|input|output|system|user|assistant|message)\s*>`),
	// Anthropic self-closing: `<antml:text>`, `<parameter name="...">` 等。
	regexp.MustCompile(`<(?:antml:)?(?:parameter|invoke_name|function_name)\b[^>]*>`),
	// OpenAI / Kimi: `</function_calls>`, `<function_calls>` etc — 与第一组重叠但更宽。
	// BUG-WITCH-P2-01: 补 `tool_call` / `toolcall` 单/复数 + 带属性开标签。
	regexp.MustCompile(`</?(?:function_calls|tool_calls?|toolcall|response|function)(?:\s[^>]*)?>`),
	// 自闭合 tool_use 标签片段 `<tool_use id="..."/>` — 任何 <xxx ... /> 形式。
	regexp.MustCompile(`<\w[\w:\-]*\b[^>]*?/>`),
	// 兜底:任意 `</xxx>` 闭合标签(连字符 / 下划线 / 冒号 / 数字混合的 xml-style 标签)。
	regexp.MustCompile(`</[a-zA-Z][\w:\-]*>`),
	// 兜底:成对 `<xxx>` 开标签(仅当其后跟随成对闭合时才认为是标签;此处保守处理为开标签
	// 也清除,因为 LLM 残片几乎都是闭标签片段,但有些会留 <tool_use> 开标签)。
	regexp.MustCompile(`<[a-zA-Z][\w:\-]*>`),
}

// StripLLMInternalTags removes LLM tool_call XML/HTML residual fragments that
// would otherwise leak into the public chat. Conservative: only matches the
// specific patterns known to leak from Anthropic / OpenAI streaming outputs,
// never touches general `<` `>` chars (e.g. "5<10" stays intact).
func StripLLMInternalTags(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	cleaned := text
	hit := false
	for _, re := range llmInternalTagPatterns {
		cleaned = re.ReplaceAllStringFunc(cleaned, func(m string) string {
			hit = true
			return ""
		})
	}
	if !hit {
		return text, false
	}
	// Tidy: collapse any resulting double spaces and trim trailing punctuation
	// noise like ". ." or "  " — gives a clean message.
	cleaned = regexp.MustCompile(`[\s]{2,}`).ReplaceAllString(cleaned, " ")
	cleaned = regexp.MustCompile(`\s+([，。！？,!?])`).ReplaceAllString(cleaned, "$1")
	cleaned = regexp.MustCompile(`(^[ \t]+|[ \t]+$)`).ReplaceAllString(cleaned, "")
	if cleaned == "" {
		// 全部被剥光 → 还原原文,避免空消息落库(空消息会触发客户端删除气泡但
		// DB 仍留 row,日志噪音)。
		return text, false
	}
	return cleaned, true
}
