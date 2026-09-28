/**
 * HighwayGates — 批次 31「混合式路网与一环路」：高速联络线收费站（城市出入口）。
 *
 * 16 个外环区的放射联络线（roadNetwork edgeLink）在 r=55 处穿城而出，每线 1 座
 * 收费站：门式龙门架（双柱 + 横梁）+ 右侧双收费亭（绿顶白身，中国高速收费站风格）。
 *
 * 渲染：drei <Instances> 实例化 —— 柱 ×2/座、横梁 ×1/座、亭身 ×2/座、亭顶 ×2/座，
 * 全城 ≈ 5 draw call（照 TrafficSignals 静态件模式，批次 24 §6 同款裁决：
 * 小体量程序化几何不用 GLB）。
 *
 * 尺寸（米制经 cityScale.u()，1u=10m）：
 *   龙门架净高 5.5m（真实收费站棚净空 5.0~5.5m）、柱 ⌀0.4m、
 *   横梁宽 = 路面 14m + 两侧各 1.5m 外挑；收费亭 1.5×1.5m 见方、高 2.4m，
 *   亭顶出檐 1.8×1.8m，立于联络线行进方向右侧（右行制）。
 *
 * 契约：lag_docs/虚拟城市/已实现/31-混合式路网与一环路/01-方案设计.md §4.2。
 */

import { Instances, Instance } from '@react-three/drei';
import { u } from '../cityScale';
import type { GateSpot } from '../roadNetwork';
import {
  useObjectInfoProps,
  instancedEventsRaycast,
} from '../objectInfo/useObjectInfoProps';

// ── 几何尺寸（米制经 u()）───────────────────────────────────────────
/** 龙门架净高 5.5m（柱高）。 */
const PILLAR_H = u(5.5);
/** 龙门架柱 ⌀0.4m。 */
const PILLAR_R = u(0.2);
/** 龙门架横梁高 0.6m 厚 0.4m。 */
const BEAM_H = u(0.6);
const BEAM_D = u(0.4);
/** 横梁半长：路面半宽 7m + 两侧外挑 1.5m。 */
const BEAM_HALF = u(7 + 1.5);
/** 收费亭：1.5×1.5 见方、高 2.4m。 */
const BOOTH_W = u(1.5);
const BOOTH_H = u(2.4);
/** 亭顶：1.8×1.8 出檐、厚 0.15m。 */
const CANOPY_W = u(1.8);
const CANOPY_H = u(0.15);
/** 收费亭距路面中心偏移（右侧路缘外 1.2m：路面半宽 7m + 1.2m）。 */
const BOOTH_SIDE_OFF = u(7 + 1.2);
/** 收费亭沿路向前后错开 ±1.2m（双亭分列）。 */
const BOOTH_ALONG_OFF = u(1.2);

interface Props {
  /** 布点（roadNetwork.buildRoadNetwork 产出）。 */
  gates: GateSpot[];
}

/**
 * gate local 系：+z = 径向外（联络线行进方向区→高速），+x = 行进右侧。
 * spot.rotation = atan2(ux, uz)，故 world = (x·sin+x·cos) 由 rotation 旋转 local 偏移。
 */
function localToWorld(s: GateSpot, lx: number, lz: number): [number, number] {
  const sin = Math.sin(s.rotation);
  const cos = Math.cos(s.rotation);
  return [s.x + sin * lz + cos * lx, s.z + cos * lz - sin * lx];
}

export function HighwayGates({ gates }: Props) {
  const info = useObjectInfoProps('road.gate', { anchorY: 3.2 });

  const renderPart = (
    key: string,
    geometry: JSX.Element,
    material: JSX.Element,
    items: Array<{ key: string; pos: [number, number, number]; rot: number }>,
  ) => (
    <Instances
      {...info}
      raycast={instancedEventsRaycast}
      key={key}
      limit={items.length}
      range={items.length}
    >
      {geometry}
      {material}
      {items.map((it) => (
        <Instance key={it.key} position={it.pos} rotation={[0, it.rot, 0]} />
      ))}
    </Instances>
  );

  // 双柱：local x = ±BEAM_HALF（横梁两端下方）
  const pillars = gates.flatMap((g, i) =>
    ([-1, 1] as const).map((side) => {
      const [x, z] = localToWorld(g, BEAM_HALF * side, 0);
      return {
        key: `gate-pillar-${i}-${side}`,
        pos: [x, PILLAR_H / 2, z] as [number, number, number],
        rot: g.rotation,
      };
    }),
  );
  // 横梁：柱顶，沿 local x 横跨
  const beams = gates.map((g, i) => ({
    key: `gate-beam-${i}`,
    pos: [g.x, PILLAR_H + BEAM_H / 2, g.z] as [number, number, number],
    rot: g.rotation,
  }));
  // 双收费亭：右侧（local +x = BOOTH_SIDE_OFF）前后错开
  const booths = gates.flatMap((g, i) =>
    ([-1, 1] as const).map((k) => {
      const [x, z] = localToWorld(g, BOOTH_SIDE_OFF, BOOTH_ALONG_OFF * k);
      return {
        key: `gate-booth-${i}-${k}`,
        pos: [x, BOOTH_H / 2, z] as [number, number, number],
        rot: g.rotation,
      };
    }),
  );
  const canopies = gates.flatMap((g, i) =>
    ([-1, 1] as const).map((k) => {
      const [x, z] = localToWorld(g, BOOTH_SIDE_OFF, BOOTH_ALONG_OFF * k);
      return {
        key: `gate-canopy-${i}-${k}`,
        pos: [x, BOOTH_H + CANOPY_H / 2, z] as [number, number, number],
        rot: g.rotation,
      };
    }),
  );

  if (!gates.length) return null;

  return (
    <group>
      {/* ① 龙门架柱 ×2N → 1 draw call（交通灰） */}
      {renderPart(
        'gate-pillar',
        <cylinderGeometry args={[PILLAR_R, PILLAR_R, PILLAR_H, 8]} />,
        <meshStandardMaterial color="#8a9099" roughness={0.55} metalness={0.35} />,
        pillars,
      )}
      {/* ② 龙门架横梁 ×N → 1 draw call（交通灰 + 绿底横梁带由亭顶呼应） */}
      {renderPart(
        'gate-beam',
        <boxGeometry args={[BEAM_HALF * 2, BEAM_H, BEAM_D]} />,
        <meshStandardMaterial color="#7d8894" roughness={0.55} metalness={0.35} />,
        beams,
      )}
      {/* ③ 收费亭亭身 ×2N → 1 draw call（白身） */}
      {renderPart(
        'gate-booth',
        <boxGeometry args={[BOOTH_W, BOOTH_H, BOOTH_W]} />,
        <meshStandardMaterial color="#e8e6e0" roughness={0.8} />,
        booths,
      )}
      {/* ④ 收费亭顶 ×2N → 1 draw call（高速绿出檐） */}
      {renderPart(
        'gate-canopy',
        <boxGeometry args={[CANOPY_W, CANOPY_H, CANOPY_W]} />,
        <meshStandardMaterial color="#2e8b57" roughness={0.7} />,
        canopies,
      )}
    </group>
  );
}
