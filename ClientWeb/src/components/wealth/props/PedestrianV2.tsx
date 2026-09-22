/**
 * PedestrianV2 — 2.5D 街道行人 V2（15-3D城市全面真实感深化 · 阶段 K）：
 *
 * V1 → V2 升级点：
 *   - V1：Billboard sprite（贴纸感，无体积） + 单圆环原地微抖
 *   - V2：圆柱身体（u(0.18) r × u(1.6) h，体积感）+ 头部 Billboard sprite +
 *        沿折线 path 漫步 + 4 套装色（business/casual/bright/khaki）
 *
 * 折线漫步：
 *   - path: 2-3 段折线（streetLayer 内部生成），每段匀速，行至端点停顿 0.5s 后返程
 *   - 总长 6-10 单位，避免走出城区底板 8×8
 *
 * 降级链（15 轮 §10）：
 *   - 头部 sprite 缺失 → circleGeometry 圆片（outfit 主色）
 *   - outfit 不在枚举 → 默认 'casual'
 *
 * 2026-09-21 高度系统：身体高度 u(1.6) ≈ 真实 1.6m，符合行人尺度。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §2。
 */

import { useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Billboard } from '@react-three/drei';
import { pedestrianHeadUrl, type PedestrianHeadOutfit } from '@/assets/images/wealth';
import { u } from '../cityScale';
import { useSharedTexture } from '../textureCache';

type Outfit = PedestrianHeadOutfit;

interface Props {
  /** path 起点（城区中心附近，世界坐标）。 */
  x: number;
  /** path 起点 z。 */
  z: number;
  /** 折线路径：[(x1,z1), (x2,z2), ...]。每段匀速，端点停顿 0.5s 折返。 */
  path: Array<[number, number]>;
  /** outfit 套装（business/casual/bright/khaki）。 */
  outfit?: Outfit;
  /** 速度（世界单位/秒），默认 0.4。 */
  speed?: number;
  /** 起始相位（决定 path 上初始偏移 0..1）。 */
  phase?: number;
  /** 静止（中央公园看景的行人）。 */
  stationary?: boolean;
}

/** outfit → 身体圆柱颜色（深→浅梯度，避免全部同色）。 */
const OUTFIT_BODY: Record<Outfit, string> = {
  business: '#2a4a6e',  // 深蓝西装
  casual:   '#7a7d82',  // 浅灰休闲
  bright:   '#c8453a',  // 亮红运动
  khaki:    '#8a7a5a',  // 卡其户外
};

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** 身体尺寸（世界单位，1 单位 = 10m）。 */
const BODY_R = u(0.18);  // 圆柱半径 ≈ 0.18m
const BODY_H = u(1.6);   // 圆柱高 ≈ 1.6m（腰→肩）
/** 头部 Billboard 偏移 = 身体顶 + 头高 1/2。 */
const HEAD_Y_OFFSET = BODY_H + u(0.18);  // 头中心 y ≈ 1.78m
const HEAD_W = u(0.32);
const HEAD_H = u(0.32);

/**
 * 求 path 上当前 t（[0..1] 含路径往返）对应的世界坐标。
 * path 总长度已算好；折返用 `forward` 布尔标志切换。
 */
function posAtPath(
  path: Array<[number, number]>,
  t: number,           // [0, 1]
  forward: boolean,    // true = 沿 path 顺序；false = 逆序
): [number, number] {
  // 计算每段长度
  const segs: number[] = [];
  let total = 0;
  for (let i = 1; i < path.length; i++) {
    const dx = path[i][0] - path[i - 1][0];
    const dz = path[i][1] - path[i - 1][1];
    const len = Math.sqrt(dx * dx + dz * dz);
    segs.push(len);
    total += len;
  }
  // 决定真实 t
  const realT = forward ? t : 1 - t;
  let acc = 0;
  for (let i = 0; i < segs.length; i++) {
    const segLen = segs[i] / total;
    if (acc + segLen >= realT || i === segs.length - 1) {
      const localT = (realT - acc) / segLen;
      const a = path[i];
      const b = path[i + 1];
      return [
        a[0] + (b[0] - a[0]) * localT,
        a[1] + (b[1] - a[1]) * localT,
      ];
    }
    acc += segLen;
  }
  return path[path.length - 1];
}

export function PedestrianV2({
  x,
  z,
  path,
  outfit = 'casual',
  speed = 0.4,
  phase = 0,
  stationary = false,
}: Props) {
  const groupRef = useRef<THREE.Group>(null);
  // t 累加 [0..1)；端点停顿时 pausedUntil > now 暂停累加
  const tRef = useRef(phase % 1);
  const forwardRef = useRef(true);
  const pausedUntilRef = useRef(0);

  // 共享缓存：头部 sprite（缺失 → 几何兜底）
  const tex = useSharedTexture(pedestrianHeadUrl(outfit));

  // 计算 path 总长（缓存）
  const totalLen = useMemo(() => {
    let total = 0;
    for (let i = 1; i < path.length; i++) {
      const dx = path[i][0] - path[i - 1][0];
      const dz = path[i][1] - path[i - 1][1];
      total += Math.sqrt(dx * dx + dz * dz);
    }
    return total;
  }, [path]);

  useFrame((_state, now) => {
    const g = groupRef.current;
    if (!g) return;
    if (REDUCED_MOTION || stationary) {
      // 静止：吸附到 (x, z) 不动
      const start = path[0] ?? [x, z];
      g.position.x = start[0];
      g.position.z = start[1];
      g.position.y = 0;
      return;
    }
    // 端点停顿处理
    if (now < pausedUntilRef.current) {
      // 停顿期间仍模拟轻微上下浮动（步态停顿时体重转移）
      g.position.y = Math.abs(Math.sin(now * 0.8)) * u(0.02);
      return;
    }
    // 累加 t（按 path 总长归一化）
    tRef.current += (1 / 60) * speed / totalLen; // 假设 ~60fps 估算 delta
    if (tRef.current >= 1) {
      tRef.current = 0;
      forwardRef.current = !forwardRef.current;
      pausedUntilRef.current = now + 500; // 端点停顿 0.5s
    }
    const [px, pz] = posAtPath(path, tRef.current, forwardRef.current);
    g.position.x = px;
    g.position.z = pz;
    // 步态上下浮动 u(0.04)
    g.position.y = Math.abs(Math.sin(now * 4)) * u(0.04);
  });

  const bodyColor = OUTFIT_BODY[outfit];

  return (
    <group ref={groupRef} position={[x, 0, z]}>
      {/* 身体：圆柱（腰→肩高度 1.6m），站姿贴地 */}
      <mesh position={[0, BODY_H / 2, 0]} castShadow={false}>
        <cylinderGeometry args={[BODY_R, BODY_R * 1.1, BODY_H, 10]} />
        <meshStandardMaterial color={bodyColor} roughness={0.75} metalness={0.05} />
      </mesh>
      {/* 头部 Billboard（始终面向相机），缺失降级到圆片兜底 */}
      <Billboard position={[0, HEAD_Y_OFFSET, 0]}>
        {tex ? (
          <mesh>
            <planeGeometry args={[HEAD_W, HEAD_H]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        ) : (
          <mesh>
            <circleGeometry args={[HEAD_W / 2, 16]} />
            <meshBasicMaterial color={bodyColor} transparent opacity={0.92} />
          </mesh>
        )}
      </Billboard>
    </group>
  );
}

export default PedestrianV2;