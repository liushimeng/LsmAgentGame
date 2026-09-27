/**
 * CloudLayer — 天空云层（16-3D城市WebGL质感与城市补全 · 阶段 U）：
 *
 * 6 团云，每团 3-4 片 Billboard 云朵（sky/cloud_puff.png 贴图；缺失 →
 * 白色扁球 opacity 0.12 depthWrite=false 兜底）。高度 y=60-80，沿 +x
 * 慢速漂移（0.15-0.3 单位/s），x > 42 回绕 -42；prefers-reduced-motion 静止。
 *
 * 布点确定性：mulberry32('clouds-v1')，避开地图中心正上（不遮 CBD 顶视）。
 *
 * 批次 27 §4.3：云 opacity 基准（贴图 0.35 / 兜底 0.12）× §3.3 镜像表
 * cloudOpacity 系数（晴 0.10 → 暴雪 0.90），useFrame 读 cityTimeStore，
 * ~1.2s 时间常数渐变（无天气数据 = 系数 1 → 视觉零回归）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §9。
 */

import { useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Billboard } from '@react-three/drei';
import { skyUrl } from '@/assets/images/virtualCity';
import { useSharedTexture } from '@/engine3d';
import { sample, weatherVisual } from '../cityTimeStore';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** 批次 27：云 opacity 基准（贴图 / 白球兜底两链路各自的旧版值）。 */
const CLOUD_BASE_TEX = 0.35;
const CLOUD_BASE_FALLBACK = 0.12;

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

/** 单团云（贴图 billboard 或白色扁球兜底；材质共享于本团 puff，opacity 随天气渐变）。 */
function Cloud({ spec, tex }: { spec: CloudSpec; tex: THREE.Texture | null }) {
  const groupRef = useRef<THREE.Group>(null);
  // 批次 27：一团一材质（puff 共享），useFrame 只改 opacity —— 免逐 puff 材质 ref 收集。
  const material = useMemo(() => {
    const m = new THREE.MeshBasicMaterial({
      transparent: true,
      depthWrite: false,
      fog: false,
    });
    if (tex) {
      m.map = tex;
      m.alphaTest = 0.01;
      m.opacity = CLOUD_BASE_TEX;
    } else {
      m.color = new THREE.Color('#f4f6f9');
      m.opacity = CLOUD_BASE_FALLBACK;
    }
    return m;
  }, [tex]);
  useEffect(() => () => material.dispose(), [material]);
  const baseOpacity = tex ? CLOUD_BASE_TEX : CLOUD_BASE_FALLBACK;

  useFrame((_state, delta) => {
    const g = groupRef.current;
    if (g && !REDUCED_MOTION) {
      g.position.x += spec.speed * delta;
      if (g.position.x > 42) g.position.x = -42;
    }
    // 批次 27：云量 × 天气系数（§3.3 镜像表；无天气数据 = 1）。指数平滑 ≈1.2s 渐变。
    const target = baseOpacity * weatherVisual(sample().weather).cloudOpacity;
    material.opacity += (target - material.opacity) * Math.min(1, delta * 0.8);
  });
  return (
    <group ref={groupRef} position={[spec.x, spec.y, spec.z]}>
      {spec.puffs.map((p, i) =>
        tex ? (
          <Billboard key={`puff-${i}`} position={[p.dx, p.dy, p.dz]}>
            <mesh material={material}>
              <planeGeometry args={[p.w, p.h]} />
            </mesh>
          </Billboard>
        ) : (
          <mesh
            key={`puff-${i}`}
            material={material}
            position={[p.dx, p.dy, p.dz]}
            scale={[p.w * 0.4, p.h * 0.4, p.w * 0.4]}
          >
            <sphereGeometry args={[1, 8, 6]} />
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
  // 批次 28 B2：云层信息交互（根组承接子云事件冒泡）。
  const info = useObjectInfoProps('sky.cloud', { anchorY: 0 });
  return (
    <group {...info}>
      {clouds.map((c, i) => (
        <Cloud key={`cloud-${i}`} spec={c} tex={tex} />
      ))}
    </group>
  );
}

export default CloudLayer;
