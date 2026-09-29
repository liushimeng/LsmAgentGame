// 虚拟城市 时间比例与昼夜季节天气 i18n（en）— 与 virtualCityTimeWeather-zh.ts
// 键集合完全对齐（批次 27 §3.1 13 档预设表 / §3.2 季节 / §3.3 8 种天气）。
const virtualCityTimeWeather = {
  'virtualCity.timeRatio': 'Time ratio',
  'virtualCity.timeRatio.h1': '1 min : 1 hour',
  'virtualCity.timeRatio.h2': '1 min : 2 hours',
  'virtualCity.timeRatio.h4': '1 min : 4 hours',
  'virtualCity.timeRatio.h8': '1 min : 8 hours',
  'virtualCity.timeRatio.d1': '1 min : 1 day',
  'virtualCity.timeRatio.d2': '1 min : 2 days',
  'virtualCity.timeRatio.d4': '1 min : 4 days',
  'virtualCity.timeRatio.d8': '1 min : 8 days',
  'virtualCity.timeRatio.m1': '1 min : 1 month',
  'virtualCity.timeRatio.m2': '1 min : 2 months',
  'virtualCity.timeRatio.m4': '1 min : 4 months',
  'virtualCity.timeRatio.m8': '1 min : 8 months',
  'virtualCity.timeRatio.y1': '1 min : 1 year',
  'virtualCity.timeRatioGroup.hours': 'Hours',
  'virtualCity.timeRatioGroup.days': 'Days',
  'virtualCity.timeRatioGroup.months': 'Months',
  'virtualCity.timeRatioGroup.years': 'Years',
  'virtualCity.timeRatioHint': '1 sim month ≈ {month} · full cycle 420 months ≈ {total}',
  'virtualCity.timeRatioSeasonTip':
    'Seasons rotate very slowly at this tier; pick 1 min : 4 days (≈1.1 h per season) or 1 min : 1 month (≈3 min per season) to watch them change',
  'virtualCity.timeRatioBadge': 'Time ×{label}',
  'virtualCity.season.spring': 'Spring',
  'virtualCity.season.summer': 'Summer',
  'virtualCity.season.autumn': 'Autumn',
  'virtualCity.season.winter': 'Winter',
  'virtualCity.weather.clear': 'Clear',
  'virtualCity.weather.cloudy': 'Cloudy',
  'virtualCity.weather.fog': 'Fog',
  'virtualCity.weather.drizzle': 'Drizzle',
  'virtualCity.weather.rain': 'Rain',
  'virtualCity.weather.storm': 'Storm',
  'virtualCity.weather.snow': 'Snow',
  'virtualCity.weather.blizzard': 'Blizzard',
  // ── 批次 33：真实城市（建房下拉 + 主界面日出日落徽章；方案 33 §3.1/§3.4）──
  'virtualCity.city': 'City',
  'virtualCity.cityDefault': 'Default Virtual City',
  'virtualCity.cityRandom': '🎲 Random (Top-20 World City)',
  'virtualCity.cityRandomHint': 'A real city is picked deterministically by the room seed on entry — clock and sunrise/sunset follow that city\'s timezone and latitude',
  // 选中真实城市 hint：{name}/{country}/{lat}/{lng}
  'virtualCity.cityHint': '{name} ({country}) · lat {lat}, lng {lng} — city clock and sunrise/sunset follow the local timezone',
  // 主界面日出日落徽章：{rise}/{set} = HH:MM
  'virtualCity.citySunTimes': '🌅 {rise} · 🌇 {set}',
};

export default virtualCityTimeWeather;
