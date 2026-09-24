// バーチャルシティ バッチ20 FE-2 i18n（ja）：副業価格競争 / 株式マイクロ構造 / 市長選挙。
// virtualCityKeys.ts の VirtualCityDict バッチ20セグメントと一致
// （ドキュメント2 §5 + ドキュメント3 A4/B4。≤1800行制約のため分離）。
const virtualCityBatch20 = {
  // 副業プライシング（ActionPanel 副業ブロック + 開業モーダルの档選択）
  'virtualCity.sidePrice.label': '開業価格档',
  'virtualCity.sidePrice.low': '低価格',
  'virtualCity.sidePrice.mid': '中価格',
  'virtualCity.sidePrice.high': '高価格',
  'virtualCity.sideShare': 'シェア {pct}%',
  'virtualCity.sideExpected': '予測 ¥{amount}',
  'virtualCity.sideCompetitors': '同業の競合 {n}',
  'virtualCity.sidePriceGate': '今月は価格変更済み · 月1回まで',
  // 株式マイクロ構造（MarketPanel 双方向価 / スプレッド / T+1 / サーキットブレーカー）
  'virtualCity.micro.buyUnit': '買 {price}',
  'virtualCity.micro.sellUnit': '売 {price}',
  'virtualCity.micro.spread': 'スプレッド {bps}bp',
  'virtualCity.micro.t1Locked': 'T+1 凍結 {n}',
  'virtualCity.micro.breaker': 'サーキットブレーカー：株式取引を停止（第{n}月まで）',
  // 市長選挙（作成トグル / バナー / 政務票型パネル）
  'virtualCity.election.title': '市長選挙',
  'virtualCity.election.switch': '市長選挙を有効化（48ヶ月ごと、デフォルト無効）',
  'virtualCity.election.mayor': '現職市長：#{seat} 番住民{name}',
  'virtualCity.election.bannerClose': '閉じる',
  'virtualCity.election.stipendStopped': '市長手当の支給停止（国庫不足）',
  'virtualCity.election.votePanel': '政務 · 得票',
  'virtualCity.election.nextElection': '次回選挙：第 {m} 月',
  'virtualCity.election.termProgress': '任期 {elapsed}/{interval} ヶ月',
  'virtualCity.election.colScore': '総合',
  'virtualCity.election.colWealth': '富',
  'virtualCity.election.colNetwork': '人脈',
  'virtualCity.election.colSatisfaction': '満足度',
};

export default virtualCityBatch20;
