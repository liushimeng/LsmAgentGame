/**
 * Vehicle — 2.5D 街道车辆（P1-C）：
 *
 * 简化几何（缺失贴图）：小盒子 + 主色（按 variant）。
 * 加载 props/vehicle/<variant>_vehicle.png 后：Billboard 朝相机 sprite。
 *
 * 沿 from→to 路径循环移动：useFrame 内 lerp t += speed * dt，过 t ≥ 1 → 重置。
 * y=0.05（高于地面、低于路灯）；rotation 始终朝向运动方向（与 Billboard 不冲突，
 * sprite 始终朝相机，几何盒子朝向运动方向）。
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/wealth';

type VehicleVariant = 'sedan' | 'truck' | 'bus' | 'taxi';

interface Props {
  /** 起始坐标。 */
  from: [number, number];
  /** 终点坐标（沿 from→to 直线路径循环）。 */
  to: [number, number];
  variant?: VehicleVariant;
  /** 循环速度（t / 秒）。 */
  speed?: number;
  /** 起始相位偏移 [0, 1)，避免多辆车完全同步。 */
  phase?: number;
}

const VEHICLE_COLORS: Record<VehicleVariant, string> = {
  sedan: '#3b6bb0',
  truck: '#8a6a3d',
  bus:  '#d8c44a',
  taxi: '#e8b930',
};

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export function Vehicle({
  from,
  to,
  variant = 'sedan',
  speed = 0.06,
  phase = 0,
}: Props) {
  const url = propUrl('vehicle', variant);
  const [tex, setTex] = useState<THREE.Texture | null>(null);
  const groupRef = useRef<THREE.Group>(null);

  // 路径向量
  const { dx, dz, angle } = useMemo(() => {
    const dx = to[0] - from[0];
    const dz = to[1] - from[1];
    return { dx, dz, angle: Math.atan2(dx, dz) };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [from[0], from[1], to[0], to[1]]);

  // 起始位置
  const tRef = useRef(phase);

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

  useFrame((_state, delta) => {
    const g = groupRef.current;
    if (!g) return;
    if (REDUCED_MOTION) return;
    tRef.current += delta * speed;
    if (tRef.current >= 1) tRef.current -= 1;
    const t = tRef.current;
    g.position.x = from[0] + dx * t;
    g.position.z = from[1] + dz * t;
    g.position.y = 0.05;
  });

  return (
    <group ref={groupRef} position={[from[0], 0.05, from[1]]} rotation={[0, angle, 0]}>
      {tex ? (
        <Billboard>
          <mesh>
            <planeGeometry args={[0.45, 0.3]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      ) : (
        // 缺失贴图：低矮盒子 + 主色
        <mesh castShadow={false}>
          <boxGeometry args={[0.3, 0.12, 0.18]} />
          <meshStandardMaterial color={VEHICLE_COLORS[variant]} roughness={0.6} metalness={0.3} />
        </mesh>
      )}
    </group>
  );
}