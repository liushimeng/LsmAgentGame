// Virtual City i18n keys (extracted from types.ts Dict to keep that file < 1800 lines).
//
// Covers: P0 虚拟城市基础 / P1 央行+明斯基+经济循环+社会调研+LPR+提前还款 /
// P2 玩家间交易与财富流动系统。locales/zh-CN.ts · locales/en.ts 按本接口实现。

export interface VirtualCityDict {
  // ── 虚拟城市 / virtualCity — 第 7 款游戏（Agent 组，2026-09-14 P0）──
  // 键清单出处：lag_docs/虚拟城市/已实现/02-架构设计/虚拟城市-前端架构与3D地图-v2.md §7（批次 22 起；v1 已改名重写）
  // + 实现补齐（面板 / 动作表单 / 终局 / 建房弹窗）。districts/actions/endings
  // 子键由代码枚举展开，locales 按扁平键书写。
  'nav.virtualCity': string;
  'virtualCity.title': string;
  'virtualCity.subtitle': string;
  'virtualCity.createRoom': string;
  'virtualCity.roomName': string;
  'virtualCity.monthMs': string;
  'virtualCity.monthMs.fast': string;
  'virtualCity.monthMs.normal': string;
  'virtualCity.monthMs.slow': string;
  'virtualCity.seed': string;
  'virtualCity.join': string;
  'virtualCity.spectate': string;
  'virtualCity.full': string;
  'virtualCity.noRooms': string;
  'virtualCity.createFirst': string;
  'virtualCity.lobbyHint': string;
  'virtualCity.startEarly': string;
  'virtualCity.startHint': string;
  'virtualCity.month': string;
  'virtualCity.age': string;
  'virtualCity.runningTime': string;
  'virtualCity.mySeat': string;
  'virtualCity.waiting': string;
  'virtualCity.spectating': string;
  'virtualCity.joinHint': string;
  'virtualCity.leaveRoom': string;
  'virtualCity.confirmLeave': string;
  'virtualCity.phase.acting': string;
  'virtualCity.phase.settling': string;
  'virtualCity.cycle.recovery': string;
  'virtualCity.cycle.boom': string;
  'virtualCity.cycle.recession': string;
  'virtualCity.cycle.depression': string;
  'virtualCity.lpr': string;
  'virtualCity.cpi': string;
  'virtualCity.stockIndex': string;
  'virtualCity.goldPrice': string;
  'virtualCity.bondYield': string;
  'virtualCity.housePrice': string;
  // 央行货币政策 — P1 央行引擎
  'virtualCity.cb.title': string;
  'virtualCity.cb.m0': string;
  'virtualCity.cb.m1': string;
  'virtualCity.cb.m2': string;
  'virtualCity.cb.mb': string;
  'virtualCity.cb.multiplier': string;
  'virtualCity.cb.policyRate': string;
  'virtualCity.cb.creditTightness': string;
  'virtualCity.cb.loanQuota': string;
  'virtualCity.cb.tightness.loose': string;
  'virtualCity.cb.tightness.neutral': string;
  'virtualCity.cb.tightness.tight': string;
  'virtualCity.cb.tightness.cautious': string;
  'virtualCity.district.finance': string;
  'virtualCity.district.tech': string;
  'virtualCity.district.industry': string;
  'virtualCity.district.oldtown': string;
  'virtualCity.district.commerce': string;
  'virtualCity.district.residential': string;
  'virtualCity.district.suburb': string;
  'virtualCity.district.riverside': string;
  // v2.12 阶段 2 扩展城区（8→16；与 types/virtualCity.ts VIRTUAL_CITY_DISTRICTS 同步）；
  // 批次 20 再扩至 32（下方 16 新键）。
  'virtualCity.district.logistics_port': string;
  'virtualCity.district.hightech_park': string;
  'virtualCity.district.edu_district': string;
  'virtualCity.district.medical_city': string;
  'virtualCity.district.industrial_park': string;
  'virtualCity.district.central_park': string;
  'virtualCity.district.transport_hub': string;
  'virtualCity.district.cultural_creative': string;
  // 批次 20 城市扩张新增 16 区（名 = 文档 1 §2.1 三语表；与 types/virtualCity.ts 同步）。
  'virtualCity.district.fin_sub_center': string;
  'virtualCity.district.software_park': string;
  'virtualCity.district.airport_town': string;
  'virtualCity.district.air_logistics': string;
  'virtualCity.district.auto_city': string;
  'virtualCity.district.mountain_resort': string;
  'virtualCity.district.chem_park': string;
  'virtualCity.district.agri_park': string;
  'virtualCity.district.health_town': string;
  'virtualCity.district.steel_town': string;
  'virtualCity.district.old_city_culture': string;
  'virtualCity.district.university_town': string;
  'virtualCity.district.wetland_park': string;
  'virtualCity.district.sports_new_city': string;
  'virtualCity.district.bay_new_town': string;
  'virtualCity.district.highspeed_rail_town': string;
  'virtualCity.cash': string;
  'virtualCity.netWorth': string;
  'virtualCity.fiIndex': string;
  'virtualCity.passiveIncome': string;
  'virtualCity.monthlyNet': string;
  'virtualCity.monthlyIncome': string;
  'virtualCity.monthlyExpense': string;
  'virtualCity.monthlyTax': string;
  'virtualCity.monthlySocial': string;
  'virtualCity.pension': string;
  'virtualCity.energy': string;
  'virtualCity.network': string;
  'virtualCity.cognition': string;
  'virtualCity.creditScore': string;
  'virtualCity.incomeBand.low': string;
  'virtualCity.incomeBand.mid': string;
  'virtualCity.incomeBand.high': string;
  'virtualCity.incomeBand.top': string;
  'virtualCity.family': string;
  'virtualCity.family.married': string;
  'virtualCity.family.single': string;
  'virtualCity.assets': string;
  'virtualCity.loans': string;
  'virtualCity.noAssets': string;
  'virtualCity.noLoans': string;
  'virtualCity.loan.units': string;
  'virtualCity.loan.value': string;
  'virtualCity.loan.balance': string;
  'virtualCity.loan.rate': string;
  'virtualCity.loan.monthly': string;
  'virtualCity.loan.consumer': string;
  'virtualCity.loan.credit': string;
  'virtualCity.loan.business': string;
  'virtualCity.asset.stock': string;
  'virtualCity.asset.bond': string;
  'virtualCity.asset.gold': string;
  'virtualCity.biz.delivery': string;
  'virtualCity.biz.content': string;
  'virtualCity.biz.tutoring': string;
  'virtualCity.biz.freelance': string;
  'virtualCity.goals': string;
  'virtualCity.ledger.title': string;
  'virtualCity.ledger.monthly': string;
  'virtualCity.ledger.category': string;
  'virtualCity.ledger.count': string;
  'virtualCity.ledger.amount': string;
  'virtualCity.tab.finance': string;
  'virtualCity.tab.market': string;
  'virtualCity.tab.ledger': string;
  // ── 13-3D城市渲染优化 · 阶段 E（2026-09-22）：侧栏 Tab 分组标题 ──
  'virtualCity.tabgroup.data': string;
  'virtualCity.tabgroup.trade': string;
  'virtualCity.panel.waiting': string;
  'virtualCity.panel.spectatorEmpty': string;
  'virtualCity.panel.empty': string;
  'virtualCity.panel.income': string;
  'virtualCity.panel.balance': string;
  'virtualCity.panel.cashflow': string;
  'virtualCity.market.cycleLeft': string; // {n}
  'virtualCity.map.rent': string;
  'virtualCity.map.players': string;
  'virtualCity.actionBudget': string; // {n}
  'virtualCity.action.buyAsset': string;
  'virtualCity.action.sellAsset': string;
  'virtualCity.action.buyHouse': string;
  'virtualCity.action.takeLoan': string;
  'virtualCity.action.repayLoan': string;
  'virtualCity.action.sideBusiness': string;
  'virtualCity.action.stopBusiness': string;
  'virtualCity.action.study': string;
  'virtualCity.action.socialize': string;
  'virtualCity.action.rest': string;
  'virtualCity.action.workOvertime': string;
  'virtualCity.action.moveDistrict': string;
  'virtualCity.action.consume': string;
  'virtualCity.action.donate': string;
  'virtualCity.action.submitMonth': string;
  'virtualCity.action.confirm': string;
  'virtualCity.action.asset': string;
  'virtualCity.action.pickAsset': string;
  'virtualCity.action.amount': string;
  'virtualCity.action.minAmount': string; // {n}
  'virtualCity.action.units': string;
  'virtualCity.action.unitsRange': string; // {n}
  'virtualCity.action.district': string;
  'virtualCity.action.pickDistrict': string;
  'virtualCity.action.downpay': string;
  'virtualCity.action.downpayAmount': string;
  'virtualCity.action.housePrice': string;
  'virtualCity.action.loanKind': string;
  'virtualCity.action.loanAmount': string;
  'virtualCity.action.creditTiers': string;
  'virtualCity.action.noLoan': string;
  'virtualCity.action.repayMin': string; // {n}
  'virtualCity.action.repayFull': string;
  'virtualCity.action.repayOver': string;
  'virtualCity.action.businessKind': string;
  'virtualCity.action.reason': string;
  'virtualCity.error.budget': string;
  'virtualCity.error.cash': string;
  'virtualCity.error.generic': string;
  'virtualCity.error.timeout': string;
  'virtualCity.botPanel.title': string;
  'virtualCity.botPanel.empty': string;
  'virtualCity.botPanel.decision': string;
  'virtualCity.botPanel.heart': string;
  'virtualCity.botPanel.tool': string;
  'virtualCity.ticker.empty': string;
  'virtualCity.ticker.summary': string;
  'virtualCity.ticker.cashDelta': string;
  'virtualCity.ticker.note': string;
  'virtualCity.gameOver.title': string;
  'virtualCity.gameOver.report': string;
  'virtualCity.gameOver.total': string;
  'virtualCity.gameOver.fiScore': string;
  'virtualCity.gameOver.lifeScore': string;
  'virtualCity.gameOver.socialScore': string;
  'virtualCity.gameOver.netWorthCurve': string;
  'virtualCity.gameOver.viewLedger': string;
  'virtualCity.gameOver.backToLobby': string;
  'virtualCity.gameOver.seat': string;
  'virtualCity.gameOver.board': string;
  'virtualCity.gameOver.endingCol': string;
  'virtualCity.ending.winner': string;
  'virtualCity.ending.affluent': string;
  'virtualCity.ending.ordinary': string;
  'virtualCity.ending.indebted': string;
  'virtualCity.ending.bankrupt': string;
  'virtualCity.ending.lonely_rich': string;
  'virtualCity.create.namePlaceholder': string;
  'virtualCity.create.meSeat': string;
  'virtualCity.create.noModels': string;
  'virtualCity.create.failed': string;
  // 2026-09-16 §财商流10–12座位改造 — 建房弹窗座位档位 / 创建者身份 残留键。
  'virtualCity.create.seatNo': string;
  'virtualCity.create.creatorRole': string;
  'virtualCity.create.watchOnly': string;
  // ── §20260921 建房解耦 + 城市背景层（契约 04 §3 键表）──
  // 建房弹窗：背景居民规模数值控件 + LLM 线路池信息行。
  'virtualCity.residentCount': string;
  'virtualCity.residentCountHint': string;
  /** 居民规模兜底校验（正常被 clamp 不触发；契约 01 §3.3）。 */
  'virtualCity.residentCountRequired': string;
  /** {n} = Σ concurrency_lines（线路池容量）。 */
  'virtualCity.linePoolInfo': string;
  'virtualCity.linePoolEmpty': string;
  /** 2026-09-25 §LLM线路池配额 — 建房弹窗 stepper 行标签（max=min(池总量,64)）。 */
  'virtualCity.llmLines': string;
  /** 游戏内城市面板：{n} = 本房生效线路数。 */
  'virtualCity.cityDriverLines': string;
  // CityStatsPanel（右侧栏城市面板）。
  'virtualCity.cityTitle': string;
  'virtualCity.cityPopulation': string;
  'virtualCity.cityEmployment': string;
  'virtualCity.cityMedianIncome': string;
  'virtualCity.citySavings': string;
  'virtualCity.cityStress': string;
  'virtualCity.cityVoices': string;
  /** {month} = 声音所属游戏月。 */
  'virtualCity.cityVoiceOfMonth': string;
  /** 城区人口条形紧凑模式聚合行：{n} = 未展示城区数（v2.12 阶段 2，>12 区时）。 */
  'virtualCity.cityOtherDistricts': string;
  // 批次 20 §3.4：32 区紧凑模式「展开全部 N 区 / 收起」折叠列表按钮。
  'virtualCity.cityDistrictsExpand': string;
  'virtualCity.cityDistrictsCollapse': string;
  /** 驱动层指示（17-CityHuman 02 §6）：{n} = 上月实际驱动居民数。 */
  'virtualCity.cityDriven': string;
  'virtualCity.seatsCount': string; // {n} {max} — 抽样展示居民
  'virtualCity.seatsWaiting': string; // 无占位符
  'virtualCity.botPanel.seat': string;
  'virtualCity.botPanel.titleCount': string; // {count}
  // 2026-09-19 §财商流观战 UI — 12 Agent 卡片过滤与出局状态可读性。
  'virtualCity.botPanel.titleFiltered': string; // {visible} {total}
  'virtualCity.botPanel.filterGroup': string;
  'virtualCity.botPanel.filterActive': string;
  'virtualCity.botPanel.filterDecision': string;
  'virtualCity.botPanel.filterAll': string;
  'virtualCity.botPanel.filterEmpty': string;
  'virtualCity.botPanel.statusActive': string;
  'virtualCity.botPanel.statusOut': string;
  // 2026-09-22 §CityHuman重构 — 居民感知三段式（看见/听见/闻到）。
  'virtualCity.botPanel.sense.title': string;
  'virtualCity.botPanel.sense.see': string;
  'virtualCity.botPanel.sense.hear': string;
  'virtualCity.botPanel.sense.smell': string;
  'virtualCity.botPanel.sense.empty': string;
  'virtualCity.chat.playerFallback': string;
  'virtualCity.minimap.aria': string;

