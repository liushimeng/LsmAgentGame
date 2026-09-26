// Virtual City city clock i18n (en) — split from en.ts to satisfy the ≤1800-line cap.
// Aligned with the VirtualCityDict cityClock* keys in virtualCityKeys.ts
// (batch 25 §3.4, 60x narrative layer: 1 real minute = 1 city hour).
const virtualCityCityClock = {
  // Topbar clock tooltip (falls back to runningTime when a legacy frame lacks city_clock_ms).
  'virtualCity.cityClock': 'City clock',
  // Display template: {month}/{day} {time} (HH:MM) ({phase}).
  'virtualCity.cityClockDisplay': '{month}/{day} {time} ({phase})',
  // Phase labels (6-9 dawn / 9-17 daytime / 17-20 dusk / 20-6 night).
  'virtualCity.cityClockPhaseDawn': 'Dawn',
  'virtualCity.cityClockPhaseDay': 'Daytime',
  'virtualCity.cityClockPhaseDusk': 'Dusk',
  'virtualCity.cityClockPhaseNight': 'Night',
};

export default virtualCityCityClock;
