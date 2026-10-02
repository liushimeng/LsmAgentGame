// 虚拟城市 批次52 代际财富转移引擎 i18n（zh-CN）— 拆自 zh-CN.ts 以满足 ≤1800 行约束。
// 与 virtualCityKeys.ts 的 VirtualCityDict 批次52 段键集合完全对齐；
// 键文案出处：lag_docs/虚拟城市/已实现/52-代际财富转移引擎/虚拟城市-批次52-代际财富转移引擎-实施设计-v1.md §8.3。
const virtualCityFamily = {
  // ── FamilyPanel 三段布局 ──
  'virtualCity.family.title': '家庭 / 代际财富',
  'virtualCity.family.empty': '暂无家庭数据',
  'virtualCity.family.spectatorHint': '观战模式 · 家庭面板只读',
  'virtualCity.family.parents': '父母',
  'virtualCity.family.parentsAlive': '在世',
  'virtualCity.family.parentsGone': '已故',
  'virtualCity.family.parentsAge': '{age} 岁',
  'virtualCity.family.health.good': '健康良好',
  'virtualCity.family.health.fair': '健康一般',
  'virtualCity.family.health.poor': '健康欠佳',
  'virtualCity.family.kids': '子女',
  'virtualCity.family.noKids': '尚无子女',
  'virtualCity.family.childrenCount': '{n} 名子女',
  'virtualCity.family.kidEdu.public': '公立',
  'virtualCity.family.kidEdu.private': '私立',
  // 本月现金流三行
  'virtualCity.family.monthlySupport': '本月赡养',
  'virtualCity.family.monthlyEdu': '本月教育',
  'virtualCity.family.monthlyChildIn': '本月回流',
  // 累计折叠段
  'virtualCity.family.totalsTitle': '累计',
  'virtualCity.family.totalSupport': '累计赡养',
  'virtualCity.family.totalEdu': '累计教育',
  'virtualCity.family.totalChildIn': '累计回流',
  // 操作区
  'virtualCity.family.upgradeEduBtn': '教育升级（{age} 岁 → 私立 ¥{price}）',
  'virtualCity.family.eduUnaffordable': '现金不足，无法升级私立教育（需 ¥200,000）',
  'virtualCity.family.paySupportLabel': '自愿加赡养',
  'virtualCity.family.paySupportAmount': '金额（元）',
  'virtualCity.family.paySupportBtn': '加赡养',
  'virtualCity.family.paySupportCap': '上限 ¥{n}（净资产 30%）',
  'virtualCity.family.paySupportInvalid': '请输入大于 0 的赡养金额',
  // 遗产分配预览
  'virtualCity.family.previewBtn': '遗产分配预览',
  'virtualCity.family.previewTitle': '遗产分配预览',
  'virtualCity.family.previewEmpty': '遗产为 0（扣除丧葬费后无剩余）',
  'virtualCity.family.previewEstate': '可分配遗产',
  'virtualCity.family.previewFuneral': '丧葬费（已扣）',
  'virtualCity.family.previewSpouse': '配偶继承',
  'virtualCity.family.previewPerChild': '每名子女（{n} 名）',
  'virtualCity.family.previewWorld': '无继承人 · 充公',
  'virtualCity.family.previewNote': '预演按当前资产负债实时计算，不落账；实际分配以身故当月清算为准。',
  // ── 终局（GameOverModal）──
  'virtualCity.gameOver.familyScore': '代际贡献分',
  'virtualCity.gameOver.familyCol': '代际',
};

export default virtualCityFamily;