  // ── P1 第二期：真实经济循环引擎 + 社会调研系统（2026-09-16）──
  // economy = EconomyPanel；survey = SurveyPanel；consumption = ActionPanel 档位组。
  'virtualCity.tab.economy': string;
  'virtualCity.tab.survey': string;
  'virtualCity.action.setConsumption': string;
  // CPI 八大类（id 与 goods.go 权重表一致）
  'virtualCity.goods.food': string;
  'virtualCity.goods.clothing': string;
  'virtualCity.goods.housing': string;
  'virtualCity.goods.household': string;
  'virtualCity.goods.transport': string;
  'virtualCity.goods.education': string;
  'virtualCity.goods.healthcare': string;
  'virtualCity.goods.misc': string;
  // 经济循环仪表盘
  'virtualCity.economy.title': string;
  'virtualCity.economy.unavailable': string;
  'virtualCity.economy.cpiYoy': string;
  'virtualCity.economy.cpiMom': string;
  'virtualCity.economy.goodsTitle': string;
  'virtualCity.economy.weight': string;
  'virtualCity.economy.priceIdx': string;
  'virtualCity.economy.up': string;
  'virtualCity.economy.down': string;
  'virtualCity.economy.legendNote': string;
  'virtualCity.economy.laborTitle': string;
  'virtualCity.economy.layoff': string;
  'virtualCity.economy.layoff.0': string;
  'virtualCity.economy.layoff.1': string;
  'virtualCity.economy.layoff.2': string;
  'virtualCity.economy.layoff.3': string;
  'virtualCity.economy.unemployment': string;
  'virtualCity.economy.employment': string;
  'virtualCity.economy.wageGrowth': string;
  'virtualCity.economy.firmRevenue': string;
  'virtualCity.economy.naturalRate': string;
  'virtualCity.economy.societyTitle': string;
  'virtualCity.economy.gini': string;
  'virtualCity.economy.giniWarn': string;
  'virtualCity.economy.quintiles': string;
  'virtualCity.economy.circles': string;
  'virtualCity.economy.circle.survival': string;
  'virtualCity.economy.circle.accumulate': string;
  'virtualCity.economy.circle.freedom': string;
  'virtualCity.economy.eduToggle': string;
  'virtualCity.economy.eduP1': string;
  'virtualCity.economy.eduP2': string;
  'virtualCity.economy.eduEnergy': string; // {up} {down}
  'virtualCity.economy.eduNote': string;
  // 社会调研
  'virtualCity.survey.title': string;
  'virtualCity.survey.launchTitle': string;
  'virtualCity.survey.question': string;
  'virtualCity.survey.questionPlaceholder': string;
  'virtualCity.survey.questionRequired': string;
  'virtualCity.survey.optionPlaceholder': string; // {n}
  'virtualCity.survey.addOption': string;
  'virtualCity.survey.removeOption': string;
  'virtualCity.survey.invalidOptions': string;
  'virtualCity.survey.submit': string;
  'virtualCity.survey.notPlaying': string;
  'virtualCity.survey.limitOpen': string;
  'virtualCity.survey.limitMonth': string;
  'virtualCity.survey.disabled': string;
  'virtualCity.survey.spectatorHint': string;
  'virtualCity.survey.openTag': string;
  'virtualCity.survey.closedTag': string;
  'virtualCity.survey.deadline': string; // {m}
  'virtualCity.survey.launchedAt': string; // {m}
  'virtualCity.survey.answersCount': string; // {n}
  'virtualCity.survey.progress': string;
  'virtualCity.survey.waitingBots': string;
  'virtualCity.survey.total': string; // {n}
  'virtualCity.survey.topReasons': string;
  'virtualCity.survey.noAnswers': string;
  'virtualCity.survey.history': string;
  'virtualCity.survey.empty': string;
  'virtualCity.survey.loadFailed': string;
  // 消费档位（ActionPanel）
  'virtualCity.consumption.title': string;
  'virtualCity.consumption.level.frugal': string;
  'virtualCity.consumption.level.normal': string;
  'virtualCity.consumption.level.refined': string;
  'virtualCity.consumption.level.luxury': string;
  'virtualCity.consumption.forcedDown': string;
  'virtualCity.consumption.forcedHint': string;
  'virtualCity.consumption.invalid': string;

