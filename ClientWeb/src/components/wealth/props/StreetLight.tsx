/**
 * StreetLight — 2.5D 街道路灯（P1-C）：
 *
 * 简化几何（缺失贴图时）：底座小圆柱 + 主杆细圆柱 + 灯头小盒子（带 emissive 暖光）。
 * 加载 props/streetlamp/<variant>_streetlamp.png 贴图（透明）后：
 *   - 立杆贴图作为 sprite 朝相机方向显示（Billboard）。
 *   - 几何保留作为骨架（透明贴图遮罩）。
 *
 * 默认 castShadow=false（性能预算；路灯阴影非必要）。
 */

import { useEffect, useState } from 'react';
import * as THREE from 'three';
import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/wealth';

interface Props {
  x: number;
  z: number;
  /** 朝向（弧度，绕 Y 轴）；通常与所在道路方向一致。 */
  rotation?: number;
  variant?: 'a' | 'b' | 'c';
}

export function StreetLight({ x, z, rotation = 0, variant = 'a' }: Props) {
  const url = propUrl('streetlamp', variant);
  const [tex, setTex] = useState<THREE.Texture | null>(null);

  useEffect(() => {
    if (!url) {
      setTex(null);
      return;
    }
    let disposed = false;
    const loader = new THREE.TextureLoader();
    loader.load(
      url,
      (loaded) => {
        if (disposed) {
          loaded.dispose();
          return;
        }
        loaded.colorSpace = THREE.SRGBColorSpace;
        loaded.magFilter = THREE.LinearFilter;
        loaded.minFilter = THREE.LinearMipmapLinearFilter;
        setTex(prev => {
          if (prev) prev.dispose();
          return loaded;
        });
      },
      undefined,
      () => {
        setTex(null);
      },
    );
    return () => {
      disposed = true;
    };
  }, [url]);

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 底座 */}
      <mesh position={[0, 0.04, 0]}>
        <cylinderGeometry args={[0.08, 0.12, 0.08, 8]} />
        <meshStandardMaterial color="#4a4f5a" roughness={0.7} metalness={0.4} />
      </mesh>
      {/* 主杆 */}
      <mesh position={[0, 0.7, 0]}>
        <cylinderGeometry args={[0.04, 0.06, 1.3, 6]} />
        <meshStandardMaterial color="#6b7280" roughness={0.55} metalness={0.6} />
      </mesh>
      {/* 灯头（emissive 暖光） */}
      <mesh position={[0, 1.38, 0]}>
        <boxGeometry args={[0.18, 0.1, 0.18]} />
        <meshStandardMaterial
          color={tex ? '#ffffff' : '#aaa9a0'}
          emissive="#fff5b8"
          emissiveIntensity={0.55}
          roughness={0.4}
        />
      </mesh>
      {/* 灯头顶部贴图 sprite（Billboard 朝相机） */}
      {tex && (
        <Billboard position={[0, 1.4, 0]}>
          <mesh>
            <planeGeometry args={[0.32, 0.6]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      )}
    </group>
  );
}