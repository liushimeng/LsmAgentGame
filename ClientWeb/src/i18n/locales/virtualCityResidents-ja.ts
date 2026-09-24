// 仮想都市住民人物カードプロファイル固定 i18n（ja）— ≤1800 行制約により ja.ts から分割。
// virtualCityResidents-zh.ts と同一キー集合（プロファイル固定設計 §8.4）。
const virtualCityResidents = {
  // CityStatsPanel 固定進捗 + MonthTicker 終端イベント + ドロワー共用
  'virtualCity.cityProfiles.title': '都市住民プロファイル',
  'virtualCity.cityProfiles.anchorReady': '実プロファイル {n} 件を固定しました',
  'virtualCity.cityProfiles.anchorProgress': 'プロファイル固定中 {done}/{total}（プール {pool} 枚）',
  'virtualCity.cityProfiles.anchorFailed': 'プロファイル固定に失敗 — 合成データを表示中',
  'virtualCity.cityProfiles.anchorIdle': 'この対局ではプロファイル固定は無効（精選デッキ）',
  'virtualCity.cityProfiles.anchorDoneEvent': '都市人物プロファイルの固定が完了 {done}/{total}',
  'virtualCity.cityProfiles.anchorFailedEvent': '都市人物プロファイルの固定に失敗 — 合成データにフォールバック',
  'virtualCity.cityProfiles.openDrawer': '住民プロファイル',
  // ResidentProfileDrawer
  'virtualCity.residentDrawer.title': '住民プロファイル',
  'virtualCity.residentDrawer.close': 'ドロワーを閉じる',
  'virtualCity.residentDrawer.searchPlaceholder': '氏名 / 職業 / カード番号で検索',
  'virtualCity.residentDrawer.prev': '前へ',
  'virtualCity.residentDrawer.next': '次へ',
  'virtualCity.residentDrawer.pageInfo': '{page} / {pages} ページ',
  'virtualCity.residentDrawer.matched': '{n} 人の住民が該当',
  'virtualCity.residentDrawer.income': '月収入',
  'virtualCity.residentDrawer.expense': '月支出',
  'virtualCity.residentDrawer.savings': '貯蓄',
  'virtualCity.residentDrawer.employed': '就業',
  'virtualCity.residentDrawer.unemployed': '失業',
  'virtualCity.residentDrawer.stressed': '貯蓄不足',
  'virtualCity.residentDrawer.goal': '5 年目標',
  'virtualCity.residentDrawer.personality': '性格特性',
  'virtualCity.residentDrawer.openingHook': 'プロファイル導入',
  'virtualCity.residentDrawer.marital': '婚姻',
  'virtualCity.residentDrawer.healthGrade': '健康ランク',
  'virtualCity.residentDrawer.sourceFile': 'ソースファイル',
  'virtualCity.residentDrawer.empty': '該当する住民がいません',
  'virtualCity.residentDrawer.voiceOf': '「{name}」のプロファイルを見る',
  'virtualCity.residentDrawer.age': '年齢',
  'virtualCity.residentDrawer.district': '区',
  'virtualCity.residentDrawer.occupation': '職業',
  'virtualCity.residentDrawer.domain': '業種',
  'virtualCity.residentDrawer.cardId': 'カード番号',
  'virtualCity.residentDrawer.notFound': '該当する住民プロファイルが見つかりません',
};

export default virtualCityResidents;
