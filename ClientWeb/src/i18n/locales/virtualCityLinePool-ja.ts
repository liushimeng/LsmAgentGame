// 仮想都市 LLM ラインプール i18n（ja）— ≤1800 行制約により ja.ts から分割。
// virtualCityLinePool-zh.ts と同一キー集合（2026-09-25 §LLMラインプール枠）。
const virtualCityLinePool = {
  // 建室モーダル stepper 行ラベル（max = min(Σ concurrency_lines, 64)）
  'virtualCity.llmLines': 'LLM ラインプール',
  // ゲーム内都市パネル：{n} = 本室の有効ライン数
  'virtualCity.cityDriverLines': 'LLM ライン：{n} 条',
  // 2026-10-23 §batch79 — 建室モーダル：City-Chrome 収集エージェントのライン。
  'virtualCity.cityChromeLine': '収集エージェント回線',
  'virtualCity.cityChromeLineRandom': 'ランダムのみ',
  'virtualCity.cityChromeLineHint':
    'LsmAgentGame-City-Chrome は収集ミッションごとに LLM ラインプールからランダムに 1 本選びます。特定のモデル回線に固定することもできます。',
};

export default virtualCityLinePool;
