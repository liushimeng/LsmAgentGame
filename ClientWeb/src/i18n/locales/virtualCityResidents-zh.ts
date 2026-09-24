// 虚拟城市居民人物卡档案锚定 i18n（zh-CN）— 拆自 zh-CN.ts 以满足 ≤1800 行约束。
// 与 virtualCityKeys.ts 的 VirtualCityDict cityProfiles/residentDrawer 段键集合完全对齐
// （档案锚定设计 §8.4）。
const virtualCityResidents = {
  // CityStatsPanel 锚定进度条 + MonthTicker 终态事件 + 抽屉复用
  'virtualCity.cityProfiles.title': '城市人物档案',
  'virtualCity.cityProfiles.anchorReady': '已锚定 {n} 份真实档案',
  'virtualCity.cityProfiles.anchorProgress': '人物档案锚定中 {done}/{total}（卡池 {pool} 张）',
  'virtualCity.cityProfiles.anchorFailed': '档案锚定失败，当前展示合成数据',
  'virtualCity.cityProfiles.anchorIdle': '本局未启用人物档案锚定（精选手卡模式）',
  'virtualCity.cityProfiles.anchorDoneEvent': '城市人物档案锚定完成 {done}/{total}',
  'virtualCity.cityProfiles.anchorFailedEvent': '城市人物档案锚定失败，已回退合成数据',
  'virtualCity.cityProfiles.openDrawer': '居民档案',
  // ResidentProfileDrawer 抽屉
  'virtualCity.residentDrawer.title': '居民档案',
  'virtualCity.residentDrawer.close': '关闭抽屉',
  'virtualCity.residentDrawer.searchPlaceholder': '搜索姓名 / 职业 / 卡号',
  'virtualCity.residentDrawer.prev': '上一页',
  'virtualCity.residentDrawer.next': '下一页',
  'virtualCity.residentDrawer.pageInfo': '{page} / {pages} 页',
  'virtualCity.residentDrawer.matched': '命中 {n} 位居民',
  'virtualCity.residentDrawer.income': '月收入',
  'virtualCity.residentDrawer.expense': '月支出',
  'virtualCity.residentDrawer.savings': '储蓄',
  'virtualCity.residentDrawer.employed': '就业',
  'virtualCity.residentDrawer.unemployed': '失业',
  'virtualCity.residentDrawer.stressed': '储蓄告急',
  'virtualCity.residentDrawer.goal': '5 年目标',
  'virtualCity.residentDrawer.personality': '人格特质',
  'virtualCity.residentDrawer.openingHook': '档案开场',
  'virtualCity.residentDrawer.marital': '婚姻',
  'virtualCity.residentDrawer.healthGrade': '健康档',
  'virtualCity.residentDrawer.sourceFile': '档案来源',
  'virtualCity.residentDrawer.empty': '没有匹配的居民',
  'virtualCity.residentDrawer.voiceOf': '查看「{name}」的档案',
  'virtualCity.residentDrawer.age': '年龄',
  'virtualCity.residentDrawer.district': '城区',
  'virtualCity.residentDrawer.occupation': '职业',
  'virtualCity.residentDrawer.domain': '行业',
  'virtualCity.residentDrawer.cardId': '卡号',
  'virtualCity.residentDrawer.notFound': '未找到该居民档案',
};

export default virtualCityResidents;
