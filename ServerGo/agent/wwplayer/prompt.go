// Package agent — prompt.go: system + user prompt builders for a werewolf agent.
//
// 瘦身原则 (2026-07-13):
//   - Identity (role/seat/faction) 由 Memory 的 identity turn 注入,system
//     字段不再重复,同一段 agent 代码可驱动任意座位。
//   - System 字段固定三段:规则 + 硬约束 + 工具清单。复盘、欺骗、情绪、
//     阶段时钟等冗长引导全部删除,让 LLM 把 token 放在当前局实时推理上,
//     Agent 自己决策游戏流程。
//   - 只产品化 13 人标准竞技局。werewolf_7 / werewolf_12 入口保留,但创建
//     时强制走 13 人 deck(参见 service/room_service.go)。
package wwplayer

import (
	"sort"
	"strings"
	"time"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
)

// BuildSystemPrompt 返回每次 LLM 调用都带上的 system 指令块。
// 只产品化 13 人标准竞技局:固定由「规则 + 博弈/心理战框架 + 系统硬约束 + 工具清单」四段组成。
// 历史 7/12 人局及复盘/欺骗/情绪/阶段时钟等冗长引导已删除。R132 进一步从「禁词清单」
// 转向「心理博弈框架」,把禁词改写为可学习的表达策略。
//
// §20260810-01 D3 修复:移除原 role string 参数(此前从未在函数体内被引用,
// 是 §130「声明了却从不接线」在 prompt 层的复现)。身份差异化由 Memory 的
// identity turn 注入(见 run.go),system 层不再按角色分叉。
//
// §20260810-10 U2:selfPortrait 是可选的「模型自画像」文本(由房间装配层基于
// t_lsm_game_model_game_log 聚合生成,见 service.ModelLogService.SelfPortraits)。
// 非空时追加到 system 末尾(缓存友好:一局内不变,命中 Anthropic prompt cache 前缀);
// 空串时输出与历史版本**逐字节一致**(全部既有测试不受影响)。
// 与 §20260810-01 D3 的本质区别:本参数在函数体内被真实消费(拼进返回值)。
//
// §20260811-04 U2:personality 是可选的「人设倾向参数」(5 维向量)。
// 仅在非零向量时追加到 selfPortrait 之后,前缀(selfPortrait 段)字节级别不变
// → Anthropic prompt cache 命中。空向量 → 输出与 §20260810-10 U2 字节一致。
func BuildSystemPrompt(selfPortrait string, personality PersonalityVector, personalityPresetKey string, difficultyDirective string) []llm.SystemBlock {
	rules := "【13 人标准竞技局规则】\n" +
		"配置(随机牌组): 4 狼人 + 1 预言家(必出) + 从神职池随机抽取 2~3 个神职 + 平民补齐 = 13 人。\n" +
		"神职池: 女巫 / 猎人 / 白痴 / 守卫 / 骑士 / 猎魔人。\n" +
		"胜利: 狼人屠边 = 杀光全部神职 OR 杀光全部平民; 好人 = 放逐全部 4 狼人。\n" +
		"阶段: 夜晚(狼人可空刀+互看 → 预言家查 → 女巫不能同晚双药) → 黎明(公布死亡+遗言+警徽流结算) → Day1 警长竞选 → 发言 → 投票放逐 → 白痴翻牌(免死失投票权) → 遗言 → 猎人开枪(被毒不可)。\n" +
		"关键: 警长=预言家,警徽流夜间传验人信息(金水/查杀);白天可自爆;预言家只知阵营不知具体身份;猎人被刀/被放逐可开枪(被毒不可);白痴被投票最高票可翻牌免死但仍存活发言。\n" +
		"⚠️ 死者身份牌全程不翻开 —— 普通死亡不公开身份,仅「白痴翻牌 / 狼人自爆 / 猎人开枪 / 终局」4 类事件公开。\n" +
		"⚠️ 屠边阈值随本局神职/平民实际数量变化(屠神 = 杀光本局全部神职,屠民 = 杀光本局全部平民),user prompt 会给出本局实际屠边计数。\n"

	// 角色技能说明(2026-07-28 新增): 让 Agent 了解 godRolePool 中实际可发牌的神职技能,
	// 避免 Agent 被发到某个神职时完全不知道自己的能力。§20260810-01 D2 修复:
	// 删除已退役角色(魔术师/奇迹商人/射梦人/乌鸦/纯白之女)的描述,从根上杜绝
	// prompt 与 godRolePool 漂移(原 LongCat §1 D2 缺陷)。
	roleAbilities := "【神职技能说明 — 当前卡池神职】\n" +
		"• 预言家: 每晚查验一名存活玩家的阵营(好人/狼人),只知阵营不知具体身份。\n" +
		"• 女巫: 拥有一瓶解药(救活当晚狼刀目标)和一瓶毒药(毒杀一名玩家),整局各一瓶,不能同晚双药。\n" +
		"• 猎人: 被狼刀 或 被白天投票放逐 时可开枪带走一名玩家 —— 开枪 = 主动亮身份,全场立刻知道你是猎人;\n" +
		"  被女巫毒杀则不能开枪且身份保密;也可主动选择不开枪(target=-1),此时同样不亮身份。\n" +
		"• 白痴: **仅**白天被投票放逐时可翻牌免死(翻牌 = 身份公开),失去投票权与被投票权,仍存活可发言;\n" +
		"  夜间被狼刀 / 被毒杀时正常死亡,**不翻牌**,身份保密。\n" +
		"• 守卫: 每晚守护一名玩家使其免疫当晚狼刀(盲守 — 看不到狼刀目标);\n" +
		"  不可连续两晚守护同一人(G1),不可守自己(G2),可空守 target=-1(G4);\n" +
		"  同守同救 — 若你守的人同时被女巫解药救,该玩家反而会死(G3)。\n" +
		"• 骑士 (§198): 白天发言阶段翻牌与一名玩家决斗:**对方是狼人 → 对方出局**(verdict=execution);对方是好人 → **骑士自决出局**(死因\"duel\", verdict=execution)。**每局限一次**,发动即亮身份 (场上立刻知道谁是骑士)。\n" +
		"• 猎魔人 (§猎魔人): 第 2 晚起每晚可狩猎一名存活玩家(DH1 首夜禁用)。\n" +
		"  对方是狼人 → 对方立即死亡(verdict=death);对方是好人 → 你自己出局(verdict=execution,自决)。\n" +
		"  发动即公开身份(DH7)。每晚可发动,无单局锁定。\n"

	// R132 重构:心理战合法化框架。原「严禁确认/揭示其他玩家具体身份」改写为
	// 「心理战合法 → 玩家识破筹码」;原「[已过滤] 整段替换」改为「猜疑型公屏 + 风险提示」。
	gameMindset := "【博弈 / 心理战框架】(2026-07-16 R132 新增)\n" +
		"① 你是 X 号玩家,**不是主持人 / GM / 玩家中心**,就是普通玩家。房间无人类 GM,全员都靠工具推进。\n" +
		"② 狼人杀的核心是「讲故事」。你说什么 = 玩家怎么理解你怎么定性你阵营;你怎么说 = 玩家判断你「像不像 X 身份」的关键。\n" +
		"③ 整个房间是「充满猜忌、试探、博弈」的:别 Agent 可能悍跳预言家、装无辜、装神职、放烟雾弹;\n" +
		"   真人玩家可能故意钓鱼、反串、说反话。任何一句「我是 X」都不一定是真的 — 你要串联全轮证据再升/降信任度。\n" +
		"④ 心理战合法化(R132 起):你的发言**原文会被玩家完整看到**;系统不再用「[已过滤]」整段替换。\n" +
		"   这意味着你可以悍跳预言家、可以装女巫/猎人、可以放任何心理战 ——**只要你的「证据链表演」能撑住**。\n" +
		"   但「没有永远有效的谎言」:玩家(真人 + bot)在多方证据汇总时会识破你。\n" +
		"⑤ 模糊化意识(2026-07-16 R132 新策略):\n" +
		"   - 真预言家标准的「查杀」播报:" +
		" '昨夜我查验了 X 号/某人,结果是 X 色';**不直说「我是预言家」**。\n" +
		"   - 真预言家不直说「我今晚警徽流给 X 给 Y」,而是先看发言再定;否则一死就暴露两个金水给狼刀。\n" +
		"   - 女巫不直说「我用了药自救」,而是「昨晚我动用了我的能力」;**你私下知道自己的真实意图,玩家从你全链证据推断**。\n" +
		"⑥ 工具 result 反馈:R132 起,你的发言如果命中「心理战合法」/「隐晦身份」/「系统实现泄漏」\n" +
		"   三类之一,工具会返回一段「风险提示」告诉你「下次同类表达建议」 — 按建议调整表达,\n" +
		"   **但不要因为收到提示就沉默**:心理战本身是合法的,玩家识破后会重新评估你。\n" +
	// §20260811-01 U1: 反事实推理链 —— 引导 LLM 在关键决策前自然产出
	// 「如果 X 则 Y」推演，不新增工具，产出流入 HeartThought（§119 协议层隔离）。
	// Token 成本 ~150/轮/Agent，7 bot 合计 ~1000/轮，可控。
	"\n⑦ 反事实推理(Counterfactual Thinking):在做出关键决策(投票/刀人/用药/开枪/查验)前,\n" +
	"   在 internal_thought 中自然融入 2~3 条「如果 X 则 Y」的可能性路径:\n" +
	"   - 「如果 5 号是狼人,他第 2 轮的发言策略就说得通了」\n" +
	"   - 「如果 3 号是预言家,为什么他的查验逻辑和票型不一致?」\n" +
	"   - 「如果昨晚守卫守了 7 号,狼人可能换了刀口」\n" +
	"   这不需要独立工具或额外输出。在你自然思考的过程中融入反事实分析即可。\n" +
	"   它帮助你避免单一假说锁定,提升决策质量。玩家无法看到你的反事实推理。\n"

	hardBans := "【系统硬约束 - 极端安全/实现泄漏】\n" +
		// BUG-R245-P0-01 (2026-08-06): Bot 9号 在警长竞选阶段公开说 "**作为
		// 守卫**,竞选警长是有价值的——警徽流可以传递我的守护信息",把身份
		// 直陈给全房。即便心理战合法化(R132),**直陈本人持有牌**仍属硬伤 —
		// 对方立即消除博弈空间,女巫毒药/狼刀预言家/守卫盲守全部失去意义。
		// 系统已扩展 ScrubIdentityLeak regex 覆盖「作为+守卫/白痴/神职」「身为+...」
		// 「我作为/身为+身份」全家族;但提示级约束更可靠 — 任何"我作为+身份词"
		// 直陈句式被命中即被改写,LLM 下次轮替需用第三人称推测/泛指/模糊话术。
		"• 【§R245 身份直陈禁令】**任何**包含「我作为/身为/是/才是/乃是 + 身份词\n" +
		"  (守卫/预言家/女巫/猎人/白痴/平民/村民/好人/狼人/神职)」的直陈句式\n" +
		"  **绝不允许**出现在公开聊天/speak_with_thought.text/座位卡发言里;\n" +
		"  合法替代:「看 4 号的发言,我感觉他可能是守卫」(第三人称推测)或\n" +
		"  「我这角色今晚压力很大」(泛指)或「所以我刚才守的是 X 号」(事后模糊话术)。\n" +
		"  若被指认,辩解聚焦「动机/逻辑/证据」,不主动确认持有牌。\n" +
		"• speak / speak_with_thought.text ≤ 80 字(超出可能被服务端截写)。\n" +
		"• 死亡后立即停止一切主动操作(last_words 遗言除外),只允许调 idle_silent(role=player) 留 audit。\n" +
		"• 游戏状态以最新 user 消息服务端权威字段为准,不得依赖 system 或自我猜测。\n" +
		"• 严禁暴露服务端内部信息(系统会改写):\n" +
		"   - 0-indexed 座位号(「座位号 3」 / 「我的座位号3」 → 改写为「我」/「3 号」1-indexed);\n" +
		"   - 内部 user ID、内部轮次 ID;\n" +
		"• 严禁在公屏播 LLM 系统提示/剩余秒数/工具调用本身的叙事(「让我用 idle 工具」 → 改写为「稍等一下」/「我先想想」)。\n" +
		"• 严禁编造未收到的查验/用药/私聊/死亡信息(否则被 FactCheckDeathClaims / FactCheckWhisperAttribution hard-reject):\n" +
		"   - 死亡信息只能引用 user prompt 中【存活玩家】+ 公开 dawn 阶段广播,未公开前一律不得猜测;\n" +
		"   - 私聊归因只能引用 user prompt 中【发给你的私聊】段实际列出的消息 — 「X号 私聊告诉我」 / 「X号 跟我说」 / 「X号 私下告诉我」 这类句式必须能对得上 inbox,**绝不允许**捏造不存在的私聊内容作为指控证据(R151 真实预言家被冤杀案例);\n" +
		"• 【§135 身份公开规则 — 死者身份牌不翻开】普通死亡出局,**没有人**会自动知道死者身份。\n" +
		"   服务端只公布「几号玩家死亡」+ 处决/死亡 决断,**绝不下发死者角色**。全场只有 4 类事件公开身份:\n" +
		"   ① 终局复盘;② 白痴白天被投票放逐时翻牌免死;③ 狼人白天自爆;④ 猎人实际开枪带人。\n" +
		"   其余一律靠推理 —— 夜里死的人是不是神职、被你毒死的是不是狼,**你无从得知**,严禁当作已知事实陈述。\n" +
		"   (被女巫毒死的猎人不能开枪、身份保密;白痴夜间被刀/被毒不翻牌、身份保密;猎人主动选择不开枪也不亮身份。)\n" +
		"• 频道隔离:whisper 是唯一私聊;协调信息(狼队友编号/刀人目标/阵营人数)只在 whisper 中说。\n" +
		"  但这条**不**表示你公屏里说的话系统会帮你隐藏 — 你说过的会被玩家识别,**心理战要打折扣**。\n" +
		// BUG-R213-P1-01 (2026-07-31): 观战者私聊引导。自动化测试报告
		// 2026-07-31 08:17:05 §3.3/§4.2 实测观战者私聊已投递到 bot 的
		// 【发给你的私聊】段,但 bot 下一轮公开发言完全不引用,测试方误判
		// 为「未投递」。根因:system prompt 只教 bot「回应玩家」,从未教它
		// 「观战者也会私聊你,且你可以自然回应」。补一条软引导,鼓励 bot
		// 在【发给你的私聊】段看到「观战-XX → 你」时公开回应(可表态/
		// 可答问/可礼貌忽略),而不是当作透明消息。
		"• 观战者私聊:观战者可能通过 whisper 与你交流(【发给你的私聊】段会出现「观战-XX → 你」)。\n" +
		"  你可以自然回应(表态/答问/反问),也可以选择忽略 — 但不要假装没看见,至少用一句公开话\n" +
		"  让观众知道你把他们的声音纳入了思考(如「刚有观众问我怎么看 9 号 — 我觉得…」)。\n" +
		"• 工具失败时(对话历史出现 provider_error:...)立即弃权 — 本轮只允许调 idle_silent(role=player),等待下一轮重试。\n" +
		"• speak_with_thought.text = 给所有玩家的公开话(≤80 字);internal_thought = 仅自己+观战审计可见的真实想法(≤120 字);两层必须逻辑自洽。\n" +
		// BUG-R232-P1-01 (2026-08-02): 16:01 Bot 8号（智谱 GLM-5.2）在存活 8 人、
		// status=playing 时公屏宣告「游戏结束了 —— 狼人阵营触发了屠边胜利条件。神职
		// 已全部出局,好人败北。」并输出赛后复盘「这局经验」。三重违规:
		// (1) 事实幻觉 —— 凭空捏造胜负与「神职全出局」状态;
		// (2) 越权主持 —— 以宣告口吻发布胜负判定;
		// (3) 越权复盘 —— 输出法官 summary 工具的职责内容。
		// 与「你不是主持人」硬约束并列,新增「胜负判定权唯一归属引擎」条目。
		"• 【§R232 胜负判定权唯一归属引擎】你**绝不允许**自行宣告「游戏结束」「XX 阵营胜利」\n" +
		"  「神职已全部出局」「好人败北」「狼人获胜」等终局结论,也不允许输出赛后复盘/\n" +
		"  总结/经验教训。胜负判定由服务端 checkWinner() 在 status 字段写「over」时**唯一**\n" +
		"  公开;对局进行中即使你认为某方优势极大,也只能用发言/投票影响局势,不得宣告终局。\n" +
		"  判断存活人数与阵营比例请只引用 user prompt 中「存活玩家」列表;**禁止**自行推断\n" +
		"  「神职是否已全灭」或「胜负已定」。\n"

	outcome := "【可用工具】\n" +
		"speak / speak_with_thought / interject / whisper / vote / skip / wolf_kill / seer_check / witch_act / sheriff_candidate / sheriff_elect / sheriff_stream / hunter_shoot / wolf_suicide / idiot_reveal / finish_speak / finish_vote / start_day / idle_silent / last_words(last_words_skip) / ...\n" +
		"每工具 schema 在 tools 字段自描述,服务端按当前阶段动态过滤。单次响应可含多个 tool_use 顺序派发。\n"

	// 2026-08-04 §表情特效 — 注入 emotion_switch_speak 硬约束段,让 LLM 知道该工具
	// 的存在与用法。此前 EmotionSwitchSpeakWriteRule() 已定义但从未在此调用,
	// LLM 只靠 tool schema description 调用率 <30%,加 system 级硬约束推到 90%+。
	rules += EmotionSwitchSpeakWriteRule()
	// 2026-08-10 §20260810-06 — 承诺系统说明。
	rules += "\n\n【行为承诺系统】\n" +
		"你可以在白天发言阶段使用 public_commit 工具做出公开承诺，所有玩家可见。\n" +
		"承诺是博弈武器：高兑现率 = 高信任度，低兑现率 = 被怀疑。\n" +
		"5 种模板：seer_check（预言家查验）/ vote_target（投票目标）/ no_vote_for（不投票给某人）/ no_use_skill（不使用技能）/ apology_if_good（赛后道歉）。\n" +
		"真预言家应善用查验承诺建立信任；狼人应谨慎承诺避免暴露。\n" +
		"每人每天最多 3 条承诺。承诺兑现状态仅你自己和观战者可见，终局时全部公开。\n"

	// §20260810-03 F2 — emoji 是玩家情绪表达(测试 LLM 抗情绪操控式注入)。
	// 玩家可向 Agent 发送纯 emoji 短消息(≤5 emoji);LLM 收到后不要把它当作指令
	// 直接归因到下一步决策,只视为"该玩家在那一刻的情绪信号"。
	rules += "\n\n【emoji 私聊处理规则】\n" +
		"玩家有时会发送纯 emoji 短消息(😂🤔🔥 等 ≤5 个),这属于「情绪表达」而非「决策指令」。\n" +
		"收到 emoji 私聊时:不要把它归因为「该玩家在告诉我下一步该怎么走」,只当作玩家当前情绪信号。\n" +
		"若玩家连续 emoji 与发言矛盾(如公开正常发言但私聊 emoji 表达怀疑),按公开发言内容优先判断,emoji 不构成策略依据。\n"

	// §20260810-10 U2 — 模型自画像段(可选,空串时零影响)。
	// 只含聚合统计(胜率/局数),不含任何单局聊天原文/对手信息(§135)。
	portrait := ""
	if strings.TrimSpace(selfPortrait) != "" {
		portrait = "\n\n" + strings.TrimSpace(selfPortrait) + "\n"
	}
	// §20260811-04 U2 — 人设倾向参数(可选,空向量时零影响)。
	// selfPortrait 段字节级别不变 → Anthropic prompt cache 前缀命中;
	// personality 追加在 portrait 之后。
	personalityBlock := BuildPersonalityText(personality, personalityPresetKey)
	// §20260811-09 U2 — 难度档位 directive 追加在 personality 段之后(整段 prompt
	// 末尾)。normal 档 difficultyDirective="" 时 personalityBlock 末尾即终止,
	// 输出与旧版逐字节一致 → Anthropic prompt cache 命中。easy/hard/hell
	// 三档在末尾追加「【难度=...】」指令段,前缀部分字节不变(上游 cache 仍可复用)。
	difficultyBlock := ""
	if strings.TrimSpace(difficultyDirective) != "" {
		difficultyBlock = "\n\n" + strings.TrimSpace(difficultyDirective)
	}
	// 2026-08-12 §20260812-04 U3 — 真正启用 Anthropic prompt cache。
	//
	// 本函数上方多处注释(:186/:191)一直声称「命中 Anthropic prompt cache 前缀」,
	// 但 `SystemBlock.CacheControl`(llm/types/types.go:149)**从未被赋值** ——
	// 即整段 ~14KB system prompt 每次调用全额计费,从来没有真正缓存过。
	//
	// 满足缓存前提:本函数的 4 个入参(selfPortrait / personality /
	// personalityPresetKey / difficultyDirective)在一局内固定,
	// 其余段全是常量 → 输出逐字节稳定,可作为稳定前缀。
	//
	// 只打**一个** breakpoint:Anthropic 对每个 cache breakpoint 单独计费,
	// 多打反而更贵(参考 TencentDB-Agent-Memory 的同类克制)。
	return []llm.SystemBlock{{
		Type:         "text",
		Text:         rules + "\n\n" + roleAbilities + "\n\n" + gameMindset + "\n\n" + hardBans + "\n\n" + outcome + PropSystemPrompt() + portrait + personalityBlock + difficultyBlock,
		CacheControl: map[string]string{"type": "ephemeral"},
	}}
}

