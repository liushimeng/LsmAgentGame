// Translation dictionary type. Every locale file (zh-CN.ts / en.ts / ja.ts)
// implements this exact shape so missing keys surface as type errors.
export interface Dict {
  // 通用 / common
  'common.appName': string;
  'common.loading': string;

  // 狼人杀 13 人标准竞技局 / werewolf — 第 6 款游戏
  'werewolf.title': string;
  'werewolf.createRoom': string;
  // R187-4: gameState 尚未到达时区分「连接服务器」与「同步游戏状态」。
  'werewolf.connecting': string;
  'werewolf.syncingState': string;
  /** 观战者等待 game.state 到达时的过渡文案(Debug-2026-08-12-01 P3-7)。 */
  'werewolf.spectatorSyncing': string;
  // BUG-R212-P1-03 (2026-07-30): 同步超时后的可操作错误态(替代无限 spinner)。
  'werewolf.syncStalled': string;
  'werewolf.syncStalledHint': string;
  'werewolf.syncRetry': string;
  'werewolf.backToLobby': string;
  // 2026-07-30 BUG-FIX: §130 人类等待窗口 — 倒计时文案({sec} 秒后自动开始)。
  'werewolf.humanWaitCountdown': string;
  'werewolf.noRooms': string;
  'werewolf.createFirst': string;
  'werewolf.joinRoom': string;
  'werewolf.capacityHint': string;
  'werewolf.leaveRoom': string;
  'werewolf.confirmLeave': string;
  'werewolf.day': string;
  'werewolf.mySeat': string;
  'werewolf.myRole': string;
  'werewolf.phase': string;
  'werewolf.action.wolfSuicide': string;
  'werewolf.action.hunterShootPrompt': string;
  'werewolf.eyesClosed': string;
  'werewolf.role.werewolf': string;
  'werewolf.role.seer': string;
  'werewolf.role.witch': string;
  'werewolf.role.hunter': string;
  // 2026-07-10 12 人局 — 新增 白痴(idiot)神职。
  'werewolf.role.idiot': string;
  'werewolf.role.villager': string;
  // 2026-07-11: 扩展神职角色(13人随机牌组池)
  'werewolf.role.guard': string;
  // §198 骑士角色 — 重新启用,加回中文显示名。
  'werewolf.role.knight': string;
  // §猎魔人 猎魔人角色 — 重新启用,加回中文显示名。
  'werewolf.role.demon_hunter': string;
  // ⚠️ 2026-07-29 已退役:无引擎/工具/美术实现,前端隐藏
  // 'werewolf.role.magician': string;
  // 'werewolf.role.merchant': string;
  // 'werewolf.role.dreamer': string;
  // 'werewolf.role.crow': string;
  // 'werewolf.role.scarecrow': string;
  // 'werewolf.role.prince': string;
  // 'werewolf.role.pure_white': string;
  // R185 中文化: 未揭示身份占位符 + 死因 verdict 文案
  'werewolf.role.unknown': string;
  // 2026-08-06 §20260806-03 创建房间自选角色
  'werewolf.role.random': string;
  'werewolf.rolePick.label': string;
  'werewolf.rolePick.hint': string;
  // 2026-08-11 BUG-ROLE-MISMATCH-P0 — 自选角色未生效提示(GameInfoPanel 就地 + toast)
  'werewolf.rolePick.unmetInline': string; // ⚠️ 自选「{role}」未生效
  'werewolf.rolePick.unmetToast': string;  // ⚠️ 自选角色「{pref}」本局未生效,已随机分配为「{actual}」
  'werewolf.verdict.execution': string;
  'werewolf.verdict.death': string;
  // 2026-07-29 §134 守卫角色 — 守卫操作面板文案(NightActionPanel 第四形态)。
  'werewolf.guard.title': string;             // 🛡️ 守卫请守护一名玩家
  'werewolf.guard.protect': string;           // 守护(主按钮)
  'werewolf.guard.skip': string;              // 空守
  'werewolf.guard.lastNight': string;         // 昨晚已守(座位 tooltip)
  'werewolf.guard.blindHint': string;         // 看不到狼刀目标
  'werewolf.guard.noConsecutiveHint': string; // 不可连守
  // §198 骑士决斗 UI 文案(7 键 — DayControlPanel speak 阶段骑士按钮 + 活动流广播)
  'werewolf.knight.title': string;             // ⚔️ 骑士决斗 — 选择一名玩家
  'werewolf.knight.cta': string;               // ⚔️ 决斗
  'werewolf.knight.confirm': string;           // 对 {target} 号 发动决斗
  'werewolf.knight.skip': string;              // 放弃本轮(技能保留)
  'werewolf.knight.risk': string;              // ⚠️ 风险提示
  'werewolf.knight.broadcastSuccess': string;  // 命中狼:目标出局
  'werewolf.knight.broadcastFail': string;     // 自决:骑士出局
  'werewolf.knight.confirmDialog': string;     // §20260811-06 U1 — 二次确认弹层文案
  'werewolf.knight.confirmCta': string;        // §20260811-06 U1 — 二次确认 CTA 按钮文案
  // §猎魔人 猎魔人狩猎 UI 文案(9 键 — NightActionPanel 第五形态 + 活动流广播)
  'werewolf.demon_hunter.title': string;             // 🎯 猎魔人狩猎阶段
  'werewolf.demon_hunter.cta': string;               // 🎯 选择今晚要狩猎的目标
  'werewolf.demon_hunter.confirm': string;           // 确认狩猎
  'werewolf.demon_hunter.skip': string;              // 放弃狩猎(空过)
  'werewolf.demon_hunter.risk': string;              // ⚠️ 狩猎好人 = 你自己出局
  'werewolf.demon_hunter.firstNightDisabled': string;// 首夜不可发动,明日可用
  'werewolf.demon_hunter.broadcastHit': string;      // 命中狼:X 号 出局
  'werewolf.demon_hunter.broadcastMiss': string;     // 不是狼,猎魔人自决出
  'werewolf.demon_hunter.firstNightNotice': string;  // 🌙 首夜狩猎尚未解锁
  'werewolf.phase.filling': string;
  // BUG Round 40 §95: pre_wolves 升级为"首夜强制发言阶段"
  'werewolf.phase.pre_wolves': string;
  // 2026-07-29 §134 守卫角色 — 「盲守」夜间阶段(在狼刀之前)
  'werewolf.phase.night_guard': string;
  'werewolf.phase.night_wolves': string;
  'werewolf.phase.night_seer': string;
  'werewolf.phase.night_witch': string;
  // §猎魔人 猎魔人夜间阶段(在女巫之后、dawn 之前)
  'werewolf.phase.night_demon_hunter': string;
  'werewolf.phase.dawn': string;
  'werewolf.phase.sheriff': string;
  'werewolf.phase.speak': string;
  'werewolf.phase.vote': string;
  // 2026-07-10 12 人局 — 白痴翻牌阶段(投票放逐白痴时触发)。
  'werewolf.phase.idiot_reveal': string;
  'werewolf.phase.death_lyric': string;
  'werewolf.phase.hunter_shoot': string;
  'werewolf.phase.over': string;
  'werewolf.phase.restart_vote': string;

  // 2026-07-10 §重构 — Agent LLM 调用相位状态机的多态文案(驱动 BotPhaseIndicator)。
  // 5 态:idle / calling / streaming / retrying / quarantined。
  // 每态有不同文案 + 视觉,模仿 ChatGPT o1 / Claude.ai / Perplexity 阶梯指示器。
  /** 调用开始 <3s:`即将发言…` 蓝紫脉冲 + 错峰跳点。 */
  'werewolf.thinking.soon': string;
  /** 调用中 ≥3s:`思考中 {seconds} 秒…` 蓝紫脉冲 + 倒计时。seconds 由前端 i18n 模板参数化。 */
  'werewolf.thinking.active': string; // {seconds}
  /** 调用中 ≥15s:`深度思考中 {seconds} 秒…` 渐变加深 + 倒计时。 */
  'werewolf.thinking.deep': string; // {seconds}
  /** retry loop 内,首 token 未到:`重试 {current}/{max}…` 黄脉冲 + N/M 徽章。 */
  'werewolf.thinking.retry': string; // {current}/{max}
  /** 流式响应首 token 到达(可选):`生成中…` 蓝绿脉冲。 */
  'werewolf.thinking.streaming': string;
  /** 永久禁用(连续 LLM 失败超阈值,§R81 阈值为 10):`已禁用 · {reason}` 红边框 + ⚠️。 */
  'werewolf.thinking.quarantined': string; // {reason}
  /** §R180-P3-OBS4:服务端未给出 reason 时的兜底文案(替代旧硬编码「5 连失败」)。 */
  'werewolf.thinking.quarantinedFallback': string;
  /** 上游 429 rate limit:`上游限流,冷却中…` 红脉冲 + 倒计时。 */
  'werewolf.thinking.cooling': string;
  /** 5xx 退避重试:`连接中断,重试中…` 黄脉冲 + 倒计时。 */
  'werewolf.thinking.reconnecting': string;
  /** 等待 LLM 并发槽(排队中) — 2026-07-12 §127 */
  'werewolf.thinking.queued': string;
  /** 被 LLMCallLimiter 限流(冷却中) — 2026-07-12 §127 */
  'werewolf.thinking.throttled': string;
  /** 全局 Header 聚合统计文案。count 是正在思考的 bot 数,total 是 13 人局总座位。 */
  'werewolf.thinking.headerSummary': string; // {count}/{total}
  /** 全局 Header 徽章的 a11y 标签(给屏幕阅读器)。 */
  'werewolf.thinking.headerAriaLabel': string; // {count}/{total}

