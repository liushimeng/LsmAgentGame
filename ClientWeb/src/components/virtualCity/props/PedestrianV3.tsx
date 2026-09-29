/**
 * PedestrianV3 — 体积行人 + 刚体步态（18-3D城市PBR材质与真实城市冲刺 · 阶段 Z）：
 *
 * V2 → V3 升级点：
 *   - V2：圆柱躯干 + 头部 Billboard sprite（近景仍是「图钉」，无四肢）
 *   - V3：盒体躯干 + 球头 + 双臂双腿（6 mesh / 人），肩/髋 pivot 摆臂摆腿（反相）
 *
 * pivot 技巧（本文件重点）：肢段几何先 `BufferGeometry.translate()` 把顶点整体下移半长，
 * mesh 的 `position` 才是旋转枢轴（肩 y=SHOULDER_Y / 髋 y=LEG_H）——
 * 否则肢体会绕自身中心打转，像风车而不是走路。
 *
 * 批次 29：身高 / 总宽取自 cityScale.REAL_DIMS_M.pedestrian（1.67 m × 0.55 m），
 * 竖向分段与臂间距全部由表值导出 ⇒ 与 characters/pedestrian_walk.glb 包围盒一致
 * （GLB 原点在脚底，见表 minY=0）。
 *
 * 行走：沿 path 折线推进（推进语义复用 PedestrianV2：t 归一化 0..1、端点折返、
 * 端点停顿 0.5s），组朝向 = 行进方向 `Math.atan2(dx, dz)`。
 *
 * REDUCED_MOTION：不摆肢且不位移，吸附 path 起点（与 PedestrianV2 / StreetPropsLayer
 * 既有「全部静止」约定一致）。speed=0 = 站定（中央公园看景），同样不摆肢。
 *
 * 契约：lag_docs/虚拟城市/已实现/18-3D城市PBR材质与真实城市冲刺/02-架构设计 §4.1。
 */

