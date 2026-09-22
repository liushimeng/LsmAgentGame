/**
 * StreetLight — 2.5D 街道路灯（P1-C）：
 *
 * 简化几何（缺失贴图时）：底座小圆柱 + 主杆细圆柱 + 灯头小盒子（带 emissive 暖光）。
 * 加载 props/streetlamp/<variant>_streetlamp.png 贴图（透明）后：
 *   - 立杆贴图作为 sprite 朝相机方向显示（Billboard）。
 *   - 几何保留作为骨架（透明贴图遮罩）。
 *
 * 默认 castShadow=false（性能预算；路灯阴影非必要）。
 *
 * 2026-09-21 高度系统：新增 kind 分档 —— 主干道总高 ~1.10（真实 11m），
 * 次干道 ~0.70（真实 7m），基座 / 灯杆 / 灯头尺寸按比例参数化（见 cityScale.ts）。
 */

import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/wealth';
import { useSharedTexture } from '../textureCache';

interface Props {
  x: number;
  z: number;
  /** 朝向（弧度，绕 Y 轴）；通常与所在道路方向一致。 */
  rotation?: number;
  variant?: 'a' | 'b' | 'c';
  /** 'main' = 主干道（11m）；'side' = 次干道（7m）。默认 'main'。 */
  kind?: 'main' | 'side';
}

/** 分档尺寸（世界单位，1 单位 = 10m）。总高 = 基座 + 灯杆 + 灯头。 */
const KIND_DIMS = {
  main: {
    baseH: 0.06, baseRTop: 0.07, baseRBot: 0.10,
    poleH: 0.96, poleRTop: 0.035, poleRBot: 0.05,
    headW: 0.16, headH: 0.08,
    spriteW: 0.30, spriteH: 0.55,
  },
  side: {
    baseH: 0.05, baseRTop: 0.055, baseRBot: 0.08,
    poleH: 0.60, poleRTop: 0.025, poleRBot: 0.04,
    headW: 0.12, headH: 0.05,
    spriteW: 0.20, spriteH: 0.38,
  },
} as const;

export function StreetLight({ x, z, rotation = 0, variant = 'a', kind = 'main' }: Props) {
  // 14-3D渲染深化：共享贴图缓存（路灯阵列同 variant 贴图只上传一次）
  const tex = useSharedTexture(propUrl('streetlamp', variant));
  const d = KIND_DIMS[kind];
  const poleY = d.baseH + d.poleH / 2;
  const headY = d.baseH + d.poleH + d.headH / 2;

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 底座 */}
      <mesh position={[0, d.baseH / 2, 0]}>
        <cylinderGeometry args={[d.baseRTop, d.baseRBot, d.baseH, 8]} />
        <meshStandardMaterial color="#4a4f5a" roughness={0.7} metalness={0.4} />
      </mesh>
      {/* 主杆 */}
      <mesh position={[0, poleY, 0]}>
        <cylinderGeometry args={[d.poleRTop, d.poleRBot, d.poleH, 6]} />
        <meshStandardMaterial color="#6b7280" roughness={0.55} metalness={0.6} />
      </mesh>
      {/* 灯头（emissive 暖光） */}
      <mesh position={[0, headY, 0]}>
        <boxGeometry args={[d.headW, d.headH, d.headW]} />
        <meshStandardMaterial
          color={tex ? '#ffffff' : '#aaa9a0'}
          emissive="#fff5b8"
          emissiveIntensity={0.55}
          roughness={0.4}
        />
      </mesh>
      {/* 灯头顶部贴图 sprite（Billboard 朝相机） */}
      {tex && (
        <Billboard position={[0, headY, 0]}>
          <mesh>
            <planeGeometry args={[d.spriteW, d.spriteH]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      )}
    </group>
  );
}