  // 2026-07-24 — WerewolfStatusBar 聚合 chip(原硬编码中文,i18n 化)。
  // quarantine 语义软化:后端 manager 自动代打(auto-skip),不再用"已禁用/已停止"措辞。
  /** 状态栏标题。 */
  'werewolf.statusBar.title': string;
  /** 正在响应的 Agent 聚合 chip:`🧠 {count} 名 Agent 响应中`。 */
  'werewolf.statusBar.agentsResponding': string; // {count}
  /** 响应中 chip 子计数:调用中。 */
  'werewolf.statusBar.subCalls': string; // {count}
  /** 响应中 chip 子计数:流式生成中。 */
  'werewolf.statusBar.subStreaming': string; // {count}
  /** 响应中 chip 子计数:重试中。 */
  'werewolf.statusBar.subRetrying': string; // {count}
  /** quarantine 聚合 chip:`⏳ {count} 名 Agent 系统代打中`。 */
  'werewolf.statusBar.agentsAutoPlay': string; // {count}
  /** 真人混合房间标记 chip。 */
  'werewolf.statusBar.mixedRoom': string;
  /** 阶段倒计时:`⏱ 剩余 {seconds}s`。 */
  'werewolf.statusBar.clockRemaining': string; // {seconds}
  /** 阶段超时提示:`⌛ 等待阶段推进…`。 */
  'werewolf.statusBar.clockOverdue': string;
  // 2026-07-30 方案-20260730-03 Fix-UI1 — 终局收编 chip(status==='over' 时
  // 替代「Agent 响应中 / 阶段倒计时」,消除「对局结束 vs 等待阶段推进…」矛盾)。
  /** 终局 chip:`🏁 对局已结束 · 复盘模式`。 */
  'werewolf.statusBar.gameOverReview': string;
  /** header 副标题终局胜方:`对局结束 · 胜者:{winner}`。 */
  'werewolf.header.gameOverWinner': string; // {winner}
  // §2026-08-07 — 合并主标题+法官横幅+状态条 为 GameStatusHeader,新增折叠相关 i18n 键。
  /** GameStatusHeader 折叠按钮(收起)。 */
  'werewolf.gameStatusHeader.fold': string;
  /** GameStatusHeader 折叠按钮(展开)。 */
  'werewolf.gameStatusHeader.expand': string;
  /** GameStatusHeader 区域 aria-label。 */
  'werewolf.gameStatusHeader.title': string;
  /** GameStatusHeader 副标题(观战中)。 */
  'werewolf.gameStatusHeader.spectator': string;
  /** GameStatusHeader 副标题(对局中)。 */
  'werewolf.gameStatusHeader.playing': string;
  // 2026-07-30 §统计增强 — 状态栏聚合 API/Token 统计 chip。
  /** API 调用统计 chip:`📊 调用 {callCount} · 成功 {successCount} · 失败 {failCount}`。 */
  'werewolf.statusBar.apiStats': string; // {callCount} {successCount} {failCount}
  /** Token 统计 chip:`🔤 Token: 输入 {input} · 输出 {output} · 总计 {total}`。 */
  'werewolf.statusBar.tokenStats': string; // {input} {output} {total}
  /** §20260817-03 U3 — 每小时 Token 消耗 chip(紧跟运行时长):`⚡ ≈{rate}/小时`。 */
  'werewolf.statusBar.tokensPerHour': string; // {rate}
  /** §20260817-03 U3 — 每小时 Token 消耗 chip 的 tooltip 说明。 */
  'werewolf.statusBar.tokensPerHourTip': string; // {total} {hours}
  /** 法官 Token 统计 chip。 */
  'werewolf.statusBar.judgeTokenStats': string; // {callCount} {successCount} {failCount} {input} {output} {total}
  // 2026-07-30 §统计增强 — 座位卡 Token 行。
  /** 座位卡 Token 行:`🔤 输入 {input} · 输出 {output}`。 */
  'werewolf.seat.tokenRow': string; // {input} {output}
  /** 缓存命中提示。 */
  'werewolf.seat.cacheHit': string;

  // 重开局投票面板(2026-07-10)
  'werewolf.restartVote.title': string;
  'werewolf.restartVote.subtitle': string;
  'werewolf.restartVote.winner.wolf': string;
  'werewolf.restartVote.winner.good': string;
  'werewolf.restartVote.deadlineHint': string; // {sec}
  'werewolf.restartVote.yesBtn': string;
  'werewolf.restartVote.noBtn': string;
  'werewolf.restartVote.abstainBtn': string;
  'werewolf.restartVote.yesCol': string;
  'werewolf.restartVote.noCol': string;
  'werewolf.restartVote.abstainCol': string;
  'werewolf.restartVote.voted': string; // {choice}
  'werewolf.restartVote.passed': string;
  'werewolf.restartVote.rejected': string;
  'werewolf.restartVote.timeout': string;
  'werewolf.restartVote.quorum': string; // {cur}/{quota}
  'werewolf.restartVote.spec': string;
  'werewolf.restartVote.spectatorHint': string;

