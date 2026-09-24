/**
 * CloudLayer — 天空云层（16-3D城市WebGL质感与城市补全 · 阶段 U）：
 *
 * 6 团云，每团 3-4 片 Billboard 云朵（sky/cloud_puff.png 贴图；缺失 →
 * 白色扁球 opacity 0.30 depthWrite=false 兜底）。高度 y=14-20，沿 +x
 * 慢速漂移（0.15-0.3 单位/s），x > 42 回绕 -42；prefers-reduced-motion 静止。
 *
 * 布点确定性：mulberry32('clouds-v1')，避开地图中心正上（不遮 CBD 顶视）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §9。
 */

import { useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Billboard } from '@react-three/drei';
import { skyUrl } from '@/assets/images/virtualCity';
import { useSharedTexture } from '../textureCache';

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** 确定性伪随机（同源 StreetPropsLayer）。 */
function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

interface CloudSpec {
  x: number;
  y: number;
  z: number;
  speed: number;
  /** 每片云朵：偏移 + 尺寸。 */
  puffs: Array<{ dx: number; dy: number; dz: number; w: number; h: number }>;
}

/** 6 团云确定性布点：|x|,|z| ∈ [10, 38]（避开中心正上），y 60-80。
 *  2026-09-22 视觉验收三轮修正（16/05 验收清单 §3.4 回归项）：
 *  (1) 俯瞰时 0.85 大板把城市盖白 → opacity 降档；
 *  (2) 相机轨道高度随缩放在 13~45 之间，云必须全程保持在相机**上方**——
 *      最终抬到 y=60-80（OrbitControls maxDistance=80、maxPolar=1.2 的
 *      相机最高点 ≈45），任何视角下云都不会插进"相机↔城市"视线，
 *      仅在地平线/仰角视野中作为天空云朵出现。 */
function cloudsFor(seedStr: string): CloudSpec[] {
  let h = 2166136261;
  for (let i = 0; i < seedStr.length; i++) {
    h ^= seedStr.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  const rnd = mulberry32(h >>> 0);
  const out: CloudSpec[] = [];
  for (let c = 0; c < 6; c++) {
    const ang = rnd() * Math.PI * 2;
    const rad = 12 + rnd() * 26;
    const puffCount = 3 + Math.floor(rnd() * 2);
    const puffs = Array.from({ length: puffCount }, (_, p) => ({
      dx: (p - (puffCount - 1) / 2) * (1.2 + rnd() * 0.6),
      dy: (rnd() - 0.5) * 0.4,
      dz: (rnd() - 0.5) * 0.6,
      w: 2.2 + rnd() * 1.2,
      h: 1.2 + rnd() * 0.7,
    }));
    out.push({
      x: Math.cos(ang) * rad,
      y: 60 + rnd() * 20,
      z: Math.sin(ang) * rad,
      speed: 0.15 + rnd() * 0.15,
      puffs,
    });
  }
  return out;
}

/** 单团云（贴图 billboard 或白色扁球兜底）。 */
function Cloud({ spec, tex }: { spec: CloudSpec; tex: THREE.Texture | null }) {
  const groupRef = useRef<THREE.Group>(null);
  useFrame((_state, delta) => {
    const g = groupRef.current;
    if (!g || REDUCED_MOTION) return;
    g.position.x += spec.speed * delta;
    if (g.position.x > 42) g.position.x = -42;
  });
  return (
    <group ref={groupRef} position={[spec.x, spec.y, spec.z]}>
      {spec.puffs.map((p, i) =>
        tex ? (
          <Billboard key={`puff-${i}`} position={[p.dx, p.dy, p.dz]}>
            <mesh>
              <planeGeometry args={[p.w, p.h]} />
              <meshBasicMaterial
                map={tex}
                transparent
                opacity={0.35}
                depthWrite={false}
                fog={false}
                alphaTest={0.01}
              />
            </mesh>
          </Billboard>
        ) : (
          <mesh key={`puff-${i}`} position={[p.dx, p.dy, p.dz]} scale={[p.w * 0.4, p.h * 0.4, p.w * 0.4]}>
            <sphereGeometry args={[1, 8, 6]} />
            <meshBasicMaterial color="#f4f6f9" transparent opacity={0.12} depthWrite={false} fog={false} />
          </mesh>
        ),
      )}
    </group>
  );
}

export function CloudLayer() {
  // 贴图加载一次；缺失返回 null → 全层走白色扁球兜底
  const tex = useSharedTexture(skyUrl('cloud_puff'));
  const clouds = useMemo(() => cloudsFor('clouds-v1'), []);
  return (
    <group>
      {clouds.map((c, i) => (
        <Cloud key={`cloud-${i}`} spec={c} tex={tex} />
      ))}
    </group>
  );
}

export default CloudLayer;
