// 仮想都市 LLM ラインプール i18n（ja）— ≤1800 行制約により ja.ts から分割。
// virtualCityLinePool-zh.ts と同一キー集合（2026-09-25 §LLMラインプール枠）。
const virtualCityLinePool = {
  // 建室モーダル stepper 行ラベル（max = min(Σ concurrency_lines, 64)）
  'virtualCity.llmLines': 'LLM ラインプール',
  // ゲーム内都市パネル：{n} = 本室の有効ライン数
  'virtualCity.cityDriverLines': 'LLM ライン：{n} 条',
};

export default virtualCityLinePool;