  // 2026-07-10 12 人局 — 警徽流声明面板(SheriffStreamPanel,预言家警长在白天声明验人顺序)。
  'werewolf.sheriffStream.title': string;
  'werewolf.sheriffStream.slot1': string;
  'werewolf.sheriffStream.slot2': string;
  'werewolf.sheriffStream.targetPlaceholder': string;
  'werewolf.sheriffStream.declare': string;
  'werewolf.sheriffStream.revoke': string;
  'werewolf.sheriffStream.hint': string; // 仅预言家警长可用
  'werewolf.sheriffStream.close': string;
  // 2026-07-10 12 人局 — 警徽流结算广播(sheriff_stream_settle)提示。
  'werewolf.sheriffStream.settled': string; // {reason}
  'werewolf.sheriffStream.successor': string; // {seat}
  'werewolf.sheriffStream.ripped': string;
  // 2026-07-10 12 人局 — 白痴翻牌面板(IdiotRevealPanel)。
  'werewolf.idiotReveal.title': string;
  'werewolf.idiotReveal.prompt': string;
  'werewolf.idiotReveal.revealBtn': string; // 翻牌
  'werewolf.idiotReveal.skipBtn': string; // 放弃
  'werewolf.idiotReveal.done.reveal': string; // 已翻牌
  'werewolf.idiotReveal.done.skip': string; // 放弃翻牌(正常放逐)
  'werewolf.idiotReveal.deadlineHint': string; // {sec}
  'werewolf.idiotReveal.spectatorHint': string;
  // 2026-07-10 12 人局 — GameInfoPanel 屠边计数(神/民/狼)。
  'werewolf.counts.divine': string;
  'werewolf.counts.plain': string;
  'werewolf.counts.wolf': string;
  'werewolf.counts.title': string;
  // 2026-07-12 §13 增强 — 阵营侧滑抽屉(FactionDrawer)。
  'werewolf.drawer.title': string;
  'werewolf.drawer.close': string;
  'werewolf.drawer.tab.task': string;
  'werewolf.drawer.tab.agent': string;
  'werewolf.drawer.tab.player': string;
  'werewolf.drawer.empty': string;
  'werewolf.drawer.task.empty': string;
  'werewolf.drawer.agent.latency': string;
  'werewolf.drawer.agent.avg': string;
  'werewolf.drawer.agent.calls': string;
  'werewolf.drawer.player.bot': string;
  'werewolf.drawer.player.human': string;
  // 2026-08-09 §20260809-01 — 观战者等待提示。
  'werewolf.drawer.waitingForPlayers': string;
  // 2026-07-18 §UX-运行时 — 对局历史抽屉(HistoryDrawer)。
  'werewolf.history.drawerTitle': string;
  'werewolf.history.headerButton': string;
  // 2026-08-06 §房间布局优化 — 折叠按钮文案。
  'werewolf.fold.toggleChat': string;
  'werewolf.fold.toggleInfo': string;
  'werewolf.fold.chatCollapsed': string;
  'werewolf.fold.infoCollapsed': string;
  'werewolf.fold.infoExpanded': string;
  // 2026-08-07 §房间聊天优化:werewolf.fold.chatExpanded 与 werewolf.chatTitle
  // 死代码已清理(外层冗余 header 删除后,无任何引用)。
  'werewolf.history.subtab.timeline': string;
  'werewolf.history.subtab.monologue': string;
  'werewolf.history.subtab.deaths': string;
  'werewolf.history.subtab.summary': string;
  'werewolf.history.subtab.judge': string;
  // 2026-08-10 §20260810-08 — 信息账本二期「信息传播时序图」tab(spectator only)。
  'werewolf.history.subtab.infoflow': string;
  // §20260811-06 U3 — 公开推理链 sub-tab(spectator only)。
  'werewolf.history.subtab.reasoning': string;
  // §20260811-08 U1/U3 — 上帝视角 sub-tab(spectator only)
  'werewolf.history.subtab.godmode': string;
  // §20260812-03 U1 — 阵营胜率热力图 sub-tab(spectator only)
  'werewolf.history.subtab.heatmap': string;
  // §20260813-02 U4 — 夜间血迹图 sub-tab(spectator only)
  'werewolf.history.subtab.bloodmap': string;
  // §20260814-01 U1 — 三个接线修复的 sub-tab。
  'werewolf.history.subtab.mindmirror': string;
  'werewolf.history.subtab.trusttrace': string;
  'werewolf.history.subtab.review': string;
  'werewolf.history.reasoning.empty': string;
  'werewolf.history.reasoning.filterAll': string;
  // §20260811-06 U5 — 黎明流言系统(5 类模板,活动流自动渲染,本键预留扩展)。
  'werewolf.rumor.label': string;
  'werewolf.rumor.village_idle': string;
  'werewolf.rumor.witch_used': string;
  'werewolf.rumor.no_kill': string;
  'werewolf.rumor.mystic_kill': string;
  'werewolf.rumor.hunter_alive': string;
  'werewolf.history.clock.running': string;
  'werewolf.history.clock.ended': string;
  'werewolf.history.clock.idle': string;
  // §20260812-03 U1 — 阵营胜率热力图(spectator only,§132 隐私隔离)
  'werewolf.heatmap.title': string;
  'werewolf.heatmap.subtitle': string;
  'werewolf.heatmap.empty': string;
  // §20260813-02 U4 — 夜间血迹图(spectator only,HistoryDrawer 第 12 sub-tab)
  'werewolf.bloodmap.title': string;
  'werewolf.bloodmap.night': string;
  'werewolf.bloodmap.empty': string;
  'werewolf.bloodmap.legend.wolfKill': string;
  'werewolf.bloodmap.legend.seerCheck': string;
  'werewolf.bloodmap.legend.witch': string;
  'werewolf.bloodmap.legend.guard': string;
  'werewolf.bloodmap.legend.demonHunt': string;
  // §20260812-03 U2 — 私下通道(白天 speak→vote 窗口)
  'werewolf.letter.title': string;
  'werewolf.letter.inbox_empty': string;
  'werewolf.letter.send': string;
  'werewolf.letter.target_label': string;
  'werewolf.letter.body_placeholder': string;
  'werewolf.letter.daily_limit': string;
  'werewolf.letter.window_closed': string;
  'werewolf.letter.err_self': string;
  // §20260812-03 U3 — 阵营赌注系统
  'werewolf.bet.title': string;
  'werewolf.bet.faction_wolf': string;
  'werewolf.bet.faction_good': string;
  'werewolf.bet.amount_label': string;
  'werewolf.bet.settled_win': string;
  'werewolf.bet.settled_lose': string;
  'werewolf.history.timeline.empty': string;
  'werewolf.history.timeline.phase': string;
  'werewolf.history.timeline.death': string;
  'werewolf.history.timeline.vote': string;
  'werewolf.history.timeline.sheriffStream': string;
  'werewolf.history.timeline.idiotReveal': string;
  'werewolf.history.timeline.day': string;
  // 2026-08-08 §20260808-02 — 遗言阶段全员可见进度条(LastWordsStage)+
  // 历史抽屉时间轴遗言条目。progress/座位名用 {done}/{total} 插值。
  'werewolf.lastWords.title': string;
  'werewolf.lastWords.progress': string;
  'werewolf.lastWords.nowSpeaking': string;
  'werewolf.lastWords.statusSpoken': string;
  'werewolf.lastWords.statusSkipped': string;
  'werewolf.lastWords.statusPending': string;
  'werewolf.history.monologue.empty': string;
  'werewolf.history.monologue.heartOnly': string;
  'werewolf.history.deaths.empty': string;
  'werewolf.history.deaths.title': string;
  'werewolf.history.summary.empty': string;
  'werewolf.history.summary.modelMemory': string;
  // 2026-08-10 §20260810-08 — 信息账本二期「信息传播时序图」专属文案(spectator only)。
  /** 顶部告警区标题:`⚠️ 疑似说漏嘴 N 条（仅供复盘参考）`。 */
  'werewolf.infoflow.leakAlertTitle': string; // {count}
  /** 顶部告警区副标题:声明检测是复盘线索而非裁决器。 */
  'werewolf.infoflow.leakDisclaimer': string;
  /** 单条说漏嘴渲染:`第 N 天 · X号 提及「Y号」— 该信息仅在【来源】中出现过`。 */
  'werewolf.infoflow.leakEntry': string; // {day} {seat} {hintSeat} {source}
  /** hover 显示「知情座位列表」:`知情:N, M, K`。 */
  'werewolf.infoflow.knowerSeats': string; // {seats}
  /** hover 显示「发言摘录」label。 */
  'werewolf.infoflow.excerpt': string;
  /** hover 显示「来源 + 阶段」label。 */
  'werewolf.infoflow.fromSource': string; // {source}
  /** 信息流主网格图标题:`信息传播时序图 · N 条`。 */
  'werewolf.infoflow.timelineTitle': string; // {count}
  /** 网格图座位表头:`座位`。 */
  'werewolf.infoflow.seatLabel': string;
  /** 网格图天数列前缀:`D`。 */
  'werewolf.infoflow.dayPrefix': string;
  /** 空状态:`账本为空,信息流尚未生成`。 */
  'werewolf.infoflow.empty': string;
  /** 6 个来源图例标签(色点 + 标签)。 */
  'werewolf.infoflow.source.public': string;
  'werewolf.infoflow.source.whisper': string;
  'werewolf.infoflow.source.wolfpack': string;
  'werewolf.infoflow.source.night': string;
  'werewolf.infoflow.source.prop': string;
  'werewolf.infoflow.source.system': string;
  // §20260809-02 U3 Identity guess accuracy keys
  'werewolf.settlement.guessAccuracy.title': string;
  'werewolf.settlement.guessAccuracy.detail': string;
  'werewolf.settlement.guessAccuracy.empty': string;
  // §20260811-07 U2 — 自动高光集锦战报。
  'werewolf.battleReport.title': string;
  'werewolf.battleReport.seat': string;
  'werewolf.battleReport.round': string;
  'werewolf.battleReport.showSource': string;
  'werewolf.battleReport.hideSource': string;
  'werewolf.battleReport.expandRest': string;
  'werewolf.battleReport.collapseRest': string;
  'werewolf.battleReport.guardian_shield': string;
  'werewolf.battleReport.witch_save': string;
  'werewolf.battleReport.witch_poison_wolf': string;
  'werewolf.battleReport.close_vote': string;
  'werewolf.battleReport.hunter_kill_wolf': string;
  'werewolf.battleReport.wolf_suicide': string;
  // §20260811-07 U1 — 死后幽灵语音。
  'werewolf.ghostVoice.label': string;
  'werewolf.ghostVoice.enter': string;
  'werewolf.judge.title': string;
  'werewolf.judge.silent': string;
  'werewolf.judge.pending': string;
  'werewolf.judge.quarantined': string;
  'werewolf.judge.summaryReady': string;
  // 2026-07-30 §重构 — 创建界面法官配置卡改为两选项:Agent 法官 / 真人法官。
  // AI 法官与原「主持人 Agent (法官)」是同一概念,真人法官当前等同 Agent 法官
  // 运行(后端真人接入尚未实现,UI 占位对齐 docs/狼人杀-重构方案/主持人Agent重构设计.md)。
  'werewolf.judge.modeLabel': string;
  'werewolf.judge.mode.agent': string;
  'werewolf.judge.mode.human': string;
  'werewolf.judge.model': string;
  'werewolf.judge.modelPlaceholder': string;
  'werewolf.judge.hint': string;
  // 2026-08-11 §20260811-09 U2 — Agent 难度分级文案 4 档 + 4 档 hint 提示。
  'werewolf.difficulty.title': string;
  'werewolf.difficulty.easy': string;
  'werewolf.difficulty.normal': string;
  'werewolf.difficulty.hard': string;
  'werewolf.difficulty.hell': string;
  'werewolf.difficulty.hint.easy': string;
  'werewolf.difficulty.hint.normal': string;
  'werewolf.difficulty.hint.hard': string;
  'werewolf.difficulty.hint.hell': string;
  'werewolf.difficulty.badge': string;
  // 2026-08-11 §20260811-09 U1 — 观战模式 AI 解说席 UI 文案。
  'werewolf.commentary.title': string;
  'werewolf.commentary.empty': string;
  'werewolf.commentary.stylePro': string;
  'werewolf.commentary.styleFun': string;
  'werewolf.commentary.enable': string;
  // 2026-08-09 §20260809-01 — 创建房间弹窗全部文案(原硬编码中文)。
  'werewolf.createModal.title': string;
  'werewolf.createModal.roomName': string;
  'werewolf.createModal.roomNamePlaceholder': string;
  'werewolf.createModal.aiCount': string; // {count}
  'werewolf.createModal.allHuman': string;
  'werewolf.createModal.humanAiMix': string; // {human} {ai}
  'werewolf.createModal.loadingModels': string;
  'werewolf.createModal.modelsUnavailable': string; // {error}
  'werewolf.createModal.aiSeats': string; // {count}
  'werewolf.createModal.reshuffle': string;
  'werewolf.createModal.aiSeatLabel': string; // {index}
  'werewolf.createModal.aiModelLabel': string; // {index}
  'werewolf.createModal.aiRoleLabel': string; // {index}
  'werewolf.createModal.seatNumber': string; // {n}
  'werewolf.createModal.cancel': string;
  'werewolf.createModal.submit': string;
  'werewolf.createModal.submitFailed': string;
  'werewolf.createModal.submitError': string; // {message}
  // 2026-08-17 §20260817-02 — ROW2 全人类空态 + ROW3 commentary 折叠提示。
  'werewolf.createModal.allHumanEmptyState': string;
  'werewolf.createModal.commentaryRowHint': string;
  'werewolf.judge.activity.title': string;
  'werewolf.judge.activity.open': string;
  'werewolf.judge.activity.llmMs': string; // {ms}
  'werewolf.judge.statusLine': string; // {emoji} {label}
  'werewolf.judge.status.ready': string;
  'werewolf.judge.status.thinking': string; // {ms}
  'werewolf.judge.status.quarantined': string;
  // 2026-07-22 §UX-法官布局 — 抽屉内法官详情面板文案。
  'werewolf.judge.panel.disabled': string;
  'werewolf.judge.panel.init': string;
  'werewolf.judge.panel.waitingFirst': string;
  'werewolf.judge.panel.quarantinedBadge': string;
  'werewolf.judge.panel.historyTitle': string;
  // 2026-07-12 §13 增强 — Agent 最后调用时间徽章(AgentCallTimeBadge)。
  'werewolf.bot.lastCall': string;
  'werewolf.bot.neverCalled': string;
  'werewolf.bot.suffixAgo': string;

