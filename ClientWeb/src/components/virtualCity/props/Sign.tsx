/**
 * Sign — 2.5D 街道标识牌（P1-C）：
 *
 * 简化几何（缺失贴图）：pole + 小色板（traffic 红 / info 蓝）。
 * 加载 props/sign/<variant>_sign.png 后：色板贴图 sprite。
 *
 * 主要在主干道分叉或路口布置；castShadow=false。
 *
 * 2026-09-21 高度系统：杆高 0.22（真实 2.2m）+ 牌 ~0.06（0.6m），
 * 总高 ~0.28（见 cityScale.ts）。
 */

import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/virtualCity';
import { useSharedTexture } from '../textureCache';

type SignVariant = 'traffic' | 'info';

interface Props {
  x: number;
  z: number;
  rotation?: number;
  variant?: SignVariant;
}

const PLATE_COLORS: Record<SignVariant, string> = {
  traffic: '#c83a3a',
  info: '#3a78c8',
};

export function Sign({ x, z, rotation = 0, variant = 'traffic' }: Props) {
  // 14-3D渲染深化：共享贴图缓存
  const tex = useSharedTexture(propUrl('sign', variant));

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* pole（2.2m） */}
      <mesh position={[0, 0.11, 0]}>
        <cylinderGeometry args={[0.008, 0.01, 0.22, 6]} />
        <meshStandardMaterial color="#5b616e" roughness={0.55} metalness={0.55} />
      </mesh>
      {/* plate 0.6m（有贴图用 sprite 缺失用纯色盒子） */}
      {tex ? (
        <Billboard position={[0, 0.25, 0]}>
          <mesh>
            <planeGeometry args={[0.09, 0.06]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      ) : (
        <mesh position={[0, 0.25, 0]}>
          <boxGeometry args={[0.09, 0.06, 0.015]} />
          <meshStandardMaterial color={PLATE_COLORS[variant]} roughness={0.55} />
        </mesh>
      )}
    </group>
  );
}