  // ── P2 第三期：玩家间交易与财富流动系统（2026-09-16）──
  // 挂单簿侧栏 Tab
  'virtualCity.tab.listing': string;
  'virtualCity.tab.loan': string;
  'virtualCity.tab.infomarket': string;
  // 挂单簿
  'virtualCity.listing.title': string;
  'virtualCity.listing.create': string;
  'virtualCity.listing.cancel': string;
  'virtualCity.listing.view': string;
  'virtualCity.listing.empty': string;
  'virtualCity.listing.type.all': string;
  'virtualCity.listing.type.asset': string;
  'virtualCity.listing.type.buy': string;
  'virtualCity.listing.type.info': string;
  'virtualCity.listing.type.loan_ofr': string;
  'virtualCity.listing.type.loan_req': string;
  'virtualCity.listing.status.open': string;
  'virtualCity.listing.status.negotiating': string;
  'virtualCity.listing.status.deal': string;
  'virtualCity.listing.status.expired': string;
  'virtualCity.listing.status.cancelled': string;
  'virtualCity.listing.col.id': string;
  'virtualCity.listing.col.type': string;
  'virtualCity.listing.col.seat': string;
  'virtualCity.listing.col.ask': string;
  'virtualCity.listing.col.status': string;
  'virtualCity.listing.col.expire': string;
  'virtualCity.listing.col.actions': string;
  'virtualCity.listing.mine': string;
  'virtualCity.listing.others': string;
  'virtualCity.listing.integrate': string;
  'virtualCity.listing.assetName': string;
  'virtualCity.listing.minPrice': string;
  'virtualCity.listing.askPrice': string;
  'virtualCity.listing.createAssetTitle': string;
  'virtualCity.listing.createBuyTitle': string;
  'virtualCity.listing.selAsset': string;
  'virtualCity.listing.confirmCreate': string;
  // 议价
  'virtualCity.negotiate.title': string;
  'virtualCity.negotiate.start': string;
  'virtualCity.negotiate.accept': string;
  'virtualCity.negotiate.reject': string;
  'virtualCity.negotiate.counter': string;
  'virtualCity.negotiate.offer': string;
  'virtualCity.negotiate.offerPlaceholder': string;
  'virtualCity.negotiate.comment': string;
  'virtualCity.negotiate.commentPlaceholder': string;
  'virtualCity.negotiate.history': string;
  'virtualCity.negotiate.yourTurn': string;
  'virtualCity.negotiate.waiting': string;
  'virtualCity.negotiate.deal': string;
  'virtualCity.negotiate.expired': string;
  'virtualCity.negotiate.rejectMsg': string;
  'virtualCity.negotiate.firstOffer': string;
  // 借贷面板
  'virtualCity.loan.title': string;
  'virtualCity.loan.subtitle': string;
  'virtualCity.loan.lend': string;
  'virtualCity.loan.borrow': string;
  'virtualCity.loan.guarantee': string;
  'virtualCity.loan.col.id': string;
  'virtualCity.loan.col.direction': string;
  'virtualCity.loan.col.seat': string;
  'virtualCity.loan.col.principal': string;
  'virtualCity.loan.col.rate': string;
  'virtualCity.loan.col.term': string;
  'virtualCity.loan.col.monthly': string;
  'virtualCity.loan.col.monthsLeft': string;
  'virtualCity.loan.col.balance': string;
  'virtualCity.loan.col.overdue': string;
  'virtualCity.loan.col.guarantor': string;
  'virtualCity.loan.col.actions': string;
  'virtualCity.loan.empty': string;
  'virtualCity.loan.mine': string;
  'virtualCity.loan.market': string;
  'virtualCity.loan.createTitle': string;
  'virtualCity.loan.selDirection': string;
  'virtualCity.loan.principal': string;
  'virtualCity.loan.maxRate': string;
  'virtualCity.loan.termN': string;
  'virtualCity.loan.needGuarantee': string;
  'virtualCity.loan.acceptBtn': string;
  'virtualCity.loan.repayBtn': string;
  'virtualCity.loan.repayAmount': string;
  'virtualCity.loan.repayFull': string;
  'virtualCity.loan.addGuarantor': string;
  'virtualCity.loan.overdueBadge': string;
  'virtualCity.loan.lender': string;
  'virtualCity.loan.borrower': string;
  'virtualCity.loan.guarantor': string;
  'virtualCity.loan.monthlyPayment': string;
  'virtualCity.loan.confirmAccept': string;
  // 拍卖
  'virtualCity.auction.title': string;
  'virtualCity.auction.english': string;
  'virtualCity.auction.sealed': string;
  'virtualCity.auction.takeIt': string;
  'virtualCity.auction.singleRound': string;
  'virtualCity.auction.status.pending': string;
  'virtualCity.auction.status.active': string;
  'virtualCity.auction.status.ended': string;
  'virtualCity.auction.bid': string;
  'virtualCity.auction.bidAmount': string;
  'virtualCity.auction.highest': string;
  'virtualCity.auction.highestSeat': string;
  'virtualCity.auction.ended': string;
  'virtualCity.auction.winner': string;
  'virtualCity.auction.reserve': string;
  'virtualCity.auction.endMonth': string;
  'virtualCity.auction.sealedHint': string;
  'virtualCity.auction.confirmBid': string;
  'virtualCity.auction.tooLow': string;
  'virtualCity.auction.listId': string;
  'virtualCity.auction.kind': string;
  // 信息市场
  'virtualCity.info.title': string;
  'virtualCity.info.subtitle': string;
  'virtualCity.info.sell': string;
  'virtualCity.info.bid': string;
  'virtualCity.info.reveal': string;
  'virtualCity.info.empty': string;
  'virtualCity.info.col.id': string;
  'virtualCity.info.col.category': string;
  'virtualCity.info.col.title': string;
  'virtualCity.info.col.seat': string;
  'virtualCity.info.col.minBid': string;
  'virtualCity.info.col.status': string;
  'virtualCity.info.col.actions': string;
  'virtualCity.info.category.market': string;
  'virtualCity.info.category.intel': string;
  'virtualCity.info.category.personal': string;
  'virtualCity.info.createTitle': string;
  'virtualCity.info.selCategory': string;
  'virtualCity.info.infoTitle': string;
  'virtualCity.info.infoTitlePlaceholder': string;
  'virtualCity.info.detail': string;
  'virtualCity.info.detailPlaceholder': string;
  'virtualCity.info.minBid': string;
  'virtualCity.info.confirmSell': string;
  'virtualCity.info.sealedHint': string;
  'virtualCity.info.revealTitle': string;
  'virtualCity.info.bidAmount': string;
  'virtualCity.info.confirmBid': string;
  // P2 交易错误
  'virtualCity.error.listingInvalid': string;
  'virtualCity.error.listingExpired': string;
  'virtualCity.error.negotiateNotFound': string;
  'virtualCity.error.notYourTurn': string;
  'virtualCity.error.loanRate': string;
  'virtualCity.error.loanCredit': string;
  'virtualCity.error.guarantorConflict': string;
  'virtualCity.error.auctionEnded': string;
  'virtualCity.error.bidTooLow': string;
  'virtualCity.error.noPrivilege': string;
  'virtualCity.error.listingFull': string;
  'virtualCity.error.selfTrade': string;

