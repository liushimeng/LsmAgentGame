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

/** 城市纪元（与后端 cityEpochBaseMs 同源：2025-01-01 08:00 +0800 = UTC 2025-01-01）。 */
export const CITY_EPOCH_MS = Date.UTC(2025, 0, 1);

const DAY_MS = 86_400_000;
/** 天气类型切换迟滞（§3.3 前端平滑：快档城市日 0.16s 不频闪）。 */
const WEATHER_HYSTERESIS_MS = 3000;

export interface CityTimeSample {
  /** 城市时钟毫秒（锚点 + (now − at) × ratio 外推；无锚点 = 0）。 */
  cityMs: number;
  /** 当日时刻 0..1（0.5 = 正午；无锚点 = 0.5 静态正午，视觉零回归）。 */
  timeOfDay01: number;
  /** 城市日序号（自纪元起）。 */
  dayIndex: number;
  season: CitySeason;
  weather: CityWeatherKind | null;
  intensity: number;
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
const seasonSubs = new Set<(s: CitySeason) => void>();

/**
 * 城市时钟锚点（帧到达 / 暂停冻结时由 GamePage 调用）。
 * ratio = 0 表示暂停冻结（连续帧 city_clock_ms 不变 ⇒ 传 0）。
 */
export function setCityClockAnchor(cityMs: number, at: number, ratio: number): void {
  anchor = {
    cityMs,
    at,
    ratio: Number.isFinite(ratio) && ratio > 0 ? ratio : 0,
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
  const cityMs = a ? a.cityMs + Math.max(0, Date.now() - a.at) * a.ratio : 0;
  if (cityMs <= 0) {
    // 无锚点（占位帧/旧后端）：静态正午（timeOfDay01 = 0.5）= 旧版观感。
    return { cityMs: 0, timeOfDay01: 0.5, dayIndex: 0, season, weather, intensity };
  }
  const dayMs = cityMs - CITY_EPOCH_MS;
  return {
    cityMs,
    timeOfDay01: (((dayMs % DAY_MS) + DAY_MS) % DAY_MS) / DAY_MS,
    dayIndex: Math.floor(dayMs / DAY_MS),
    season,
    weather,
    intensity,
  };
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
}

// ?debug=1：暴露最小注入面（§7.4/§7.5 CDP 视觉验收——强制 weather='rain'/'snow'
// / season='winter' 截图；不进生产路径判断之外的任何逻辑）。
if (typeof window !== 'undefined' && window.location.search.includes('debug=1')) {
  (window as unknown as Record<string, unknown>).__cityTimeStore = {
    setCityEnv,
    setCityClockAnchor,
    sample,
  };
}
