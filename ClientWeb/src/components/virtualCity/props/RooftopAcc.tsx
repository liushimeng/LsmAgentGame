/**
 * RooftopAcc — 楼顶杂物（P1-C）：AC 外机 / 水塔 / 卫星天线。
 *
 * 简化几何（缺失贴图）：不同 variant 不同盒子 / 圆柱 / 圆锥。
 * 加载 props/rooftop/<variant>_rooftop.png 后：sprite 朝相机。
 *
 * 由 StreetPropsLayer 布点到每个城区最高的 1-2 栋楼顶；y 偏移以楼顶面为基准。
 *
 * 2026-09-21 高度系统：水箱 / 天线高 0.30（真实 3m）、空调外机 0.08（0.8m），
 * 见 cityScale.ts。
 */

import * as THREE from 'three';
import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/virtualCity';
import { useSharedTexture } from '@/engine3d';

type RooftopVariant = 'ac' | 'tank' | 'antenna';

interface Props {
  x: number;
  y: number;       // 楼顶面 y
  z: number;
  variant?: RooftopVariant;
  /** 0..1 随机旋转。 */
  rotation?: number;
}

const ACC_COLORS: Record<RooftopVariant, string> = {
  ac: '#8b8e96',
  tank: '#a8a4a0',
  antenna: '#666870',
};

export function RooftopAcc({
  x,
  y,
  z,
  variant = 'ac',
  rotation = 0,
}: Props) {
  // 14-3D渲染深化：共享贴图缓存
  const tex = useSharedTexture(propUrl('rooftop', variant));

  // 简化几何按 variant 切换
  return (
    <group position={[x, y, z]} rotation={[0, rotation, 0]}>
      {tex ? (
        <Billboard position={[0, 0.15, 0]}>
          <mesh>
            <planeGeometry args={[0.3, 0.3]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      ) : (
        <>
          {variant === 'ac' && (
            <mesh position={[0, 0.04, 0]}>
              <boxGeometry args={[0.12, 0.08, 0.08]} />
              <meshStandardMaterial color={ACC_COLORS.ac} roughness={0.6} metalness={0.35} />
            </mesh>
          )}
          {variant === 'tank' && (
            <>
              <mesh position={[0, 0.15, 0]}>
                <cylinderGeometry args={[0.12, 0.12, 0.3, 10]} />
                <meshStandardMaterial color={ACC_COLORS.tank} roughness={0.7} metalness={0.25} />
              </mesh>
              {/* 支架 4 根细杆 */}
              {[0, 1, 2, 3].map((i) => {
                const a = (i / 4) * Math.PI * 2;
                return (
                  <mesh key={i} position={[Math.cos(a) * 0.1, 0.04, Math.sin(a) * 0.1]}>
                    <cylinderGeometry args={[0.01, 0.01, 0.08, 4]} />
                    <meshStandardMaterial color="#7a7a82" roughness={0.6} />
                  </mesh>
                );
              })}
            </>
          )}
          {variant === 'antenna' && (
            <>
              <mesh position={[0, 0.15, 0]}>
                <cylinderGeometry args={[0.012, 0.02, 0.3, 4]} />
                <meshStandardMaterial color={ACC_COLORS.antenna} roughness={0.5} metalness={0.6} />
              </mesh>
              {/* 锅状卫星天线 */}
              <mesh position={[0.06, 0.26, 0]} rotation={[0, 0, Math.PI / 6]}>
                <coneGeometry args={[0.06, 0.04, 10, 1, true]} />
                <meshStandardMaterial color="#cccccc" roughness={0.5} metalness={0.4} side={THREE.DoubleSide} />
              </mesh>
            </>
          )}
        </>
      )}
    </group>
  );
}