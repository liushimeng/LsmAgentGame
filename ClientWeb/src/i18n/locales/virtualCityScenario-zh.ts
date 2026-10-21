// 虚拟城市 2026-10-21 §城市模拟定位 — 场景推演台 / 条件触发者（zh-CN）。
// 拆成独立文件：zh-CN.ts 逼近 §4 的 1800 行硬上限。
// 键文案出处：tmpPlan/虚拟城市-模拟程序化与预测运行优化_20261021_095114.md §5.1/§5.3。
const virtualCityScenarioZH = {
  'virtualCity.scenario.open': '场景推演台',
  'virtualCity.scenario.title': '场景推演台',
  'virtualCity.scenario.subtitle': '条件触发者 · 向运行中的城市模拟注入场景条件，观察连锁反应与推演结果',
  'virtualCity.scenario.close': '关闭',
  // 场景分组（后端 group 字段 → 展示名；未知分组原样显示）
  'virtualCity.scenario.group.market': '市场与经济',
  'virtualCity.scenario.group.society': '社会与舆情',
  'virtualCity.scenario.group.nature': '自然灾害',
  'virtualCity.scenario.group.unknown': '未知灾难',
  'virtualCity.scenario.group.other': '其他场景',
  'virtualCity.scenario.magnitude': '强度',
  'virtualCity.scenario.magnitudeLevel': '{n} 级',
  'virtualCity.scenario.duration': '持续月数',
  'virtualCity.scenario.durationPlaceholder': '可选，例如 6',
  'virtualCity.scenario.target': '作用目标',
  'virtualCity.scenario.targetPlaceholder': '可选，例如某类商品 / 行业',
  'virtualCity.scenario.targetHintGoods': '可选品类：food / clothing / housing / household / transport / education / healthcare / misc（留空 = 全部）',
  'virtualCity.scenario.targetHintStance': '可选：tight（收紧）/ loose（宽松）（留空 = tight）',
  'virtualCity.scenario.note': '备注',
  'virtualCity.scenario.notePlaceholder': '可选，记录本次推演目的',
  'virtualCity.scenario.trigger': '触发场景',
  'virtualCity.scenario.confirmBody': '确认向当前模拟注入「{name}」（强度 {level}）？注入不可撤销，连锁反应将在后续月度演化中逐步体现。',
  'virtualCity.scenario.effects': '影响效果',
  'virtualCity.scenario.effectsNote': '以下为注入瞬间的指标变化，连锁反应将在后续月份持续演化。',
  'virtualCity.scenario.effectsEmpty': '本次注入未产生可观测的指标变化。',
  'virtualCity.scenario.history': '最近场景事件',
  'virtualCity.scenario.historyEmpty': '本模拟周期尚未注入场景条件。',
  'virtualCity.scenario.loading': '正在加载场景目录…',
  'virtualCity.scenario.catalogEmpty': '暂无可注入的场景目录。',
  'virtualCity.scenario.loadFailed': '场景目录加载失败：{msg}',
  'virtualCity.scenario.historyFailed': '场景记录加载失败：{msg}',
  'virtualCity.scenario.applyFailed': '场景注入失败：{msg}',
  'virtualCity.scenario.notPlaying': '模拟尚未运行，暂不能注入场景条件。',
  'virtualCity.scenario.spectatorHint': '任意登录用户均可作为条件触发者注入场景；注入记录对整个房间可见。',
  'virtualCity.scenario.appliedAt': '第 {m} 月注入',
  'virtualCity.scenario.triggered': '已注入「{name}」',
};

export default virtualCityScenarioZH;