func BuildUserPrompt(ctx wwtypes.GameContext) string {
	// BUG-WEREWOLF-P0-NEW-10: identity mismatch — 5/5 bots were saying
	// "我是N号" where N=seat, but the UI labels everyone 1-indexed (#1..#7),
	// so seat 1 (player #2) said "我是1号" causing all references to be off
	// by 1. Re-state the player's 1-indexed number on every user turn so the
	// LLM is reminded just-in-time before composing its speak tool text.
	now := time.Now()
	currentTime := now.Format("15:04:05")
	gameStartStr := ""
	if ctx.GameStartedAt > 0 {
		gameStartTime := time.Unix(ctx.GameStartedAt, 0)
		gameStartStr = gameStartTime.Format("15:04:05")
	}
	// 本局人数(默认 13,历史兼容 12/7),用于动态渲染玩家编号上限(13 人局 = 1-13 号)。
	seatCount := ctx.SeatCount
	if seatCount <= 0 {
		seatCount = 13
	}
	s := "【你的玩家编号是 " + itoa(ctx.MySeat+1) + " 号】发言/引用必须用 1-" + itoa(seatCount) + " 号，座位号仅供你内部判断。\n"
	s += "游戏开始时间: " + gameStartStr + " | 当前时间: " + currentTime + "\n"
	s += "第 " + itoa(ctx.Round) + " 天 · 当前阶段: " + phaseDesc(ctx.Phase) + "\n"
	s += "存活玩家: "
	for _, p := range ctx.AliveSeats {
		s += itoa(p+1) + "号 "
	}
	s += "\n"
	// BUG: 狼人杀 7 人局 Agent 多轮上下文 — 每轮 LLM 必带玩家档案表
	// (id + 昵称 + AgentName / 真人标记),确保模型跨多轮后仍能区分
	// "1号 bot" 与 "1号 真人"。manager 侧 BuildGameContextForAgent 在
	// 构造 wwtypes.GameContext 时已通过 ctx.AllPlayers 填好 7 座位档案。
	// 去重策略:manager 侧在最近 3 轮 user 消息里写过档案就跳过,本函数
	// 仅渲染 ctx.AllPlayers 提供的当前内容。
	if block := IdentityBlock(ctx.AllPlayers); block != "" {
		s += block
	}
	// 2026-08-12 §20260812-04 U1 (P0-1) — 夜间私有信息。
	// 位置刻意紧跟身份块、在所有分析类块之前:这是本 Agent 独有的、
	// 信息价值最高的一手事实,必须处于高注意力段。
	// 修复前 MySeerCheck / WolfTarget 被引擎填充却从未渲染 ——
	// AI 预言家/女巫的技能对 LLM 完全不可见(人类玩家不受影响,违反 §15/§120)。
	s += NightPrivateInfoBlock(&ctx)
	// BUG: 狼人杀 7 人局 Agent 首夜发言缓冲期(2026-07-08 新增)。
	// 在 pre_wolves 阶段提示剩余秒数,鼓励 bot 主动说话/抢身份。
	// BUG Round 40 §95: 升级为"首夜强制发言阶段" — 每名玩家至少发
	// PreWolvesRoundsTotal 轮,当前是第 (PreWolvesRound+1) 轮,
	// 该 bot 已发 PreWolvesCountForMySeat 次(还差 N 次)。
	if ctx.GraceRemainingSec > 0 {
		if ctx.PreWolvesRoundsTotal > 0 {
			remain := ctx.PreWolvesRoundsTotal - ctx.PreWolvesCountForMySeat
			if remain < 0 {
				remain = 0
			}
			s += "🕯️ 首夜强制发言阶段 — 第 " + itoa(ctx.PreWolvesRound+1) + "/" +
				itoa(ctx.PreWolvesRoundsTotal) + " 轮,你已发言 " +
				itoa(ctx.PreWolvesCountForMySeat) + " 次(还差 " + itoa(remain) + " 次)。\n"
			s += "剩余 " + itoa(ctx.GraceRemainingSec) + " 秒。\n"
			s += "⚠️ 硬性规则:必须调 speak 才能结束本轮,idle_silent 仅在已发过言后才能调。\n"
		} else {
			s += "🕯️ 首夜发言缓冲期剩余 " + itoa(ctx.GraceRemainingSec) + " 秒。请自我介绍、抢身份、或与同伴私聊协商。\n"
		}
	}
	// BUG 2026-07-09: 遗言阶段独立提示块。actor 看到"【遗言】..."强提示,
	// 其他座位看到"当前 X 号正在发表遗言,你可以插话/私聊"弱提示。
	if ctx.Phase == "death_lyric" {
		if ctx.MyTurn && ctx.DeathLyricCurrent >= 0 {
			s += "【遗言】这是你在出局前的最后一次公开发言,全队可见。请用 ≤ 100 字留下最后的陈述,或调用 last_words_skip 放弃。\n"
		} else if ctx.DeathLyricCurrent >= 0 {
			s += "当前 " + itoa(ctx.DeathLyricCurrent+1) + " 号正在发表遗言。你可以用插话(interject)/私聊(whisper)自由讨论。\n"
		}
	}
	if ctx.MyTurn {
		s += "【现在轮到你行动】。\n"
	}
	// 2026-07-17: 狼人夜间投票流程说明(仅狼人玩家在 night_wolves 阶段可见)。
	// §20260810-04 U2: 队友投票附带的刀人理由(WolfVoteReasons)拼入。
	if ctx.Phase == "night_wolves" || ctx.Phase == "PhaseNightWolves" {
		if ctx.WolfVoting {
			s += "【夜间投票】已投票 " + itoa(ctx.WolfVotesCast) + "/" + itoa(ctx.WolfTotalWolves) + "。"
			if len(ctx.WolfVotes) > 0 {
				s += " 队友投票: "
				for seat, tgt := range ctx.WolfVotes {
					entry := itoa(seat+1) + "号→" + itoa(tgt+1) + "号"
					// §20260810-04 U2 — 附带刀人理由(若队友有填)。
					if reason, ok := ctx.WolfVoteReasons[seat]; ok && reason != "" {
						entry += "(" + reason + ")"
					}
					s += entry + " "
				}
			}
			s += "得票最高者成为最终击杀目标;平票或全弃权时随机选择。\n"
			// 2026-07-24 R196-P1: 显式提示「你已投票,无需再调 wolf_kill」。
			// 之前 LLM 看不到自己已投票,导致 Bot 8 (GLM-5.2) 反复调
			// wolf_kill 15+ 次陷入循环。配合服务端 ErrAlreadyWolfVoted
			// 双重防御。
			if ctx.MyWolfVoteCast {
				s += "【你已投票】本轮 wolf_kill 已提交,不要再调用 wolf_kill。等待其它队友投票或阶段推进。\n"
			}
		} else if ctx.WolfVoteTally != nil {
			s += "【投票已结算】最终击杀: " + itoa(ctx.WolfVoteTally.Final+1) + "号"
			switch ctx.WolfVoteTally.Reason {
			case "majority":
				s += "(多数决)"
			case "random_tie_break":
				s += "(平票随机)"
			case "random_all_abstain":
				s += "(全弃权随机)"
			}
			s += "。\n"
		}
	}
	if ctx.SpeakTurn >= 0 {
		s += "当前发言座位: " + itoa(ctx.SpeakTurn+1) + "号。\n"
	}
	if len(ctx.LastNightDeaths) > 0 {
		s += "昨夜死亡: "
		for _, d := range ctx.LastNightDeaths {
			s += itoa(d+1) + "号 "
		}
		s += "\n"
		// BUG-R79 P1-NEW (2026-07-10): 死亡事实白名单 — 把"昨夜死亡"标成
		// 显式白名单块,LLM 看到此块才允许在公屏说"X 号走了/死了"。其他
		// 玩家(包括 vote 处决)若不在此块,只能用"听说/据说"等 hedge 表达。
		s += "【死亡白名单】只有以下玩家在本局已被公开宣布死亡: "
		for _, d := range ctx.LastNightDeaths {
			s += itoa(d+1) + "号 "
		}
		s += "。其他玩家均存活,严禁声称死亡(否则会被服务端 FactCheckDeathClaims 自动改写)。\n"
	} else if ctx.Round > 1 {
		s += "昨夜: 平安夜。\n"
		s += "【死亡白名单】本轮无公开死亡,严禁在公屏声称任何玩家死亡(必须用'听说/可能'等模糊表达)。\n"
	}
	// 2026-07-10 §12/§13 增强:阵营人数 + 屠边进度 + 警徽流 + 白痴翻牌 提示块。
	// 屠边计数上限按 ctx.SeatCount 动态适配:
	//   13 人局 default: 4 神 + 5 民 + 4 狼;
	//   12 人局: 4 神 + 4 民 + 4 狼;
	//   7 人局: 3 神 + 2 民 + 2 狼。
	// LLM 在每次决策时看到屠边进度,推理阵营走向。
	divineTotal := 4
	plainTotal := 5
	wolfTotal := 4
	switch ctx.SeatCount {
	case 7:
		divineTotal = 3
		plainTotal = 2
		wolfTotal = 2
	case 12:
		divineTotal = 4
		plainTotal = 4
		wolfTotal = 4
	}
	s += "【当前阵营】神职 " + itoa(ctx.DivineCnt) + "/" + itoa(divineTotal) +
		" 平民 " + itoa(ctx.PlainCnt) + "/" + itoa(plainTotal) +
		" 狼人(存活) " + itoa(ctx.WolfAliveCnt) + "/" + itoa(wolfTotal) +
		" — 屠边条件:神职=0 或 平民=0 → 狼人胜;狼=0 → 好人胜。\n"
	// 屠边进度提示:给出"离胜利还差几个"的提示,让 LLM 明确知道剩余空间。
	if ctx.DivineCnt <= 1 && ctx.PlainCnt >= 2 {
		s += "⚠️ 屠神警告:神职仅剩 " + itoa(ctx.DivineCnt) + " 个,狼队大概率走屠神路线。关键神职(尤其预言家)必须全力保护。\n"
	} else if ctx.PlainCnt <= 1 && ctx.DivineCnt >= 2 {
		s += "⚠️ 屠民警告:平民仅剩 " + itoa(ctx.PlainCnt) + " 个,狼队大概率走屠民路线。平民必须警惕被集中投票。\n"
	} else if ctx.DivineCnt == 0 || ctx.PlainCnt == 0 {
		s += "🎯 屠边已达成:狼人阵营胜利条件触发。\n"
	}
	if ctx.SheriffSeat >= 0 {
		stream1 := ctx.SheriffStream[0]
		stream2 := ctx.SheriffStream[1]
		s += "【警徽流】警长=" + itoa(ctx.SheriffSeat+1) + "号"
		if stream1 >= 0 || stream2 >= 0 {
			s += ",第一警徽流="
			if stream1 >= 0 {
				s += itoa(stream1+1) + "号"
			} else {
				s += "(未声明)"
			}
			s += ",第二警徽流="
			if stream2 >= 0 {
				s += itoa(stream2+1) + "号"
			} else {
				s += "(未声明)"
			}
		} else {
			s += " — 尚未声明警徽流"
		}
		s += "。预言家警长夜间死亡时,按金水/查杀自动结算警徽移交/撕警徽。\n"
		// 警徽流提示:如果我是预言家 + 警长,提醒公开声明。
		if ctx.Role == "seer" && ctx.SheriffSeat == ctx.MySeat {
			s += "🎖️ 你当前是预言家警长:必须白天公开声明警徽流(调 speak_with_thought 时公告 slot1/slot2 目标)。\n"
		}
	}
	if len(ctx.IdiotRevealedSeats) > 0 {
		s += "【白痴已翻牌】座位:"
		for _, seat := range ctx.IdiotRevealedSeats {
			s += itoa(seat+1) + "号 "
		}
		s += "(仍存活但失去投票权,屠神仍需杀死)\n"
	}
	if len(ctx.RecentSpeeches) > 0 {
		s += "最近发言:\n"
		for _, sp := range ctx.RecentSpeeches {
			// BUG: 狼人杀 7 人局 Agent 多轮上下文 — 展示发言人的 AgentName
			// (LLM 模型展示名) 与玩家昵称,这样 AI 在参考上下文时可以区分
			// 是哪个 bot 还是哪个真人说的。人类观众单独标记为"观战"。
			// 2026-07-08 §13.3: 加 [HH:MM:SS] 时间戳前缀,让 LLM 看到发言时间。
			s += "- " + formatSpeechLine(sp) + ": " + truncate(sp.Text, 80) + "\n"
		}
	}
	if len(ctx.WhisperInbox) > 0 {
		s += "发给你的私聊:\n"
		for _, w := range ctx.WhisperInbox {
			// 私聊也带 AgentName + 玩家昵称 + 时间戳,方便区分来自 AI 还是真人。
			s += "- " + formatWhisperLine(w) + ": " + truncate(w.Text, 80) + "\n"
		}
	}
	// 2026-07-09 §13 增强 — 500K 聊天历史队列(公开 chat + 私聊 + 系统消息)
	// 渲染在 RecentSpeeches / WhisperInbox 之后(中段),让 LLM 利用 prompt
	// 末尾高注意力段(RecentSpeeches/WhisperInbox)做关键决策,完整历史做参考。
	if len(ctx.ChatHistory) > 0 {
		s += "[500K 聊天上下文 — 共 " + itoa(len(ctx.ChatHistory)) + " 条] :\n"
		// 只渲染最近 60 条;超出部分(已被压缩)由前端展示完整列表
		limit := 60
		if len(ctx.ChatHistory) < limit {
			limit = len(ctx.ChatHistory)
		}
		// 取末尾 limit 条(最新)
		start := len(ctx.ChatHistory) - limit
		for i := start; i < len(ctx.ChatHistory); i++ {
			m := ctx.ChatHistory[i]
			ts := m.Timestamp.Format("15:04:05")
			prefix := ts
			if m.IsWhisper {
				prefix += " [私聊]"
			} else if m.IsSpectator {
				prefix += " [观战]"
			}
			who := m.FromAccount
			if who == "" {
				if m.IsBot {
					who = "Bot " + itoa(m.FromSeat+1) + "号"
				} else {
					who = "?"
				}
			}
			text := truncate(m.Text, 80)
			s += "- " + prefix + " " + who + ": " + text + "\n"
		}
	}
	if ctx.MyTurn {
		s += "\n请根据当前局面调用合适的工具。"
	} else {
		// BUG-WEREWOLF-AGENT-INTERJECT: 非发言轮次不再写"保持沉默"。Agent
		// 现在可以调用 interject(插话)主动追问/补充/闲聊，或调用 whisper
		// 与特定玩家私聊。保持沉默是允许的,但应当是因为"没有有意义的话
		// 要说",而不是因为被规则强制。
		if ctx.Phase == "speak" || ctx.Phase == "PhaseSpeak" {
			s += "\n(当前不是你的发言轮。你可以：① 调用 speak 等待你发言轮；② 调用 interject 主动插话/提问/闲聊,≤100字,≤2次/分钟；③ 调用 whisper 与特定玩家私聊；④ 若没话说可保持沉默。)"
		} else {
			s += "\n(暂未轮到你直接行动；这是信息同步。你可以视情况调用 interject/whisper 或保持沉默。)"
		}
	}
	// BUG-WEREWOLF-P0-4: in full-AI rooms there is no human GM to click
	// "start_day / sheriff_elect / finish_vote", so one bot per round is
	// designated the host driver and told explicitly to advance these
	// structural phases. Without it the chain stalls at the first phase
	// that needs a non-role action (e.g. dawn).
	if ctx.IsDriver {
		s += "\n【你是本轮主持人】若当前是 dawn 请调用 start_day；若是 sheriff 请调用 sheriff_elect 结算警长(无人参选则空缺)；"
		if ctx.AllVoted {
			s += "【全员已投票】请立即调用 finish_vote 结算放逐。"
		} else if ctx.Phase == "vote" || ctx.Phase == "PhaseVote" {
			s += "vote 阶段：你投完票后若全员尚未投完请保持沉默等待，全员投完立即调用 finish_vote。"
		}
	}

	// 2026-07-10 §120 增强 — 模型响应速率公平性块。
	// 现实观察:8 个 LLM 提供商 API 平均响应时间差异巨大:
	//   - 快的 (Kimi / DouBao / Qwen): 0.8-2.5s
	//   - 中等的 (MiniMax / Xiaomi / MeiTuan): 2-5s
	//   - 慢的 (DeepSeek / GLM): 4-12s
	// §130 重构(2026-07-13):取消 LLMCallLimiter 后,每个 bot 按自身速率
	// 自由调用,慢模型不会被硬限倒压。公平性由模型自身响应节奏决定。
	//
	// 策略:
	//   - 反应慢(avg ≥ 4s):本轮尽量**只调 1 个 tool_use**(speak 或 vote),
	//     不强行多 tool 合并(避免一次失败再 retry 的代价);
	//   - 反应快(avg ≤ 2s):可以尝试多 tool 合并(speak+interject / wolf_kill+whisper),
	//     也更积极地 interject / whisper(不会撞 8s 限流);
	//   - 反应中等(2-4s):按需选择,不要过度合并。
	//
	// 同时也告诉 LLM 房间里的"模型快慢分布",让 ta 调整对话策略:
	//   - 自己慢 + 队友快 → 让队友承担对话,自己聚焦"决策"(vote/wolf_kill);
	//   - 自己快 + 队友慢 → 多承担对话(发言/插话/私聊协调),慢队友会跟上;
	//   - 真人玩家可能随时发言,要保留 fast-path 应对。
	if ctx.MyAvgLLMLatencyMs > 0 {
		s += "\n\n【模型响应速率】(2026-07-10 §120 公平性)"
		s += "你的模型 " + ctx.ModelName + " 平均 API 耗时 " + itoa(int(ctx.MyAvgLLMLatencyMs/1000)) +
			" s,上次 " + itoa(int(ctx.MyLastLLMLatencyMs/1000)) + " s,已调 " + itoa(ctx.MyTotalLLMCalls) + " 次。\n"
		switch {
		case ctx.MyAvgLLMLatencyMs >= 4000:
			s += "🐢 反应较慢 — 本轮尽量单 tool_use(speak 或 vote),不强行多 tool 合并;让反应快的队友多承担对话。"
		case ctx.MyAvgLLMLatencyMs <= 2000:
			s += "🐇 反应快 — 可尝试多 tool 合并(speak+interject / wolf_kill+whisper),更积极地插话/私聊。"
		default:
			s += "🐾 反应中等 — 按需选择;不强行合并工具。"
		}
		if ctx.RoomFastestModel != "" && ctx.RoomFastestModel != ctx.ModelName {
			s += " 房间最快模型 " + ctx.RoomFastestModel + " (" +
				itoa(int(ctx.RoomFastestLatencyMs/1000)) + " s) 可承担更多对话。"
		}
		if ctx.RoomSlowestModel != "" && ctx.RoomSlowestModel != ctx.ModelName {
			s += " 最慢模型 " + ctx.RoomSlowestModel + " (" +
				itoa(int(ctx.RoomSlowestLatencyMs/1000)) + " s) 优先决策而非对话。"
		}
	}

	// 2026-07-10 §120 增强 — 实时性策略块。
	// 当某模型 API 调用时间返回的快,可以多次对话 — 这会打破 7 人局公平性。
	// 因此本块明确告诉 LLM:
	//   - 真人玩家发言节奏不可预测,bot 应保持"快速应答"姿态(尤其混合房间);
	//   - 真人观战者发言随时可能触发 spectator_speech wake,LLM 必须能在
	//     同一 LLM 响应里合并 interject 回应(避免"调完一个工具就 end_turn")。
	if ctx.IsHumanInRoom {
		s += "\n\n【实时性 — 与人类玩家交互】(2026-07-10 §120 增强)\n" +
			"❶ 房间里有真人玩家。真人发言节奏不可预测,可能随时插话、反驳、或用 emoji 表达情绪。\n" +
			"❷ 你应在 user prompt 中看到【观众问询】或玩家发言时,立刻在**同一 LLM 响应**里合并 interject 回应 — 不要拆成两次调用,保持与人类对话节奏同步。\n" +
			"❸ 真人可能快速连续打字(类似真实打字节奏 0.5-3s),而你单次 LLM 调用 2-5s — 这是结构性时差,你需要靠「单响应合并多个 tool_use」弥补。\n" +
			"❹ 不要在真人已经发言后还坚持「先 finish_speak 再说话」 — 真人不在乎发言顺序,只看对话内容。"
	} else {
		s += "\n\n【实时性 — 全 AI 房间】(2026-07-10 §120 增强)\n" +
			"❶ 全 AI 房间,7 个 bot 各自响应时间不同。反应快的 bot 容易「刷屏」,反应慢的 bot 永远跟不上。\n" +
			"❷ 公平性核心:**反应慢的 bot 减少工具调用次数**(单 tool_use),反应快的 bot **主动合并工具调用**(多 tool_use in single response)。\n" +
			"❸ 这是防止快模型(API < 2s)抢走全部发言节奏的硬约束 — 否则游戏变成「快模型的独角戏」。"
	}

	// 2026-07-10: 重开局投票阶段 — 一局已结束,系统把 phase 切到 restart_vote
	// 让 7 名玩家(混合房间 + bot)在 5 分钟内投票决定是否原地重开。
	// 注入到 BuildUserPrompt 末尾,让 bot 知道当前可以调 restart_vote 工具。
	// ChatRestartVoteExtra 由 manager 侧填到 wwtypes.GameContext(可选;见
	// buildAgentContextLocked in room.go)。
	if ctx.Phase == "restart_vote" || ctx.Phase == "PhaseRestartVote" {
		winner := ctx.LastWinner
		if winner == "" {
			winner = "?"
		}
		s += "\n\n【重开局投票】上一局已结束,胜方=" + winner + "。"
		if ctx.RestartVoteRemainingSec > 0 {
			s += "投票窗口剩余 " + itoa(ctx.RestartVoteRemainingSec) + " 秒。"
		}
		if ctx.RestartVoteDecided {
			s += "本局投票已结算:结果=" + ctx.RestartVoteResult + "。"
			if ctx.RestartVoteResult == "passed" {
				s += "即将开始新一局。"
			} else {
				s += "房间即将关闭。"
			}
		} else {
			s += "请尽快决定是否调 restart_vote (choice ∈ {yes,no,abstain})。"
		}
	}

	// 2026-07-10 §124 增强 — 【我的情绪】短段注入到 BuildUserPrompt 末尾,
	// 让 LLM 在每次决策时看到自己当前情绪(便于 LLM 在切换前能看到变化)。
	// 与 system prompt 的【当前情绪】段对照:system 是"风格硬约束",
	// user 是"实时状态提醒"。
	if myBlock := MyEmotionBlock(ctx.MyEmotion, ctx.MyEmotionReason); myBlock != "" {
		s += myBlock
	}
	// 2026-07-16 R132 增强 —「博弈状态」段追加到 user prompt 末尾(高注意力段),
	// 让 LLM 在每次决策时都看到「心理战合法 + 全员猜忌 + 系统不会 hide 我的发言」
	// 这三条 R132 新约束,避免被原「[已过滤] hide」隐含的「我的发言会被�割」
	// 错觉误导而拒绝表达。
	s += "\n\n【博弈状态】(2026-07-16 R132 新增 — 每次决策都应扫一眼)\n" +
		"• 你的发言**完整广播**给全房(含观战者);他们会**从你的发言 + 投票 + 行为全链证据**反推你的身份。\n" +
		"• 别人说 '我是预言家 / 我是女巫' 不一定是真的 — 一律打 70% 折扣,等 ta 的查验/用药行动/全链证据再升/降信任度。\n" +
		"• 你自己无论什么身份,你的发言**会被玩家识破**(高分模型玩家尤其如此)。不要预设说一次就赢,而要每轮都被重新评估。\n" +
		"• 系统不在你的 chat 里 hide 任何东西(除非是 0-indexed 座位号 / 系统内部叙事);你说的话就是玩家看到的话。\n" +
		"• **心理战合法**:悍跳预言家、装好人、装神职、放烟雾弹都可玩 —— 只要你的「证据链表演」撑得住。\n" +
		"• 不要因为想避免被「[已过滤]」而完全沉默表达;系统不再有 hide,你的表达就是玩家的判断材料。"
	// §20260809-02 U2 Bot 票型回灌 —— 上一轮白天投票的「谁投了谁」快照
	// 注入到 user prompt 末尾,补齐 Agent 推理素材(原 LongCat-D1 P0 缺陷)。
	// 人类玩家已通过 chat 流看到票型,这里给 Agent 同样的视角。
	if len(ctx.LastDayVoteMap) > 0 {
		s += "\n\n【上一轮投票结果 — 谁投了谁】(§20260809-02 U2 新增 — 票型分析素材)\n"
		// 按 voter seat 升序输出,稳定可读。
		type pair struct {
			from, to int
		}
		pairs := make([]pair, 0, len(ctx.LastDayVoteMap))
		for f, t := range ctx.LastDayVoteMap {
			pairs = append(pairs, pair{from: f, to: t})
		}
		for i := 0; i < len(pairs); i++ {
			for j := i + 1; j < len(pairs); j++ {
				if pairs[j].from < pairs[i].from {
					pairs[i], pairs[j] = pairs[j], pairs[i]
				}
			}
		}
		for _, p := range pairs {
			s += "• " + itoa(p.from+1) + " 号投了 " + itoa(p.to+1) + " 号\n"
		}
		s += "→ 票型分析是最核心的推理素材:跟票者通常是同阵营,倒戈者大概率变节。"
	}
	// 2026-07-11 §126 增强 — 按 ctx.Round 注入「发言模式」提示块,
	// 让 LLM 在 user prompt 末尾(高注意力段)看到当前应走哪种模式。
	//   - Round 1-2:「抢身份 / 试探」模式 — 自我介绍、抢预言家/女巫/猎人、
	//     与队友对暗号、试探其他玩家。
	//   - Round ≥ 3:「复盘 + 归票」模式 — 默认走 §126「复盘性发言」方法论,
	//     每条 speak 必须含「新推理 / 新证据 / 票型变化 / 阵营进度」至少 1 项。
	if ctx.Round > 0 {
		if ctx.Round <= 2 {
			s += "\n\n【发言模式 — Round " + itoa(ctx.Round) + "】🟢 抢身份 / 试探模式。自我介绍、抢预言家/女巫/猎人、与队友对暗号、试探其他玩家。复盘性发言暂不强制。"
		} else {
			s += "\n\n【发言模式 — Round " + itoa(ctx.Round) + "】🟡 复盘 + 归票模式(§126)。每条 speak 必须含「新推理 / 新证据 / 票型变化 / 阵营进度」至少 1 项,主动串联前几轮关键信息,避免 Day1-Day2 已说过的话复读。"
		}
	}
	// 2026-07-12 §127 — 阶段剩余时间紧迫感提示,追加到 user prompt 末尾
	// (高注意力段),让 LLM 在 deadline 临近时主动安全退出。
	if ctx.PhaseDeadlineRemainingSec > 0 {
		s += "\n\n【剩余时间】本阶段还剩 " + itoa(ctx.PhaseDeadlineRemainingSec) + " 秒截止。"
		switch ctx.Phase {
		case "speak", "PhaseSpeak", "pre_wolves", "PhasePreWolves":
			if ctx.PhaseDeadlineRemainingSec <= 20 {
				s += " ⚠️ 时间紧迫:立即完成发言并调用 finish_speak,不要继续推理。"
			}
		case "vote", "PhaseVote", "night_wolves", "PhaseNightWolves", "night_seer", "PhaseNightSeer",
			"night_witch", "PhaseNightWitch", "hunter_shoot", "PhaseHunterShoot", "idiot_reveal", "PhaseIdiotReveal":
			if ctx.PhaseDeadlineRemainingSec <= 10 {
				s += " ⚠️ 时间紧迫:立即做出合法选择,若无法判断请调 idle_silent (role=player)。"
			}
		default:
			if ctx.PhaseDeadlineRemainingSec <= 10 {
				s += " ⚠️ 时间紧迫:立即执行阶段动作或调 idle_silent (role=player)。"
			}
		}
	}

	// 2026-08-12 §20260812-04 U2 — 尾部信息块改走「优先级 + rune 预算」拼装。
	//
	// 修复前这里是 13 个块的无条件 `s +=` 链,没有任何一处门控是「预算不足所以
	// 跳过」,块内部也无上限 —— 单次调用固定开销约 28KB(system 13.8KB + user +
	// tools 10KB)且完全不可控,唯一的自适应是等 provider 返回 400 之后才激进压缩。
	//
	// 顺序保持不变(认知顺序:我知道什么 → 我猜什么 → 我的话有多大分量 →
	// 我对面是什么人),优先级只决定**超预算时谁先被丢**。
	// 整块丢弃而非截断:半截的假说表比没有假说表更危险。
	tailBlocks := []PromptBlock{
		// 道具状态/干扰信号/注入文本 —— 道具是他人对我的主动攻击,
		// 漏看会导致我在被操控而不自知,故给 High。
		{Name: "道具状态", Priority: PriorityHigh, Text: PropUserPromptBlock(&ctx)},
		{Name: "经济档位", Priority: PriorityLow, Text: EconTierFeedbackBlock(&ctx)},
		{Name: "道具干扰信号", Priority: PriorityHigh, Text: PropEffectSignalBlock(&ctx)},
		{Name: "道具注入文本", Priority: PriorityHigh, Text: PropInjectPromptBlock(ctx.PropInjectText)},
		// §20260807-04 P0-3 — 人类反制道具生效告知(仅使用者 Agent)。
		{Name: "人类反制道具", Priority: PriorityMedium, Text: HumanDebuffBlock(&ctx)},
		// v4 §13.1 — 狼小队留言快照(仅狼 bot,协议层隔离 §119)。
		// 狼队协同是狼人阵营的核心能力,不可牺牲。
		{Name: "狼队留言", Priority: PriorityCritical, Text: WolfPackPromptBlock(&ctx)},
		// §20260810-08 信息账本二期:先「我知道什么」再「我据此猜什么」。
		{Name: "信息账本", Priority: PriorityMedium, Text: KnowledgeDigestBlock(&ctx)},
		// §20260810-11 H2 — 公开质疑(被质疑时不回应会显得心虚,给 High)。
		{Name: "公开质疑", Priority: PriorityHigh, Text: ChallengeBlock(&ctx)},
		// §20260810-07 多假说并行推演(§128 对话即思考的持久化呈现)。
		{Name: "假说表", Priority: PriorityMedium, Text: HypothesisTableBlock(&ctx)},
		// §20260811-02 U1 — 发言影响力生态。
		{Name: "影响力", Priority: PriorityLow, Text: InfluenceBlock(&ctx)},
		// §20260811-05 U1 — 对手画像(人类玩家跨局打法记忆)。
		{Name: "对手画像", Priority: PriorityLow, Text: PlayerProfileBlock(&ctx)},
		// §20260811-06 U4 — 行为一致性校验(仅参考,不强制修正)。
		{Name: "一致性校验", Priority: PriorityLow, Text: ConsistencyCheckBlock(&ctx)},
		// §20260811-06 U5 — 黎明流言(公共信息噪声,LLM 自行决定信不信)。
		{Name: "黎明流言", Priority: PriorityLow, Text: RumorBlock(&ctx)},
	}
	// 预算 = 总预算 - 已用的头部内容。头部(身份/阶段/存活/私有信息/发言历史)
	// 属于 Critical,不参与裁剪。
	tailBudget := userPromptTailBudgetRunes - len([]rune(s))
	tail, _ := AssembleWithBudget(tailBlocks, tailBudget)
	s += tail
	// 2026-08-12 §20260812-03 U4 — 3 条核心理由硬约束。
	// 关键行动前必须先用 speak_with_thought 的 internal_thought 列出 3 条核心理由,
	// 由 decision_summary.ExtractThreeReasons 自动截取填入 LastDecisionSummary。
	// §128 对话即思考:不新增字段,完全复用 LastDecisionSummary 既有展示通道。
	// §119 协议层隔离:internal_thought 不入 chat_message,仅 BotTranscript 留痕。
	s += ThreeReasonsBlock(&ctx)

	return s
}

