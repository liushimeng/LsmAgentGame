// 虚拟城市批次 20 FE-2 i18n（zh-CN）：副业定价战 / 股票微观结构 / 市长选举。
// 与 virtualCityKeys.ts 的 VirtualCityDict 批次 20 段键集合完全对齐
// （文档 2 §5 + 文档 3 A4/B4；拆出以满足 ≤1800 行约束，先例 virtualCityP2-*）。
const virtualCityBatch20 = {
  // 副业定价（ActionPanel 副业区块 + 开业弹窗档位）
  'virtualCity.sidePrice.label': '开业定价档',
  'virtualCity.sidePrice.low': '低价',
  'virtualCity.sidePrice.mid': '中价',
  'virtualCity.sidePrice.high': '高价',
  'virtualCity.sideShare': '份额 {pct}%',
  'virtualCity.sideExpected': '预期 ¥{amount}',
  'virtualCity.sideCompetitors': '同品类对手 {n}',
  'virtualCity.sidePriceGate': '本月已改过价，每月限 1 次',
  // 股票微观结构（MarketPanel 双价 / 价差 / T+1 / 熔断）
  'virtualCity.micro.buyUnit': '买 {price}',
  'virtualCity.micro.sellUnit': '卖 {price}',
  'virtualCity.micro.spread': '价差 {bps}bp',
  'virtualCity.micro.t1Locked': 'T+1 冻结 {n} 份',
  'virtualCity.micro.breaker': '熔断暂停股票交易（至第 {n} 月）',
  // 市长选举（建房开关 / 当选横幅 / 政务票型面板）
  'virtualCity.election.title': '市长选举',
  'virtualCity.election.switch': '启动市长选举（48 月一届，默认关闭）',
  'virtualCity.election.mayor': '现任市长：{seat} 号居民{name}',
  'virtualCity.election.bannerClose': '关闭',
  'virtualCity.election.stipendStopped': '市长津贴停发（国库不足）',
  'virtualCity.election.votePanel': '政务 · 票型',
  'virtualCity.election.nextElection': '下届选举：第 {m} 月',
  'virtualCity.election.termProgress': '任期 {elapsed}/{interval} 月',
  'virtualCity.election.colScore': '综合',
  'virtualCity.election.colWealth': '财富',
  'virtualCity.election.colNetwork': '人脉',
  'virtualCity.election.colSatisfaction': '满意度',
};

export default virtualCityBatch20;
