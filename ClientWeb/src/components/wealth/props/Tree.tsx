/**
 * Tree — 2.5D 街道树木（P1-C）：
 *
 * 简化几何（缺失贴图）：棕色细圆柱树干 + 绿色 sphere 树冠。
 * 加载 props/tree/<variant>_tree.png 后：树冠用 Billboard 朝相机的 sprite。
 * 唯一保留 castShadow 的 prop（树影是城市感关键）。
 */

import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/wealth';
import { useSharedTexture } from '../textureCache';

type TreeVariant = 'oak' | 'pine' | 'palm';

interface Props {
  x: number;
  z: number;
  variant?: TreeVariant;
  /** 树高 scale（世界单位），默认 0.7；范围 0.5~1.0 ≈ 真实 5~10m 行道树（见 cityScale.ts）。 */
  scale?: number;
}

// 树冠简化几何颜色（缺失贴图时）
const CANOPY_COLORS: Record<TreeVariant, string> = {
  oak:  '#3f7d4d',
  pine: '#2c5e3e',
  palm: '#5fa86a',
};
const TRUNK_COLOR = '#6b4f32';

export function Tree({ x, z, variant = 'oak', scale = 0.7 }: Props) {
  // 14-3D渲染深化：共享贴图缓存（全部 oak/pine/palm 各只上传一次）
  const tex = useSharedTexture(propUrl('tree', variant));

  return (
    <group position={[x, 0, z]} scale={[scale, scale, scale]}>
      {/* 树干（castShadow 唯一保留的 prop） */}
      <mesh castShadow position={[0, 0.25, 0]}>
        <cylinderGeometry args={[0.06, 0.08, 0.5, 6]} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.85} />
      </mesh>
      {/* 树冠 — 有贴图用 Billboard sprite，缺失用 sphere */}
      {tex ? (
        <Billboard position={[0, 0.7, 0]}>
          <mesh>
            <planeGeometry args={[0.9, 1.0]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      ) : (
        <mesh position={[0, 0.7, 0]} castShadow>
          <sphereGeometry args={[0.4, 8, 6]} />
          <meshStandardMaterial color={CANOPY_COLORS[variant]} roughness={0.85} />
        </mesh>
      )}
    </group>
  );
}