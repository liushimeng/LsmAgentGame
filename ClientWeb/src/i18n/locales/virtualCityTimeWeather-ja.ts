// 虚拟城市 时间比例与昼夜季节天气 i18n（ja）— 与 virtualCityTimeWeather-zh.ts
// 键集合完全对齐（批次 27 §3.1 13 档预设表 / §3.2 季节 / §3.3 8 种天气）。
const virtualCityTimeWeather = {
  'virtualCity.timeRatio': '時間比率',
  'virtualCity.timeRatio.h1': '1分：1時間',
  'virtualCity.timeRatio.h2': '1分：2時間',
  'virtualCity.timeRatio.h4': '1分：4時間',
  'virtualCity.timeRatio.h8': '1分：8時間',
  'virtualCity.timeRatio.d1': '1分：1日',
  'virtualCity.timeRatio.d2': '1分：2日',
  'virtualCity.timeRatio.d4': '1分：4日',
  'virtualCity.timeRatio.d8': '1分：8日',
  'virtualCity.timeRatio.m1': '1分：1か月',
  'virtualCity.timeRatio.m2': '1分：2か月',
  'virtualCity.timeRatio.m4': '1分：4か月',
  'virtualCity.timeRatio.m8': '1分：8か月',
  'virtualCity.timeRatio.y1': '1分：1年',
  'virtualCity.timeRatioGroup.hours': '時間',
  'virtualCity.timeRatioGroup.days': '日',
  'virtualCity.timeRatioGroup.months': '月',
  'virtualCity.timeRatioGroup.years': '年',
  'virtualCity.timeRatioHint': '1シミュレーション月 ≈ {month} · 全周期420月 ≈ {total}',
  'virtualCity.timeRatioSeasonTip':
    'この設定では季節の移り変わりがとても遅いです。季節を見たい場合は「1分：4日」（1季節約1.1時間）か「1分：1か月」（1季節約3分）をおすすめします',
  'virtualCity.timeRatioBadge': '時間 ×{label}',
  'virtualCity.season.spring': '春',
  'virtualCity.season.summer': '夏',
  'virtualCity.season.autumn': '秋',
  'virtualCity.season.winter': '冬',
  'virtualCity.weather.clear': '晴れ',
  'virtualCity.weather.cloudy': '曇り',
  'virtualCity.weather.fog': '霧',
  'virtualCity.weather.drizzle': '小雨',
  'virtualCity.weather.rain': '雨',
  'virtualCity.weather.storm': '暴雨',
  'virtualCity.weather.snow': '雪',
  'virtualCity.weather.blizzard': '猛吹雪',
  // ── 批次 33：真实城市（建房下拉 + 主界面日出日落徽章；方案 33 §3.1/§3.4）──
  'virtualCity.city': '都市',
  'virtualCity.cityDefault': 'デフォルト仮想都市',
  'virtualCity.cityRandom': '🎲 ランダム（世界トップ20都市）',
  'virtualCity.cityRandomHint': '入室時にルーム seed で決定的に 1 都市を抽選 —— 時計と日の出/日の入りはその都市のタイムゾーンと緯度に従います',
  // 选中真实城市 hint：{name}/{country}/{lat}/{lng}
  'virtualCity.cityHint': '{name}（{country}）· 緯度 {lat}、経度 {lng} —— 都市時計と日の出/日の入りは現地タイムゾーンで表示',
  // 主界面日出日落徽章：{rise}/{set} = HH:MM
  'virtualCity.citySunTimes': '🌅 {rise} · 🌇 {set}',
};

export default virtualCityTimeWeather;
