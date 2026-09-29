/**
 * engine3d/DayNightCycle — 昼夜循环光 rig（游戏无关）。
 *
 * 批次 27「时间比例与昼夜季节天气」抽象入引擎层（原始契约见
 * lag_docs/虚拟城市/已实现/27-时间比例与昼夜季节天气/01-方案设计-v1.md §4.2）：
 * 替代「静态正午」光 rig，由调用方每帧采样 timeOfDay01 驱动太阳轨迹（6:00 升 /
 * 18:00 落）与全套光照曲线。
 *
 * 批次 30 B1/B3 改造（审计实测：drei `<Sky>` 夜间天带亮度 229 vs 正午 233 ——
 * 恒白，是「夜景读成白天」的唯一主因）：
 *   - 天空穹换 engine3d/DayNightSky 自写渐变 shader（夜 / 晨 / 昼 / 昏 四段色带
 *     + 星 + 月）；`scene.background` 与 `scene.fog.color` **同一 horizon 色**
 *     同帧写入（同源同色，杜绝白天空 + 深色雾）；
 *   - `renderer.toneMappingExposure` 随昼夜（昼 ≈1.05 → 夜 ≈0.62）——夜景整体
 *     真正变暗而不是只换灯色（楼体/地面维持 40~70 亮度区间，不压死）；
 *   - DayNightSnapshot.sunElev01 死字段删除（全仓零消费者；配色在本组件内部用）。
 *
 * 契约：
 *   - 分层：只依赖 three / @react-three/fiber / 本目录 quality/DayNightSky；
 *     **禁止 import 游戏私有模块**（CLAUDE.md §2.1 规则 5）。正午基准数值
 *     （太阳距离 / 雾近远 / 阴影相机半宽）由调用方经 props 注入。
 *   - sample() 由调用方提供（读模块单例，不触发 React 重渲染）。
 *   - outRef 每帧写 { dayFactor01 }：游戏组件（路灯 emissive / 窗灯 / 车灯）
 *     判据 dayFactor01（<0.25 = 夜间）。
 *   - 夜间主方向光切月光方向（太阳反相、仰角钳正），避免全黑场景无阴影。
 *   - scene.environmentIntensity 逐帧下调（0.35 基准 × 日光系数）——
 *     **不重烘焙 PMREM**（EnvBinder 一次性烘焙契约不动）。
 *   - 雾由本组件接管 scene.fog 生命周期（挂载创建 / 卸载置 null），逐帧改色与近远。
 */

import { useEffect, useMemo, useRef } from 'react';
import type { MutableRefObject } from 'react';
import { useFrame, useThree } from '@react-three/fiber';
import * as THREE from 'three';
import { QUALITY_PRESETS, detectQualityTier } from './quality';
import type { QualityTier } from './quality';
import { DayNightSky } from './DayNightSky';
import type { DayNightSkyState } from './DayNightSky';

/** 采样契约：调用方每帧提供（字段缺省容忍）。 */
export interface DayNightSample {
  /** 当日时刻 0..1（0 = 0:00，0.5 = 正午）。 */
  timeOfDay01: number;
  /** 云量 0..1（遮光系数；无天气数据传 0 = 不衰减）。 */
  cloudiness01: number;
  /** 天气日光系数 0..1（§3.3 镜像表 sunScale；缺省 1）。 */
  sunScale01?: number;
  /** 雾密度系数 0..1（§3.3 镜像表 fogScale；缺省 0）。 */
  fogDensity01?: number;
  /**
   * 季节暖度 0..1（缺省 0.5 中性）：0 = 冷（冬，天带偏冷灰）、1 = 暖（夏/秋）。
   * 批次 30 B4：天空季节色调的唯一入口（引擎不感知"季节"概念）。
   */
  warmth01?: number;
  /**
   * 日出时刻（当地日分数 0..1；缺省 0.25 = 6:00）。批次 33：真实城市
   * 逐月日出日落表驱动（虚拟城市）；缺省保持批次 27 固定 6:00/18:00。
   */
  sunrise01?: number;
  /** 日落时刻（当地日分数 0..1；缺省 0.75 = 18:00）。 */
  sunset01?: number;
}

