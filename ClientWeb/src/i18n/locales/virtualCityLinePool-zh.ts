// 虚拟城市 LLM 线路池 i18n（zh-CN）— 拆自 zh-CN.ts 以满足 ≤1800 行约束。
// 与 virtualCityKeys.ts 的 VirtualCityDict llmLines/cityDriverLines 键集合
// 完全对齐（2026-09-25 §LLM线路池配额）。
const virtualCityLinePool = {
  // 建房弹窗 stepper 行标签（max = min(Σ concurrency_lines, 64)）
  'virtualCity.llmLines': 'LLM 线路池',
  // 游戏内城市面板：{n} = 本房生效线路数
  'virtualCity.cityDriverLines': 'LLM 线路：{n} 条',
};

export default virtualCityLinePool;