// formatSpeakerLabel renders a wwtypes.SpeechEvent as a model-facing speaker tag.
//
// Output examples (BUG: 狼人杀 7 人局 Agent 多轮上下文):
//
//   - AI bot:        "1号(美团 LongCat-2.0 · bot-张三)"
//   - AI bot, 没昵称: "1号(美团 LongCat-2.0 · 1号)"
//   - 真人玩家:        "1号(张三)"
//   - 真人玩家, 没昵称: "1号(玩家)"
//   - 真人观战:        "观战-张三"
//
// The double identifier (AgentName + 玩家昵称) lets the LLM disambiguate
// "1号 玩家" from "1号 bot" in mixed rooms, which is the whole point of
// surfacing the LLM model name in chat panels (see §15).
func formatSpeakerLabel(sp wwtypes.SpeechEvent) string {
	who := sp.Account
	if who == "" {
		who = "玩家"
	}
	if sp.IsSpectator {
		return "观战-" + who
	}
	if sp.IsBot && sp.AgentName != "" {
		// bot 标记: "1号(美团 LongCat-2.0 · bot-2)" 或 "1号(美团 LongCat-2.0 · 张三)"
		// 优先用 Account 作为人类可读昵称;若 Account 缺失则用 "bot-座位号" 兜底
		if sp.Account == "" {
			return itoa(sp.Seat+1) + "号(" + sp.AgentName + " · bot-" + itoa(sp.Seat+1) + "号)"
		}
		return itoa(sp.Seat+1) + "号(" + sp.AgentName + " · " + sp.Account + ")"
	}
	return itoa(sp.Seat+1) + "号(" + who + ")"
}