  // ── P1 明斯基引擎 + LPR 重定价 + 提前还款 ──
  'minsky.title': string;
  'minsky.hedge': string;
  'minsky.speculative': string;
  'minsky.ponzi': string;
  'minsky.cooldown': string; // {n}
  'minsky.moment': string;
  'minsky.ponziRatio': string;
  'minsky.threshold': string;
  'minsky.triggeredCount': string; // {n}
  'minsky.globalWarning': string; // {pct}
  'minsky.totalAlive': string; // {n}
  'minsky.unavailable': string;
  'minsky.eduToggle': string;
  'minsky.eduP1': string;
  'minsky.eduP2': string;
  'minsky.eduWarn': string;
  'lpr.5y': string;
  'lpr.5yNote': string;
  'lpr.5yHint': string;
  'lpr.reprice_notice': string; // {old} {new}
  'earlyrepay.title': string;
  'earlyrepay.trigger': string; // {rate} {yield}
  'earlyrepay.full': string;
  'earlyrepay.partial': string;
  'earlyrepay.hold': string;
  'earlyrepay.confirmFullTitle': string;
  'earlyrepay.confirmPartialTitle': string;
  'earlyrepay.loanLabel': string;
  'earlyrepay.principal': string;
  'earlyrepay.penalty': string;
  'earlyrepay.savedInterest': string;
  'earlyrepay.partialAmount': string;
  'earlyrepay.dangerTitle': string;
  'earlyrepay.dangerBody': string; // {amount}
  // ── P2 v2 财富流动可视化(2026-09-19 §P2-可视化 §13.4) ──
  'virtualCity.dashboard.structure': string;
  'virtualCity.dashboard.lorenz.title': string;
  'virtualCity.dashboard.lorenz.myPos': string;
  'virtualCity.dashboard.lorenz.myRank': string; // {rank} {total}
  'virtualCity.dashboard.lorenz.equalLine': string;
  'virtualCity.dashboard.lorenz.lorenzCurve': string;
  'virtualCity.dashboard.lorenz.giniBadge': string; // {value}
  'virtualCity.dashboard.lorenz.empty': string;
  'virtualCity.dashboard.pyramid.title': string;
  'virtualCity.dashboard.pyramid.survival': string;
  'virtualCity.dashboard.pyramid.accumulation': string;
  'virtualCity.dashboard.pyramid.freedom': string;
  'virtualCity.dashboard.pyramid.peopleCount': string; // {n}
  'virtualCity.dashboard.pyramid.virtualCityPct': string; // {pct}
  'virtualCity.dashboard.flow.title': string;
  'virtualCity.dashboard.flow.in': string;
  'virtualCity.dashboard.flow.out': string;
  'virtualCity.dashboard.flow.empty': string;
  'virtualCity.dashboard.flow.node.salary': string;
  'virtualCity.dashboard.flow.node.firms': string;
  'virtualCity.dashboard.flow.node.market': string;
  'virtualCity.dashboard.flow.node.bank': string;
  'virtualCity.dashboard.flow.node.gov': string;
  'virtualCity.dashboard.flow.node.player': string;
  'virtualCity.dashboard.flow.node.world': string;
  'virtualCity.dashboard.flow.tooltip': string; // {from} {to} {amount}

