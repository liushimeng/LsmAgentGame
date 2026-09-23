// 仮想都市住民人物カードプロファイル固定 i18n（ja）— ≤1800 行制約により ja.ts から分割。
// wealthResidents-zh.ts と同一キー集合（プロファイル固定設計 §8.4）。
const wealthResidents = {
  // CityStatsPanel 固定進捗 + MonthTicker 終端イベント + ドロワー共用
  'wealth.cityProfiles.title': '都市住民プロファイル',
  'wealth.cityProfiles.anchorReady': '実プロファイル {n} 件を固定しました',
  'wealth.cityProfiles.anchorProgress': 'プロファイル固定中 {done}/{total}（プール {pool} 枚）',
  'wealth.cityProfiles.anchorFailed': 'プロファイル固定に失敗 — 合成データを表示中',
  'wealth.cityProfiles.anchorIdle': 'この対局ではプロファイル固定は無効（精選デッキ）',
  'wealth.cityProfiles.anchorDoneEvent': '都市人物プロファイルの固定が完了 {done}/{total}',
  'wealth.cityProfiles.anchorFailedEvent': '都市人物プロファイルの固定に失敗 — 合成データにフォールバック',
  'wealth.cityProfiles.openDrawer': '住民プロファイル',
  // ResidentProfileDrawer
  'wealth.residentDrawer.title': '住民プロファイル',
  'wealth.residentDrawer.close': 'ドロワーを閉じる',
  'wealth.residentDrawer.searchPlaceholder': '氏名 / 職業 / カード番号で検索',
  'wealth.residentDrawer.prev': '前へ',
  'wealth.residentDrawer.next': '次へ',
  'wealth.residentDrawer.pageInfo': '{page} / {pages} ページ',
  'wealth.residentDrawer.matched': '{n} 人の住民が該当',
  'wealth.residentDrawer.income': '月収入',
  'wealth.residentDrawer.expense': '月支出',
  'wealth.residentDrawer.savings': '貯蓄',
  'wealth.residentDrawer.employed': '就業',
  'wealth.residentDrawer.unemployed': '失業',
  'wealth.residentDrawer.stressed': '貯蓄不足',
  'wealth.residentDrawer.goal': '5 年目標',
  'wealth.residentDrawer.personality': '性格特性',
  'wealth.residentDrawer.openingHook': 'プロファイル導入',
  'wealth.residentDrawer.marital': '婚姻',
  'wealth.residentDrawer.healthGrade': '健康ランク',
  'wealth.residentDrawer.sourceFile': 'ソースファイル',
  'wealth.residentDrawer.empty': '該当する住民がいません',
  'wealth.residentDrawer.voiceOf': '「{name}」のプロファイルを見る',
  'wealth.residentDrawer.age': '年齢',
  'wealth.residentDrawer.district': '区',
  'wealth.residentDrawer.occupation': '職業',
  'wealth.residentDrawer.domain': '業種',
  'wealth.residentDrawer.cardId': 'カード番号',
  'wealth.residentDrawer.notFound': '該当する住民プロファイルが見つかりません',
};

export default wealthResidents;
