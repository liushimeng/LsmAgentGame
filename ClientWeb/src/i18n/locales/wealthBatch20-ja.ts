// バーチャルシティ バッチ20 FE-2 i18n（ja）：副業価格競争 / 株式マイクロ構造 / 市長選挙。
// wealthKeys.ts の WealthDict バッチ20セグメントと一致
// （ドキュメント2 §5 + ドキュメント3 A4/B4。≤1800行制約のため分離）。
const wealthBatch20 = {
  // 副業プライシング（ActionPanel 副業ブロック + 開業モーダルの档選択）
  'wealth.sidePrice.label': '開業価格档',
  'wealth.sidePrice.low': '低価格',
  'wealth.sidePrice.mid': '中価格',
  'wealth.sidePrice.high': '高価格',
  'wealth.sideShare': 'シェア {pct}%',
  'wealth.sideExpected': '予測 ¥{amount}',
  'wealth.sideCompetitors': '同業の競合 {n}',
  'wealth.sidePriceGate': '今月は価格変更済み · 月1回まで',
  // 株式マイクロ構造（MarketPanel 双方向価 / スプレッド / T+1 / サーキットブレーカー）
  'wealth.micro.buyUnit': '買 {price}',
  'wealth.micro.sellUnit': '売 {price}',
  'wealth.micro.spread': 'スプレッド {bps}bp',
  'wealth.micro.t1Locked': 'T+1 凍結 {n}',
  'wealth.micro.breaker': 'サーキットブレーカー：株式取引を停止（第{n}月まで）',
  // 市長選挙（作成トグル / バナー / 政務票型パネル）
  'wealth.election.title': '市長選挙',
  'wealth.election.switch': '市長選挙を有効化（48ヶ月ごと、デフォルト無効）',
  'wealth.election.mayor': '現職市長：#{seat} 番住民{name}',
  'wealth.election.bannerClose': '閉じる',
  'wealth.election.stipendStopped': '市長手当の支給停止（国庫不足）',
  'wealth.election.votePanel': '政務 · 得票',
  'wealth.election.nextElection': '次回選挙：第 {m} 月',
  'wealth.election.termProgress': '任期 {elapsed}/{interval} ヶ月',
  'wealth.election.colScore': '総合',
  'wealth.election.colWealth': '富',
  'wealth.election.colNetwork': '人脈',
  'wealth.election.colSatisfaction': '満足度',
};

export default wealthBatch20;