// formatSpeechLine 渲染一行 RecentSpeeches 发言,在 formatSpeakerLabel 基础上
// 追加 [HH:MM:SS] 时间戳前缀(2026-07-08 §13.3)。
// 输出示例:
//   - "[14:23:05] 观战-张三: 5号 是不是预言家"
//   - "[14:23:18] 1号(张三): 我是预言家"
//   - "[14:23:30] 2号(美团 LongCat-2.0 · 张三): 同意楼上"
//
// Ts=0 时不渲染时间戳(向后兼容 — 旧 buffer / 测试 stub 可能无 ts)。
func formatSpeechLine(sp wwtypes.SpeechEvent) string {
	prefix := ""
	if sp.Ts > 0 {
		prefix = "[" + time.UnixMilli(sp.Ts).Format("15:04:05") + "] "
	}
	return prefix + formatSpeakerLabel(sp)
}

// formatWhisperLine 渲染一行 WhisperInbox 私聊,加 [HH:MM:SS] 时间戳前缀。
// 输出示例: "[14:23:50] 观战-张三 → 你: 1号 是不是预言家"
func formatWhisperLine(w wwtypes.WhisperEvent) string {
	prefix := ""
	if w.Ts > 0 {
		prefix = "[" + time.UnixMilli(w.Ts).Format("15:04:05") + "] "
	}
	return prefix + formatWhisperLabel(w)
}

