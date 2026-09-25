/**
 * TreeV2 — 3D 城市场景树木 V2（15-3D城市全面真实感深化 · 阶段 L）：
 *
 * V1 → V2 升级点：
 *   - V1：单 sphere 树冠（贴纸感，平面看是球） / Billboard sprite
 *   - V2：3 层 sphere 叠加树冠（主冠 + 副冠1 + 副冠2 错位），制造蓬松立体感；
 *        颜色深浅叠加（variant 主色 → 亮色 → 暗色），贴近真实树木层次；
 *        仍保留 castShadow（树影是城市感关键）
 *
 * 行道树布点：StreetPropsLayer 内部沿主干道等距布点（错相位 π/2 避开路灯）。
 *
 * 降级链：
 *   - props/tree/<variant>_tree.png 仍可用（Billboard 叠加做树叶装饰），缺失则纯几何
 *
 * 2026-09-21 高度系统：树干 u(0.5)，主冠半径 u(0.4)，符合 5-10m 行道树尺度。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §3。
 */

import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/virtualCity';
import { u } from '../cityScale';
import { useSharedTexture } from '@/engine3d';

type TreeVariant = 'oak' | 'pine' | 'palm';

interface Props {
  x: number;
  z: number;
  variant?: TreeVariant;
  /** 整体缩放（世界单位），默认 0.7；范围 0.5~1.0 ≈ 真实 5~10m。 */
  scale?: number;
}

// variant 主色 → 亮色 / 暗色（3 层叠加）
const CANOPY_COLORS: Record<TreeVariant, { main: string; light: string; dark: string }> = {
  oak:  { main: '#3f7d4d', light: '#5fa86a', dark: '#2c5e3e' },
  pine: { main: '#2c5e3e', light: '#3f7d4d', dark: '#1f4530' },
  palm: { main: '#5fa86a', light: '#7bc285', dark: '#3f7d4d' },
};
const TRUNK_COLOR = '#6b4f32';

/** 主冠/副冠半径（世界单位，按 variant 区分）。 */
const CANOPY_R: Record<TreeVariant, { main: number; sub1: number; sub2: number }> = {
  oak:  { main: u(0.40), sub1: u(0.30), sub2: u(0.25) },
  pine: { main: u(0.35), sub1: u(0.25), sub2: u(0.22) },
  palm: { main: u(0.40), sub1: u(0.30), sub2: u(0.25) },
};
/** 副冠偏移（让 3 层树冠错位产生蓬松感）。 */
const SUB_OFFSETS: Array<[number, number, number]> = [
  [+u(0.10), +u(0.15), 0],
  [-u(0.08), -u(0.05), +u(0.05)],
];

export function TreeV2({ x, z, variant = 'oak', scale = 0.7 }: Props) {
  // 兼容 V1：保留 sprite 叠加（缺失则纯几何）
  const tex = useSharedTexture(propUrl('tree', variant));
  const c = CANOPY_COLORS[variant];
  const r = CANOPY_R[variant];

  return (
    <group position={[x, 0, z]} scale={[scale, scale, scale]}>
      {/* 树干（唯一保留 castShadow 的 prop） */}
      <mesh castShadow position={[0, u(0.25), 0]}>
        <cylinderGeometry args={[u(0.06), u(0.08), u(0.5), 6]} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.85} />
      </mesh>
      {/* 树冠 3 层（主冠 + 副冠1 + 副冠2）*/}
      {/* 主冠 */}
      <mesh position={[0, u(0.7), 0]} castShadow>
        <sphereGeometry args={[r.main, 10, 8]} />
        <meshStandardMaterial color={c.main} roughness={0.85} />
      </mesh>
      {/* 副冠 1（亮色，偏移 +X +Y） */}
      <mesh position={SUB_OFFSETS[0]} castShadow>
        <sphereGeometry args={[r.sub1, 10, 8]} />
        <meshStandardMaterial color={c.light} roughness={0.85} />
      </mesh>
      {/* 副冠 2（暗色，偏移 -X -Y +Z） */}
      <mesh position={SUB_OFFSETS[1]} castShadow>
        <sphereGeometry args={[r.sub2, 10, 8]} />
        <meshStandardMaterial color={c.dark} roughness={0.85} />
      </mesh>
      {/* 树叶 sprite 装饰（兼容 V1 贴图，缺失跳过） */}
      {tex && (
        <Billboard position={[0, u(0.7), 0]}>
          <mesh>
            <planeGeometry args={[u(0.9), u(1.0)]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      )}
    </group>
  );
}

export default TreeV2;