/** 逐帧输出快照（供路灯 / 窗灯等游戏组件 useFrame 读取）。 */
export interface DayNightSnapshot {
  /** 0 深夜 → 1 白昼（晨昏平滑过渡）。 */
  dayFactor01: number;
}

export interface DayNightCycleProps {
  /** 时间采样（每帧调用，必须廉价）。 */
  sample: () => DayNightSample;
  /** 渲染质量档（缺省自动检测：软件渲染器 → low）。 */
  quality?: QualityTier;
  /** 每帧快照输出 ref。 */
  outRef?: MutableRefObject<DayNightSnapshot | null>;
  /** 主方向光轨道半径（世界单位；正午基准 [30,48,24] 模长 ≈ 61.5）。 */
  sunDistance?: number;
  /** 天空球穹半径（世界单位；须 < camera.far）。 */
  skyRadius?: number;
  /** 雾近平面（晴空正午基准）。 */
  fogNear?: number;
  /** 雾远平面（晴空正午基准）。 */
  fogFar?: number;
  /** 方向光阴影相机半宽（覆盖全城用）。 */
  shadowCameraHalf?: number;
  /** 方向光阴影相机远平面。 */
  shadowCameraFar?: number;
  /** scene.environmentIntensity 白昼基准（与 EnvBinder intensity 同源）。 */
  envIntensity?: number;
}

// ── 颜色曲线常量（方案 §4.2：正午暖白 → 晨昏橙 → 夜冷蓝月光）──────────
const LIGHT_NOON = new THREE.Color('#fff2e0');
const LIGHT_DUSK = new THREE.Color('#ffb27a');
const LIGHT_NIGHT = new THREE.Color('#2a3b5c');
/** 太阳轨道南向偏置（正午方向 ≈ 旧 SUN_POSITION [30,48,24] 的 +z 分量语义）。 */
const SOUTH_TILT = 0.62;

// ── 批次 30 B1：天穹四段色带（夜 / 晨 / 昼 / 昏；horizon 与雾同源）──────────
/** 夜（深蓝；目标天带亮度 ≤60）。 */
const SKY_NIGHT_TOP = new THREE.Color('#0a1428');
const SKY_NIGHT_HORIZON = new THREE.Color('#16294a');
/** 白昼（horizon 与 FOG_DAY #aeb8c6 同值 = 同源锚点）。 */
const SKY_DAY_TOP = new THREE.Color('#4a7fc8');
const SKY_DAY_HORIZON = new THREE.Color('#aeb8c6');
/** 黄昏（暖橙；cosT < 0 = 太阳在西）。实测 #e8956a ⇒ 天带 152 > 目标 120 ⇒ 压暗一档。 */
const SKY_DUSK_TOP = new THREE.Color('#22305a');
const SKY_DUSK_HORIZON = new THREE.Color('#bd7549');
/** 黎明（冷粉；cosT > 0 = 太阳在东）。同上压暗（原 #e8a0b0 ⇒ 170）。 */
const SKY_DAWN_TOP = new THREE.Color('#26385f');
const SKY_DAWN_HORIZON = new THREE.Color('#bd8390');
/** 阴天灰（按云量向其插值）。 */
const SKY_CLOUD_GRAY = new THREE.Color('#6a7482');

function clamp01(v: number): number {
  return Math.min(1, Math.max(0, v));
}

function smoothstep(edge0: number, edge1: number, x: number): number {
  const t = clamp01((x - edge0) / (edge1 - edge0));
  return t * t * (3 - 2 * t);
}