  // ── P1-4 商业保险与风险转移引擎（2026-09-19 §财商流P1-4 §10.2）──
  // insurance = InsurancePanel 侧栏 Tab；locales 拆到 virtualCityInsurance-*.ts（≤1800 行约束）。
  'virtualCity.tab.insurance': string;
  'virtualCity.insurance.title': string;
  'virtualCity.insurance.monthlyTotal': string;
  'virtualCity.insurance.kind.critical_illness': string;
  'virtualCity.insurance.kind.medical_million': string;
  'virtualCity.insurance.kind.term_life': string;
  'virtualCity.insurance.kind.accident': string;
  'virtualCity.insurance.status.active': string;
  'virtualCity.insurance.status.waiting': string; // {n}
  'virtualCity.insurance.status.grace': string;
  'virtualCity.insurance.status.lapsed': string;
  'virtualCity.insurance.coverage': string;
  'virtualCity.insurance.reimburse': string; // {pct}
  'virtualCity.insurance.annualPremium': string;
  'virtualCity.insurance.monthlyPremium': string; // {n}
  'virtualCity.insurance.paidMonths': string; // {n}
  'virtualCity.insurance.claimsTotal': string;
  'virtualCity.insurance.buy': string;
  'virtualCity.insurance.cancel': string;
  'virtualCity.insurance.cancelConfirm': string;
  'virtualCity.insurance.empty': string;
  'virtualCity.insurance.spectatorHint': string;
  'virtualCity.insurance.deathClaim': string;
  'virtualCity.insurance.quoteAtAge': string;
  // 保险错误码 35037–35041（现金不足复用 virtualCity.error.cash 35007）
  'virtualCity.error.insuranceKindInvalid': string;
  'virtualCity.error.insuranceExists': string;
  'virtualCity.error.insuranceNotFound': string;
  'virtualCity.error.insuranceAgeGate': string;
  'virtualCity.error.insuranceDisabled': string;
  // 结局 id 新增：意外身故（§5.3 HandleDeath）
  'virtualCity.ending.accident_death': string;

