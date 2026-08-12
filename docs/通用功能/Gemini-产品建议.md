针对当前的 LsmWebGame 狼人杀 13 人局系统架构，结合现有代码（如 ServerGo/agent/ 的决策引擎、wwplayer/wwjudge、MEMORY.md 跨局记忆、道具注入系统）以及 LLM 大模型的特性，从高可玩性、高观赏性、精巧架构设计、凸显 LLM 特性与博弈猜疑链四个维度，为您梳理并提出了以下功能添加与优化建议：

一、 增强博弈与猜疑链的功能设计 (Game Mechanics & Deception)
大模型最擅长自然语言博弈、角色扮演和逻辑推演，但容易在长对话中“露怯”或产生惯性。以下功能可大幅加深猜疑链：

1. 深度“伪造/验明”黑市机制（暗线信件与情报打折）
设计理念：目前已有道具注入，但缺乏“玩家间私下传递伪造证据”的博弈机制。

功能实现：

暗线信件 (Secret Letter)：夜间允许任意玩家（或持有特殊道具的玩家）向另一玩家发送匿名加密私信。

情报黑市 (Information Black Market)：LLM Agent 可在夜间选择“向黑市购买情报”或“投递虚假情报（如假预言家查验）”。法官（Judge Agent）根据一定概率注入真假混杂的信息。

对 LLM 的凸显：迫使 Agent 在 GameContext 中不仅要分析公屏发言，还要在内部 Memory 中构建“私信信任链”，形成“公屏套话 vs 私下勾心斗角”的双重猜疑。

2. 猜疑链可视化：“心理信任矩阵”与“假说树” (Hypothesis Tree)
设计理念：人类观众看不懂 Agent 到底在“装蠢”还是“真瞎”。

功能实现：

后端给 Agent 增加工具或强结构化输出：build_trust_matrix，输出它对其余 12 名玩家的身份猜想（例如：Seat 3: 🐺狼人 (80%) / 🔮假预言家 (20%)）。

假说树模拟：Agent 内部维护 2~3 个竞争性假说（Hypothesis A: 3是预言家；Hypothesis B: 3是悍跳狼，5才是真预）。

观赏性价值：前端可增加一个“Agent 逻辑透视面板 (Mind-Map Visualizer)”，观众能实时看到某个 Agent 的“内心猜疑链”是如何因人类或其它 Agent 的一句“致命发言”而发生断裂和剧烈动摇的。

二、 观赏性与娱乐性创新 (Audience & Viewer Experience)
作为 Web 平台与混合对局游戏，观赏性（Spectator Experience）是吸引用户的核心。

1. “导演/观众”弹幕介入与赌局系统 (Audience Interaction System)
功能设计：

观众下注与高光预测：观众可以在白天投票前用平台金币下注“谁将被放逐”或“今晚狼人杀谁”。

“神之声”干扰弹幕 (Voice of God)：允许观众消耗金币发送“天降神启”提示（如“3号刚刚逻辑前后矛盾”）。法官 Agent 接收后，可选择性作为“环境事件”广播给特定或全体 Agent，测试 Agent 对突发外部噪扰的抗干扰与归因能力。

2. Agent 情绪与高光高能时刻 (Emotional Avatar & Highlighting System)
设计理念：利用现有的 emotion_switch_speak 架构（§213），将 Agent 的情绪变化升华。

功能实现：

“心态崩溃/强行镇定”视觉特效：当 Agent 的思考耗时过长、受道具击中或被连续围攻时，前端 Avatar 触发“汗流浃背”、“眼神闪烁”、“拍桌怒吼”的 Live2D / CSS 动态特效。

高光剪辑 (Highlight Clips Generation)：每局结束后，利用 wwjudge 的整局总结，自动提取本局最戏剧化的 3 个时刻（如：“女巫毒错人现场”、“狼人精准悍跳成功”），在结算界面生成可一键分享的“AI 逻辑对决高光剧本”。

三、 LLM 驱动与 Agent 精巧架构设计优化 (Agent Engineering)
结合现有 ServerGo/agent/ 的代码结构（如 wwplayer、wwjudge、MEMORY.md 跨局迭代），在技术架构上做深度的精巧优化：

1. 引入“双过程思维模型” (Dual-System Thought Process: 快思考与慢思考)
现状问题：慢模型（如 DeepSeek、GLM）响应长，快模型逻辑欠缺，单一 prompt 容易产生局限。

架构优化：

System 1 (快思考 - 直觉与情绪反应)：用轻量级快速模型（或低 Token 限制）实时分析当前公屏发言，快速产生“警觉度”与“情绪倾向”。

System 2 (慢思考 - 严密逻辑推理)：在 MyTurn 触发前，并行在后台根据 ChatHistoryQueue 增量推演全局身份和逻辑链，最后融合生成包含 internal_thought 与 text 的发言。

效果：极大降低 LLM 响应延迟感，且发言兼具情绪真实感与严密的推理逻辑。

2. Agent“人格化与历史包袱”系统 (Dynamic Personality & Rivalry)
现状：已具备 MEMORY.md 跨局学习（§131），但 Agent 之间缺少“宿敌关系”。

优化方案：

宿敌记忆 (Rivalry Memory)：当 Agent A 和 Agent B 多次在同桌对局时，记忆中追加“宿敌标签”（例如：“Kimi 在上一局作为狼人骗了我，这局我对他的信任度默认 -20%”）。

性格倾向参数 (Personality Profiles)：给 Agent 注入不同性格性格模板（如：“激进激烈的逻辑狂魔”、“语速缓和的煽动大师”、“低调的苟活派”），使 7 个 Agent 不再是“同一种语气”，大幅提升对局的丰富度。

3. “心口不一”能力深化与黑话/暗号系统 (Subtext & Secret Code Engine)
功能设计：

狼人阵营 Agent 在 wolf_whisper（狼队交流，§133）中，可以协商建立“公屏发言暗号”（例如：“如果我白天说‘3号逻辑很清爽’，代表今晚刀 3”）。

白天发言时，Agent 会尝试在 speak_with_thought 中隐晦嵌入暗号，并让队友在后台解析。

技术亮点：展现大模型对于“隐喻、上下文暗示与长距离暗号协同”的高阶能力，极其凸显 LLM 驱动的智能感。

四、 落地实现路线图建议 (Actionable Roadmap)
为保持系统的稳定性与高可用性（吸取 §92a 死锁、§130 未接线等历史教训），建议分三步走：

第一阶段（轻量高收益 - 观赏性提升）：

在前端增加 Agent 信任度/猜疑链 UI 透视面板（基于 Agent 现有的决策摘要）。

增加 “高光对决剧本” 总结落地页，增强分享与观赏属性。

第二阶段（玩法创新 - 猜疑与道具）：

扩展道具系统：新增 “暗线伪造信件” 与 “情报黑市” 机制。

引入 “狼队暗号系统”，升级 wolfpack_room.go。

第三阶段（架构重构 - Agent 智能演进）：

结合 MEMORY.md 升级  Agent 宿敌与性格系统。

优化 LLM 调用的“快慢双系统推理”，降低等待时延，提升博弈流畅度。