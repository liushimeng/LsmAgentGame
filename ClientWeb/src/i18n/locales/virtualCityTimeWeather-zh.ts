// 虚拟城市 时间比例与昼夜季节天气 i18n（zh-CN）— 拆自 zh-CN.ts 以满足 ≤1800 行约束。
// 与 virtualCityKeys.ts 的 VirtualCityDict timeRatio*/season*/weather* 键集合
// 完全对齐（批次 27 §3.1 13 档预设表 / §3.2 季节 / §3.3 8 种天气）。
const virtualCityTimeWeather = {
  // 建房下拉行标签 + HUD 比例徽章 tooltip 语义。
  'virtualCity.timeRatio': '时间比例',
  // 13 档预设（§3.1 预设表逐行；UI 顺序 = 数组顺序，默认第 1 档）。
  'virtualCity.timeRatio.h1': '1分钟比1小时',
  'virtualCity.timeRatio.h2': '1分钟比2小时',
  'virtualCity.timeRatio.h4': '1分钟比4小时',
  'virtualCity.timeRatio.h8': '1分钟比8小时',
  'virtualCity.timeRatio.d1': '1分钟比1天',
  'virtualCity.timeRatio.d2': '1分钟比2天',
  'virtualCity.timeRatio.d4': '1分钟比4天',
  'virtualCity.timeRatio.d8': '1分钟比8天',
  'virtualCity.timeRatio.m1': '1分钟比1个月',
  'virtualCity.timeRatio.m2': '1分钟比2个月',
  'virtualCity.timeRatio.m4': '1分钟比4个月',
  'virtualCity.timeRatio.m8': '1分钟比8个月',
  'virtualCity.timeRatio.y1': '1分钟比1年',
  // 下拉 optgroup 四组标题。
  'virtualCity.timeRatioGroup.hours': '小时',
  'virtualCity.timeRatioGroup.days': '天',
  'virtualCity.timeRatioGroup.months': '月',
  'virtualCity.timeRatioGroup.years': '年',
  // 下拉下方 hint 信息行：{month}/{total} = 紧凑时长（60s / 105min / 7h）。
  'virtualCity.timeRatioHint': '1 模拟月 ≈ {month} · 全周期 420 模拟月 ≈ {total}',
  // 慢档（≤1分钟比2小时）附加的季节观感提示（§3.2 观感指引）。
  'virtualCity.timeRatioSeasonTip':
    '此档位四季轮转很慢；想看季节变化可选 1分钟比4天（一季约 1.1 小时）或 1分钟比1个月（一季约 3 分钟）',
  // HUD 比例徽章（非 13 档的自定义 ratio 兜底）：{label} = 如 1440×。
  'virtualCity.timeRatioBadge': '时间 ×{label}',
  // 季节四键（§3.2；HUD「季节 · 天气」徽章）。
  'virtualCity.season.spring': '春',
  'virtualCity.season.summer': '夏',
  'virtualCity.season.autumn': '秋',
  'virtualCity.season.winter': '冬',
  // 天气 8 键（§3.3）。
  'virtualCity.weather.clear': '晴',
  'virtualCity.weather.cloudy': '多云',
  'virtualCity.weather.fog': '雾',
  'virtualCity.weather.drizzle': '小雨',
  'virtualCity.weather.rain': '雨',
  'virtualCity.weather.storm': '暴雨',
  'virtualCity.weather.snow': '雪',
  'virtualCity.weather.blizzard': '暴雪',
};

export default virtualCityTimeWeather;