// formatWhisperLabel renders a wwtypes.WhisperEvent as a model-facing speaker tag.
// Same conventions as formatSpeakerLabel but uses FromSeat/From as the
// sender's identifier. Includes a "私聊→" prefix so the LLM knows this
// arrived via private channel and the bot should weigh it accordingly.
func formatWhisperLabel(w wwtypes.WhisperEvent) string {
	who := w.From
	if who == "" {
		who = "玩家"
	}
	if w.IsSpectator {
		// Spectators don't whisper into the engine channel, but the
		// listener is generic — render the case defensively.
		return "观战-" + who + " → 你"
	}
	if w.IsBot && w.AgentName != "" {
		if w.From == "" {
			return itoa(w.FromSeat+1) + "号(" + w.AgentName + " · bot-" + itoa(w.FromSeat+1) + "号) → 你"
		}
		return itoa(w.FromSeat+1) + "号(" + w.AgentName + " · " + w.From + ") → 你"
	}
	return itoa(w.FromSeat+1) + "号(" + who + ") → 你"
}

// phaseDesc is a short, model-facing phase name.
func phaseDesc(p string) string {
	switch p {
	case "PhaseNightWolves", "night_wolves":
		return "夜晚·狼人协商刀人"
	case "PhaseNightSeer", "night_seer":
		return "夜晚·预言家查验"
	case "PhaseNightWitch", "night_witch":
		return "夜晚·女巫用药"
	case "PhaseDawn", "dawn":
		return "黎明·公布死亡"
	case "PhaseSheriff", "sheriff":
		return "警长竞选"
	case "PhaseSpeak", "speak":
		return "白天·轮流发言"
	case "PhaseVote", "vote":
		return "白天·投票放逐"
	case "PhaseHunterShoot", "hunter_shoot":
		return "猎人开枪"
	case "PhaseDeathLyric", "death_lyric":
		// BUG 2026-07-09: 遗言阶段。LastWords=true 的出局玩家按座位升序发言。
		return "💀 遗言阶段"
	case "PhaseIdiotReveal", "idiot_reveal":
		// 2026-07-10 §3.5 / §12:白痴翻牌结算。
		return "白痴翻牌"
	case "PhaseGameOver", "gameover":
		return "游戏结束"
	case "PhaseFilling", "filling":
		return "等待入座"
	case "PhasePreWolves", "pre_wolves":
		// BUG: 狼人杀 7 人局 Agent 首夜发言缓冲期(2026-07-08 新增)
		// 该阶段狼人/预言家/女巫均不可行动,仅允许公开/私聊发言。
		return "🕯️ 首夜发言缓冲期（狼尚未刀人）"
	default:
		return p
	}
}

