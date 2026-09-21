// 虚拟城市 P1-4 商业保险 i18n（zh-CN）— 拆自 zh-CN.ts 以满足 ≤1800 行约束。
// 与 wealthKeys.ts 的 WealthDict P1-4 段键集合完全对齐；
// 键文案出处：lag_docs/虚拟城市/已实现/10-P1保险系统/虚拟城市-P1-商业保险与风险转移引擎-v1.md §10.2。
const wealthInsurance = {
  // 侧栏 Tab
  'wealth.tab.insurance': '保险',
  // 面板（§10.2 的 22 键 + 月缴单卡展示）
  'wealth.insurance.title': '商业保险',
  'wealth.insurance.monthlyTotal': '月缴合计',
  'wealth.insurance.kind.critical_illness': '重疾险',
  'wealth.insurance.kind.medical_million': '百万医疗险',
  'wealth.insurance.kind.term_life': '定期寿险',
  'wealth.insurance.kind.accident': '意外险',
  'wealth.insurance.status.active': '有效',
  'wealth.insurance.status.waiting': '等待期剩 {n} 月',
  'wealth.insurance.status.grace': '宽限期',
  'wealth.insurance.status.lapsed': '已失效',
  'wealth.insurance.coverage': '保额',
  'wealth.insurance.reimburse': '报销 {pct}%',
  'wealth.insurance.annualPremium': '年缴',
  'wealth.insurance.monthlyPremium': '月缴 ¥{n}',
  'wealth.insurance.paidMonths': '已缴 {n} 月',
  'wealth.insurance.claimsTotal': '累计赔付',
  'wealth.insurance.buy': '立即投保',
  'wealth.insurance.cancel': '退保',
  'wealth.insurance.cancelConfirm': '消费型保险退保不退还已缴保费，确认退保？',
  'wealth.insurance.empty': '尚未投保 — 保险不产生收益，只转移风险',
  'wealth.insurance.spectatorHint': '观战模式 · 保险面板只读',
  'wealth.insurance.deathClaim': '身故理赔已计入遗产',
  'wealth.insurance.quoteAtAge': '当前年龄报价',
  // 错误码 35037–35041（现金不足复用 wealth.error.cash 35007）
  'wealth.error.insuranceKindInvalid': '险种非法（须为重疾 / 百万医疗 / 定期寿险 / 意外之一）',
  'wealth.error.insuranceExists': '该险种已有有效保单（每人每险种限 1 张）',
  'wealth.error.insuranceNotFound': '该险种没有有效保单',
  'wealth.error.insuranceAgeGate': '超过 55 岁不能再新投保',
  'wealth.error.insuranceDisabled': '保险引擎未开启',
  // 结局 id 新增：意外身故（§5.3 HandleDeath）
  'wealth.ending.accident_death': '意外身故',
};

export default wealthInsurance;
