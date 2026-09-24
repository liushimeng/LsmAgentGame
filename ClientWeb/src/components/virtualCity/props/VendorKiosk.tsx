/**
 * VendorKiosk — 报刊亭 / 商铺（15-3D城市全面真实感深化 · 阶段 M）：
 *
 * 木色主体 + 红色顶棚 + 蓝色招牌 + 玻璃窗；
 * 可选贴图 props/vendor/vendor_kiosk.png 叠加（缺失走纯几何兜底）。
 *
 * 米制统一经 cityScale.u()（2.0m 高 × 1.2m 宽）。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §4.1。
 */

import { vendorUrl } from '@/assets/images/virtualCity';
import { u } from '../cityScale';
import { useSharedTexture } from '../textureCache';

const WOOD = '#6b4f3a';
const ROOF = '#c8453a';
const SIGN = '#3a78c8';
const GLASS = '#a8d5e8';

interface Props {
  x: number;
  z: number;
  rotation?: number;
}

export function VendorKiosk({ x, z, rotation = 0 }: Props) {
  const tex = useSharedTexture(vendorUrl('vendor_kiosk'));
  // 几何参数（米制）
  const W = u(1.2);
  const D = u(0.8);
  const H = u(2.0);
  const ROOF_T = u(0.08);

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 主体（玻璃箱） */}
      <mesh castShadow position={[0, H / 2, 0]}>
        <boxGeometry args={[W, H, D]} />
        <meshStandardMaterial color={WOOD} roughness={0.7} />
      </mesh>
      {/* 玻璃窗（前面，z = +D/2） */}
      <mesh position={[0, H * 0.55, D / 2 + 0.001]}>
        <planeGeometry args={[W * 0.85, H * 0.6]} />
        <meshStandardMaterial
          color={GLASS}
          transparent
          opacity={0.45}
          roughness={0.1}
          metalness={0.2}
        />
      </mesh>
      {/* 红色顶棚 */}
      <mesh castShadow position={[0, H + ROOF_T / 2, 0]}>
        <boxGeometry args={[W + u(0.15), ROOF_T, D + u(0.15)]} />
        <meshStandardMaterial color={ROOF} roughness={0.7} metalness={0.1} />
      </mesh>
      {/* 蓝色招牌（顶棚下沿） */}
      <mesh position={[0, H - u(0.08), D / 2 + 0.001]}>
        <planeGeometry args={[W * 0.85, u(0.3)]} />
        <meshStandardMaterial color={SIGN} roughness={0.6} />
      </mesh>
      {/* 贴图 sprite 叠加（缺失跳过） */}
      {tex && (
        <mesh position={[0, H * 0.55, D / 2 + 0.005]}>
          <planeGeometry args={[W * 0.85, H * 0.6]} />
          <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
        </mesh>
      )}
    </group>
  );
}

export default VendorKiosk;