  // ── 城市居民人物卡档案锚定（2026-09-21 档案锚定设计 §8.4）──
  // cityProfiles = CityStatsPanel 锚定进度条 + MonthTicker 终态事件 + 建房 docs 池提示；
  // residentDrawer = ResidentProfileDrawer（搜索 / 分页列表 / 详情卡）。
  'virtualCity.cityProfiles.title': string;
  /** {n} = 已锚定居民数。 */
  'virtualCity.cityProfiles.anchorReady': string;
  /** {done} {total} = 水合进度；{pool} = 文档池卡总数。 */
  'virtualCity.cityProfiles.anchorProgress': string;
  'virtualCity.cityProfiles.anchorFailed': string;
  'virtualCity.cityProfiles.anchorIdle': string;
  /** MonthTicker city_profiles 终态行：{done} {total}。 */
  'virtualCity.cityProfiles.anchorDoneEvent': string;
  'virtualCity.cityProfiles.anchorFailedEvent': string;
  'virtualCity.cityProfiles.openDrawer': string;
  'virtualCity.residentDrawer.title': string;
  'virtualCity.residentDrawer.close': string;
  'virtualCity.residentDrawer.searchPlaceholder': string;
  'virtualCity.residentDrawer.prev': string;
  'virtualCity.residentDrawer.next': string;
  /** {page} {pages}。 */
  'virtualCity.residentDrawer.pageInfo': string;
  /** {n} = q 过滤命中总数。 */
  'virtualCity.residentDrawer.matched': string;
  'virtualCity.residentDrawer.income': string;
  'virtualCity.residentDrawer.expense': string;
  'virtualCity.residentDrawer.savings': string;
  'virtualCity.residentDrawer.employed': string;
  'virtualCity.residentDrawer.unemployed': string;
  'virtualCity.residentDrawer.stressed': string;
  'virtualCity.residentDrawer.goal': string;
  'virtualCity.residentDrawer.personality': string;
  'virtualCity.residentDrawer.openingHook': string;
  'virtualCity.residentDrawer.marital': string;
  'virtualCity.residentDrawer.healthGrade': string;
  'virtualCity.residentDrawer.sourceFile': string;
  'virtualCity.residentDrawer.empty': string;
  /** {name} = 发声居民姓名（城市之声可点击条目 tooltip/aria）。 */
  'virtualCity.residentDrawer.voiceOf': string;
  'virtualCity.residentDrawer.age': string;
  'virtualCity.residentDrawer.district': string;
  'virtualCity.residentDrawer.occupation': string;
  'virtualCity.residentDrawer.domain': string;
  'virtualCity.residentDrawer.cardId': string;
  /** focusCardId 单卡查询未命中（35013）。 */
  'virtualCity.residentDrawer.notFound': string;

