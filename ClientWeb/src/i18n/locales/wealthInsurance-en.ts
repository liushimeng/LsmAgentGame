// Wealth P1-4 commercial insurance i18n (en) — split from en.ts (≤1800 line cap).
// Key set mirrors the WealthDict P1-4 section in wealthKeys.ts;
// wording source: lag_docs/虚拟城市/已实现/10-P1保险系统/虚拟城市-P1-商业保险与风险转移引擎-v1.md §10.2.
const wealthInsurance = {
  // Sidebar tab
  'wealth.tab.insurance': 'Insurance',
  // Panel (§10.2's 22 keys + per-card monthly)
  'wealth.insurance.title': 'Commercial Insurance',
  'wealth.insurance.monthlyTotal': 'Monthly Total',
  'wealth.insurance.kind.critical_illness': 'Critical Illness',
  'wealth.insurance.kind.medical_million': 'Million Medical',
  'wealth.insurance.kind.term_life': 'Term Life',
  'wealth.insurance.kind.accident': 'Accident',
  'wealth.insurance.status.active': 'Active',
  'wealth.insurance.status.waiting': 'Waiting, {n} mo left',
  'wealth.insurance.status.grace': 'Grace Period',
  'wealth.insurance.status.lapsed': 'Lapsed',
  'wealth.insurance.coverage': 'Coverage',
  'wealth.insurance.reimburse': '{pct}% Reimbursement',
  'wealth.insurance.annualPremium': 'Annual Premium',
  'wealth.insurance.monthlyPremium': 'Monthly ¥{n}',
  'wealth.insurance.paidMonths': 'Paid {n} months',
  'wealth.insurance.claimsTotal': 'Total Claims',
  'wealth.insurance.buy': 'Buy Now',
  'wealth.insurance.cancel': 'Cancel Policy',
  'wealth.insurance.cancelConfirm': 'Term insurance cancellation refunds NO paid premiums. Cancel this policy?',
  'wealth.insurance.empty': 'No policies yet — insurance yields nothing, it only transfers risk',
  'wealth.insurance.spectatorHint': 'Spectator mode · insurance panel is read-only',
  'wealth.insurance.deathClaim': 'Death claim paid into the estate',
  'wealth.insurance.quoteAtAge': 'Quote at current age',
  // Error codes 35037–35041 (insufficient cash reuses wealth.error.cash 35007)
  'wealth.error.insuranceKindInvalid': 'Invalid insurance kind',
  'wealth.error.insuranceExists': 'An active policy already exists for this kind',
  'wealth.error.insuranceNotFound': 'No active policy for this kind',
  'wealth.error.insuranceAgeGate': 'New purchases not allowed above age 55',
  'wealth.error.insuranceDisabled': 'Insurance engine disabled',
  // New ending id: accidental death (§5.3 HandleDeath)
  'wealth.ending.accident_death': 'Accidental Death',
};

export default wealthInsurance;
