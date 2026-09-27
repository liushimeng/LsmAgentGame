/**
 * CityEnvironmentLayer — 城市环境层（批次 27 §4.3，Canvas 内挂载）。
 *
 * 取代 VirtualCityCityMap 原静态正午光 rig（Sky/fog/ambient/hemisphere/
 * 填充光/主方向光/EnvBinder 整段迁入本层）：
 *   - <DayNightCycle>：sample 由 cityTimeStore 派生（timeOfDay01 + §3.3 镜像表
 *     日光/雾系数折算 cloudiness01/sunScale01/fogDensity01）；
 *   - <WeatherFX>：drizzle/storm 强度折算（intensity × particleScale）+ 分天气风力；
 *   - <EnvBinder>：PMREM 一次性烘焙正午 Sky（契约不动），动态部分走
 *     scene.environmentIntensity（DayNightCycle 逐帧调）；
 *   - outRef → setDayNight 转存 cityTimeStore（路灯 emissive 等组件 useFrame 读；
 *     一帧迟滞无观感影响）。
 *
 * 正午基准常量（太阳距离 / 雾近远 / 阴影相机）留在 VirtualCityCityMap（§4.3
 * 「正午基准见常量」），经 props 注入 —— 引擎层不 import 游戏常量。
 *
 * 契约：lag_docs/虚拟城市/已实现/27-时间比例与昼夜季节天气/01-方案设计-v1.md §4.2/§4.3。
 */

import { useCallback, useRef } from 'react';
import { useFrame } from '@react-three/fiber';
import { DayNightCycle, EnvBinder, WeatherFX } from '@/engine3d';
import type { DayNightSnapshot } from '@/engine3d';
import { weatherSpriteUrl } from '@/assets/images/virtualCity';
import { sample, setDayNight, weatherVisual } from './cityTimeStore';
import type { CityWeatherKind } from './cityTimeStore';

/** 分天气水平风力（世界单位/秒；雪受风最明显，暴雨/暴雪整体平推）。 */
const WIND_BY_KIND: Partial<Record<CityWeatherKind, number>> = {
  drizzle: 0.8,
  rain: 1.2,
  storm: 5,
  snow: 1.5,
  blizzard: 7,
};

function clamp01(v: number): number {
  return Math.min(1, Math.max(0, v));
}

interface Props {
  /** 雾近平面（正午基准）。 */
  fogNear: number;
  /** 雾远平面（正午基准）。 */
  fogFar: number;
  /** 主方向光轨道半径（正午基准 SUN_POSITION 模长）。 */
  sunDistance: number;
  /** Sky 太阳方向（正午基准，EnvBinder PMREM 烘焙同源）。 */
  skySunPosition: [number, number, number];
  /** 阴影相机半宽（覆盖全城）。 */
  shadowCameraHalf: number;
  /** 阴影相机远平面。 */
  shadowCameraFar: number;
}

export function CityEnvironmentLayer({
  fogNear,
  fogFar,
  sunDistance,
  skySunPosition,
  shadowCameraHalf,
  shadowCameraFar,
}: Props) {
  const snapRef = useRef<DayNightSnapshot | null>(null);

  // 昼夜采样：timeOfDay01 + §3.3 镜像表折算（无天气数据 = 中性参数零衰减）。
  const dnSample = useCallback(() => {
    const s = sample();
    const vis = weatherVisual(s.weather);
    return {
      timeOfDay01: s.timeOfDay01,
      cloudiness01: s.weather === null ? 0 : vis.cloudOpacity,
      sunScale01: vis.sunScale,
      fogDensity01: vis.fogScale,
    };
  }, []);

  // 粒子采样：强度 × particleScale（drizzle 0.5 / storm 1.2 / blizzard 1.4）。
  const wxSample = useCallback(() => {
    const s = sample();
    const vis = weatherVisual(s.weather);
    return {
      kind: vis.particle,
      intensity01: clamp01(s.intensity * vis.particleScale),
      windX: (s.weather && WIND_BY_KIND[s.weather]) || 0,
    };
  }, []);

  // outRef → cityTimeStore 转存（路灯/窗灯判据 dayFactor01 < 0.25）。
  useFrame(() => {
    setDayNight(snapRef.current);
  });

  return (
    <>
      {/* PMREM 烘焙正午 Sky → scene.environment（一次性；契约见 engine3d/EnvBinder 头注） */}
      <EnvBinder sunPosition={skySunPosition} turbidity={6} rayleigh={1.2} intensity={0.35} />
      <DayNightCycle
        sample={dnSample}
        outRef={snapRef}
        sunDistance={sunDistance}
        fogNear={fogNear}
        fogFar={fogFar}
        shadowCameraHalf={shadowCameraHalf}
        shadowCameraFar={shadowCameraFar}
        envIntensity={0.35}
      />
      {/* 雨滴/雪花 sprite（批次 27 §6.1 art 资产；缺失 → '' → 引擎自绘默认贴图） */}
      <WeatherFX
        sample={wxSample}
        sprite={weatherSpriteUrl('rain_drop') || undefined}
        snowSprite={weatherSpriteUrl('snow_flake') || undefined}
      />
    </>
  );
}

export default CityEnvironmentLayer;
