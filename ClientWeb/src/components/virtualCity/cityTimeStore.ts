/**
 * cityTimeStore — 城市时间 / 季节 / 天气 渲染数据源（批次 27 §4.1 · 模块单例）。
 *
 * 设计动机：高时间倍率下城市日可短至 0.16s（1分钟比1年），昼夜/粒子/路灯
 * 若走 React 状态逐帧刷新会卡顿 —— 本模块是**可变单例 + useFrame 直读**，
 * 不触发任何重渲染；仅 season 用低频订阅（变化才 setState，供贴图/材质换季）。
 *
 * 数据流：game.state 帧（GamePage 既有锚点逻辑）→ setCityClockAnchor /
 * setCityEnv → CityEnvironmentLayer（DayNightCycle + WeatherFX 采样）、
 * HUD（季节/天气徽章）、逐组件 useFrame（路灯 emissive / 树冠 tint / 云 opacity）。
 *
 * 契约：lag_docs/虚拟城市/已实现/27-时间比例与昼夜季节天气/01-方案设计-v1.md
 * §3.2（季节）/ §3.3（天气镜像表，与服务端 time_weather.go 双端同步纪律）/ §4.1。
 */

import type { DayNightSnapshot } from '@/engine3d';
import type { VirtualCitySeason, VirtualCityWeatherKind } from '@/types/virtualCity';

export type CitySeason = VirtualCitySeason;
export type CityWeatherKind = VirtualCityWeatherKind;

/**
 * 批次84：显示指令的「覆盖时刻」相位（与后端 DisplayCommand 的 phase 逐字一致）。
 * dawn / day / dusk / night 四相 —— 引擎只认时刻（0..1），相位在此折算。
 */
export type CityPhaseOverride = 'dawn' | 'day' | 'dusk' | 'night';

/** 相位 → 当日时刻（0..1；0 = 0:00，0.5 = 正午）。 */
const PHASE_TIME_OF_DAY: Record<CityPhaseOverride, number> = {
  dawn: 0.25,   // 6:00
  day: 0.5,     // 12:00
  dusk: 0.75,   // 18:00
  night: 0.0,   // 0:00
};

/**
 * 批次84：显示指令天气 → 游戏侧天气类型的**近似映射**。
 *
 * 游戏的天气类型是 8 种（clear/cloudy/fog/drizzle/rain/storm/snow/blizzard），
 * 显示指令用的是另一套 7 种（clear/rain/snow/fog/haze/sandstorm/storm）——
 * 两套命名空间不完全重叠，故在此显式折算：
 *   - haze（薄霭）→ fog 弱强度（游戏侧无独立 haze）；
 *   - sandstorm（沙尘暴）→ storm 强强度（**近似**：游戏侧无沙尘粒子，
 *     用强风暴 + 高雾密度近似，视觉上表现为「灰黄 + 强降水」）。
 * 其余一一对应。
 */
const DISPLAY_WEATHER_MAP: Record<string, { kind: CityWeatherKind; intensity: number }> = {
  clear: { kind: 'clear', intensity: 0 },
  rain: { kind: 'rain', intensity: 0.85 },
  snow: { kind: 'snow', intensity: 0.85 },
  fog: { kind: 'fog', intensity: 0.8 },
  haze: { kind: 'fog', intensity: 0.35 },
  sandstorm: { kind: 'storm', intensity: 0.9 },
  storm: { kind: 'storm', intensity: 1 },
};

/**
 * 批次84：显示指令时间流速 → 时钟倍率（相对服务端 time_ratio 的倍数）。
 * pause = 0（客户端时钟冻结）/ slow = 0.25 / normal = 1 / fast = 4 / xfast = 16。
 */
const DISPLAY_SPEED_MAP: Record<string, number> = {
  pause: 0,
  slow: 0.25,
  normal: 1,
  fast: 4,
  xfast: 16,
};

/** 城市纪元（与后端 cityEpochBaseMs 同源：2025-01-01 08:00 +0800 = UTC 2025-01-01）。 */
export const CITY_EPOCH_MS = Date.UTC(2025, 0, 1);

const DAY_MS = 86_400_000;
/** 天气类型切换迟滞（§3.3 前端平滑：快档城市日 0.16s 不频闪）。 */
const WEATHER_HYSTERESIS_MS = 3000;

