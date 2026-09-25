// Virtual City LLM line pool i18n (en) — split from en.ts for the
// ≤1800-line cap. Mirrors the virtualCityLinePool-zh.ts key set exactly
// (2026-09-25 §LLM line-pool quota).
const virtualCityLinePool = {
  // Create-room modal stepper row label (max = min(Σ concurrency_lines, 64))
  'virtualCity.llmLines': 'LLM line pool',
  // In-game city panel: {n} = effective line count for this room
  'virtualCity.cityDriverLines': 'LLM lines: {n}',
};

export default virtualCityLinePool;