  // 2026-07-21 §13 道具系统 — PropPanel 文案。
  'werewolf.prop.title': string;
  'werewolf.prop.balance': string; // {balance}
  'werewolf.prop.balanceLoading': string; // 金币余额尚未同步(loading 哨兵)
  'werewolf.prop.remaining': string; // {count}
  'werewolf.prop.cooldown': string; // {sec}
  'werewolf.prop.empty': string;
  'werewolf.prop.use': string;
  'werewolf.prop.confirmUse': string; // {target}
  'werewolf.prop.useWithTarget': string; // {target} — BUG-R186-P3 一次点击直接提交时的按钮文案
  'werewolf.prop.targetLabel': string;
  'werewolf.prop.recentEvents': string; // {count}
  'werewolf.prop.event.hit': string;
  'werewolf.prop.event.miss': string;
  'werewolf.prop.error.unknown': string;
  'werewolf.prop.error.remainingZero': string;
  'werewolf.prop.error.cooldown': string; // {sec}
  'werewolf.prop.error.insufficient': string; // {price} {balance}
  'werewolf.prop.error.noTarget': string;
  'werewolf.prop.error.sameFaction': string;
  'werewolf.prop.error.budgetExhausted': string; // {remain} {price}
  // 2026-08-09 §20260808-03 — 道具面板折叠/死亡状态相关键。
  'werewolf.prop.collapse': string;
  'werewolf.prop.expand': string;
  'werewolf.prop.deadBadge': string;
  'werewolf.prop.deadLockedHint': string;
  // 2026-08-23 §20260823-02 — 弹出式/堆叠式面板优化:统一折叠骨架 + 提交即收起摘要。
  'werewolf.panel.collapse': string;
  'werewolf.panel.expand': string;
  'werewolf.panel.close': string;
  'werewolf.panel.sentProp': string; // {name}
  'werewolf.panel.sentLetter': string; // {seat}
  'werewolf.panel.sentBet': string; // {amount}
  'werewolf.panel.voted': string; // {seat}
  'werewolf.panel.submitted': string;
  'werewolf.panel.restartVoted': string; // {choice}
  // v20260830-01: 非角色卡牌模块折叠标题
  'werewolf.panel.nightAction': string;
  'werewolf.panel.dayControl': string;
  'werewolf.panel.prop': string;
  'werewolf.panel.secretLetter': string;
  'werewolf.panel.factionBet': string;
  'werewolf.panel.commitments': string;
  'werewolf.panel.lastWords': string;
  // v20260830-01: 狼队友标注
  'werewolf.wolfTeammate': string;
  'werewolf.wolfIdentity.human': string;
  'werewolf.wolfIdentity.agent': string;
  // 2026-08-09 §20260808-03 — 死亡玩家在各阶段看到的观察者提示。
  'werewolf.dead.title': string; // {phase}
  'werewolf.dead.nightHint': string;
  'werewolf.dead.voteHint': string; // {tally}
  'werewolf.dead.speakHint': string; // {seat}
  'werewolf.dead.idiotHint': string; // {seat}
  'werewolf.dead.hunterDeadHint': string;
  'werewolf.dead.actions.title': string; // {phase}
  'werewolf.dead.actions.watch': string;
  'werewolf.dead.actions.lastwords': string;
  // v2 重设计 2026-07-21：道具命中干扰信号 / 全局预算 / 经济文案。
  'werewolf.prop.survivalTitle': string;
  'werewolf.prop.survivalBody': string;
  'werewolf.prop.budgetLabel': string; // {remain} {budget}
  'werewolf.prop.effect.expose': string;
  'werewolf.prop.effect.scatter': string;
  'werewolf.prop.effect.emotion': string; // {emotion}
  'werewolf.prop.effect.guide': string; // {seat}
  'werewolf.prop.effectDisclaimer': string;
  // §20260811-08 U5 — 模型风格标识符「流派」标签(M3 §5 P2)
  'werewolf.modelstyle.logic':       string;
  'werewolf.modelstyle.emotion':     string;
  'werewolf.modelstyle.textbook':    string;
  'werewolf.modelstyle.steady':      string;
  'werewolf.modelstyle.aggressive':  string;
  'werewolf.modelstyle.drama':       string;
  'werewolf.modelstyle.calculator':  string;
  'werewolf.modelstyle.unknown':     string;