export interface CityTimeSample {
  /** 城市时钟毫秒（锚点 + (now − at) × ratio 外推；无锚点 = 0）。 */
  cityMs: number;
  /** 当日时刻 0..1（0.5 = 正午；无锚点 = 0.5 静态正午，视觉零回归）。
   *  批次 33：选中真实城市时为**城市当地**时刻（UTC + tzOffsetMin）。 */
  timeOfDay01: number;
  /** 城市日序号（自纪元起）。 */
  dayIndex: number;
  season: CitySeason;
  weather: CityWeatherKind | null;
  intensity: number;
  /** 批次 33：当日日出/日落（当地 0..1；无城市 = 0.25/0.75 = 批次 27 的 6:00/18:00）。 */
  sunrise01: number;
  sunset01: number;
}

/** §3.3 视觉参数镜像表行（8 类型逐值照抄方案）。 */
export interface WeatherVisual {
  /** 日光系数（乘主方向光/环境强度）。 */
  sunScale: number;
  /** 云 opacity 系数（× CloudLayer 基准 0.35）。 */
  cloudOpacity: number;
  /** 雾密度系数（收拢 fog near/far）。 */
  fogScale: number;
  /** 粒子类型（null = 无）。 */
  particle: 'rain' | 'snow' | null;
  /** 粒子强度/尺寸缩放（drizzle 0.5 / storm 1.2 / blizzard 1.4）。 */
  particleScale: number;
}

/**
 * §3.3 视觉参数镜像表 —— 与服务端 time_weather.go 保持逐值一致
 * （双端同步纪律：改表必须同步方案文档 + 后端）。
 */
export const WEATHER_VISUALS: Record<CityWeatherKind, WeatherVisual> = {
  clear: { sunScale: 1.0, cloudOpacity: 0.1, fogScale: 0, particle: null, particleScale: 1 },
  cloudy: { sunScale: 0.78, cloudOpacity: 0.45, fogScale: 0.06, particle: null, particleScale: 1 },
  fog: { sunScale: 0.55, cloudOpacity: 0.75, fogScale: 0.55, particle: null, particleScale: 1 },
  drizzle: { sunScale: 0.6, cloudOpacity: 0.6, fogScale: 0.15, particle: 'rain', particleScale: 0.5 },
  rain: { sunScale: 0.42, cloudOpacity: 0.7, fogScale: 0.22, particle: 'rain', particleScale: 1 },
  storm: { sunScale: 0.25, cloudOpacity: 0.85, fogScale: 0.32, particle: 'rain', particleScale: 1.2 },
  snow: { sunScale: 0.6, cloudOpacity: 0.7, fogScale: 0.25, particle: 'snow', particleScale: 1 },
  blizzard: { sunScale: 0.35, cloudOpacity: 0.9, fogScale: 0.5, particle: 'snow', particleScale: 1.4 },
};

/** 旧帧无天气数据的中性参数：所有系数 = 视觉与批次 26 完全一致（云 0.35 不缩放）。 */
export const NEUTRAL_VISUAL: WeatherVisual = {
  sunScale: 1,
  cloudOpacity: 1,
  fogScale: 0,
  particle: null,
  particleScale: 1,
};

const SEASONS: readonly CitySeason[] = ['spring', 'summer', 'autumn', 'winter'];
const WEATHER_KINDS: readonly CityWeatherKind[] = [
  'clear', 'cloudy', 'fog', 'drizzle', 'rain', 'storm', 'snow', 'blizzard',
];

function isSeason(v: unknown): v is CitySeason {
  return typeof v === 'string' && (SEASONS as readonly string[]).includes(v);
}

function isWeatherKind(v: unknown): v is CityWeatherKind {
  return typeof v === 'string' && (WEATHER_KINDS as readonly string[]).includes(v);
}

// ── 单例状态（模块级可变；页面 reset 时经 resetCityTime 清空）──────────
interface Anchor {
  cityMs: number;
  at: number;
  ratio: number;
}

let anchor: Anchor | null = null;
let season: CitySeason = 'summer';
let weather: CityWeatherKind | null = null;
let intensity = 0;
let lastWeatherAt = 0;
let dayNight: DayNightSnapshot | null = null;
// 批次 33：真实城市时区（分钟，东正西负；0 = 默认城市/旧帧，UTC 口径）。
let tzOffsetMin = 0;
// 批次 33：当日日出/日落（当地日分数；缺省 6:00/18:00 = 批次 27 语义）。
let sunrise01 = 0.25;
let sunset01 = 0.75;
// ── 批次84：显示指令「覆盖」通道（null = 未覆盖，走服务端真值）──────────
/** 覆盖时刻（0..1 当日时刻）。 */
let timeOfDayOverride: number | null = null;
/** 覆盖天气（类型 + 强度）。 */
let weatherOverride: { kind: CityWeatherKind; intensity: number } | null = null;
/** 覆盖时间流速（相对服务端 time_ratio 的倍数；null = 不覆盖）。 */
let timeSpeedMultiplier: number | null = null;
const seasonSubs = new Set<(s: CitySeason) => void>();

