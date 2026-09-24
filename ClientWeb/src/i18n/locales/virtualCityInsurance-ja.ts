// 仮想都市 P1-4 商業保険 i18n（ja）— ja.ts から分割（≤1800 行制約）。
// virtualCityKeys.ts の VirtualCityDict P1-4 セクションと完全に対応；
// 文言出典：lag_docs/虚拟城市/已实现/10-P1保险系统/虚拟城市-P1-商业保险与风险转移引擎-v1.md §10.2。
const virtualCityInsurance = {
  // サイドバータブ
  'virtualCity.tab.insurance': '保険',
  // パネル（§10.2 の 22 キー + カードごとの月払い表示）
  'virtualCity.insurance.title': '商業保険',
  'virtualCity.insurance.monthlyTotal': '月払い合計',
  'virtualCity.insurance.kind.critical_illness': '重大疾病保険',
  'virtualCity.insurance.kind.medical_million': '百万医療保険',
  'virtualCity.insurance.kind.term_life': '定期生命保険',
  'virtualCity.insurance.kind.accident': '傷害保険',
  'virtualCity.insurance.status.active': '有効',
  'virtualCity.insurance.status.waiting': '待機期間 残り {n} ヶ月',
  'virtualCity.insurance.status.grace': '猶予期間',
  'virtualCity.insurance.status.lapsed': '失効',
  'virtualCity.insurance.coverage': '保障額',
  'virtualCity.insurance.reimburse': '{pct}% 給付',
  'virtualCity.insurance.annualPremium': '年払い',
  'virtualCity.insurance.monthlyPremium': '月払い ¥{n}',
  'virtualCity.insurance.paidMonths': '支払済み {n} ヶ月',
  'virtualCity.insurance.claimsTotal': '累計保険金',
  'virtualCity.insurance.buy': 'すぐ加入',
  'virtualCity.insurance.cancel': '解約',
  'virtualCity.insurance.cancelConfirm': '掛け捨て保険の解約は払込済み保険料の返金がありません。解約しますか？',
  'virtualCity.insurance.empty': '未加入 — 保険は収益を生まず、リスクを移転するだけです',
  'virtualCity.insurance.spectatorHint': '観戦モード · 保険パネルは読み取り専用',
  'virtualCity.insurance.deathClaim': '死亡保険金は遺産に計上されました',
  'virtualCity.insurance.quoteAtAge': '現在の年齢での見積り',
  // エラーコード 35037–35041（現金不足は virtualCity.error.cash 35007 を流用）
  'virtualCity.error.insuranceKindInvalid': '保険種別が不正です',
  'virtualCity.error.insuranceExists': 'この種別には既に有効な契約があります',
  'virtualCity.error.insuranceNotFound': 'この種別の有効な契約がありません',
  'virtualCity.error.insuranceAgeGate': '55 歳を超えると新規加入はできません',
  'virtualCity.error.insuranceDisabled': '保険エンジンは無効です',
  // 新規エンディング ID：事故死（§5.3 HandleDeath）
  'virtualCity.ending.accident_death': '事故死',
};

export default virtualCityInsurance;