  // 2026-08-04 §重构 — emotion 标签(狼人杀 Agent 表情系统)
  'werewolf.emotion.confident': string;
  'werewolf.emotion.excited':   string;
  'werewolf.emotion.calm':      string;
  'werewolf.emotion.panic':     string;
  'werewolf.emotion.wary':      string;
  'werewolf.emotion.irritated': string;
  'werewolf.emotion.grievance': string;
  'werewolf.emotion.confused':  string;
  'werewolf.emotion.guilty':    string;
  'werewolf.emotion.tired':     string;
  // 2026-08-04 §表情特效(设计 20260804-02)— neutral 头像 label + 8 特效名(tooltip/调试)
  'werewolf.emotion.neutral':   string;
  'werewolf.emotionfx.effect.pulse':         string;
  'werewolf.emotionfx.effect.shake':         string;
  'werewolf.emotionfx.effect.sweat':         string;
  'werewolf.emotionfx.effect.rage':          string;
  'werewolf.emotionfx.effect.tears':         string;
  'werewolf.emotionfx.effect.spin_question': string;
  'werewolf.emotionfx.effect.glow':          string;
  'werewolf.emotionfx.effect.drowsy':        string;
  // HistoryDrawer 情绪变化曲线
  'werewolf.history.emotionHistory.title': string;
  // 2026-07-23 §道具特效 — PropUseOverlay 道具使用视觉特效叠加层文案。
  'werewolf.propOverlay.badgeHit': string;   // 中招标记
  'werewolf.propOverlay.badgeMiss': string;  // 未中招标记
  'werewolf.propOverlay.targetAOE': string;  // AOE 目标文案(所有玩家)

  // v5 重构 2026-07-21：EconTier 5 档徽章。
  'werewolf.prop.econTier.label': string; // 经济档位
  'werewolf.prop.econTier.boom': string;     // 🟣 Boom（暴富）
  'werewolf.prop.econTier.health': string;   // 🟢 Health（健康）
  'werewolf.prop.econTier.caution': string;  // 🟡 Caution（警戒）
  'werewolf.prop.econTier.danger': string;   // 🟠 Danger（危险）
  'werewolf.prop.econTier.critical': string; // 🔴 Critical（危急）
  'werewolf.prop.econTier.absorbPct': string; // {pct}% 销毁率

  // 2026-07-22 §任务2 — 玩家身份猜测 UI 文案(SeatCell 上的徽章 + 弹出层)。
  'werewolf.guess.title': string;            // 弹出层 title/aria-label
  'werewolf.guess.label': string;            // 缩略徽章(无猜测时):「+ 猜身份」
  'werewolf.guess.popoverTitle': string;     // {seat} 弹出层提示
  'werewolf.guess.clear': string;            // 「清除猜测」按钮
  'werewolf.guess.revealedTruth': string;    // tooltip:死亡后服务端揭示的真实身份

  // 标题栏 / 工具栏 — header
  'header.openMenu': string;
  'header.collapse': string;
  'header.expand': string;
  'header.buildTime': string;
  'header.logout': string;
  'header.logoutConfirm': string;
  'header.language': string;
  'header.gitLog': string;
  'header.wiki': string;
  'header.srcStats': string;

  // 菜单栏 — sidebar
  'nav.home': string;
  'nav.games': string;
  'nav.xiangqi': string;
  'nav.chess': string;
  'nav.junqi': string;
  'nav.doudizhu': string;
  'nav.profile': string;
  'nav.adminUsers': string;
  'nav.about': string;
  'sidebar.collapse': string;
  'sidebar.expand': string;
  'nav.group.traditional': string;
  'nav.group.agent': string;

  // 聊天 — chat
  'chat.lobbyTitle': string;
  'chat.roomTitle': string; // 含 {id}
  'chat.empty': string;
  'chat.placeholder': string;
  'chat.connecting': string;
  'chat.send': string;
  'chat.collapse': string;
  'chat.expand': string;
  'chat.gameTitle': string;
  'chat.settings': string;
  // Pagination / history UI
  'chat.loadingMore': string;
  'chat.noOlderMessages': string;
  // Whisper (private message) — room-scoped DM visible to sender, recipient, admins.
  'chat.whisperTag': string;
  'chat.whisperTo': string;         // {name}
  'chat.whisperPlaceholder': string; // {name}
  'chat.whisperSend': string;
  'chat.selectUser': string;

  // 聊天设置 — chat settings modal
  'chatSettings.title': string;
  'chatSettings.cleanupTitle': string;
  'chatSettings.cleanupDesc': string;
  'chatSettings.startTime': string;
  'chatSettings.endTime': string;
  'chatSettings.cleanupBtn': string;
  'chatSettings.cleaning': string;
  'chatSettings.cleanupSuccess': string; // {count}
  'chatSettings.cleanupFailed': string;
  'chatSettings.errorTimeRange': string;
  'chatSettings.errorEndBeforeStart': string;

  // 页面 — pages
  'home.title': string;
  'games.title': string;
  // §20260819-02 大厅菜单分类
  'games.categories.traditional': string;
  'games.categories.agent': string;
  // §20260819-02 通用 UI
  'common.backToLobby': string;
  'profile.title': string;
  'profile.userId': string;
  'profile.account': string;
  'profile.nickname': string;
  'profile.editNickname': string;
  'profile.nicknamePlaceholder': string;
  'profile.nicknameRequired': string;
  'profile.nicknameSaved': string;
  'profile.myInviteCode': string;
  'profile.copy': string;
  'profile.copied': string;
  'profile.referralCount': string;
  'profile.referralsTitle': string;
  'profile.referralsEmpty': string;
  'profile.loginRequired': string;
  'about.title': string;
  'about.version': string;
  'about.gitSha': string;
  'about.buildTime': string;
  'about.gitShaUnknown': string;

  // 鉴权 — auth
  'auth.signIn': string;
  'auth.createAccount': string;
  'auth.register': string;
  'auth.account': string;
  'auth.phone': string;
  'auth.password': string;
  'auth.captcha': string;
  'auth.refreshCaptcha': string;
  'auth.signingIn': string;
  'auth.email': string;
  'auth.inviteCode': string;
  'auth.inviteCodePlaceholder': string;
  'auth.creating': string;
  'auth.registerSuccessTitle': string;
  'auth.registerSuccessHint': string;
  'auth.goToLogin': string;
  // Session expired / invalid / missing — friendly notices shown via the global
  // auth-error toast. These REPLACE the internal server jargon
  // ("authorization token expired", errcode 10003) — we never show that to users.
  'auth.sessionExpired': string;
  'auth.sessionInvalid': string;
  'auth.sessionMissing': string;

  // git 提交记录弹窗 — git log modal
  'gitLog.title': string;
  'gitLog.loading': string;
  'gitLog.loadingDetail': string;
  'gitLog.empty': string;
  'gitLog.loadError': string; // {msg}
  'gitLog.id': string;
  'gitLog.time': string;
  'gitLog.message': string;
  'gitLog.files': string;
  'gitLog.add': string;
  'gitLog.del': string;
  'gitLog.file': string;
  'gitLog.noFiles': string;
  'gitLog.clickToExpand': string;
  'gitLog.pageInfo': string; // {page} {total} {count}
  'gitLog.prev': string;
  'gitLog.next': string;
  'gitLog.close': string;
  'gitLog.pageSize': string;
  'gitLog.pageSizeUnit': string;
  'gitLog.summary': string; // {commits} {files} {adds} {dels}
  'gitLog.search': string;
  'gitLog.searchPlaceholder': string;
  'gitLog.expandAll': string;
  'gitLog.collapseAll': string;
  'gitLog.refresh': string;
  'gitLog.filtered': string; // {n}
  'gitLog.fileSummary': string; // {n} {adds} {dels}

  // 源码统计弹窗 — source code statistics modal
  'sourceStats.title': string;
  'sourceStats.builtAt': string; // {time}
  'sourceStats.files': string;
  'sourceStats.lines': string;
  'sourceStats.bytes': string;
  'sourceStats.ext': string;
  'sourceStats.bar': string;
  'sourceStats.error': string;

  // 规则说明 — rules viewer (5 款游戏共用一组 key)
  'rules.button': string; // 规则说明按钮文案
  'rules.title.xiangqi': string;
  'rules.title.chess': string;
  'rules.title.junqi': string;
  'rules.title.doudizhu': string;
  'rules.title.texasholdem': string;
  'rules.title.werewolf': string;
  'rules.loadError': string;
  'rules.loadErrorHint': string; // 例如:刷新页面或联系管理员
  'rules.dragHint': string; // "↕ 拖动 · Esc 关闭"
  'rules.close': string;

  // 中国象棋 — xiangqi
  'xiangqi.title': string;
  'xiangqi.createRoom': string;
  'xiangqi.noRooms': string;
  'xiangqi.createFirst': string;
  'xiangqi.joinRoom': string;
  'xiangqi.exitRoom': string;
  'xiangqi.red': string;
  'xiangqi.black': string;
  'xiangqi.you': string;
  'xiangqi.yourTurn': string;
  'xiangqi.opponentTurn': string;
  'xiangqi.check': string;
  'xiangqi.waiting': string;
  'xiangqi.waitingOpponent': string;
  'xiangqi.moveCount': string;
  'xiangqi.lastMove': string;
  'xiangqi.resign': string;
  'xiangqi.confirmResign': string;
  'xiangqi.confirmLeave': string;
  'xiangqi.youWin': string;
  'xiangqi.youLose': string;
  'xiangqi.redWin': string;
  'xiangqi.blackWin': string;
  'xiangqi.draw': string;
  'xiangqi.reason.checkmate': string;
  'xiangqi.reason.resign': string;
  'xiangqi.reason.stalemate': string;
  'xiangqi.style': string;
  'xiangqi.styleWarring': string;
  'xiangqi.styleRobot': string;
  'xiangqi.playerDisconnecting': string; // {userId}
  'xiangqi.autoRemoveNotice': string;

