/**
 * TreeV3（18-Z 后续并入 18-AA · §4.3）—— 立体树冠 3 球错落 + foliage 法线凹凸。
 *   替换 StreetPropsLayer 中 TreeV2 调用；V1/V2 文件保留。
 *   形态：主干（圆柱）+ 1–2 分枝 + 3 球错落树冠（icosahedronGeometry）。
 *   材质：树干 #5a4634 粗糙；树冠 #2f7a3a + `pbr/synth/foliage_n/r`。
 *   props 形状故意与 TreeV2 对齐（x/z/scale + 内部 hashStr 生成 seed），
 *   让 StreetPropsLayer 无痛替换。
 */
import { useMemo } from 'react';
import { useSynthPBR } from '../textureCache';
import { u } from '../cityScale';

const TRUNK_COLOR = '#5a4634';
const CROWN_COLORS = ['#2f7a3a', '#3a8a45', '#4a9a55'];

export interface TreeV3Props {
  /** 世界坐标 x。 */
  x: number;
  /** 世界坐标 z。 */
  z: number;
  /** 形态种子（确定性；默认由 hashStr(x,z) 派生）。 */
  seed?: number;
  /** 整体缩放（默认 1.0）。 */
  scale?: number;
  /** 是否 castShadow（性能控制：远景可关）。 */
  castShadow?: boolean;
}

/** 与 DistrictBlock.tsx 同款的 24 行 FNV-1a（避免主路径跨文件引用）。 */
function hashStr(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

export function TreeV3({ x, z, seed, scale = 1, castShadow = true }: TreeV3Props) {
  const foliage = useSynthPBR('foliage', { normalScale: [1.2, 1.2] });
  const fp = foliage.matProps;

  const effectiveSeed = seed ?? hashStr(`tree:${x.toFixed(3)}:${z.toFixed(3)}`);

  const { branchCount, crowns } = useMemo(() => {
    const h = (s: number) => {
      let xh = (s * 2654435761) >>> 0;
      xh ^= xh >>> 13;
      xh = Math.imul(xh, 2246822519) >>> 0;
      xh ^= xh >>> 16;
      return xh / 0xffffffff;
    };
    const r1 = h(effectiveSeed);
    const r2 = h(effectiveSeed * 31 + 1);
    const r3 = h(effectiveSeed * 131 + 7);
    return {
      branchCount: r1 > 0.5 ? 2 : 1,
      crowns: [
        { x: u(0), y: u(2.3) + r1 * u(0.3), z: u(0), r: u(0.7) + r2 * u(0.2), color: 0 },
        { x: (r1 - 0.5) * u(0.7), y: u(2.45) + r2 * u(0.25), z: (r3 - 0.5) * u(0.7), r: u(0.85) + r3 * u(0.15), color: 1 },
        { x: (r2 - 0.5) * u(0.7), y: u(2.15) + r3 * u(0.3), z: (r1 - 0.5) * u(0.7), r: u(0.6) + r1 * u(0.15), color: 2 },
      ],
    };
  }, [effectiveSeed]);

  return (
    <group position={[x, 0, z]} scale={scale}>
      {/* 主干 */}
      <mesh position={[0, u(0.8), 0]} castShadow={castShadow}>
        <cylinderGeometry args={[u(0.09), u(0.13), u(1.6), 8]} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
      </mesh>
      {/* 分枝 */}
      {Array.from({ length: branchCount }).map((_, i) => {
        const a = (i / Math.max(1, branchCount)) * Math.PI * 2 + effectiveSeed * 0.0001;
        return (
          <mesh
            key={`branch-${i}`}
            position={[Math.cos(a) * u(0.3), u(1.4), Math.sin(a) * u(0.3)]}
            rotation={[0.4, a, 0.5]}
            castShadow={castShadow}
          >
            <cylinderGeometry args={[u(0.04), u(0.06), u(0.6), 6]} />
            <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
          </mesh>
        );
      })}
      {/* 3 球树冠 */}
      {crowns.map((c, i) => (
        <mesh key={`crown-${i}`} position={[c.x, c.y, c.z]} castShadow={castShadow}>
          <icosahedronGeometry args={[c.r, 1]} />
          <meshStandardMaterial
            color={CROWN_COLORS[c.color]}
            {...fp}
            roughness={fp.roughnessMap ? undefined : 0.85}
            metalness={0.05}
          />
        </mesh>
      ))}
    </group>
  );
}