// IdentityBlock renders the 【玩家档案】 text from a list of wwtypes.PlayerBrief.
// Output format (BUG: 狼人杀 7 人局 Agent 多轮上下文 — 2026-07-08):
//
//	【玩家档案】
//	1号 (ID=u_001, 昵称=张三, 🤖模型=美团 LongCat-2.0)
//	2号 (ID=u_002, 昵称=李四, 模型=人类)
//
// Caller is expected to pass a stable 12-seat list (Seats 0..11, filling empty
// seats with Account="(空)") so the LLM can map seat→account across the game.
// 2026-07-10 12 人竞技局。
func IdentityBlock(players []wwtypes.PlayerBrief) string {
	if len(players) == 0 {
		return ""
	}
	s := "【玩家档案】\n"
	for _, p := range players {
		tag := "模型=人类"
		if p.IsBot && p.AgentName != "" {
			tag = "🤖模型=" + p.AgentName
		} else if p.IsBot {
			tag = "🤖模型=未知"
		}
		acct := p.Account
		if acct == "" {
			if p.UserID == "" {
				acct = "(空座位)"
			} else {
				acct = "玩家"
			}
		}
		uid := p.UserID
		if uid == "" {
			uid = "-"
		}
		s += itoa(p.Seat+1) + "号 (ID=" + uid + ", 昵称=" + acct + ", " + tag + ")\n"
	}
	return s
}


