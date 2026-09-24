// 虚拟城市批次 20 FE-2 i18n（zh-CN）：副业定价战 / 股票微观结构 / 市长选举。
// 与 wealthKeys.ts 的 WealthDict 批次 20 段键集合完全对齐
// （文档 2 §5 + 文档 3 A4/B4；拆出以满足 ≤1800 行约束，先例 wealthP2-*）。
const wealthBatch20 = {
  // 副业定价（ActionPanel 副业区块 + 开业弹窗档位）
  'wealth.sidePrice.label': '开业定价档',
  'wealth.sidePrice.low': '低价',
  'wealth.sidePrice.mid': '中价',
  'wealth.sidePrice.high': '高价',
  'wealth.sideShare': '份额 {pct}%',
  'wealth.sideExpected': '预期 ¥{amount}',
  'wealth.sideCompetitors': '同品类对手 {n}',
  'wealth.sidePriceGate': '本月已改过价，每月限 1 次',
  // 股票微观结构（MarketPanel 双价 / 价差 / T+1 / 熔断）
  'wealth.micro.buyUnit': '买 {price}',
  'wealth.micro.sellUnit': '卖 {price}',
  'wealth.micro.spread': '价差 {bps}bp',
  'wealth.micro.t1Locked': 'T+1 冻结 {n} 份',
  'wealth.micro.breaker': '熔断暂停股票交易（至第 {n} 月）',
  // 市长选举（建房开关 / 当选横幅 / 政务票型面板）
  'wealth.election.title': '市长选举',
  'wealth.election.switch': '启动市长选举（48 月一届，默认关闭）',
  'wealth.election.mayor': '现任市长：{seat} 号居民{name}',
  'wealth.election.bannerClose': '关闭',
  'wealth.election.stipendStopped': '市长津贴停发（国库不足）',
  'wealth.election.votePanel': '政务 · 票型',
  'wealth.election.nextElection': '下届选举：第 {m} 月',
  'wealth.election.termProgress': '任期 {elapsed}/{interval} 月',
  'wealth.election.colScore': '综合',
  'wealth.election.colWealth': '财富',
  'wealth.election.colNetwork': '人脉',
  'wealth.election.colSatisfaction': '满意度',
};

export default wealthBatch20;
