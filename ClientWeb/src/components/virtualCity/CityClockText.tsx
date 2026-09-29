/**
 * CityClockText — 顶栏城市时钟 / 运行时长文本（批次 28 A1 拆出）。
 *
 * 拆分动机：原 `VirtualCityGamePage` 持有 `setNow` 250ms 定时器，整页（含
 * `<VirtualCityCityMap>` R3F 场景树）每 250ms 被打回 re-render。本组件把
 * `now` 时钟与 250ms tick 收进叶子组件（`useNowTick`），页面组件不再持有
 * 该 state —— 时钟只有这个小文本块每 250ms 更新，场景树零打扰。
 *
 * 城市时钟语义（批次 25 §3.4，原样搬移）：
 *   显示 = 帧锚点 cityMs + (now − 锚点时刻 at) × speed（运行 60×/time_ratio，暂停 0）；
 *   city_clock_ms 缺失（旧帧/旧后端）→ 兜底现实运行时长 fmtElapsed(game_started_at)。
 */

import { useEffect, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { currentTzOffsetMin } from './cityTimeStore';

/** 帧锚点（页面在帧到达时写入，本组件只读外推）。 */
export interface CityClockAnchor {
  cityMs: number;
  at: number;
  speed: number;
}

/** 轻量 now 订阅：每 `ms` 毫秒刷新一次的当前时间戳（仅本组件 re-render）。 */
export function useNowTick(ms = 250): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), ms);
    return () => window.clearInterval(timer);
  }, [ms]);
  return now;
}

// 现实运行时长 HH:MM:SS —— 批次 25 §3.4 起仅作 city_clock_ms 缺失（旧帧/旧后端）时的兜底显示。
function fmtElapsed(startedAtSec: number, nowMs: number): string {
  if (!startedAtSec) return '--:--';
  const s = Math.max(0, Math.floor(nowMs / 1000) - startedAtSec);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const p = (n: number) => String(n).padStart(2, '0');
  return h > 0 ? `${h}:${p(m)}:${p(sec)}` : `${p(m)}:${p(sec)}`;
}

/** 批次 25 §3.4 城市时钟：时段文案键（6–9 清晨 / 9–17 白天 / 17–20 傍晚 / 20–6 夜晚）。 */
function cityPhaseKey(hour: number): TKey {
  if (hour >= 6 && hour < 9) return 'virtualCity.cityClockPhaseDawn' as TKey;
  if (hour >= 9 && hour < 17) return 'virtualCity.cityClockPhaseDay' as TKey;
  if (hour >= 17 && hour < 20) return 'virtualCity.cityClockPhaseDusk' as TKey;
  return 'virtualCity.cityClockPhaseNight' as TKey;
}

type TranslateFn = (key: TKey, vars?: Record<string, string | number>) => string;

/** 批次 25 §3.4：城市时间 epoch 毫秒 →「M月D日 HH:MM（时段）」。
 *  批次 33：选中真实城市时按城市时区格式化（ms + tzMin 走 UTC getters）；
 *  默认城市（tz=0）沿用浏览器本地格式（与批次 25 逐分不差）。 */
function fmtCityClock(cityMs: number, tzOffsetMin: number, t: TranslateFn): string {
  const d = tzOffsetMin !== 0 ? new Date(cityMs + tzOffsetMin * 60_000) : new Date(cityMs);
  const p = (n: number) => String(n).padStart(2, '0');
  const month = tzOffsetMin !== 0 ? d.getUTCMonth() + 1 : d.getMonth() + 1;
  const day = tzOffsetMin !== 0 ? d.getUTCDate() : d.getDate();
  const hour = tzOffsetMin !== 0 ? d.getUTCHours() : d.getHours();
  const min = tzOffsetMin !== 0 ? d.getUTCMinutes() : d.getMinutes();
  return t('virtualCity.cityClockDisplay' as TKey, {
    month,
    day,
    time: `${p(hour)}:${p(min)}`,
    phase: t(cityPhaseKey(hour)),
  });
}

interface Props {
  /** 城市时钟帧锚点（页面持有并在帧到达时重锚定）。 */
  cityClockRef: React.MutableRefObject<CityClockAnchor | null>;
  /** game_started_at（秒；旧帧兜底运行时长起点）。 */
  startedAt: number;
  /** 帧内 city_clock_ms（缺失/≤0 = 无城市时钟，走兜底）。 */
  cityClockMs: number | undefined;
}

export function CityClockText({ cityClockRef, startedAt, cityClockMs }: Props) {
  const t = useT();
  const now = useNowTick(250);
  const cityClock = cityClockRef.current;
  const hasCityClock =
    cityClock !== null && typeof cityClockMs === 'number' && cityClockMs > 0;
  const clockTitle = hasCityClock
    ? t('virtualCity.cityClock' as TKey)
    : t('virtualCity.runningTime' as TKey);
  const clockText = hasCityClock
    ? `🏙 ${fmtCityClock(cityClock.cityMs + (now - cityClock.at) * cityClock.speed, currentTzOffsetMin(), t)}`
    : `⏱ ${fmtElapsed(startedAt, now)}`;
  return (
    <span className="virtualCity-topbar__item" title={clockTitle}>
      {clockText}
    </span>
  );
}

export default CityClockText;
