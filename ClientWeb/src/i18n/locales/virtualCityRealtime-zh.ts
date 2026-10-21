// 虚拟城市 批次65 City-Chrome 实时数据通道 + 实时看板 i18n（zh-CN）。
// 拆成独立文件：zh-CN.ts 逼近 §4 的 1800 行硬上限。
// 键文案出处：lag_docs/虚拟城市/已实现/65-CityChrome实时数据通道与实时看板/01-方案设计.md §5。
const virtualCityRealtimeZH = {
  // ── 侧栏 Tab ──
  'virtualCity.rt.tab': '实时',
  // ── 通道状态徽章 ──
  'virtualCity.rt.channel.online': '数据通道在线',
  'virtualCity.rt.channel.offline': '数据通道离线',
  'virtualCity.rt.channel.updating': '采集中',
  // ── 「更新于」时间差（just 无 {n} 占位）──
  'virtualCity.rt.updated.just': '刚刚更新',
  'virtualCity.rt.updated.min': '{n} 分钟前更新',
  'virtualCity.rt.updated.hour': '{n} 小时前更新',
  'virtualCity.rt.updated.day': '{n} 天前更新',
  'virtualCity.rt.refresh': '刷新',
  'virtualCity.rt.refreshFail': '刷新失败：{msg}',
  // ── 子 Tab ──
  'virtualCity.rt.sub.overview': '总览',
  'virtualCity.rt.sub.a_share': 'A股',
  'virtualCity.rt.sub.hk': '港股',
  'virtualCity.rt.sub.us': '美股',
  'virtualCity.rt.sub.metal': '贵金属',
  'virtualCity.rt.sub.futures': '期货',
  'virtualCity.rt.sub.crypto': '比特币',
  'virtualCity.rt.sub.news': '新闻',
  // ── 报价表 ──
  'virtualCity.rt.col.name': '名称',
  'virtualCity.rt.col.price': '价格',
  'virtualCity.rt.col.change': '涨跌幅',
  'virtualCity.rt.col.trend': '走势',
  'virtualCity.rt.source': '来源',
  'virtualCity.rt.collecting': '采集中',
  'virtualCity.rt.candleAlt': '5 分钟蜡烛图（自采价点聚合）',
  // ── 总览 / 空态 ──
  'virtualCity.rt.headline': '最新头条',
  'virtualCity.rt.empty.feed': '暂无实时数据：点击右上角刷新发起采集',
  'virtualCity.rt.empty.category': '该类目暂无数据',
  'virtualCity.rt.empty.news': '暂无新闻',
  // ── 批次 66 外盘情绪锚 ──
  'virtualCity.rt.exo.title': '外盘情绪锚',
  'virtualCity.rt.exo.sentimentUp': '偏多',
  'virtualCity.rt.exo.sentimentDown': '偏空',
  'virtualCity.rt.exo.sentimentFlat': '中性',
  'virtualCity.rt.exo.stockBias': '股票漂移',
  'virtualCity.rt.exo.goldBias': '黄金漂移',
  'virtualCity.rt.exo.inactive': '外盘锚未激活',
  'virtualCity.rt.exo.updated': '数据更新于 {n} 分钟前',
  'virtualCity.rt.exo.effectHint': '对城市模拟影响',
};

export default virtualCityRealtimeZH;
