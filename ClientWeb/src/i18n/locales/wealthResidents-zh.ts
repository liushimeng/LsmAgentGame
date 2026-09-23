// 虚拟城市居民人物卡档案锚定 i18n（zh-CN）— 拆自 zh-CN.ts 以满足 ≤1800 行约束。
// 与 wealthKeys.ts 的 WealthDict cityProfiles/residentDrawer 段键集合完全对齐
// （档案锚定设计 §8.4）。
const wealthResidents = {
  // CityStatsPanel 锚定进度条 + MonthTicker 终态事件 + 抽屉复用
  'wealth.cityProfiles.title': '城市人物档案',
  'wealth.cityProfiles.anchorReady': '已锚定 {n} 份真实档案',
  'wealth.cityProfiles.anchorProgress': '人物档案锚定中 {done}/{total}（卡池 {pool} 张）',
  'wealth.cityProfiles.anchorFailed': '档案锚定失败，当前展示合成数据',
  'wealth.cityProfiles.anchorIdle': '本局未启用人物档案锚定（精选手卡模式）',
  'wealth.cityProfiles.anchorDoneEvent': '城市人物档案锚定完成 {done}/{total}',
  'wealth.cityProfiles.anchorFailedEvent': '城市人物档案锚定失败，已回退合成数据',
  'wealth.cityProfiles.openDrawer': '居民档案',
  // ResidentProfileDrawer 抽屉
  'wealth.residentDrawer.title': '居民档案',
  'wealth.residentDrawer.close': '关闭抽屉',
  'wealth.residentDrawer.searchPlaceholder': '搜索姓名 / 职业 / 卡号',
  'wealth.residentDrawer.prev': '上一页',
  'wealth.residentDrawer.next': '下一页',
  'wealth.residentDrawer.pageInfo': '{page} / {pages} 页',
  'wealth.residentDrawer.matched': '命中 {n} 位居民',
  'wealth.residentDrawer.income': '月收入',
  'wealth.residentDrawer.expense': '月支出',
  'wealth.residentDrawer.savings': '储蓄',
  'wealth.residentDrawer.employed': '就业',
  'wealth.residentDrawer.unemployed': '失业',
  'wealth.residentDrawer.stressed': '储蓄告急',
  'wealth.residentDrawer.goal': '5 年目标',
  'wealth.residentDrawer.personality': '人格特质',
  'wealth.residentDrawer.openingHook': '档案开场',
  'wealth.residentDrawer.marital': '婚姻',
  'wealth.residentDrawer.healthGrade': '健康档',
  'wealth.residentDrawer.sourceFile': '档案来源',
  'wealth.residentDrawer.empty': '没有匹配的居民',
  'wealth.residentDrawer.voiceOf': '查看「{name}」的档案',
  'wealth.residentDrawer.age': '年龄',
  'wealth.residentDrawer.district': '城区',
  'wealth.residentDrawer.occupation': '职业',
  'wealth.residentDrawer.domain': '行业',
  'wealth.residentDrawer.cardId': '卡号',
  'wealth.residentDrawer.notFound': '未找到该居民档案',
};

export default wealthResidents;
