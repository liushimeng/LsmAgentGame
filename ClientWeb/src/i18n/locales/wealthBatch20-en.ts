// Virtual City batch 20 FE-2 i18n (en): side-business price war / stock
// microstructure / mayoral election. Keys align with wealthKeys.ts batch-20
// segment (doc 2 §5 + doc 3 A4/B4). Split out for the ≤1800-line rule.
const wealthBatch20 = {
  // Side-business pricing (ActionPanel side block + start modal tier picker)
  'wealth.sidePrice.label': 'Starting price tier',
  'wealth.sidePrice.low': 'Low',
  'wealth.sidePrice.mid': 'Mid',
  'wealth.sidePrice.high': 'High',
  'wealth.sideShare': 'Share {pct}%',
  'wealth.sideExpected': 'Est. ¥{amount}',
  'wealth.sideCompetitors': 'Rivals {n}',
  'wealth.sidePriceGate': 'Already repriced this month · 1 change/month max',
  // Stock microstructure (MarketPanel two-sided price / spread / T+1 / breaker)
  'wealth.micro.buyUnit': 'Buy {price}',
  'wealth.micro.sellUnit': 'Sell {price}',
  'wealth.micro.spread': 'Spread {bps}bp',
  'wealth.micro.t1Locked': 'T+1 locked {n}',
  'wealth.micro.breaker': 'Circuit breaker: stock trading halted (until month {n})',
  // Mayoral election (create toggle / banner / civic vote panel)
  'wealth.election.title': 'Mayoral Election',
  'wealth.election.switch': 'Enable mayoral election (every 48 months, off by default)',
  'wealth.election.mayor': 'Mayor: resident #{seat}{name}',
  'wealth.election.bannerClose': 'Close',
  'wealth.election.stipendStopped': 'Mayor stipend stopped (empty treasury)',
  'wealth.election.votePanel': 'Civic · Votes',
  'wealth.election.nextElection': 'Next election: month {m}',
  'wealth.election.termProgress': 'Term {elapsed}/{interval} months',
  'wealth.election.colScore': 'Score',
  'wealth.election.colWealth': 'Wealth',
  'wealth.election.colNetwork': 'Network',
  'wealth.election.colSatisfaction': 'Approval',
};

export default wealthBatch20;
