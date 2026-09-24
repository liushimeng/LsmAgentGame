// 虚拟城市 P1-4 商业保险 i18n（zh-CN）— 拆自 zh-CN.ts 以满足 ≤1800 行约束。
// 与 virtualCityKeys.ts 的 VirtualCityDict P1-4 段键集合完全对齐；
// 键文案出处：lag_docs/虚拟城市/已实现/10-P1保险系统/虚拟城市-P1-商业保险与风险转移引擎-v1.md §10.2。
const virtualCityInsurance = {
  // 侧栏 Tab
  'virtualCity.tab.insurance': '保险',
  // 面板（§10.2 的 22 键 + 月缴单卡展示）
  'virtualCity.insurance.title': '商业保险',
  'virtualCity.insurance.monthlyTotal': '月缴合计',
  'virtualCity.insurance.kind.critical_illness': '重疾险',
  'virtualCity.insurance.kind.medical_million': '百万医疗险',
  'virtualCity.insurance.kind.term_life': '定期寿险',
  'virtualCity.insurance.kind.accident': '意外险',
  'virtualCity.insurance.status.active': '有效',
  'virtualCity.insurance.status.waiting': '等待期剩 {n} 月',
  'virtualCity.insurance.status.grace': '宽限期',
  'virtualCity.insurance.status.lapsed': '已失效',
  'virtualCity.insurance.coverage': '保额',
  'virtualCity.insurance.reimburse': '报销 {pct}%',
  'virtualCity.insurance.annualPremium': '年缴',
  'virtualCity.insurance.monthlyPremium': '月缴 ¥{n}',
  'virtualCity.insurance.paidMonths': '已缴 {n} 月',
  'virtualCity.insurance.claimsTotal': '累计赔付',
  'virtualCity.insurance.buy': '立即投保',
  'virtualCity.insurance.cancel': '退保',
  'virtualCity.insurance.cancelConfirm': '消费型保险退保不退还已缴保费，确认退保？',
  'virtualCity.insurance.empty': '尚未投保 — 保险不产生收益，只转移风险',
  'virtualCity.insurance.spectatorHint': '观战模式 · 保险面板只读',
  'virtualCity.insurance.deathClaim': '身故理赔已计入遗产',
  'virtualCity.insurance.quoteAtAge': '当前年龄报价',
  // 错误码 35037–35041（现金不足复用 virtualCity.error.cash 35007）
  'virtualCity.error.insuranceKindInvalid': '险种非法（须为重疾 / 百万医疗 / 定期寿险 / 意外之一）',
  'virtualCity.error.insuranceExists': '该险种已有有效保单（每人每险种限 1 张）',
  'virtualCity.error.insuranceNotFound': '该险种没有有效保单',
  'virtualCity.error.insuranceAgeGate': '超过 55 岁不能再新投保',
  'virtualCity.error.insuranceDisabled': '保险引擎未开启',
  // 结局 id 新增：意外身故（§5.3 HandleDeath）
  'virtualCity.ending.accident_death': '意外身故',
};

export default virtualCityInsurance;
