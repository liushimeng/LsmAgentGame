/**
 * FarBuildingSilhouette — 远景建筑剪影（15-3D城市全面真实感深化 · 阶段 P）：
 *
 * 在地图 4 个方向各放 1-2 组远景楼栋剪影（暗色 plane + 简单 box 组合），
 * 制造城市天际线感。位于雾化远端（FOG_FAR 外），自然融入背景。
 *
 * 纯几何 + 暗色，无贴图依赖。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §7.2。
 */

import { useMemo } from 'react';
import { WORLD_SIZE } from './WealthCityMap';

const SILHOUETTE_COLOR = '#3a4250';
const FLOOR_HEIGHT_M = 3; // 与 cityScale.DISTRICT_FLOORS 同源

interface SilhouetteBuildingSpec {
  x: number;
  z: number;
  w: number;
  d: number;
  floors: number;
  /** 0..1 颜色亮度抖动（接近主色 = 远景灰度）。 */
  shade: number;
}

/**
 * 沿 side 方向生成 N 个楼栋剪影；side = 'north' 放地图北边（z 远端），依此类推。
 * 使用 mulberry32 伪随机（同源 DistrictBlock）保证确定性。
 */
function silhouettesForSide(
  side: 'north' | 'south' | 'east' | 'west',
  seed: number,
  count: number,
): SilhouetteBuildingSpec[] {
  const out: SilhouetteBuildingSpec[] = [];
  let a = seed >>> 0;
  const rnd = () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const W = WORLD_SIZE;
  // 远端位置：地图边缘外 u(8) 偏移
  const edgeOffset = W / 2 + 8;
  for (let i = 0; i < count; i++) {
    const t = i / count; // 0..1
    const lateralSpread = W * 1.2; // 横向延展超过地图
    const lateral = -lateralSpread / 2 + t * lateralSpread + (rnd() - 0.5) * 4;
    const cx =
      side === 'east' ? edgeOffset :
      side === 'west' ? -edgeOffset :
      lateral;
    const cz =
      side === 'north' ? -edgeOffset :
      side === 'south' ? edgeOffset :
      lateral;
    const floors = 8 + Math.floor(rnd() * 30); // 8-37 层
    const w = 2.0 + rnd() * 2.5;
    const d = 1.5 + rnd() * 2.0;
    out.push({
      x: cx,
      z: cz,
      w,
      d,
      floors,
      shade: 0.7 + rnd() * 0.3,
    });
  }
  return out;
}

interface Props {
  side: 'north' | 'south' | 'east' | 'west';
  count?: number;
}

export function FarBuildingSilhouette({ side, count = 18 }: Props) {
  // 确定性 seed = side 字符串 hash
  const seed = useMemo(() => {
    let h = 2166136261;
    const s = `far:${side}`;
    for (let i = 0; i < s.length; i++) {
      h ^= s.charCodeAt(i);
      h = Math.imul(h, 16777619);
    }
    return h >>> 0;
  }, [side]);

  const buildings = useMemo(
    () => silhouettesForSide(side, seed, count),
    [side, seed, count],
  );

  return (
    <group>
      {buildings.map((b, i) => {
        const h = (b.floors * FLOOR_HEIGHT_M) / 10; // m → 世界单位
        return (
          <mesh key={i} position={[b.x, h / 2, b.z]}>
            <boxGeometry args={[b.w, h, b.d]} />
            <meshStandardMaterial
              color={SILHOUETTE_COLOR}
              roughness={1}
              metalness={0}
            />
          </mesh>
        );
      })}
    </group>
  );
}

export default FarBuildingSilhouette;