  // 国际象棋 — chess
  'chess.title': string;
  'chess.createRoom': string;
  'chess.noRooms': string;
  'chess.createFirst': string;
  'chess.joinRoom': string;
  'chess.exitRoom': string;
  'chess.white': string;
  'chess.black': string;
  'chess.you': string;
  'chess.yourTurn': string;
  'chess.opponentTurn': string;
  'chess.check': string;
  'chess.waiting': string;
  'chess.waitingOpponent': string;
  'chess.moveCount': string;
  'chess.lastMove': string;
  'chess.resign': string;
  'chess.confirmResign': string;
  'chess.confirmLeave': string;
  'chess.youWin': string;
  'chess.youLose': string;
  'chess.whiteWin': string;
  'chess.blackWin': string;
  'chess.draw': string;
  'chess.reason.checkmate': string;
  'chess.reason.resign': string;
  'chess.reason.stalemate': string;
  'chess.reason.fiftyMove': string;
  'chess.reason.insufficient': string;
  'chess.reason.threefold': string;
  'chess.style': string;
  'chess.styleEuropean': string;
  'chess.styleCyberpunk': string;
  'chess.promotion.title': string;
  'chess.promotion.prompt': string;
  'chess.promotion.queen': string;
  'chess.promotion.rook': string;
  'chess.promotion.bishop': string;
  'chess.promotion.knight': string;

  // 中国军棋 — junqi
  'junqi.title': string;
  'junqi.createRoom': string;
  'junqi.noRooms': string;
  'junqi.createFirst': string;
  'junqi.joinRoom': string;
  'junqi.exitRoom': string;
  'junqi.red': string;
  'junqi.black': string;
  'junqi.you': string;
  'junqi.yourTurn': string;
  'junqi.opponentTurn': string;
  'junqi.waiting': string;
  'junqi.waitingOpponent': string;
  'junqi.moveCount': string;
  'junqi.resign': string;
  'junqi.confirmResign': string;
  'junqi.confirmLeave': string;
  'junqi.youWin': string;
  'junqi.youLose': string;
  'junqi.phaseLayout': string;
  'junqi.layoutHint': string;
  'junqi.layoutSubmit': string;
  'junqi.layoutSubmitted': string;
  'junqi.opponentSubmitted': string;
  'junqi.style': string;
  'junqi.styleNaruto': string;
  'junqi.styleAntiJapanese': string;
  'junqi.modeHidden': string;
  'junqi.modeOpen': string;

  // 斗地主 — doudizhu
  'doudizhu.title': string;
  'doudizhu.createRoom': string;
  'doudizhu.noRooms': string;
  'doudizhu.createFirst': string;
  'doudizhu.joinRoom': string;
  'doudizhu.exitRoom': string;
  'doudizhu.waiting': string;
  'doudizhu.resign': string;
  'doudizhu.confirmResign': string;
  'doudizhu.confirmLeave': string;
  'doudizhu.yourTurn': string;
  'doudizhu.opponentTurn': string;
  'doudizhu.landlord': string;
  'doudizhu.farmer': string;
  'doudizhu.landlordWin': string;
  'doudizhu.farmerWin': string;
  'doudizhu.seatYou': string;
  'doudizhu.seat': string;
  'doudizhu.phaseBidding': string;
  'doudizhu.phaseOver': string;
  'doudizhu.bidPass': string;
  'doudizhu.bidScore': string;
  'doudizhu.bidWaiting': string;
  'doudizhu.play': string;
  'doudizhu.pass': string;
  'doudizhu.multiplier': string;
  'doudizhu.style': string;
  'doudizhu.style_traditional_landlord': string;
  'doudizhu.style_urban_worker': string;

  // 德州扑克 — texasholdem
  'nav.texasholdem': string;

  // 狼人杀 7 人局 — werewolf (第 6 款游戏)
  'nav.werewolf': string;
  'texasholdem.title': string;
  'texasholdem.createRoom': string;
  'texasholdem.noRooms': string;
  'texasholdem.createFirst': string;
  'texasholdem.joinRoom': string;
  'texasholdem.exitRoom': string;
  // §20260823-01 — 玩家身份的离开按钮文案（观战者仍使用 spectator.exitRoom）。
  // 命名「AsPlayer」明确与 spectator.exitRoom 区分，避免 §130 「声明了却从不接线」。
  'texasholdem.exitRoomAsPlayer': string;
  'texasholdem.waiting': string;
  'texasholdem.resign': string;
  'texasholdem.confirmResign': string;
  'texasholdem.confirmLeave': string;
  'texasholdem.yourTurn': string;
  'texasholdem.opponentTurn': string;
  'texasholdem.fold': string;
  'texasholdem.check': string;
  'texasholdem.call': string;
  'texasholdem.bet': string;
  'texasholdem.raise': string;
  'texasholdem.minRaiseTo': string;
  'texasholdem.allIn': string;
  'texasholdem.pot': string;
  'texasholdem.dealer': string;
  'texasholdem.hand': string;
  'texasholdem.showdown': string;
  'texasholdem.winners': string;
  'texasholdem.splitPot': string;
  'texasholdem.seatYou': string;
  'texasholdem.style': string;
  'texasholdem.style_western_cowboy': string;
  'texasholdem.style_wilderness_escape': string;

  // 2026-08-19 §TexasHoldemAgent — RoomCreateModal + Bot UI
  'texasholdem.createModal.title': string;
  'texasholdem.createModal.roomName': string;
  'texasholdem.createModal.roomNamePlaceholder': string;
  'texasholdem.createModal.aiCount': string;
  'texasholdem.createModal.allHuman': string;
  'texasholdem.createModal.humanAiMix': string;
  'texasholdem.createModal.aiSeats': string;
  'texasholdem.createModal.reshuffle': string;
  'texasholdem.createModal.aiSeatLabel': string;
  'texasholdem.createModal.aiModelLabel': string;
  'texasholdem.createModal.seatNumber': string;
  'texasholdem.createModal.loadingModels': string;
  'texasholdem.createModal.modelsUnavailable': string;
  'texasholdem.createModal.cancel': string;
  'texasholdem.createModal.submit': string;
  'texasholdem.createModal.submitFailed': string;
  'texasholdem.createModal.submitError': string;
  'texasholdem.bot.badge': string;
  'texasholdem.bot.thinking': string;
  'texasholdem.bot.recentDecision': string;
  'texasholdem.bot.heartThought': string;
  'texasholdem.bot.chatPreview': string;
  'texasholdem.bot.actionDisabled': string;

  // 观察者 / 观战 — spectator mode (applies across all 5 games)
  'chat.spectatorTag': string;
  // R91-P0-3 (2026-07-11): Interject tag — bot 主动插话 / 主动发言标记。
  'chat.interjectTag': string;
  // 2026-08-05 §Agent聊天显示优化 F4 — 遗言(death_lyric_spoken 活动事件)
  // 在房间聊天中按「发言体」渲染时使用的徽章文案。
  'chat.lastWordsTag': string;
  // 2026-08-08 §20260808-02 — 遗言三节点视觉成体系:开始/放弃节点徽章。
  'chat.lastWordsStartTag': string;
  'chat.lastWordsSkipTag': string;
  'spectator.badge': string;
  'spectator.exitRoom': string;
  'lobby.watchButton': string;

  // 房间列表表格 — shared RoomListTable (列标题 + 排序 + 分页)
  'lobby.colId': string;
  'lobby.colName': string;
  'lobby.colCreated': string;
  'lobby.colPlayers': string;
  'lobby.colStatus': string;
  'lobby.colActions': string;
  'lobby.pageSize': string;       // 每页
  'lobby.pageInfo': string;       // 第 {page}/{total} 页
  'lobby.prev': string;
  'lobby.next': string;
  'lobby.join': string;           // 加入
  'lobby.full': string;           // 已满
  'lobby.reenter': string;        // 进入房间(BUG-R200-P2-05:刷新后重新进入)
  'lobby.copyId': string;         // 复制编号
  'lobby.copied': string;         // 已复制
  // §R180-P3-OBS1:房间状态可读标签 — 替代 raw status 字符串。
  'lobby.status.open': string;    // 等待中
  'lobby.status.playing': string; // 游戏中
  'lobby.status.over': string;    // 已结束

