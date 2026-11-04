// 仮想都市 バッチ81「システム対話」i18n（ja）。
// ≤1800 行制約により ja.ts から分割。
// virtualCitySystem-zh.ts と同一キー集合（3言語同期、§12）。
const virtualCitySystemJA = {
  'virtualCity.systemDialog.title': 'システム対話',
  'virtualCity.systemDialog.subtitle': '都市システムのメインエージェントと対話 — 自然言語で状態確認・シナリオ注入・シミュレーションとカメラ制御',
  'virtualCity.systemDialog.open': 'システム対話',
  'virtualCity.systemDialog.close': '閉じる',
  'virtualCity.systemDialog.minimize': '最小化',
  'virtualCity.systemDialog.restore': '対話を復元',
  'virtualCity.systemDialog.inputPlaceholder': 'コマンドを入力（例：都市状態を表示 / マグニチュード3の地震を注入）…（Enter で送信、Shift+Enter で改行）',
  'virtualCity.systemDialog.send': '送信',
  'virtualCity.systemDialog.clear': 'クリア',
  'virtualCity.systemDialog.busy': 'システム処理中…',
  'virtualCity.systemDialog.emptyTitle': '都市システムとの対話を始めましょう',
  'virtualCity.systemDialog.emptyHint': '例：都市状態を表示 / マグニチュード3の地震を注入 / カメラを市中心部へ移動',
  'virtualCity.systemDialog.errorPrefix': 'システムエラー：',
  'virtualCity.systemDialog.toolPrefix': 'ツール',
  'virtualCity.systemDialog.showCommandBadge': 'カメラを調整しました',
  'virtualCity.systemDialog.reportBadge': '予測レポート',
  'virtualCity.systemDialog.minimizedBar': 'システム対話',
  'virtualCity.systemDialog.connected': 'システムエージェント接続済み',
  'virtualCity.systemDialog.disconnected': 'システムエージェント未接続',
  'virtualCity.systemDialog.disabled': 'システム対話はサーバー側で停止中です（[city_system] enabled=false）。コマンドは送信できません。',
  // バッチ83 §3.1：ヘッダー ドラッグハンドルのヒント（title + aria-label）。
  'virtualCity.systemDialog.dragHint': 'ドラッグで移動、タイトルバーのダブルクリックでリセット',
  'virtualCity.systemDialog.toolPending': '実行中',
  'virtualCity.systemDialog.toolFailed': '失敗',
  'virtualCity.systemDialog.toolOk': '成功',
  'virtualCity.systemDialog.toolInput': '引数',
  'virtualCity.systemDialog.toolOutput': '出力',
  'virtualCity.systemDialog.toolRunning': '呼び出し中…',
  // 建室モーダル「システムエージェント回線」（収集エージェント回線と同構造）。
  'virtualCity.citySystemLine': 'システムエージェント回線',
  'virtualCity.citySystemLineRandom': 'ランダムのみ',
  'virtualCity.citySystemLineHint':
    'LsmAgentGame-City-System はターごとに LLM ラインプールからランダムに 1 本選びます。特定のモデル回線に固定することもできます。',
};

export default virtualCitySystemJA;