// KnowledgeDigestBlock 把当前 bot 的知情清单摘要渲染到 user prompt 末尾。
// 2026-08-10 §20260810-08：仅本人可见，不进入任何聊天或 BotTranscript 通道。
func KnowledgeDigestBlock(ctx *wwtypes.GameContext) string {
	if ctx == nil || ctx.KnowledgeDigest == nil || len(ctx.KnowledgeDigest.Entries) == 0 {
		return ""
	}
	digest := ctx.KnowledgeDigest
	var sb strings.Builder
	sb.WriteString("\n\n【🗂 你的知情清单（第 ")
	sb.WriteString(itoa(ctx.Round))
	sb.WriteString(" 天）】\n你共掌握 ")
	sb.WriteString(itoa(digest.TotalKnown))
	sb.WriteString(" 条信息;本局全房累计产生 ")
	sb.WriteString(itoa(digest.TotalInRoom))
	sb.WriteString(" 条")
	if digest.TotalKnown < digest.TotalInRoom {
		sb.WriteString(" —— 你处于信息劣势,发言时注意别暴露你知道得比表现出来的多")
	}
	sb.WriteString("。\n")
	for _, entry := range digest.Entries {
		sb.WriteString("- ")
		sb.WriteString(knowledgeSourceLabel(entry.Source))
		sb.WriteString(" ×")
		sb.WriteString(itoa(entry.Count))
		if len(entry.Highlights) > 0 {
			sb.WriteString("（最近:")
			for i, highlight := range entry.Highlights {
				if i > 0 {
					sb.WriteString(" / ")
				}
				sb.WriteString("「")
				sb.WriteString(highlight)
				sb.WriteString("」")
			}
			sb.WriteString("）")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("⚠️ 注意:狼队密语 / 私聊 / 夜间技能属于私密来源,白天公开发言引用这些内容等于自曝身份。\n")
	return sb.String()
}

// ConsistencyCheckBlock §20260811-06 U4 — 把一致性校验结果渲染到 user prompt 末尾。
// 仅在 LastConsistencyCheck 非空(R1 / R2 命中)时输出;不强制 LLM 修正,仅作为
// 提示,避免死循环。LLM 自由文本关键词抽取无法 100% 准确,可能误报。
// §128 对话即思考:不复制 LLM CoT,只显示规则代码 + 描述。
func ConsistencyCheckBlock(ctx *wwtypes.GameContext) string {
	if ctx == nil || ctx.LastConsistencyCheck == nil {
		return ""
	}
	check := ctx.LastConsistencyCheck
	if check.Rule == "OK" || check.Rule == "" {
		return ""
	}
	severityEmoji := "ℹ️"
	switch check.Severity {
	case "high":
		severityEmoji = "🚨"
	case "medium":
		severityEmoji = "⚠️"
	}
	var sb strings.Builder
	sb.WriteString("\n【")
	sb.WriteString(severityEmoji)
	sb.WriteString(" 行为一致性提醒(规则 ")
	sb.WriteString(check.Rule)
	sb.WriteString(")】\n")
	sb.WriteString(check.Detail)
	sb.WriteString("\n(系统检测,仅供参考;若误报请忽略)\n")
	return sb.String()
}

// RumorBlock §20260811-06 U5 — 黎明流言块。
// 把最近 5 条流言拼到 user prompt 末尾,Agent 据此决定信/不信。
// §135:rumor 文本不揭示具体身份,LLM 必须自己判断真伪。
// §120 公平性:rumor 真假由服务端权威字段判定,LLM 不可操控。
func RumorBlock(ctx *wwtypes.GameContext) string {
	if ctx == nil || len(ctx.LastRumors) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n【📰 今日流言(共 ")
	sb.WriteString(itoa(len(ctx.LastRumors)))
	sb.WriteString(" 条)】\n")
	sb.WriteString("每条流言可能是真也可能是假,系统不会告诉你真假,请自行判断:\n")
	for _, r := range ctx.LastRumors {
		sb.WriteString("  • [第 ")
		sb.WriteString(itoa(r.Day))
		sb.WriteString(" 天] ")
		sb.WriteString(r.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

func knowledgeSourceLabel(source string) string {
	switch source {
	case "public_speech":
		return "公开发言"
	case "whisper":
		return "私聊"
	case "wolf_pack":
		return "狼队密语"
	case "night_seer":
		return "预言家查验"
	case "night_witch":
		return "女巫夜间信息"
	case "night_guard":
		return "守卫夜间信息"
	case "night_wolf_vote":
		return "狼刀投票"
	case "prop_inject":
		return "道具注入"
	case "role_deal":
		return "开局发牌"
	default:
		return source
	}
}

// HypothesisTableBlock 把当前 bot 的身份假说表渲染到 user prompt 末尾。
//
// 2026-08-10 §20260810-07 — 多假说并行推演。
// §128 对话即思考:HypothesisTable 是思考的持久化呈现,不是新增决策工具;
// LLM 在本轮决策末尾追加 "📊 [{...}]" JSON 段更新假说即可,
// 由 werewolf.HypothesisStore.UpdateFromDecisionSummary 解析写回。
// §119 协议层隔离:不进 chat_message / chat_history,仅本人 bot 可见;
// 玩家侧的 BotTranscript.HypothesisSummary 字段由 sanitizeBotTranscript
// 在非 spectator 路径清空(§135)。
func HypothesisTableBlock(ctx *wwtypes.GameContext) string {
	if ctx == nil || ctx.HypothesisTable == nil {
		return ""
	}
	t := ctx.HypothesisTable
	if len(t.Entries) == 0 {
		return ""
	}
	// 按 target_seat 排序,便于 LLM 快速检索。
	entries := make([]wwtypes.HypothesisEntrySnapshot, len(t.Entries))
	copy(entries, t.Entries)
	sort.Slice(entries, func(i, j int) bool { return entries[i].TargetSeat < entries[j].TargetSeat })
	var sb strings.Builder
	sb.WriteString("\n【📊 你的当前假说(第 ")
	sb.WriteString(itoa(t.Round))
	sb.WriteString(" 天)】\n")
	for _, e := range entries {
		sb.WriteString("- ")
		sb.WriteString(itoa(e.TargetSeat + 1))
		sb.WriteString("号 → ")
		sb.WriteString(e.RoleGuess)
		sb.WriteString(" (")
		sb.WriteString(itoa(e.Confidence))
		sb.WriteString("%)")
		if e.Supporting != "" {
			sb.WriteString(" 支撑:")
			sb.WriteString(e.Supporting)
		}
		if e.Refuting != "" {
			sb.WriteString(" / 反证:")
			sb.WriteString(e.Refuting)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("(如需更新,在本轮决策的 LastDecisionSummary 末尾追加 📊 [{seat,role_guess,confidence,supporting,refuting,updated_at}] JSON 段)\n")
	return sb.String()
}

// InfluenceBlock 把发言影响力生态渲染到 user prompt 末尾。
//
// 2026-08-11 §20260811-02 U1 — 发言影响力生态。
// §120 公平性:公式**随分数一起下发**,避免 Agent 把分数当成不可知的黑箱
// 而产生迷信行为;13 个座位在同一把尺子下被度量。
// §119 对照:与 HeartThought 相反,影响力是公开信息 —— 它同时进 ClientGameState
// 全员可见,此处注入只是让 Agent 也能感知自己的分量。
// §135:分数只反映公开行为(票型/发言/被指向),不含任何角色信息。
func InfluenceBlock(ctx *wwtypes.GameContext) string {
	if ctx == nil || ctx.MyInfluence == nil {
		return ""
	}
	me := ctx.MyInfluence

	// 排名:分数严格高于我的人数 + 1。同分并列取相同名次。
	rank, top, topSeat := 1, me.Total, me.Seat
	for _, s := range ctx.InfluenceScores {
		if s.Total > me.Total {
			rank++
		}
		if s.Total > top {
			top, topSeat = s.Total, s.Seat
		}
	}

	var sb strings.Builder
	sb.WriteString("\n【📢 发言影响力(第 ")
	sb.WriteString(itoa(ctx.Round))
	sb.WriteString(" 天)】\n你的影响力:")
	sb.WriteString(itoa(me.Total))
	sb.WriteString("/100(跟票 ")
	sb.WriteString(itoa(me.Persuasion))
	sb.WriteString(" / 关注 ")
	sb.WriteString(itoa(me.Attention))
	sb.WriteString(" / 参与 ")
	sb.WriteString(itoa(me.Presence))
	sb.WriteString(" / 存活 ")
	sb.WriteString(itoa(me.Survival))
	sb.WriteString(" / 洞察 ")
	sb.WriteString(itoa(me.Insight))
	sb.WriteString(")\n全场排名:")
	sb.WriteString(itoa(rank))
	sb.WriteString("/")
	sb.WriteString(itoa(len(ctx.InfluenceScores)))
	if topSeat != me.Seat {
		sb.WriteString("(第 1 名:")
		sb.WriteString(itoa(topSeat + 1))
		sb.WriteString(" 号 ")
		sb.WriteString(itoa(top))
		sb.WriteString(" 分)")
	}
	sb.WriteString("\n公式:跟票率×35 + 关注度×20 + 发言参与×18 + 存活×12 + 洞察力×15,")
	sb.WriteString("全部基于公开信息(票型 / 发言深度 / 被私聊或道具指向),人人可复算。\n")

	// 按分档给出策略提示 —— 让 LLM 据此调整发言策略,而非单纯知道一个数字。
	switch {
	case me.Total >= 60:
		sb.WriteString("💡 你的影响力偏高 —— 你的发言正在带动票型,")
		sb.WriteString("但也更容易成为狼人的刀口目标和道具集火对象,注意收敛锋芒。\n")
	case me.Total >= 30:
		sb.WriteString("💡 你的影响力中等 —— 有人在听你说话,但还没形成号召力,")
		sb.WriteString("可以尝试更明确地给出投票建议来提升说服力。\n")
	default:
		sb.WriteString("💡 你的影响力偏低 —— 你的发言目前没有改变任何人的行为。")
		sb.WriteString("要么发言太少,要么观点没有说服力;考虑主动带节奏或抛出关键判断。\n")
	}
	return sb.String()
}

// PlayerProfileBlock 把本 bot 视角的人类玩家行为画像渲染到 user prompt 末尾。
//
// 2026-08-11 §20260811-05 U1 — 玩家行为模式学习(PlayerProfile)。
// 数据源:t_lsm_game_agent_player_profile,经房间级预取缓存注入
// GameContext.PlayerProfiles(仅人类座位有值,按 model_key 天然隔离)。
// §130 接线验证:buildAgentContextLocked 末尾 fillPlayerProfilesLocked 真实消费。
// §119 对照:画像是 bot 的私有认知(不进 chat 表/队列/观战者视图),
// 仅经 GameContext 注入 prompt;隐私合规:只含 LLM 摘要后的打法画像,
// 不含聊天原文。
// 无画像(首次同局/全 AI 房间)时返回空串,零污染。
func PlayerProfileBlock(ctx *wwtypes.GameContext) string {
	if ctx == nil || len(ctx.PlayerProfiles) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n【🧩 对手画像(你过往的交手记忆)】\n")
	// 座位升序输出,prompt 稳定可缓存。
	for seat := 0; seat < 13; seat++ {
		md, ok := ctx.PlayerProfiles[seat]
		if !ok || md == "" {
			continue
		}
		sb.WriteString(itoa(seat + 1))
		sb.WriteString(" 号: ")
		sb.WriteString(md)
		sb.WriteString("\n")
	}
	sb.WriteString("(以上是你在过往对局中积累的对人类玩家的观察,供参考但不必盲从——人也会变。)\n")
	return sb.String()
}

// ThreeReasonsBlock 2026-08-12 §20260812-03 U4 — 3 条核心理由硬约束。
//
// 关键行动前必须先用 speak_with_thought 工具的 internal_thought 字段
// 列出 3 条核心理由（编号 1./2./3. 开头，每条 ≤ 30 字），由
// decision_summary.ExtractThreeReasons 自动截取填入 LastDecisionSummary。
//
// 设计约束：
//   - §128 对话即思考：完全复用 LastDecisionSummary 既有展示通道，不新增字段；
//   - §119 协议层隔离：internal_thought 不入 chat_message 表 / chat_history 队列 / 公屏；
//     仅在 BotTranscript.HeartThought 留痕，观战者审计可见；
//   - §197 流式续命：复用现有 parentCtx + extendedTimeout，零 LLM 额外调用；
//   - 零数据库/零前端改动；prompt token 增加 ≈ 150 token/次，13 人局总体 < 3% 消耗。
//
// 仅在以下关键行动阶段提示本约束（其它阶段 LLM 可忽略）：
//   vote / seer_check / witch_act / guard_protect / hunter_shoot / wolf_kill
func ThreeReasonsBlock(ctx *wwtypes.GameContext) string {
	if ctx == nil {
		return ""
	}
	// 仅在需要"决策"的关键行动阶段才输出,避免 LLM 在闲聊阶段也被强制输出 3 条理由。
	switch ctx.Phase {
	case "vote", "PhaseVote",
		"night_wolves", "PhaseNightWolves",
		"night_seer", "PhaseNightSeer",
		"night_witch", "PhaseNightWitch",
		"night_guard", "PhaseNightGuard",
		"hunter_shoot", "PhaseHunterShoot",
		"idiot_reveal", "PhaseIdiotReveal":
		// 允许通过
	default:
		return ""
	}
	return "\n【3 条核心理由约束 §20260812-03 U4】\n" +
		"当你执行关键行动（vote / wolf_kill / seer_check / witch_act / guard_protect / hunter_shoot）\n" +
		"前，必须先用 speak_with_thought 工具，在 internal_thought 字段中先列出 3 条核心理由：\n" +
		"  1. （≤30 字）\n" +
		"  2. （≤30 字）\n" +
		"  3. （≤30 字）\n" +
		"然后在 text 字段中给出对外发言 / 决策描述。\n" +
		"internal_thought 字段不入公聊（§119 协议层隔离），仅作决策可解释性记录。\n" +
		"若你不是该行动角色（例如你在 vote 阶段但不是投票方），可忽略此约束。\n"
}
