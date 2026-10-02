// 虚擬都市 バッチ52 世代間富の移転エンジン i18n（ja）— ja.ts から分割（≤1800 行制約）。
// virtualCityKeys.ts の VirtualCityDict バッチ52 段のキー集合と完全一致。
const virtualCityFamily = {
  // ── FamilyPanel 3 セクション布局 ──
  'virtualCity.family.title': '家庭 / 世代間富',
  'virtualCity.family.empty': '家庭データなし',
  'virtualCity.family.spectatorHint': '観戦モード · 家庭パネルは読み取り専用',
  'virtualCity.family.parents': '両親',
  'virtualCity.family.parentsAlive': '存命',
  'virtualCity.family.parentsGone': '他界',
  'virtualCity.family.parentsAge': '{age} 歳',
  'virtualCity.family.health.good': '健康良好',
  'virtualCity.family.health.fair': '健康普通',
  'virtualCity.family.health.poor': '健康不良',
  'virtualCity.family.kids': '子供',
  'virtualCity.family.noKids': '子供なし',
  'virtualCity.family.childrenCount': '子供 {n} 人',
  'virtualCity.family.kidEdu.public': '公立',
  'virtualCity.family.kidEdu.private': '私立',
  // 今月のキャッシュフロー 3 行
  'virtualCity.family.monthlySupport': '今月の仕送り',
  'virtualCity.family.monthlyEdu': '今月の教育費',
  'virtualCity.family.monthlyChildIn': '今月の子からの送金',
  // 折りたたみ累計
  'virtualCity.family.totalsTitle': '累計',
  'virtualCity.family.totalSupport': '仕送り累計',
  'virtualCity.family.totalEdu': '教育費累計',
  'virtualCity.family.totalChildIn': '子送金累計',
  // 操作エリア
  'virtualCity.family.upgradeEduBtn': '教育アップグレード（{age} 歳 → 私立 ¥{price}）',
  'virtualCity.family.eduUnaffordable': '現金不足のため私立教育へ升级できません（¥200,000 必要）',
  'virtualCity.family.paySupportLabel': '追加仕送り',
  'virtualCity.family.paySupportAmount': '金額（元）',
  'virtualCity.family.paySupportBtn': '仕送りする',
  'virtualCity.family.paySupportCap': '上限 ¥{n}（純資産の 30%）',
  'virtualCity.family.paySupportInvalid': '0 を超える仕送り金額を入力してください',
  // 遺産分配プレビュー
  'virtualCity.family.previewBtn': '遺産分配プレビュー',
  'virtualCity.family.previewTitle': '遺産分配プレビュー',
  'virtualCity.family.previewEmpty': '遺産は 0（葬儀費用控除後残なし）',
  'virtualCity.family.previewEstate': '分配可能遺産',
  'virtualCity.family.previewFuneral': '葬儀費用（控除済み）',
  'virtualCity.family.previewSpouse': '配偶者へ',
  'virtualCity.family.previewPerChild': '子供 1 人あたり（{n} 人）',
  'virtualCity.family.previewWorld': '相続人なし · 国庫帰属',
  'virtualCity.family.previewNote': 'プレビューは現時点の資産・負債で試算するもので、会計は確定しません。実際の分配は死亡月の清算時に行われます。',
  // ── ゲーム終了 ──
  'virtualCity.gameOver.familyScore': '世代間貢献スコア',
  'virtualCity.gameOver.familyCol': '世代間',
};

export default virtualCityFamily;
