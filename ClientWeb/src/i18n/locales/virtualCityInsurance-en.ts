// VirtualCity P1-4 commercial insurance i18n (en) — split from en.ts (≤1800 line cap).
// Key set mirrors the VirtualCityDict P1-4 section in virtualCityKeys.ts;
// wording source: lag_docs/虚拟城市/已实现/10-P1保险系统/虚拟城市-P1-商业保险与风险转移引擎-v1.md §10.2.
const virtualCityInsurance = {
  // Sidebar tab
  'virtualCity.tab.insurance': 'Insurance',
  // Panel (§10.2's 22 keys + per-card monthly)
  'virtualCity.insurance.title': 'Commercial Insurance',
  'virtualCity.insurance.monthlyTotal': 'Monthly Total',
  'virtualCity.insurance.kind.critical_illness': 'Critical Illness',
  'virtualCity.insurance.kind.medical_million': 'Million Medical',
  'virtualCity.insurance.kind.term_life': 'Term Life',
  'virtualCity.insurance.kind.accident': 'Accident',
  'virtualCity.insurance.status.active': 'Active',
  'virtualCity.insurance.status.waiting': 'Waiting, {n} mo left',
  'virtualCity.insurance.status.grace': 'Grace Period',
  'virtualCity.insurance.status.lapsed': 'Lapsed',
  'virtualCity.insurance.coverage': 'Coverage',
  'virtualCity.insurance.reimburse': '{pct}% Reimbursement',
  'virtualCity.insurance.annualPremium': 'Annual Premium',
  'virtualCity.insurance.monthlyPremium': 'Monthly ¥{n}',
  'virtualCity.insurance.paidMonths': 'Paid {n} months',
  'virtualCity.insurance.claimsTotal': 'Total Claims',
  'virtualCity.insurance.buy': 'Buy Now',
  'virtualCity.insurance.cancel': 'Cancel Policy',
  'virtualCity.insurance.cancelConfirm': 'Term insurance cancellation refunds NO paid premiums. Cancel this policy?',
  'virtualCity.insurance.empty': 'No policies yet — insurance yields nothing, it only transfers risk',
  'virtualCity.insurance.spectatorHint': 'Spectator mode · insurance panel is read-only',
  'virtualCity.insurance.deathClaim': 'Death claim paid into the estate',
  'virtualCity.insurance.quoteAtAge': 'Quote at current age',
  // Error codes 35037–35041 (insufficient cash reuses virtualCity.error.cash 35007)
  'virtualCity.error.insuranceKindInvalid': 'Invalid insurance kind',
  'virtualCity.error.insuranceExists': 'An active policy already exists for this kind',
  'virtualCity.error.insuranceNotFound': 'No active policy for this kind',
  'virtualCity.error.insuranceAgeGate': 'New purchases not allowed above age 55',
  'virtualCity.error.insuranceDisabled': 'Insurance engine disabled',
  // New ending id: accidental death (§5.3 HandleDeath)
  'virtualCity.ending.accident_death': 'Accidental Death',
};

export default virtualCityInsurance;
