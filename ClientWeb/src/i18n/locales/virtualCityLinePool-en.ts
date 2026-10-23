// Virtual City LLM line pool i18n (en) — split from en.ts for the
// ≤1800-line cap. Mirrors the virtualCityLinePool-zh.ts key set exactly
// (2026-09-25 §LLM line-pool quota).
const virtualCityLinePool = {
  // Create-room modal stepper row label (max = min(Σ concurrency_lines, 64))
  'virtualCity.llmLines': 'LLM line pool',
  // In-game city panel: {n} = effective line count for this room
  'virtualCity.cityDriverLines': 'LLM lines: {n}',
  // 2026-10-23 §batch79 — create-room modal: City-Chrome collector agent line.
  'virtualCity.cityChromeLine': 'Collector agent line',
  'virtualCity.cityChromeLineRandom': 'Random only',
  'virtualCity.cityChromeLineHint':
    'LsmAgentGame-City-Chrome picks a random line from the LLM line pool for each collection mission; you can also pin a specific model line.',
};

export default virtualCityLinePool;
