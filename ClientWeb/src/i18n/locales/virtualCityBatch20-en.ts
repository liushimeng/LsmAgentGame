// Virtual City batch 20 FE-2 i18n (en): side-business price war / stock
// microstructure / mayoral election. Keys align with virtualCityKeys.ts batch-20
// segment (doc 2 §5 + doc 3 A4/B4). Split out for the ≤1800-line rule.
const virtualCityBatch20 = {
  // Side-business pricing (ActionPanel side block + start modal tier picker)
  'virtualCity.sidePrice.label': 'Starting price tier',
  'virtualCity.sidePrice.low': 'Low',
  'virtualCity.sidePrice.mid': 'Mid',
  'virtualCity.sidePrice.high': 'High',
  'virtualCity.sideShare': 'Share {pct}%',
  'virtualCity.sideExpected': 'Est. ¥{amount}',
  'virtualCity.sideCompetitors': 'Rivals {n}',
  'virtualCity.sidePriceGate': 'Already repriced this month · 1 change/month max',
  // Stock microstructure (MarketPanel two-sided price / spread / T+1 / breaker)
  'virtualCity.micro.buyUnit': 'Buy {price}',
  'virtualCity.micro.sellUnit': 'Sell {price}',
  'virtualCity.micro.spread': 'Spread {bps}bp',
  'virtualCity.micro.t1Locked': 'T+1 locked {n}',
  'virtualCity.micro.breaker': 'Circuit breaker: stock trading halted (until month {n})',
  // Mayoral election (create toggle / banner / civic vote panel)
  'virtualCity.election.title': 'Mayoral Election',
  'virtualCity.election.switch': 'Enable mayoral election (every 48 months, off by default)',
  'virtualCity.election.mayor': 'Mayor: resident #{seat}{name}',
  'virtualCity.election.bannerClose': 'Close',
  'virtualCity.election.stipendStopped': 'Mayor stipend stopped (empty treasury)',
  'virtualCity.election.votePanel': 'Civic · Votes',
  'virtualCity.election.nextElection': 'Next election: month {m}',
  'virtualCity.election.termProgress': 'Term {elapsed}/{interval} months',
  'virtualCity.election.colScore': 'Score',
  'virtualCity.election.colWealth': 'VirtualCity',
  'virtualCity.election.colNetwork': 'Network',
  'virtualCity.election.colSatisfaction': 'Approval',
};

export default virtualCityBatch20;
