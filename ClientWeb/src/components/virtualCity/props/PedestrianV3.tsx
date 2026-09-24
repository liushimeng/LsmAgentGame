/**
 * PedestrianV3 — 体积行人 + 刚体步态（18-3D城市PBR材质与真实城市冲刺 · 阶段 Z）：
 *
 * V2 → V3 升级点：
 *   - V2：圆柱躯干 + 头部 Billboard sprite（近景仍是「图钉」，无四肢）
 *   - V3：盒体躯干 + 球头 + 双臂双腿（6 mesh / 人），肩/髋 pivot 摆臂摆腿（反相）
 *
 * pivot 技巧（本文件重点）：肢段几何先 `BufferGeometry.translate()` 把顶点整体下移半长，
 * mesh 的 `position` 才是旋转枢轴（肩 y=u(1.34) / 髋 y=u(0.82)）——
 * 否则肢体会绕自身中心打转，像风车而不是走路。
 *
 * 行走：沿 path 折线推进（推进语义复用 PedestrianV2：t 归一化 0..1、端点折返、
 * 端点停顿 0.5s），组朝向 = 行进方向 `Math.atan2(dx, dz)`。
 *
 * REDUCED_MOTION：不摆肢且不位移，吸附 path 起点（与 PedestrianV2 / StreetPropsLayer
 * 既有「全部静止」约定一致）。speed=0 = 站定（中央公园看景），同样不摆肢。
 *
 * 契约：lag_docs/虚拟城市/已实现/18-3D城市PBR材质与真实城市冲刺/02-架构设计 §4.1。
 */

import { useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { u } from '../cityScale';
import { useSharedGLTF } from '../modelCache';
import { modelUrl } from '@/assets/models';

export interface PedestrianV3Props {
  /** 漫步路径折线（世界坐标 x,z；首尾不闭合，到端点折返）。 */
  path: Array<[number, number]>;
  /** 步速（世界单位/秒，默认 0.55）；0 = 站定（中央公园看景）。 */
  speed?: number;
  /** 0..3 四套服装色（商务蓝 / 休闲灰 / 亮色红 / 卡其）。 */
  outfit?: 0 | 1 | 2 | 3;
  /** 起始相位（0..1，错开步态与 path 初值，避免全员同步摆腿）。 */
  phase?: number;
}

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

/** 肩/髋枢轴 x 偏移（臂/腿贴躯干 u(0.32) 宽两侧）。 */
const ARM_X = u(0.20);
const LEG_X = u(0.07);
/** 摆臂/摆腿振幅（rad）。 */
const ARM_SWING = 0.35;
const LEG_SWING = 0.45;
/** 步态角频率系数（rad/s per 单位步速）：正常步速 ≈ 1.2 步频。 */
const GAIT_OMEGA = 12;

/**
 * 求 path 上 t（0..1）处的位置与行进方向（forward=false 时逆序折返）。
 * 推进语义与 PedestrianV2.posAtPath 同源：按段长归一化插值。
 */
function samplePath(
  path: Array<[number, number]>,
  t: number,
  forward: boolean,
): { x: number; z: number; dx: number; dz: number } {
  if (path.length === 0) return { x: 0, z: 0, dx: 0, dz: 1 };
  if (path.length === 1) return { x: path[0][0], z: path[0][1], dx: 0, dz: 1 };
  const segs: number[] = [];
  let total = 0;
  for (let i = 1; i < path.length; i++) {
    const dx = path[i][0] - path[i - 1][0];
    const dz = path[i][1] - path[i - 1][1];
    const len = Math.sqrt(dx * dx + dz * dz) || 1e-6;
    segs.push(len);
    total += len;
  }
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

export function PedestrianV3({ path, speed = 0.55, outfit = 0, phase = 0 }: PedestrianV3Props) {
  // 19-Blender3D模型集成：.glb 模式优先级最高，绕过原 6 mesh + 摆臂逻辑
  const modelUrlStr = modelUrl('characters', 'pedestrian_walk');
  const blenderOn = typeof window === 'undefined' ||
    window.localStorage.getItem('disable-blender-models') !== '1';
  const useGLB = !!modelUrlStr && blenderOn;
  void useGLB; // 标记保留：未来 v19.5 通过此 flag 控制 GLB vs 程序化几何 fallback
  const { scene: glbScene, animations } = useSharedGLTF(modelUrlStr);
  const glbCloned = useMemo(() => (glbScene ? glbScene.clone(true) : null), [glbScene]);
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
  // 尺寸自洽：腿长 = 髋 y，脚端才贴地（y=0）；躯干中心 y = (髋 y + 肩 y)/2；头中心 y = 肩 y + u(0.21)。
  // （02-架构 §4.1 修正版：脚端 y = u(0.82) − u(0.82) = 0 贴地，总高 u(1.67)）
  const torsoGeo = useMemo(() => new THREE.BoxGeometry(u(0.32), u(0.58), u(0.18)), []);
  const headGeo = useMemo(() => new THREE.SphereGeometry(u(0.12), 12, 10), []);
  // 臂/腿：translate 把顶点下移半长 → mesh.position 即肩/髋枢轴（见文件头注释）
  const armGeo = useMemo(() => {
    const g = new THREE.BoxGeometry(u(0.09), u(0.52), u(0.09));
    g.translate(0, -u(0.26), 0);
    return g;
  }, []);
  const legGeo = useMemo(() => {
    const g = new THREE.BoxGeometry(u(0.11), u(0.82), u(0.11));
    g.translate(0, -u(0.41), 0);
    return g;
  }, []);
  useEffect(() => () => torsoGeo.dispose(), [torsoGeo]);
  useEffect(() => () => headGeo.dispose(), [headGeo]);
  useEffect(() => () => armGeo.dispose(), [armGeo]);
  useEffect(() => () => legGeo.dispose(), [legGeo]);

  // path 总长（推进归一化）
  const totalLen = useMemo(() => {
    let total = 0;
    for (let i = 1; i < path.length; i++) {
      const dx = path[i][0] - path[i - 1][0];
      const dz = path[i][1] - path[i - 1][1];
      total += Math.sqrt(dx * dx + dz * dz);
    }
    return total || 1;
  }, [path]);

  const [topColor, pantsColor, skinColor] = PEDESTRIAN_OUTFITS[outfit] ?? PEDESTRIAN_OUTFITS[0];

  /** 四肢归零（站立/静止）。 */
  const stillLimbs = () => {
    if (armLRef.current) armLRef.current.rotation.x = 0;
    if (armRRef.current) armRRef.current.rotation.x = 0;
    if (legLRef.current) legLRef.current.rotation.x = 0;
    if (legRRef.current) legRRef.current.rotation.x = 0;
  };

  useFrame((_state, delta) => {
    // 19-Blender3D模型集成：GLB 模式 mixer 推进 walk clip（与 path 推进 / 摆臂逻辑并行）
    mixerRef.current?.update(delta);
    const g = groupRef.current;
    if (!g) return;
    // 全静止：吸附 path 起点 + 四肢归零（与 V2 REDUCED_MOTION 行为一致）
    if (REDUCED_MOTION) {
      const start = path[0] ?? [0, 0];
      g.position.x = start[0];
      g.position.z = start[1];
      g.position.y = 0;
      stillLimbs();
      return;
    }
    // 站定（中央公园看景）：位置停在相位处，不摆肢
    if (speed <= 0) {
      const p = samplePath(path, tRef.current, forwardRef.current);
      g.position.x = p.x;
      g.position.z = p.z;
      stillLimbs();
      return;
    }
    elapsedRef.current += delta;
    // 端点停顿：折返前停 0.5s（站立）
    if (elapsedRef.current < pauseUntilRef.current) {
      stillLimbs();
      return;
    }
    tRef.current += (delta * speed) / totalLen;
    if (tRef.current >= 1) {
      tRef.current -= 1;
      forwardRef.current = !forwardRef.current;
      pauseUntilRef.current = elapsedRef.current + 0.5;
    }
    const p = samplePath(path, tRef.current, forwardRef.current);
    g.position.x = p.x;
    g.position.z = p.z;
    // 步态沉浮（沿 V2 幅度 u(0.04)）
    g.position.y = Math.abs(Math.sin(elapsedRef.current * 4)) * u(0.04);
    g.rotation.y = Math.atan2(p.dx, p.dz);
    // 摆肢反相：左臂 + 左腿反相、右半身再 +π
    gaitRef.current += delta * GAIT_OMEGA * speed;
    const s = Math.sin(gaitRef.current);
    if (armLRef.current) armLRef.current.rotation.x = s * ARM_SWING;
    if (armRRef.current) armRRef.current.rotation.x = -s * ARM_SWING;
    if (legLRef.current) legLRef.current.rotation.x = -s * LEG_SWING;
    if (legRRef.current) legRRef.current.rotation.x = s * LEG_SWING;
  });

  const start = path[0] ?? [0, 0];

  return (
    <group ref={groupRef} position={[start[0], 0, start[1]]}>
      {/* 19-Blender3D模型集成：GLB 模式优先级最高（GLB 自带摆臂动画） */}
      {glbCloned ? (
        <primitive object={glbCloned} />
      ) : (
        <>
          {/* 躯干（上衣色；跨度 u(0.82)–u(1.40)） */}
          <mesh geometry={torsoGeo} position={[0, u(1.11), 0]}>
            <meshStandardMaterial color={topColor} roughness={0.75} metalness={0.05} />
          </mesh>
          {/* 头（肤色；跨度 u(1.43)–u(1.67)） */}
          <mesh geometry={headGeo} position={[0, u(1.55), 0]}>
            <meshStandardMaterial color={skinColor} roughness={0.7} metalness={0.02} />
          </mesh>
          {/* 左/右臂（上衣色；pivot 落肩 y=u(1.34)，手端垂到 u(0.82)） */}
          <mesh ref={armLRef} geometry={armGeo} position={[-ARM_X, u(1.34), 0]}>
            <meshStandardMaterial color={topColor} roughness={0.75} metalness={0.05} />
          </mesh>
          <mesh ref={armRRef} geometry={armGeo} position={[ARM_X, u(1.34), 0]}>
            <meshStandardMaterial color={topColor} roughness={0.75} metalness={0.05} />
          </mesh>
          {/* 左/右腿（裤色；pivot 落髋 y=u(0.82)，脚端 y=u(0.82)−u(0.82)=0 贴地） */}
          <mesh ref={legLRef} geometry={legGeo} position={[-LEG_X, u(0.82), 0]}>
            <meshStandardMaterial color={pantsColor} roughness={0.8} metalness={0.03} />
          </mesh>
          <mesh ref={legRRef} geometry={legGeo} position={[LEG_X, u(0.82), 0]}>
            <meshStandardMaterial color={pantsColor} roughness={0.8} metalness={0.03} />
          </mesh>
        </>
      )}
    </group>
  );
}

export default PedestrianV3;
