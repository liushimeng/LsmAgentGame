// 財商流 P1-4 商業保険 i18n（ja）— ja.ts から分割（≤1800 行制約）。
// wealthKeys.ts の WealthDict P1-4 セクションと完全に対応；
// 文言出典：lag_docs/财商流游戏/已实现/10-P1保险系统/财商流游戏-P1-商业保险与风险转移引擎-v1.md §10.2。
const wealthInsurance = {
  // サイドバータブ
  'wealth.tab.insurance': '保険',
  // パネル（§10.2 の 22 キー + カードごとの月払い表示）
  'wealth.insurance.title': '商業保険',
  'wealth.insurance.monthlyTotal': '月払い合計',
  'wealth.insurance.kind.critical_illness': '重大疾病保険',
  'wealth.insurance.kind.medical_million': '百万医療保険',
  'wealth.insurance.kind.term_life': '定期生命保険',
  'wealth.insurance.kind.accident': '傷害保険',
  'wealth.insurance.status.active': '有効',
  'wealth.insurance.status.waiting': '待機期間 残り {n} ヶ月',
  'wealth.insurance.status.grace': '猶予期間',
  'wealth.insurance.status.lapsed': '失効',
  'wealth.insurance.coverage': '保障額',
  'wealth.insurance.reimburse': '{pct}% 給付',
  'wealth.insurance.annualPremium': '年払い',
  'wealth.insurance.monthlyPremium': '月払い ¥{n}',
  'wealth.insurance.paidMonths': '支払済み {n} ヶ月',
  'wealth.insurance.claimsTotal': '累計保険金',
  'wealth.insurance.buy': 'すぐ加入',
  'wealth.insurance.cancel': '解約',
  'wealth.insurance.cancelConfirm': '掛け捨て保険の解約は払込済み保険料の返金がありません。解約しますか？',
  'wealth.insurance.empty': '未加入 — 保険は収益を生まず、リスクを移転するだけです',
  'wealth.insurance.spectatorHint': '観戦モード · 保険パネルは読み取り専用',
  'wealth.insurance.deathClaim': '死亡保険金は遺産に計上されました',
  'wealth.insurance.quoteAtAge': '現在の年齢での見積り',
  // エラーコード 35037–35041（現金不足は wealth.error.cash 35007 を流用）
  'wealth.error.insuranceKindInvalid': '保険種別が不正です',
  'wealth.error.insuranceExists': 'この種別には既に有効な契約があります',
  'wealth.error.insuranceNotFound': 'この種別の有効な契約がありません',
  'wealth.error.insuranceAgeGate': '55 歳を超えると新規加入はできません',
  'wealth.error.insuranceDisabled': '保険エンジンは無効です',
  // 新規エンディング ID：事故死（§5.3 HandleDeath）
  'wealth.ending.accident_death': '事故死',
};

export default wealthInsurance;