/**
 * 城市时钟锚点（帧到达 / 暂停冻结时由 GamePage 调用）。
 * ratio = 0 表示暂停冻结（连续帧 city_clock_ms 不变 ⇒ 传 0）。
 */
export function setCityClockAnchor(cityMs: number, at: number, ratio: number): void {
  const r = Number.isFinite(ratio) && ratio > 0 ? ratio : 0;
  // 批次84：时间流速覆盖生效中 ⇒ **不采纳服务端的绝对时刻**，改为保持本地
  // 连续推进（否则每帧重锚会把覆写倍率冲掉，覆盖形同虚设）。
  if (timeSpeedMultiplier !== null && anchor) {
    const now = Date.now();
    const local = anchor.cityMs
      + Math.max(0, now - anchor.at) * anchor.ratio * timeSpeedMultiplier;
    anchor = { cityMs: local, at: now, ratio: r };
    return;
  }
  anchor = {
    cityMs,
    at,
    ratio: r,
  };
}

/**
 * 季节 / 天气写入（帧到达时调用；季节低频订阅在此分发）。
 * 天气类型切换 ≥3s 迱滞（§3.3：丢弃中间值）；opts.force 供 ?debug=1 强制注入。
 */
export function setCityEnv(
  nextSeason: unknown,
  nextWeather: unknown,
  nextIntensity: unknown,
  opts?: { force?: boolean },
): void {
  if (isSeason(nextSeason) && nextSeason !== season) {
    season = nextSeason;
    seasonSubs.forEach((cb) => cb(season));
  }
  if (isWeatherKind(nextWeather) && nextWeather !== weather) {
    const now = Date.now();
    if (opts?.force || now - lastWeatherAt >= WEATHER_HYSTERESIS_MS) {
      weather = nextWeather;
      lastWeatherAt = now;
    }
    // 迟滞窗口内的类型翻转 → 丢弃（快档城市日 0.16s 不频闪）。
  }
  if (typeof nextIntensity === 'number' && Number.isFinite(nextIntensity)) {
    intensity = Math.min(1, Math.max(0, nextIntensity));
  }
}

/** DayNightSnapshot 转存（CityEnvironmentLayer 每帧写；路灯等组件读）。 */
export function setDayNight(snap: DayNightSnapshot | null): void {
  dayNight = snap;
}

/** 读取最近一帧昼夜快照（无 = 白昼基准，视觉零回归）。 */
export function getDayNight(): DayNightSnapshot | null {
  return dayNight;
}

/** 当前季节（组件 useState 初始化用；后续变化走 subscribeSeason）。 */
export function currentSeason(): CitySeason {
  return season;
}

/** 每帧采样（廉价：模块读 + 算术，无分配热点路径外的开销）。 */
export function sample(): CityTimeSample {
  const a = anchor;
  const speedMul = timeSpeedMultiplier ?? 1;
  const cityMs = a ? a.cityMs + Math.max(0, Date.now() - a.at) * a.ratio * speedMul : 0;
  // 批次84：覆盖时刻优先（相位 / 小时 → 当日时刻 0..1）。
  const effWeather = weatherOverride ? weatherOverride.kind : weather;
  const effIntensity = weatherOverride ? weatherOverride.intensity : intensity;
  if (cityMs <= 0) {
    // 无锚点（占位帧/旧后端）：静态正午（timeOfDay01 = 0.5）= 旧版观感。
    return {
      cityMs: 0,
      timeOfDay01: timeOfDayOverride ?? 0.5,
      dayIndex: 0,
      season,
      weather: effWeather,
      intensity: effIntensity,
      sunrise01,
      sunset01,
    };
  }
  // 批次 33：timeOfDay01 用城市当地时间（UTC ms + 时区偏移；tz=0 时与批次 27 逐分不差）。
  const localMs = cityMs + tzOffsetMin * 60_000;
  const dayMs = localMs - CITY_EPOCH_MS;
  return {
    cityMs,
    timeOfDay01: timeOfDayOverride ?? ((((dayMs % DAY_MS) + DAY_MS) % DAY_MS) / DAY_MS),
    dayIndex: Math.floor((cityMs - CITY_EPOCH_MS) / DAY_MS),
    season,
    weather: effWeather,
    intensity: effIntensity,
    sunrise01,
    sunset01,
  };
}

/**
 * 批次 33：真实城市档案写入（帧到达时由 GamePage 调用；frame==null =
 * 默认城市 → 复位 tz=0 与 6:00/18:00，批次 27 行为零回归）。
 * 日出/日落换算为当地日分数（分钟/1440），驱动 DayNightCycle 太阳升落。
 */
