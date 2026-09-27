/**
 * engine3d/DayNightCycle — 昼夜循环光 rig（游戏无关）。
 *
 * 批次 27「时间比例与昼夜季节天气」抽象入引擎层（原始契约见
 * lag_docs/虚拟城市/已实现/27-时间比例与昼夜季节天气/01-方案设计-v1.md §4.2）：
 * 替代「静态正午」光 rig（drei <Sky> + fog + ambient/hemisphere/填充光/主方向光），
 * 由调用方每帧采样 timeOfDay01 驱动太阳轨迹（6:00 升 / 18:00 落）与全套光照曲线。
 *
 * 契约：
 *   - 分层：只依赖 three / @react-three/fiber / @react-three/drei / 本目录 quality；
 *     **禁止 import 游戏私有模块**（CLAUDE.md §2.1 规则 5）。正午基准数值
 *     （太阳距离 / 雾近远 / 阴影相机半宽）由调用方经 props 注入，引擎只给同值默认。
 *   - sample() 由调用方提供（读模块单例，不触发 React 重渲染——高时间倍率下
 *     城市日可短至 0.16s，逐帧 setState 会卡顿）。
 *   - outRef 每帧写 { dayFactor01, sunElev01 }：游戏组件（路灯 emissive / 窗灯）
 *     判据 dayFactor01 < 0.25 = 夜间。
 *   - 夜间主方向光切月光方向（太阳反相、仰角钳正），避免全黑场景无阴影。
 *   - scene.environmentIntensity 逐帧下调（0.35 基准 × 日光系数）——
 *     **不重烘焙 PMREM**（EnvBinder 一次性烘焙契约不动）。
 *   - 雾由本组件接管 scene.fog 生命周期（挂载创建 / 卸载置 null），逐帧改色与近远。
 */

import { useEffect, useMemo, useRef } from 'react';
import type { MutableRefObject } from 'react';
import { useFrame, useThree } from '@react-three/fiber';
import { Sky } from '@react-three/drei';
import * as THREE from 'three';
import { QUALITY_PRESETS, detectQualityTier } from './quality';
import type { QualityTier } from './quality';

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
}

/** 逐帧输出快照（供路灯 / 窗灯等游戏组件 useFrame 读取）。 */
export interface DayNightSnapshot {
  /** 0 深夜 → 1 白昼（晨昏平滑过渡）。 */
  dayFactor01: number;
  /** 太阳高度 sin 值 -1..1（< 0 = 日落后）。 */
  sunElev01: number;
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
  /** Sky 太阳方向模长（shader 内部 normalize，仅量级习惯；drei 示例 ≈ 100）。 */
  skySunDistance?: number;
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
const FOG_DAY = new THREE.Color('#aeb8c6');
const FOG_NIGHT = new THREE.Color('#0a0e18');
/** 太阳轨道南向偏置（正午方向 ≈ 旧 SUN_POSITION [30,48,24] 的 +z 分量语义）。 */
const SOUTH_TILT = 0.62;

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
  skySunDistance = 100,
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

  // Sky sunPosition uniform：drei 首帧把该 Vector3 实例挂进 uniforms，
  // 之后原地改值即生效（无 React 重渲染）。
  const skySunPos = useMemo(() => new THREE.Vector3(0, 1, SOUTH_TILT), []);
  const tmpColor = useMemo(() => new THREE.Color(), []);

  // 雾生命周期：挂载创建、卸载还原（R3F 场景不留悬挂引用）。
  useEffect(() => {
    const fog = new THREE.Fog(FOG_DAY.getHex(), fogNear, fogFar);
    scene.fog = fog;
    return () => {
      if (scene.fog === fog) scene.fog = null;
    };
  }, [scene, fogNear, fogFar]);

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
    const hours = clamp01(s.timeOfDay01) * 24;
    // 太阳轨迹角：6:00 → 0（日出东方）、12:00 → π/2（正午）、18:00 → π（日落西方）；
    // 夜间 θ 越界 → sin 为负 = 太阳在地平线下。
    const theta = (Math.PI * (hours - 6)) / 12;
    const cosT = Math.cos(theta);
    const sunElev = Math.sin(theta);
    const dayFactor = smoothstep(-0.06, 0.25, sunElev);
    const cloud = clamp01(s.cloudiness01);
    const sunScale = clamp01(s.sunScale01 ?? 1);
    const fogDensity = clamp01(s.fogDensity01 ?? 0);

    // ── 天空：太阳方向（真实仰角，夜间沉入地平线下 → 夜色） ──
    const skyLen = Math.hypot(cosT, sunElev, SOUTH_TILT) || 1;
    skySunPos
      .set(cosT / skyLen, sunElev / skyLen, SOUTH_TILT / skyLen)
      .multiplyScalar(skySunDistance);

    // ── 主方向光：昼走太阳 / 夜走月亮（反相 + 仰角钳正，保住阴影与可读性） ──
    const sun = sunRef.current;
    if (sun) {
      let dx: number, dy: number, dz: number;
      if (sunElev >= 0) {
        dx = cosT;
        dy = Math.max(sunElev, 0.1);
        dz = SOUTH_TILT;
      } else {
        dx = -cosT * 0.8;
        dy = Math.max(-sunElev * 0.65, 0.3);
        dz = -SOUTH_TILT * 0.8;
      }
      const dl = Math.hypot(dx, dy, dz) || 1;
      sun.position.set(
        (dx / dl) * sunDistance,
        (dy / dl) * sunDistance,
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

    // ── 雾：色随昼夜插值，近/远随雾密度系数收拢 ──
    const fog = scene.fog;
    if (fog && (fog as THREE.Fog).isFog) {
      const f = fog as THREE.Fog;
      f.color.copy(FOG_NIGHT).lerp(FOG_DAY, dayFactor);
      f.near = fogNear * (1 - 0.75 * fogDensity);
      f.far = Math.max(f.near + 10, fogFar * (1 - 0.55 * fogDensity));
    }

    // ── 环境反射强度：不重烘焙 PMREM，仅逐帧调 environmentIntensity ──
    scene.environmentIntensity =
      envIntensity * Math.max(0.12, dayFactor * sunScale * (1 - 0.5 * cloud));

    if (outRef) {
      outRef.current = { dayFactor01: dayFactor, sunElev01: sunElev };
    }
  });

  return (
    <>
      {/* 天空穹顶（sunPosition 与主方向光同源方向；turbidity/rayleigh 静态配置） */}
      <Sky sunPosition={skySunPos} turbidity={6} rayleigh={1.2} />
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
