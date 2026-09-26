// 虚拟城市 城市时钟 i18n（zh-CN）— 拆自 zh-CN.ts 以满足 ≤1800 行约束。
// 与 virtualCityKeys.ts 的 VirtualCityDict cityClock* 键集合完全对齐
// （批次 25 §3.4，60× 叙事层：现实 1 分钟 = 城市 1 小时）。
const virtualCityCityClock = {
  // 顶栏时钟 tooltip（旧帧无 city_clock_ms 时兜底回退 runningTime）。
  'virtualCity.cityClock': '城市时钟',
  // 显示模板：{month}月{day}日 {time}（HH:MM）（{phase}时段）。
  'virtualCity.cityClockDisplay': '{month}月{day}日 {time}（{phase}）',
  // 时段四键（6–9 清晨 / 9–17 白天 / 17–20 傍晚 / 20–6 夜晚）。
  'virtualCity.cityClockPhaseDawn': '清晨',
  'virtualCity.cityClockPhaseDay': '白天',
  'virtualCity.cityClockPhaseDusk': '傍晚',
  'virtualCity.cityClockPhaseNight': '夜晚',
};

export default virtualCityCityClock;
