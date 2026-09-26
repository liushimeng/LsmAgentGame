// 仮想都市 都市時計 i18n（ja）— ≤1800 行制約のため ja.ts から分割。
// virtualCityKeys.ts の VirtualCityDict cityClock* キー集合と完全対応
// （バッチ25 §3.4、60× ナラティブ層：現実 1 分 = 都市 1 時間）。
const virtualCityCityClock = {
  // トップバー時計 tooltip（旧フレームに city_clock_ms が無い場合は runningTime にフォールバック）。
  'virtualCity.cityClock': '都市時計',
  // 表示テンプレート：{month}月{day}日 {time}（HH:MM）（{phase}時間帯）。
  'virtualCity.cityClockDisplay': '{month}月{day}日 {time}（{phase}）',
  // 時間帯 4 キー（6–9 早朝 / 9–17 昼間 / 17–20 夕方 / 20–6 夜）。
  'virtualCity.cityClockPhaseDawn': '早朝',
  'virtualCity.cityClockPhaseDay': '昼間',
  'virtualCity.cityClockPhaseDusk': '夕方',
  'virtualCity.cityClockPhaseNight': '夜',
};

export default virtualCityCityClock;