export function DayNightCycle({
  sample,
  quality,
  outRef,
  sunDistance = 61.5,
  skyRadius = 500,
  fogNear = 120,
  fogFar = 240,
  shadowCameraHalf = 60,
  shadowCameraFar = 240,
  envIntensity = 0.35,
}: DayNightCycleProps) {
  const scene = useThree((s) => s.scene);
  const gl = useThree((s) => s.gl);
  const autoTier = useMemo(() => detectQualityTier(gl), [gl]);
  const tier: QualityTier = quality ?? autoTier;
  const shadowMapSize = QUALITY_PRESETS[tier].shadowMapSize;

  const ambientRef = useRef<THREE.AmbientLight>(null);
  const hemiRef = useRef<THREE.HemisphereLight>(null);
  const fillRef = useRef<THREE.DirectionalLight>(null);
  const sunRef = useRef<THREE.DirectionalLight>(null);

  // 天穹逐帧参数（DayNightSky 只读；对象复用零分配）。
  const skyState = useRef<DayNightSkyState | null>(null);
  if (!skyState.current) {
    skyState.current = {
      top: new THREE.Color(),
      horizon: new THREE.Color(),
      sunDir: new THREE.Vector3(0, 1, 0),
      sunGlow: 0,
      stars: 0,
      moonDir: new THREE.Vector3(0, 1, 0),
      moon: 0,
    };
  }
  // scene.background 色板对象（与 fog.color 同值写入）。
  const bg = useMemo(() => new THREE.Color(), []);
  const tmpColor = useMemo(() => new THREE.Color(), []);
  const tmpTop = useMemo(() => new THREE.Color(), []);
  const tmpHor = useMemo(() => new THREE.Color(), []);

  // 雾生命周期：挂载创建、卸载还原（R3F 场景不留悬挂引用）。
  useEffect(() => {
    const fog = new THREE.Fog(bg.getHex(), fogNear, fogFar);
    scene.fog = fog;
    return () => {
      if (scene.fog === fog) scene.fog = null;
    };
  }, [scene, fogNear, fogFar, bg]);

  // ── 批次 28 A3：阴影降频 —— 太阳移动缓慢，按 shadowUpdateFrames（默认每 4 帧
  //    ≈ 15Hz）更新一次阴影贴图；其余帧整个 shadow pass 跳过（autoUpdate=false）。
  //    卸载时还原 autoUpdate=true，不影响无本组件的场景。──
  const shadowFrameRef = useRef(0);
  useEffect(() => {
    gl.shadowMap.autoUpdate = false;
    gl.shadowMap.needsUpdate = true; // 首帧出阴影
    return () => {
      gl.shadowMap.autoUpdate = true;
      gl.shadowMap.needsUpdate = false;
    };
  }, [gl]);

  useFrame(() => {
    // 阴影节流：置 needsUpdate 后 three 在下一次 render 画一次阴影贴图并自动清标记。
    const every = QUALITY_PRESETS[tier].shadowUpdateFrames;
    if (++shadowFrameRef.current >= every) {
      shadowFrameRef.current = 0;
      gl.shadowMap.needsUpdate = true;
    }
    const s = sample();
    // 批次 33：太阳轨迹角按调用方给定的日出/日落时刻映射（缺省 6:00/18:00 =
    // 批次 27 行为逐分不差）：日出 → 0（东方）、正午 → π/2、日落 → π（西方）；
    // 夜间 t 越界 → sin 为负 = 太阳在地平线下。
    const rise = clamp01(s.sunrise01 ?? 0.25);
    const set = Math.max(rise + 0.02, clamp01(s.sunset01 ?? 0.75));
    const tDay = (clamp01(s.timeOfDay01) - rise) / (set - rise);
    const theta = Math.PI * tDay;
    const cosT = Math.cos(theta);
    const sunElev = Math.sin(theta);
    const dayFactor = smoothstep(-0.06, 0.25, sunElev);
    const cloud = clamp01(s.cloudiness01);
    const sunScale = clamp01(s.sunScale01 ?? 1);
    const fogDensity = clamp01(s.fogDensity01 ?? 0);
    const warmth = clamp01(s.warmth01 ?? 0.5);

    // ── 天穹四段色带（夜 → 晨/昏 → 昼；与雾同源 horizon）──
    // night↔twilight：sunElev ∈ [-0.18, 0]；twilight↔day：[0, 0.35]。
    const tw = smoothstep(-0.18, 0, sunElev);
    const dy = smoothstep(0, 0.35, sunElev);
    const isDusk = cosT < 0;
    tmpTop.copy(SKY_NIGHT_TOP).lerp(isDusk ? SKY_DUSK_TOP : SKY_DAWN_TOP, tw);
    tmpHor.copy(SKY_NIGHT_HORIZON).lerp(isDusk ? SKY_DUSK_HORIZON : SKY_DAWN_HORIZON, tw);
    tmpTop.lerp(SKY_DAY_TOP, dy);
    tmpHor.lerp(SKY_DAY_HORIZON, dy);
    // 云量压灰 + 季节暖度微调（horizon ±12% 向暖/冷）。
    // 夜间云压灰系数衰减（实测夜间满云天带 84 > 60 目标：夜云本就更暗）。
    const cloudK = cloud * (0.35 + 0.65 * dayFactor);
    tmpTop.lerp(SKY_CLOUD_GRAY, cloudK * 0.35);
    tmpHor.lerp(SKY_CLOUD_GRAY, cloudK * 0.3);
    if (warmth !== 0.5) {
      tmpHor.lerp(warmth > 0.5 ? LIGHT_DUSK : LIGHT_NIGHT, Math.abs(warmth - 0.5) * 0.24);
    }
    const sky = skyState.current;
    if (sky) {
      sky.top.copy(tmpTop);
      sky.horizon.copy(tmpHor);
      // 太阳方向（真实仰角，夜间沉入地平线下 → 光晕自熄）。
      const skyLen = Math.hypot(cosT, sunElev, SOUTH_TILT) || 1;
      sky.sunDir.set(cosT / skyLen, sunElev / skyLen, SOUTH_TILT / skyLen);
      sky.sunGlow = smoothstep(-0.1, 0.12, sunElev) * (1 - 0.6 * cloud) * sunScale;
      sky.stars = 1 - smoothstep(-0.22, 0.02, sunElev);
      // 月亮：与太阳反相 + 仰角钳正（与主方向光夜向同源）。
      const ml = Math.hypot(-cosT, Math.max(-sunElev * 0.8, 0.25), -SOUTH_TILT) || 1;
      sky.moonDir.set(-cosT / ml, Math.max(-sunElev * 0.8, 0.25) / ml, -SOUTH_TILT / ml);
      sky.moon = sky.stars * (1 - 0.7 * cloud);
    }

    // ── scene.background 与 scene.fog.color 同源同色（horizon）──
    bg.copy(tmpHor);
    scene.background = bg;

    // ── 主方向光：昼走太阳 / 夜走月亮（反相 + 仰角钳正，保住阴影与可读性） ──
    const sun = sunRef.current;
    if (sun) {
      let dx: number, dy2: number, dz: number;
      if (sunElev >= 0) {
        dx = cosT;
        dy2 = Math.max(sunElev, 0.1);
        dz = SOUTH_TILT;
      } else {
        dx = -cosT * 0.8;
        dy2 = Math.max(-sunElev * 0.65, 0.3);
        dz = -SOUTH_TILT * 0.8;
      }
      const dl = Math.hypot(dx, dy2, dz) || 1;
      sun.position.set(
        (dx / dl) * sunDistance,
        (dy2 / dl) * sunDistance,
        (dz / dl) * sunDistance,
      );
      // 颜色/强度曲线：正午 #fff2e0×1.15 → 晨昏 #ffb27a×0.6 → 夜 #2a3b5c×0.12；
      // 天气日光系数 + 云量遮光再作乘。
      const dim = sunScale * (1 - 0.55 * cloud);
      if (sunElev >= 0) {
        const k = smoothstep(0, 0.45, sunElev);
        tmpColor.copy(LIGHT_DUSK).lerp(LIGHT_NOON, k);
        sun.intensity = (0.6 + 0.55 * k) * dim;
      } else {
        const k = smoothstep(-0.25, 0, sunElev);
        tmpColor.copy(LIGHT_NIGHT).lerp(LIGHT_DUSK, k);
        sun.intensity = (0.12 + 0.48 * k) * (1 - 0.4 * cloud);
      }
      sun.color.copy(tmpColor);
    }

    // ── 环境光族：白昼 0.45 ↔ 夜 0.12（hemisphere 0.5 ↔ 0.15，冷填充光微降） ──
    if (ambientRef.current) {
      ambientRef.current.intensity = (0.12 + 0.33 * dayFactor) * (1 - 0.22 * cloud);
    }
    if (hemiRef.current) {
      hemiRef.current.intensity = 0.15 + 0.35 * dayFactor;
    }
    if (fillRef.current) {
      fillRef.current.intensity = 0.18 + 0.12 * dayFactor;
    }

    // ── 雾：色 = 天穹 horizon（同源），近/远随雾密度系数收拢 ──
    const fog = scene.fog;
    if (fog && (fog as THREE.Fog).isFog) {
      const f = fog as THREE.Fog;
      f.color.copy(tmpHor);
      f.near = fogNear * (1 - 0.75 * fogDensity);
      f.far = Math.max(f.near + 10, fogFar * (1 - 0.55 * fogDensity));
    }

    // ── 批次 30 B3：曝光随昼夜（昼 1.05 / 夜 0.62），夜景整体变暗而非只换灯色；
    //    天气日光系数再作乘（暴雨/暴雪进一步压）。──
    gl.toneMappingExposure = (0.62 + 0.43 * dayFactor) * (0.8 + 0.2 * sunScale);

    // ── 环境反射强度：不重烘焙 PMREM，仅逐帧调 environmentIntensity ──
    scene.environmentIntensity =
      envIntensity * Math.max(0.12, dayFactor * sunScale * (1 - 0.5 * cloud));

    if (outRef) {
      outRef.current = { dayFactor01: dayFactor };
    }
  });

  return (
    <>
      {/* 天穹（批次 30 B1：自写渐变 shader + 星 + 月；参数逐帧由 skyState 写入） */}
      <DayNightSky state={skyState} radius={skyRadius} />
      <ambientLight ref={ambientRef} intensity={0.45} />
      {/* 天/地反弹（夜 0.15 ↔ 昼 0.5） */}
      <hemisphereLight ref={hemiRef} args={['#7a93b8', '#1a1f2a', 0.5]} />
      {/* 冷色填充光（背光面抬亮，不投影） */}
      <directionalLight ref={fillRef} position={[-24, 20, -18]} color="#b8cce8" intensity={0.3} />
      {/* 主方向光（昼太阳 / 夜月光；阴影相机覆盖全城，参数由调用方注入） */}
      <directionalLight
        ref={sunRef}
        castShadow
        position={[0, sunDistance, sunDistance * SOUTH_TILT]}
        color="#fff2e0"
        intensity={1.15}
        shadow-mapSize-width={shadowMapSize}
        shadow-mapSize-height={shadowMapSize}
        shadow-bias={-0.0002}
        shadow-normalBias={0.02}
        shadow-camera-left={-shadowCameraHalf}
        shadow-camera-right={shadowCameraHalf}
        shadow-camera-top={shadowCameraHalf}
        shadow-camera-bottom={-shadowCameraHalf}
        shadow-camera-far={shadowCameraFar}
      />
    </>
  );
}

export default DayNightCycle;