import { memo, useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { u, worldDims, sizeTargetFor, DISTRICT_SURFACE_Y } from '../cityScale';
import { useSharedGLTF, blenderModelsEnabled } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';
import type { CrowdAppearance } from '../crowdFormula';

export interface PedestrianV3Props {
  /** 漫步路径折线（世界坐标 x,z；首尾不闭合，到端点折返）。 */
  path: Array<[number, number]>;
  /** 步速（世界单位/秒；批次 30 P1-12 真实步速 1.2~1.5 m/s ⇒ 0.12~0.15，默认 1.4 m/s）；
   *  0 = 站定（中央公园看景）。 */
  speed?: number;
  /** 0..3 四套服装色（商务蓝 / 休闲灰 / 亮色红 / 卡其）。 */
  outfit?: 0 | 1 | 2 | 3;
  /** 起始相位（0..1，错开步态与 path 初值，避免全员同步摆腿）。 */
  phase?: number;
  /**
   * 批次 34 §5.3：居民外观投影（原型 + 色板 + 身高系数）。
   * 传入时优先取 `characters/<archetype>.glb` 并按材质槽换色；
   * 缺省则回落到 `outfit` 四套服装色 + 程序化几何。
   */
  appearance?: CrowdAppearance;
}

/**
 * 行人尺寸 = cityScale.REAL_DIMS_M.pedestrian（W0.55 × H1.67 × D0.35，米）。
 * 批次 29：**竖向分段与横向手臂间距全部由表值导出**（此前是散落的 u(1.67)/u(0.82)
 * 等硬编码）；与 characters/pedestrian_walk.glb 同行表值。
 * 各分段比例 = 原 0.82/0.58/0.12/0.52/0.09 m 除以旧总高 1.67 m（视觉不变）。
 */
const PED = worldDims('pedestrian');
/** 总高（表值）—— 头顶 = 总高，脚底 = 0。 */
const PED_H = PED.y;
/** 腿长 = 髋高（脚端 y = 髋高 − 腿长 = 0 贴地）。 */
const LEG_H = PED_H * 0.491;
/** 躯干高（跨度 髋高 → 肩下方）。 */
const TORSO_H = PED_H * 0.347;
/** 手臂长（肩枢轴向下）。 */
const ARM_H = PED_H * 0.311;
/** 头半径；头心 = 总高 − 半径（头顶恰为总高）。 */
const HEAD_R = PED_H * 0.0719;
/** 臂宽（构图常量，用于反推臂间距使总宽 = 表值 x）。 */
const ARM_W = u(0.09);
/** 肩枢轴高度 = 髋高 + 臂长。 */
const SHOULDER_Y = LEG_H + ARM_H;
/** 头心高度。 */
const HEAD_Y = PED_H - HEAD_R;
/** 躯干心高度 = 髋高 + 半个躯干。 */
const TORSO_Y = LEG_H + TORSO_H / 2;
/** GLB 尺寸/落地校验目标（dev 态；pedestrian_walk 原点在脚底，minY=0）。 */
const PED_SIZE_TARGET = sizeTargetFor('pedestrian', { label: 'characters/pedestrian_walk' });

/** [上衣, 裤, 肤] × 4 套（商务蓝 / 休闲灰 / 亮色红 / 卡其）。 */
export const PEDESTRIAN_OUTFITS: [string, string, string][] = [
  ['#2f4f7a', '#2a3340', '#e8c39a'], // 商务蓝：深蓝西装 + 深裤
  ['#8a8f98', '#5c626b', '#e0b48c'], // 休闲灰
  ['#d64541', '#3d4a5c', '#f0c9a0'], // 亮色红
  ['#a08a5c', '#6e5b3e', '#d8a878'], // 卡其
];

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** 肩/髋枢轴 x 偏移（臂贴躯干 u(0.32) 宽两侧；臂间距由表值总宽反推 → 总宽恰为 0.55 m）。 */
const ARM_X = PED.x / 2 - ARM_W / 2;
const LEG_X = u(0.07);
/** 摆臂/摆腿振幅（rad）。 */
const ARM_SWING = 0.35;
const LEG_SWING = 0.45;
/**
 * 步态角频率系数（rad/s per 单位步速）：ω = GAIT_OMEGA × speed。
 * 批次 30 P1-12：speed 切到真实 1.4 m/s（0.14 世界单位/s）后，原 12 ⇒ 0.27 Hz
 * 「慢动作踏步」——按步幅 ≈1.4 m/周期 反推 ω = speed×2π/1.4 ⇒ 系数 45，
 * 正常步速 ≈ 1 Hz 周期（2 步/秒）。
 */
const GAIT_OMEGA = 45;

/**
 * 求 path 上 t（0..1）处的位置与行进方向（forward=false 时逆序折返）。
 * 推进语义与 PedestrianV2.posAtPath 同源：按段长归一化插值。
 * 批次 28 A4：segs/total 由调用方 useMemo 预计算（原每帧重建 segs[] 数组，
 * 103 行人 × 60fps = GC churn），路径不变则零分配。
 */
function samplePath(
  path: Array<[number, number]>,
  segs: number[],
  total: number,
  t: number,
  forward: boolean,
): { x: number; z: number; dx: number; dz: number } {
  if (path.length === 0) return { x: 0, z: 0, dx: 0, dz: 1 };
  if (path.length === 1) return { x: path[0][0], z: path[0][1], dx: 0, dz: 1 };
  const realT = forward ? t : 1 - t;
  let acc = 0;
  for (let i = 0; i < segs.length; i++) {
    const segLen = segs[i] / total;
    if (acc + segLen >= realT || i === segs.length - 1) {
      const localT = Math.min(1, Math.max(0, (realT - acc) / segLen));
      const a = path[i];
      const b = path[i + 1];
      let dx = b[0] - a[0];
      let dz = b[1] - a[1];
      const len = Math.sqrt(dx * dx + dz * dz) || 1;
      dx /= len;
      dz /= len;
      if (!forward) {
        dx = -dx;
        dz = -dz;
      }
      return {
        x: a[0] + (b[0] - a[0]) * localT,
        z: a[1] + (b[1] - a[1]) * localT,
        dx,
        dz,
      };
    }
    acc += segLen;
  }
  const last = path[path.length - 1];
  return { x: last[0], z: last[1], dx: 0, dz: 1 };
}

// 批次 28 A1：memo —— path（layout useMemo）/ speed / outfit / phase 全为稳定引用。
export const PedestrianV3 = memo(function PedestrianV3({
  path,
  speed = u(1.4),
  outfit = 0,
  phase = 0,
  appearance,
}: PedestrianV3Props) {
  // 19-Blender3D模型集成：.glb 模式优先级最高，绕过原 6 mesh + 摆臂逻辑。
  // 批次 34 §5.3：有 appearance 时取对应原型 GLB；原型 GLB 缺失 → 退回 pedestrian_walk
  // （两者同骨架/同步态 clip，walk 动画仍成立）；再缺 → 程序化几何。
  const archetype = appearance?.archetype;
  const modelUrlStr = (archetype ? modelUrl('characters', archetype) : '')
    || modelUrl('characters', 'pedestrian_walk');
  const blenderOn = blenderModelsEnabled();
  const useGLB = !!modelUrlStr && blenderOn;
  void useGLB; // 标记保留：未来 v19.5 通过此 flag 控制 GLB vs 程序化几何 fallback
  // 批次 29：注册目标尺寸（dev 态 glbSizeGuard 量测 GLB 直立性/脚底贴地/总高）
  const { scene: glbScene, animations } = useSharedGLTF(modelUrlStr, PED_SIZE_TARGET);

  /**
   * 批次 34 §5.3：**克隆 + 换色必须同处一个 useMemo**。
   * `Object3D.clone(true)` **共享材质引用** —— 直接改 `material.color` 会污染
   * 同 URL 的所有行人（全城一起变色）。故先为每个 mesh 克隆材质，再改色；
   * 克隆出来的材质在 cleanup 里 dispose，否则换批居民时泄漏显存。
   */
  const glbCloned = useMemo(() => {
    if (!glbScene) return null;
    const c = glbScene.clone(true);
    if (appearance) {
      const tinted: THREE.Material[] = [];
      c.traverse((obj) => {
        const mesh = obj as THREE.Mesh;
        if (!mesh.isMesh) return;
        const apply = (src: THREE.Material): THREE.Material => {
          const m = src.clone();
          tinted.push(m);
          const name = m.name || '';
          const std = m as THREE.MeshStandardMaterial;
          if (std.color) {
            // 材质槽名约定（3d_script/build_character.py 固定）：
            // PedestrianBody / PedestrianPants / PedestrianHead / PedestrianShoes
            if (name.includes('Body')) std.color.set(appearance.top);
            else if (name.includes('Pants')) std.color.set(appearance.pants);
            else if (name.includes('Head')) std.color.set(appearance.skin);
            else if (name.includes('Shoes')) std.color.set('#1a1a1a');
            else std.color.set(appearance.top);
          }
          return m;
        };
        mesh.material = Array.isArray(mesh.material)
          ? mesh.material.map(apply)
          : apply(mesh.material);
      });
      c.userData.__tintedMats = tinted;
    }
    return c;
  }, [glbScene, appearance]);

  useEffect(() => () => {
    const mats = glbCloned?.userData.__tintedMats as THREE.Material[] | undefined;
    if (mats) for (const m of mats) m.dispose();
  }, [glbCloned]);
  // GLB 模式 mixer（per-instance，独立推进 walk clip）
  const mixerRef = useRef<THREE.AnimationMixer | null>(null);
  useEffect(() => {
    if (!glbCloned || animations.length === 0) return;
    const m = new THREE.AnimationMixer(glbCloned);
    const action = m.clipAction(animations[0]);
    action.play();
    mixerRef.current = m;
    return () => {
      m.stopAllAction();
      m.uncacheRoot(glbCloned);
      mixerRef.current = null;
    };
  }, [glbCloned, animations]);

  const groupRef = useRef<THREE.Group>(null);
  const armLRef = useRef<THREE.Mesh>(null);
  const armRRef = useRef<THREE.Mesh>(null);
  const legLRef = useRef<THREE.Mesh>(null);
  const legRRef = useRef<THREE.Mesh>(null);
  // t 累加 [0..1)；端点停顿以「累计秒」记账（不能拿 delta 当绝对时间——V2 的坑）
  const tRef = useRef(((phase % 1) + 1) % 1);
  const forwardRef = useRef(true);
  const pauseUntilRef = useRef(0);
  const elapsedRef = useRef(0);
  const gaitRef = useRef((phase % 1) * Math.PI * 2);

  // ── 几何（useMemo + 卸载 dispose）─────────────────────────────────
  // 尺寸自洽（批次 29：全部由 REAL_DIMS_M.pedestrian 导出）：腿长 = 髋高 ⇒ 脚端 y = 0 贴地；
  // 头顶 = 总高 1.67 m；臂间距由总宽反推 ⇒ 总宽 0.55 m。深度方向 0.18 m 为躯干厚度
  // （表值 z=0.35 是行走步幅包络，程序化几何不摆步幅）。
  const torsoGeo = useMemo(() => new THREE.BoxGeometry(u(0.32), TORSO_H, u(0.18)), []);
  const headGeo = useMemo(() => new THREE.SphereGeometry(HEAD_R, 12, 10), []);
  // 臂/腿：translate 把顶点下移半长 → mesh.position 即肩/髋枢轴（见文件头注释）
  const armGeo = useMemo(() => {
    const g = new THREE.BoxGeometry(ARM_W, ARM_H, ARM_W);
    g.translate(0, -ARM_H / 2, 0);
    return g;
  }, []);
  const legGeo = useMemo(() => {
    const g = new THREE.BoxGeometry(u(0.11), LEG_H, u(0.11));
    g.translate(0, -LEG_H / 2, 0);
    return g;
  }, []);
  useEffect(() => () => torsoGeo.dispose(), [torsoGeo]);
  useEffect(() => () => headGeo.dispose(), [headGeo]);
  useEffect(() => () => armGeo.dispose(), [armGeo]);
  useEffect(() => () => legGeo.dispose(), [legGeo]);

  // path 段长表 + 总长（批次 28 A4：useMemo 预计算，samplePath 不再逐帧重建 segs[]）
  const { segs, totalLen } = useMemo(() => {
    const s: number[] = [];
    let total = 0;
    for (let i = 1; i < path.length; i++) {
      const dx = path[i][0] - path[i - 1][0];
      const dz = path[i][1] - path[i - 1][1];
      const len = Math.sqrt(dx * dx + dz * dz) || 1e-6;
      s.push(len);
      total += len;
    }
    return { segs: s, totalLen: total || 1 };
  }, [path]);

  const [outfitTop, outfitPants, outfitSkin] = PEDESTRIAN_OUTFITS[outfit] ?? PEDESTRIAN_OUTFITS[0];
  // 批次 34：有 appearance 时用居民专属色板，否则回落到 outfit 四套。
  const topColor = appearance?.top ?? outfitTop;
  const pantsColor = appearance?.pants ?? outfitPants;
  const skinColor = appearance?.skin ?? outfitSkin;

  /** 四肢归零（站立/静止）。 */
  const stillLimbs = () => {
    if (armLRef.current) armLRef.current.rotation.x = 0;
    if (armRRef.current) armRRef.current.rotation.x = 0;
    if (legLRef.current) legLRef.current.rotation.x = 0;
    if (legRRef.current) legRRef.current.rotation.x = 0;
  };

  /** 批次 28 A4：动画/位移更新节流 ~30Hz（acc 合并 delta，隔帧零工作）。 */
  const stepAccRef = useRef(0);

  useFrame((_state, delta) => {
    // 批次 28 A4：节流 —— 帧间隔不足 1/30s 时累积 delta 直接返回；
    // mixer / 位移 / 摆臂统一按合并后的 step 推进（推进量守恒，时序不变）。
    stepAccRef.current += delta;
    if (stepAccRef.current < 1 / 30) return;
    const step = stepAccRef.current;
    stepAccRef.current = 0;
    // 19-Blender3D模型集成：GLB 模式 mixer 推进 walk clip（与 path 推进 / 摆臂逻辑并行）
    mixerRef.current?.update(step);
    const g = groupRef.current;
    if (!g) return;
    // 全静止：吸附 path 起点 + 四肢归零（与 V2 REDUCED_MOTION 行为一致）
    if (REDUCED_MOTION) {
      const start = path[0] ?? [0, 0];
      g.position.x = start[0];
      g.position.z = start[1];
      g.position.y = DISTRICT_SURFACE_Y;
      stillLimbs();
      return;
    }
    // 站定（中央公园看景）：位置停在相位处，不摆肢
    if (speed <= 0) {
      const p = samplePath(path, segs, totalLen, tRef.current, forwardRef.current);
      g.position.x = p.x;
      g.position.z = p.z;
      stillLimbs();
      return;
    }
    elapsedRef.current += step;
    // 端点停顿：折返前停 0.5s（站立）
    if (elapsedRef.current < pauseUntilRef.current) {
      stillLimbs();
      return;
    }
    tRef.current += (step * speed) / totalLen;
    if (tRef.current >= 1) {
      tRef.current -= 1;
      forwardRef.current = !forwardRef.current;
      pauseUntilRef.current = elapsedRef.current + 0.5;
    }
    const p = samplePath(path, segs, totalLen, tRef.current, forwardRef.current);
    g.position.x = p.x;
    g.position.z = p.z;
    // 步态沉浮（沿 V2 幅度 u(0.04)；批次 30 P0-6：基线从 y=0 → DISTRICT_SURFACE_Y
    // —— 旧基线把行人脚底压进路面 15~35 cm）
    gaitRef.current += step * GAIT_OMEGA * speed;
    g.position.y = DISTRICT_SURFACE_Y + Math.abs(Math.sin(gaitRef.current * 2)) * u(0.04);
    g.rotation.y = Math.atan2(p.dx, p.dz);
    // 摆肢反相：左臂 + 左腿反相、右半身再 +π
    const s = Math.sin(gaitRef.current);
    if (armLRef.current) armLRef.current.rotation.x = s * ARM_SWING;
    if (armRRef.current) armRRef.current.rotation.x = -s * ARM_SWING;
    if (legLRef.current) legLRef.current.rotation.x = -s * LEG_SWING;
    if (legRRef.current) legRRef.current.rotation.x = s * LEG_SWING;
  });

  const start = path[0] ?? [0, 0];
  // anchorY：悬浮卡相对命中点的上浮（批次 29 起按表值身高导出，与 Vehicle 同式）
  const info = useObjectInfoProps('actor.pedestrian', { anchorY: PED_H + 0.4 });

  return (
    <group
      {...info}
      ref={groupRef}
      userData={{ bucket: 'pedestrians' }}
      position={[start[0], DISTRICT_SURFACE_Y, start[1]]}
      // 批次 34 §5.2：原型身高系数（elder 略矮驼 / student 略高瘦，±8% 内）
      scale={appearance?.scale ?? 1}
    >
      {/* 19-Blender3D模型集成：GLB 模式优先级最高（GLB 自带摆臂动画） */}
      {glbCloned ? (
        <primitive object={glbCloned} />
      ) : (
        <>
          {/* 躯干（上衣色；跨度 髋高 → 肩） */}
          <mesh geometry={torsoGeo} position={[0, TORSO_Y, 0]}>
            <meshStandardMaterial color={topColor} roughness={0.75} metalness={0.05} />
          </mesh>
          {/* 头（肤色；头顶 = 总高 1.67 m） */}
          <mesh geometry={headGeo} position={[0, HEAD_Y, 0]}>
            <meshStandardMaterial color={skinColor} roughness={0.7} metalness={0.02} />
          </mesh>
          {/* 左/右臂（上衣色；pivot 落肩，手端垂到髋高） */}
          <mesh ref={armLRef} geometry={armGeo} position={[-ARM_X, SHOULDER_Y, 0]}>
            <meshStandardMaterial color={topColor} roughness={0.75} metalness={0.05} />
          </mesh>
          <mesh ref={armRRef} geometry={armGeo} position={[ARM_X, SHOULDER_Y, 0]}>
            <meshStandardMaterial color={topColor} roughness={0.75} metalness={0.05} />
          </mesh>
          {/* 左/右腿（裤色；pivot 落髋 = 腿长，脚端 y = 0 贴地） */}
          <mesh ref={legLRef} geometry={legGeo} position={[-LEG_X, LEG_H, 0]}>
            <meshStandardMaterial color={pantsColor} roughness={0.8} metalness={0.03} />
          </mesh>
          <mesh ref={legRRef} geometry={legGeo} position={[LEG_X, LEG_H, 0]}>
            <meshStandardMaterial color={pantsColor} roughness={0.8} metalness={0.03} />
          </mesh>
        </>
      )}
    </group>
  );
});

export default PedestrianV3;