  // ── 批次 20 FE-2：副业定价战 / 股票微观结构 / 市长选举（2026-09-24）──
  // 值表拆到 locales/virtualCityBatch20-{zh,en,ja}.ts（≤1800 行约束，先例 virtualCityP2-*）。
  // 副业定价（ActionPanel 副业区块 + 开业弹窗；批次 20 文档 2 §5）。
  /** 开业弹窗定价档行标签（文档未列，UI 需要，见实施报告疑点）。 */
  'virtualCity.sidePrice.label': string;
  'virtualCity.sidePrice.low': string;
  'virtualCity.sidePrice.mid': string;
  'virtualCity.sidePrice.high': string;
  /** 客群份额：{pct} 整数百分比（不含 % 号）。 */
  'virtualCity.sideShare': string;
  /** 预期收入估算：{amount} 已格式化金额。 */
  'virtualCity.sideExpected': string;
  /** 同品类对手折叠行：{n} = 对手数。 */
  'virtualCity.sideCompetitors': string;
  /** 同月限改禁用 tooltip（每月 ≤1 次）。 */
  'virtualCity.sidePriceGate': string;
  // 股票微观结构（MarketPanel + 买卖弹窗；批次 20 文档 3 B4）。
  'virtualCity.micro.buyUnit': string;   // {price}
  'virtualCity.micro.sellUnit': string;  // {price}
  'virtualCity.micro.spread': string;    // {bps}
  'virtualCity.micro.t1Locked': string;  // {n}
  'virtualCity.micro.breaker': string;   // {n} = 最后一个禁止月
  // 市长选举（建房开关 / 横幅 / 政务票型；批次 20 文档 3 A4）。
  'virtualCity.election.title': string;
  /** 建房弹窗开关文案。 */
  'virtualCity.election.switch': string;
  /** 横幅市长行：{seat} = 1-based 座位号，{name} = 「 · 昵称」或空串。 */
  'virtualCity.election.mayor': string;
  'virtualCity.election.bannerClose': string;
  /** 津贴停发徽标（数据源 = public_services.stipend_stopped）。 */
  'virtualCity.election.stipendStopped': string;
  /** CityStatsPanel「政务」分组标题。 */
  'virtualCity.election.votePanel': string;
  /** {m} = 下届选举月。 */
  'virtualCity.election.nextElection': string;
  /** 任期进度 tooltip：{elapsed}/{interval} 月。 */
  'virtualCity.election.termProgress': string;
  // 票型表格行内指标标签。
  'virtualCity.election.colScore': string;
  'virtualCity.election.colWealth': string;
  'virtualCity.election.colNetwork': string;
  'virtualCity.election.colSatisfaction': string;
}