  // Per-game "观战中…" label.
  'xiangqi.spectating': string;
  'chess.spectating': string;
  'junqi.spectating': string;
  'doudizhu.spectating': string;
  'texasholdem.spectating': string;
  // §20260819-02 P1-3 — 观战者进入未开局房间(<2 人)时的文案,占位 {count}。
  'texasholdem.spectatingWaiting': string;

  // 钱包 — wallet
  'wallet.title': string;
  'wallet.balance': string;
  'wallet.totalEarned': string;
  'wallet.totalSpent': string;
  'wallet.recentTx': string;
  'wallet.noTx': string;
  'wallet.claimDaily': string;
  'wallet.claimed': string;
  'wallet.claimSuccess': string;
  'wallet.amount': string;
  'wallet.reason': string;
  'wallet.time': string;
  'wallet.tx.game_win': string;
  'wallet.tx.game_lose': string;
  'wallet.tx.daily_bonus': string;
  'wallet.tx.ante': string;
  'wallet.tx.settle': string;
  'wallet.tx.register_bonus': string;
  'wallet.tx.daily_login': string;
  'wallet.tx.win_reward': string;
  'wallet.tx.lose_deduct': string;
  'wallet.tx.ante_buyin': string;
  'wallet.tx.ante_refund': string;
  'wallet.tx.task_reward': string;
  'wallet.tx.referral_bonus': string;
  'wallet.tx.admin_adjust': string;
  'wallet.tx.other': string;

  // 金币房间 / Ante room
  'ante.roomType': string;
  'ante.roomTypePractice': string;
  'ante.roomTypeAnte': string;
  'ante.title': string;
  'ante.frozen': string;
  'ante.estimatedWin': string;
  'ante.estimatedLose': string;
  'ante.minBalance': string; // {amount}
  'ante.insufficient': string;
  'ante.goClaim': string;
  'ante.selectAnte': string;

  // 结算面板 — settlement modal
  'settle.title': string;
  'settle.win': string;
  'settle.lose': string;
  'settle.draw': string;
  'settle.ante': string;
  'settle.streakBonus': string;
  'settle.total': string;
  'settle.finalBalance': string;
  'settle.platformFee': string;
  'settle.netGain': string;
  'settle.continue': string;
  'settle.netChange': string;
  'settle.wolfWin': string;
  'settle.goodWin': string;

  // 通用 extension
  'common.cancel': string;
  'common.confirm': string;

  // 底注 / 盲注公共
  'ante.cap': string;          // single-game cap
  'ante.comingSoon': string;   // locked tier tooltip

  // 国际象 — chess
  'chess.kfactor.label': string;
  'chess.kfactor.reward': string;

  // 斗地主 — doudizhu ante / settlement
  'doudizhu.ante.frozenPerSeat': string;
  'doudizhu.ante.tooltip': string;  // {ante} {frozen}
  'doudizhu.ante.yourAnte': string;
  'doudizhu.ante.maxWinFormula': string;
  'doudizhu.ante.bombBurst': string;     // "+200%"
  'doudizhu.settle.firstWinBonus': string;
  'doudizhu.settle.participationBonus': string;
  'doudizhu.settle.netChange': string;
  'doudizhu.settle.multiplierDetail': string;
  'doudizhu.settle.continue': string;

  // 德州扑克 — texasholdem blind / buy-in
  'texasholdem.blind.title': string;
  'texasholdem.blind.bb': string;
  'texasholdem.blind.sb': string;
  'texasholdem.blind.ante': string;
  'texasholdem.blind.buyin': string;
  'texasholdem.blind.default': string;
  'texasholdem.buyin.title': string;
  'texasholdem.buyin.current': string;
  'texasholdem.buyin.pctOfBalance': string;
  'texasholdem.buyin.riskWarning': string;  // {pct}
  'texasholdem.buyin.min': string;
  'texasholdem.buyin.default': string;
  'texasholdem.buyin.max': string;
  'texasholdem.pot.detailed': string;        // {pot} {sidePots}
  'texasholdem.netChange': string;
  'texasholdem.continue': string;

  // 重购对话框 — buy-back dialog
  'rebuy.title': string;
  'rebuy.message': string;      // {seconds}
  'rebuy.now': string;
  'rebuy.forfeit': string;

  // 用户列表页 — /admin/users
  'adminUsers.title': string;
  'adminUsers.tabOnline': string;
  'adminUsers.tabOffline': string;
  'adminUsers.subtitleSuper': string;
  'adminUsers.subtitleAdmin': string;
  'adminUsers.subtitleUser': string;
  'adminUsers.colNickname': string;
  'adminUsers.colOnline': string;
  'adminUsers.colAccount': string;
  'adminUsers.colUserType': string;
  'adminUsers.colPhone': string;
  'adminUsers.colEmail': string;
  'adminUsers.colInviteCode': string;
  'adminUsers.colReferralCount': string;
  'adminUsers.colCreatedAt': string;
  'adminUsers.colLastLoginAt': string;
  'adminUsers.colAction': string;
  'adminUsers.statusOnline': string;
  'adminUsers.statusOffline': string;
  'adminUsers.userTypeNormal': string;
  'adminUsers.userTypeAdmin': string;
  'adminUsers.userTypeSuper': string;
  'adminUsers.delete': string;
  'adminUsers.batchDelete': string;
  'adminUsers.selectAll': string;
  'adminUsers.deselectAll': string;
  'adminUsers.toolbarSelected': string;   // {count}
  'adminUsers.toolbarSelectedOne': string;
  'adminUsers.toolbarClear': string;
  'adminUsers.confirmDeleteTitle': string;
  'adminUsers.confirmBatchDeleteTitle': string; // {count}
  'adminUsers.confirmDeleteOk': string;
  'adminUsers.revokeSuper': string;
  'adminUsers.confirmRevokeSuperTitle': string;
  'adminUsers.revokeSuperOk': string;
  'adminUsers.confirmBatchDeleteOk': string;
  'adminUsers.errorGeneric': string;
  'adminUsers.errorBatchPartial': string;  // {success} {failed}
  'adminUsers.errorBatchAllFailed': string; // {count}
  'adminUsers.errorEmpty': string;
  'adminUsers.errorSelectAtLeastOne': string;
  'adminUsers.errorBatchSizeLimit': string;
  'adminUsers.pageSize': string;
  'adminUsers.pageSizeUnit': string;
  'adminUsers.totalCount': string; // {total}

