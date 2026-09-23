// Virtual City resident profile anchoring i18n (en) — split from en.ts for the
// ≤1800-line cap. Mirrors the wealthResidents-zh.ts key set exactly
// (profile anchoring design §8.4).
const wealthResidents = {
  // CityStatsPanel anchor progress + MonthTicker terminal event + drawer reuse
  'wealth.cityProfiles.title': 'City resident profiles',
  'wealth.cityProfiles.anchorReady': 'Anchored {n} real profiles',
  'wealth.cityProfiles.anchorProgress': 'Anchoring profiles {done}/{total} (pool {pool})',
  'wealth.cityProfiles.anchorFailed': 'Profile anchoring failed — showing synthetic data',
  'wealth.cityProfiles.anchorIdle': 'Profile anchoring not enabled this round (curated deck)',
  'wealth.cityProfiles.anchorDoneEvent': 'City profile anchoring finished {done}/{total}',
  'wealth.cityProfiles.anchorFailedEvent': 'City profile anchoring failed — fell back to synthetic data',
  'wealth.cityProfiles.openDrawer': 'Resident profiles',
  // ResidentProfileDrawer
  'wealth.residentDrawer.title': 'Resident profiles',
  'wealth.residentDrawer.close': 'Close drawer',
  'wealth.residentDrawer.searchPlaceholder': 'Search name / occupation / card ID',
  'wealth.residentDrawer.prev': 'Previous',
  'wealth.residentDrawer.next': 'Next',
  'wealth.residentDrawer.pageInfo': 'Page {page} / {pages}',
  'wealth.residentDrawer.matched': '{n} residents matched',
  'wealth.residentDrawer.income': 'Monthly income',
  'wealth.residentDrawer.expense': 'Monthly expense',
  'wealth.residentDrawer.savings': 'Savings',
  'wealth.residentDrawer.employed': 'Employed',
  'wealth.residentDrawer.unemployed': 'Unemployed',
  'wealth.residentDrawer.stressed': 'Savings tight',
  'wealth.residentDrawer.goal': '5-year goal',
  'wealth.residentDrawer.personality': 'Personality',
  'wealth.residentDrawer.openingHook': 'Profile intro',
  'wealth.residentDrawer.marital': 'Marital status',
  'wealth.residentDrawer.healthGrade': 'Health grade',
  'wealth.residentDrawer.sourceFile': 'Source file',
  'wealth.residentDrawer.empty': 'No matching residents',
  'wealth.residentDrawer.voiceOf': "View {name}'s profile",
  'wealth.residentDrawer.age': 'Age',
  'wealth.residentDrawer.district': 'District',
  'wealth.residentDrawer.occupation': 'Occupation',
  'wealth.residentDrawer.domain': 'Industry',
  'wealth.residentDrawer.cardId': 'Card ID',
  'wealth.residentDrawer.notFound': 'Resident profile not found',
};

export default wealthResidents;