export function setCityGeo(frame: {
  tz_offset_min: number;
  sunrise_min: number;
  sunset_min: number;
} | null): void {
  if (!frame || !Number.isFinite(frame.tz_offset_min)) {
    tzOffsetMin = 0;
    sunrise01 = 0.25;
    sunset01 = 0.75;
    return;
  }
  tzOffsetMin = Math.round(frame.tz_offset_min);
  sunrise01 = Math.min(0.45, Math.max(0.02, frame.sunrise_min / 1440));
  sunset01 = Math.min(0.98, Math.max(0.55, frame.sunset_min / 1440));
}

/** 当前城市时区偏移（分钟；CityClockText 城市当地时间格式化用）。 */
export function currentTzOffsetMin(): number {
  return tzOffsetMin;
}

/** 天气视觉参数查表（null/未知 → 中性参数 = 批次 26 观感）。 */
export function weatherVisual(kind: CityWeatherKind | null | undefined): WeatherVisual {
  if (kind && isWeatherKind(kind)) return WEATHER_VISUALS[kind];
  return NEUTRAL_VISUAL;
}

/** 季节低频订阅（仅 season 变化触发；供贴图/材质换季 setState）。 */
export function subscribeSeason(cb: (s: CitySeason) => void): () => void {
  seasonSubs.add(cb);
  return () => {
    seasonSubs.delete(cb);
  };
}

/** 页面 reset / 离开时清空（避免跨房残留旧锚点与季节）。 */
export function resetCityTime(): void {
  anchor = null;
  season = 'summer';
  weather = null;
  intensity = 0;
  lastWeatherAt = 0;
  dayNight = null;
  tzOffsetMin = 0;
  sunrise01 = 0.25;
  sunset01 = 0.75;
  // 批次84：显示覆盖同样属「本房状态」，换房必须清空。
  timeOfDayOverride = null;
  weatherOverride = null;
  timeSpeedMultiplier = null;
}

// ── 批次84：显示指令覆盖通道（City-WebGL-Show 的 set_time_of_day /
//    set_weather / set_time_speed 的真实落点）────────────────────────

/**
 * 覆盖昼夜时刻（phase 与 hour 二选一；hour 优先）。
 * 返回是否命中（非法相位 / 非有限小时 ⇒ false，调用方记「未支持」提示）。
 */
export function applyDisplayTimeOfDay(phase?: string, hour?: number): boolean {
  let v: number | null = null;
  if (typeof hour === 'number' && Number.isFinite(hour)) {
    v = ((hour % 24) + 24) % 24 / 24;
  } else if (phase && phase in PHASE_TIME_OF_DAY) {
    v = PHASE_TIME_OF_DAY[phase as CityPhaseOverride];
  }
  if (v === null) return false;
  timeOfDayOverride = v;
  return true;
}

/** 覆盖天气（显示指令的 7 种天气 → 游戏侧 8 种天气近似映射）。 */
export function applyDisplayWeather(weather: string): boolean {
  const mapped = DISPLAY_WEATHER_MAP[weather];
  if (!mapped) return false;
  weatherOverride = { ...mapped };
  return true;
}

/** 覆盖时间流速（pause / slow / normal / fast / xfast）。 */
export function applyDisplayTimeSpeed(speed: string): boolean {
  const mul = DISPLAY_SPEED_MAP[speed];
  if (mul === undefined) return false;
  timeSpeedMultiplier = mul;
  // 立即把本地锚点对齐到「当前显示时刻」，之后按覆写倍率自由推进。
  if (anchor) {
    const now = Date.now();
    anchor = {
      cityMs: anchor.cityMs + Math.max(0, now - anchor.at) * anchor.ratio,
      at: now,
      ratio: anchor.ratio,
    };
  }
  return true;
}

/** 清除全部显示覆盖（`clear` 显示指令 / 换房）。 */
export function clearDisplayOverrides(): void {
  timeOfDayOverride = null;
  weatherOverride = null;
  timeSpeedMultiplier = null;
}

/** 供非 React 代码（显示指令适配层）直接调用。 */
export const cityEnvApi = {
  setTimeOfDay: applyDisplayTimeOfDay,
  setWeather: applyDisplayWeather,
  setTimeSpeed: applyDisplayTimeSpeed,
  clear: clearDisplayOverrides,
};

// ?debug=1：暴露最小注入面（§7.4/§7.5 CDP 视觉验收——强制 weather='rain'/'snow'
// / season='winter' 截图；不进生产路径判断之外的任何逻辑）。
if (typeof window !== 'undefined' && window.location.search.includes('debug=1')) {
  (window as unknown as Record<string, unknown>).__cityTimeStore = {
    setCityEnv,
    setCityClockAnchor,
    sample,
  };
}
