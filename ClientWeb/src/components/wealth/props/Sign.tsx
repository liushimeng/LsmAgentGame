/**
 * Sign — 2.5D 街道标识牌（P1-C）：
 *
 * 简化几何（缺失贴图）：pole + 小色板（traffic 红 / info 蓝）。
 * 加载 props/sign/<variant>_sign.png 后：色板贴图 sprite。
 *
 * 主要在主干道分叉或路口布置；castShadow=false。
 */

import { useEffect, useState } from 'react';
import * as THREE from 'three';
import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/wealth';

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
  const url = propUrl('sign', variant);
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
      () => setTex(null),
    );
    return () => {
      disposed = true;
    };
  }, [url]);

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* pole */}
      <mesh position={[0, 0.4, 0]}>
        <cylinderGeometry args={[0.025, 0.03, 0.8, 6]} />
        <meshStandardMaterial color="#5b616e" roughness={0.55} metalness={0.55} />
      </mesh>
      {/* plate (有贴图用 sprite 缺失用纯色盒子) */}
      {tex ? (
        <Billboard position={[0, 0.85, 0]}>
          <mesh>
            <planeGeometry args={[0.28, 0.28]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      ) : (
        <mesh position={[0, 0.85, 0]}>
          <boxGeometry args={[0.28, 0.28, 0.04]} />
          <meshStandardMaterial color={PLATE_COLORS[variant]} roughness={0.55} />
        </mesh>
      )}
    </group>
  );
}