  // 模型管理 — /admin/models (LLM provider CRUD + detail + game-log)
  'nav.adminModels': string;
  'modelAdmin.title': string;
  'modelAdmin.subtitle': string;
  'modelAdmin.newProvider': string;
  'modelAdmin.editProvider': string;
  'modelAdmin.deleteConfirm': string;
  'modelAdmin.testSuccess': string;
  'modelAdmin.testFailed': string;
  'modelAdmin.reloadSuccess': string;
  'modelAdmin.apiKeyPlaceholder': string;
  'modelAdmin.empty': string;
  'modelAdmin.colAgentName': string;
  'modelAdmin.colModel': string;
  'modelAdmin.colBalance': string;
  'modelAdmin.colProviderType': string;
  'modelAdmin.colApiKeyHint': string;
  'modelAdmin.colEndpoint': string;
  'modelAdmin.colThinking': string;
  'modelAdmin.colEnabled': string;
  'modelAdmin.colUpdatedAt': string;
  'modelAdmin.colAction': string;
  'modelAdmin.fieldAgentName': string;
  'modelAdmin.fieldModel': string;
  'modelAdmin.fieldProviderType': string;
  'modelAdmin.protocolAnthropicMessages': string;
  'modelAdmin.protocolOpenaiCompletions': string;
  'modelAdmin.protocolEndpointHint': string;
  'modelAdmin.fieldApiKey': string;
  'modelAdmin.fieldEndpoint': string;
  'modelAdmin.fieldThinkingRequired': string;
  'modelAdmin.fieldThinkingBudget': string;
  'modelAdmin.fieldRemark': string;
  'modelAdmin.fieldEnabled': string;
  'modelAdmin.actionEdit': string;
  'modelAdmin.actionDelete': string;
  'modelAdmin.actionTest': string;
  'modelAdmin.actionReload': string;
  'modelAdmin.actionBack': string;
  'modelAdmin.actionAdd': string;
  'modelAdmin.test': string;
  // Test result dialog (2026-07-10 真实 Anthropic 对话弹窗)
  'modelAdmin.testDialogTitle': string;
  'modelAdmin.testDialogPrompt': string;
  'modelAdmin.testDialogReply': string;
  'modelAdmin.testDialogEmpty': string;
  'modelAdmin.testDialogError': string;
  'modelAdmin.testDialogHint': string;
  'modelAdmin.testDialogMeta': string;
  'modelAdmin.testDialogClose': string;
  // Detail page
  'modelAdmin.detail.basicInfo': string;
  'modelAdmin.detail.wallet': string;
  'modelAdmin.detail.balance': string;
  'modelAdmin.detail.totalEarned': string;
  'modelAdmin.detail.totalSpent': string;
  'modelAdmin.detail.transactions': string;
  'modelAdmin.detail.noTx': string;
  'modelAdmin.detail.games': string;
  'modelAdmin.detail.noGames': string;
  'modelAdmin.detail.colGameKind': string;
  'modelAdmin.detail.colStartedAt': string;
  'modelAdmin.detail.colResult': string;
  'modelAdmin.detail.colCoinDelta': string;
  'modelAdmin.detail.colLlmCalls': string;
  'modelAdmin.detail.colAction': string;
  'modelAdmin.detail.viewGame': string;
  // §135 — 超级管理员每日 grant
  'modelAdmin.detail.grantDaily': string;
  'modelAdmin.detail.grantDailyDesc': string;
  'modelAdmin.detail.grantAmount': string;
  'modelAdmin.detail.grantAmountHint': string;
  'modelAdmin.detail.grantAmountInvalid': string;
  'modelAdmin.detail.grantRemark': string;
  'modelAdmin.detail.grantRemarkHint': string;
  'modelAdmin.detail.grantRemarkRequired': string;
  'modelAdmin.detail.grantResult': string;
  'modelAdmin.detail.grantSkipped': string;
  'modelAdmin.detail.grantSkippedHint': string;
  'modelAdmin.detail.grantAction': string;
  'modelAdmin.detail.grantNoSuper': string;
  'modelAdmin.detail.balanceAfter': string;
  'modelAdmin.detail.errorAmountInvalid': string;
  // Game log page
  'modelAdmin.game.title': string;
  'modelAdmin.game.bot': string;
  'modelAdmin.game.room': string;
  'modelAdmin.game.duration': string;
  'modelAdmin.game.coinDelta': string;
  'modelAdmin.game.totalCalls': string;
  'modelAdmin.game.inputTokens': string;
  'modelAdmin.game.outputTokens': string;
  'modelAdmin.game.filter.toolOnly': string;
  'modelAdmin.game.filter.thinkingOnly': string;
  'modelAdmin.game.filter.expandReasoning': string;
  'modelAdmin.game.result.win': string;
  'modelAdmin.game.result.lose': string;
  'modelAdmin.game.result.draw': string;
  'modelAdmin.game.result.abandoned': string;
  'modelAdmin.game.phaseLabel': string;
  'modelAdmin.game.thinking': string;
  'modelAdmin.game.toolUse': string;
  'modelAdmin.game.toolResult': string;
  'modelAdmin.game.payload': string;
  'modelAdmin.game.reasoning': string;
  'modelAdmin.game.target': string;
  'modelAdmin.game.empty': string;
  'modelAdmin.game.loading': string;
  'modelAdmin.game.errorLoad': string;
  // §20260813-02 U1/U2 — 运营数据分析面板(胜率趋势 + 道具经济)
  'modelAdmin.analytics.toggle': string;
  'modelAdmin.analytics.empty': string;
  'modelAdmin.analytics.winTrendTitle': string;
  'modelAdmin.analytics.last30d': string;
  'modelAdmin.analytics.winRate': string;
  'modelAdmin.analytics.games': string;
  'modelAdmin.analytics.bestRole': string;
  'modelAdmin.analytics.bestSeat': string;
  'modelAdmin.analytics.sampleLow': string;
  'modelAdmin.analytics.byRole': string;
  'modelAdmin.analytics.bySeat': string;
  'modelAdmin.analytics.propEconTitle': string;
  'modelAdmin.analytics.potReturn': string;
  'modelAdmin.analytics.systemAbsorb': string;
  'modelAdmin.analytics.targetCompens': string;
  'modelAdmin.analytics.totalSpent': string;
  'modelAdmin.analytics.hitRate': string;
  'modelAdmin.analytics.baseHitRate': string;
  'modelAdmin.analytics.propName': string;
  'modelAdmin.analytics.price': string;
  'modelAdmin.analytics.uses': string;
  // §20260816-04 — 雷达图三态
  'modelAdmin.radar.loading': string;
  'modelAdmin.radar.empty': string;
  'modelAdmin.radar.error': string;
  'modelAdmin.radar.retry': string;

  // 2026-08-10 §20260810-06 — 行为承诺系统
  'werewolf.commitment.title': string;
  'werewolf.commitment.my': string;
  'werewolf.commitment.other': string;
  'werewolf.commitment.all': string;
  'werewolf.commitment.target': string;
  'werewolf.commitment.seatSuffix': string;
  'werewolf.commitment.day': string;
  'werewolf.commitment.daySuffix': string;
  'werewolf.commitment.button': string;
  'werewolf.commitment.buttonTitle': string;
  'werewolf.commitment.modalTitle': string;
  'werewolf.commitment.modalHint': string;
  'werewolf.commitment.templateLabel': string;
  'werewolf.commitment.targetLabel': string;
  'werewolf.commitment.selectPlaceholder': string;
  'werewolf.commitment.reasonLabel': string;
  'werewolf.commitment.reasonPlaceholder': string;
  'werewolf.commitment.confirm': string;
  'werewolf.commitment.selectTarget': string;
  'werewolf.commitment.reasonRequired': string;
  'werewolf.commitment.submitFailed': string;

  // 2026-08-11 §20260811-02 U1 — 发言影响力生态。
  'werewolf.influence.tooltip': string;

  // 2026-08-11 §20260811-02 U2 — 补齐后端已下发但前端从未消费的 spectator 字段。
  'werewolf.history.subtab.hypothesis': string;
  'werewolf.history.subtab.decision': string;
  'werewolf.history.hypothesis.empty': string;
  'werewolf.history.hypothesis.round': string;
  'werewolf.history.hypothesis.confidence': string;
  'werewolf.history.hypothesis.supporting': string;
  'werewolf.history.hypothesis.refuting': string;
  'werewolf.history.decision.empty': string;
  'werewolf.history.decision.took': string;
  'werewolf.history.seatFilterAll': string;
  'werewolf.settlement.revealCountdown': string;
  'werewolf.settlement.revealNow': string;
  // §20260811-05 U2 — 赛后复盘问答(RecallChatPanel)。
  'werewolf.recall.title': string;
  'werewolf.recall.hint': string;
  'werewolf.recall.placeholder': string;
  'werewolf.recall.ask': string;
  'werewolf.recall.asking': string;
  'werewolf.recall.empty': string;
  'werewolf.recall.noBots': string;
  'werewolf.recall.errForbidden': string;
  'werewolf.recall.errRateLimit': string;
  'werewolf.recall.errNetwork': string;
  'werewolf.recall.fallbackTag': string;
  // 2026-08-12 §20260812-01 U1 — 个人复盘 4 维面板
  'werewolf.review.title': string;
  'werewolf.review.voteAccuracy': string;
  'werewolf.review.speakExposure': string;
  'werewolf.review.propEfficiency': string;
  'werewolf.review.agentInteraction': string;
  'werewolf.review.overall': string;
  'werewolf.review.highlights': string;
  'werewolf.review.noData': string;
  // 2026-08-12 §20260812-01 U2 — MindMirror
  'werewolf.mindmirror.title': string;
  'werewolf.mindmirror.colYou': string;
  'werewolf.mindmirror.colAgent': string;
  'werewolf.mindmirror.colDiff': string;
  'werewolf.mindmirror.diffOpposite': string;
  'werewolf.mindmirror.diffNear': string;
  'werewolf.mindmirror.diffMatch': string;
  'werewolf.mindmirror.empty': string;
  // 2026-08-12 §20260812-01 U4 — 信任度轨迹
  'werewolf.trustTrace.title': string;
  'werewolf.trustTrace.day': string;
  'werewolf.trustTrace.legendWolf': string;
  'werewolf.trustTrace.legendGood': string;
  'werewolf.trustTrace.legendDivine': string;
  // 2026-08-12 §20260812-01 U3 — 情绪传染
  'werewolf.contagion.infected': string;
}

export type TKey = keyof Dict;
