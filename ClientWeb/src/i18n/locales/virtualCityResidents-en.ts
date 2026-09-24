// Virtual City resident profile anchoring i18n (en) — split from en.ts for the
// ≤1800-line cap. Mirrors the virtualCityResidents-zh.ts key set exactly
// (profile anchoring design §8.4).
const virtualCityResidents = {
  // CityStatsPanel anchor progress + MonthTicker terminal event + drawer reuse
  'virtualCity.cityProfiles.title': 'City resident profiles',
  'virtualCity.cityProfiles.anchorReady': 'Anchored {n} real profiles',
  'virtualCity.cityProfiles.anchorProgress': 'Anchoring profiles {done}/{total} (pool {pool})',
  'virtualCity.cityProfiles.anchorFailed': 'Profile anchoring failed — showing synthetic data',
  'virtualCity.cityProfiles.anchorIdle': 'Profile anchoring not enabled this round (curated deck)',
  'virtualCity.cityProfiles.anchorDoneEvent': 'City profile anchoring finished {done}/{total}',
  'virtualCity.cityProfiles.anchorFailedEvent': 'City profile anchoring failed — fell back to synthetic data',
  'virtualCity.cityProfiles.openDrawer': 'Resident profiles',
  // ResidentProfileDrawer
  'virtualCity.residentDrawer.title': 'Resident profiles',
  'virtualCity.residentDrawer.close': 'Close drawer',
  'virtualCity.residentDrawer.searchPlaceholder': 'Search name / occupation / card ID',
  'virtualCity.residentDrawer.prev': 'Previous',
  'virtualCity.residentDrawer.next': 'Next',
  'virtualCity.residentDrawer.pageInfo': 'Page {page} / {pages}',
  'virtualCity.residentDrawer.matched': '{n} residents matched',
  'virtualCity.residentDrawer.income': 'Monthly income',
  'virtualCity.residentDrawer.expense': 'Monthly expense',
  'virtualCity.residentDrawer.savings': 'Savings',
  'virtualCity.residentDrawer.employed': 'Employed',
  'virtualCity.residentDrawer.unemployed': 'Unemployed',
  'virtualCity.residentDrawer.stressed': 'Savings tight',
  'virtualCity.residentDrawer.goal': '5-year goal',
  'virtualCity.residentDrawer.personality': 'Personality',
  'virtualCity.residentDrawer.openingHook': 'Profile intro',
  'virtualCity.residentDrawer.marital': 'Marital status',
  'virtualCity.residentDrawer.healthGrade': 'Health grade',
  'virtualCity.residentDrawer.sourceFile': 'Source file',
  'virtualCity.residentDrawer.empty': 'No matching residents',
  'virtualCity.residentDrawer.voiceOf': "View {name}'s profile",
  'virtualCity.residentDrawer.age': 'Age',
  'virtualCity.residentDrawer.district': 'District',
  'virtualCity.residentDrawer.occupation': 'Occupation',
  'virtualCity.residentDrawer.domain': 'Industry',
  'virtualCity.residentDrawer.cardId': 'Card ID',
  'virtualCity.residentDrawer.notFound': 'Resident profile not found',
};

export default virtualCityResidents;
