/**
 * RoadMarkings — 全城标线 / 人行道**合并渲染**（draw call 攻坚 + 批次 61 方格网适配）。
 *
 * 背景：批次 31 首轮把路网扩到 88 段后 renderer.info 实测 drawCalls 2316
 * （批次 30 终态 1654，红线 ≤1500）——瓶颈是每段 Road 的标线/人行道小面片
 * （crosswalk ×2 + stopline ×2 + arrow ×2 + sidewalk ×2 ≈ 8 mesh/段）。
 *
 * 方案（设计 31 §5 / 02-实施记录二轮）：全部道路**每段只留 1 个路面 mesh**，
 * 标线与人行道按贴图**全城合并成 5 个 mesh**：
 *
 *   1. crosswalk（透明贴图，**按交叉口**布点）
 *   2. stopline（透明贴图，**主干道进口道**）
 *   3. arrow_straight（透明贴图，主干道进口道）
 *   4. sidewalk_main（PBR 砖纹，main 段 × 双侧，**在交叉口断开**）
 *   5. sidewalk_side（PBR 砖纹，side 段 × 双侧，**在交叉口断开**）
 *
 * ## 批次 61 的两处结构性修订
 * ① **标线从「线段端点」改为「交叉口」**：旧口径把斑马线/停止线放在**路段两端**，
 *    而方格网的路口在**长直线段的中部**（街道 84u 贯穿全城，路口在 z=±6/±18/±30
 *    等位置）⇒ 旧口径会把斑马线画在城市边界与运河岸边这种**没有路口**的地方。
 *    新口径按 `roadNetwork.junctions`（56 个正交交叉口）逐口布点，每个口 4 个进口道。
 * ② **人行道在交叉口断开**：旧口径沿整段铺人行道，方格网的长直段会让人行道
 *    **横穿路口**（凸起的人行道条压过交叉口）。新口径在每个路口留出「垂直道路
 *    半宽」的缺口，人行道只在街坊段内连续。
 *
 * 平铺不再用 texture.repeat（合并后只有一份贴图实例），改为**烘焙进几何 UV**；
 * 部件摆放用 matrix（T(world) · RotY(路向) · [RotZ(π)] · RotX(-π/2)）。
 * 合并走 engine3d mergeParts（内部克隆拼接，调用方几何用后自弃）。
 *
 * 契约：lag_docs/虚拟城市/已实现/61-全城连续正交方格路网/01-方案设计.md §2.6。
 */

import { useMemo } from 'react';
import * as THREE from 'three';
import { streetTileUrl, pbrNormalUrl, pbrRoughUrl } from '@/assets/images/virtualCity';
import { useSharedTexture, useSharedPBR, withPBR, mergeParts, type MergePart } from '@/engine3d';
import { ROAD_SURFACE_Y } from './cityScale';
import { ROAD_WIDTH_MAIN, ROAD_WIDTH_SIDE } from './Road';
import { isMainStreetLine } from './cityGrid';
import type { RoadJunction, RoadSegment } from './roadNetwork';

// ── 布局常量（旧 Road.tsx 同源逐字搬迁）────────────────────────────
/** 斑马线深度（3.5 m）。 */
const CROSSWALK_DEPTH = 0.35;
/** 斑马线与路口中心的额外外移（留出路缘石位置）。 */
const CROSSWALK_GAP = 0.06;
/** 停止线与斑马线内边缘间距。 */
const STOPLINE_GAP = 0.04;
/** 停止线尺寸（0.6×0.08，横跨半幅）。 */
const STOPLINE_W = 0.6;
const STOPLINE_D = 0.08;
/** 停止线中心横向偏移（半幅中心 |x|=0.35）。 */
const STOPLINE_X = 0.35;
/** 直行箭头尺寸（1.0×3.0 m）。 */
const ARROW_W = 0.1;
const ARROW_L = 0.3;
/** 箭头与停止线后沿间距。 */
const ARROW_GAP = 0.1;
/** 人行道宽度（每侧）。 */
const SIDEWALK_WIDTH = 0.25;
/** 人行道砖纹贴图覆盖边长（0.25×0.25u）。 */
const SIDEWALK_TILE = 0.25;
/** 人行道 y（批次 30 P1-10：路缘 +0.012 = 12cm，随路 yOffset 分层）。 */
const SIDEWALK_Y = ROAD_SURFACE_Y + 0.012;

interface Props {
  /** 全城道路线段（roadNetwork.buildRoadNetwork 产出）。 */
  segments: RoadSegment[];
  /** 全城正交交叉口（roadNetwork.buildRoadNetwork 产出；标线布点的唯一依据）。 */
  junctions: RoadJunction[];
}

/** 交叉口在南北向道路上的路幅（该口 x 所在街道线的等级）。 */
function nsWidthAt(j: RoadJunction): number {
  return isMainStreetLine(j.x) ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE;
}
/** 交叉口在东西向道路上的路幅。 */
function ewWidthAt(j: RoadJunction): number {
  return isMainStreetLine(j.z) ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE;
}

/**
 * 路段局部系 → 世界 matrix：local +z = from→to（照 Road.tsx group 语义；
 * plane 中心落在 lz），plane 先 RotX(-π/2) 躺平（宽沿 local x、长沿 local z），
 * flip 时追加 RotZ(π)（箭头朝向翻转）。
 */
function partMatrix(
  seg: { from: [number, number]; to: [number, number]; yOffset: number },
  lx: number,
  y: number,
  lz: number,
  flip: boolean,
): THREE.Matrix4 {
  const dx = seg.to[0] - seg.from[0];
  const dz = seg.to[1] - seg.from[1];
  const angle = Math.atan2(dx, dz);
  const sin = Math.sin(angle);
  const cos = Math.cos(angle);
  const m = new THREE.Matrix4().makeTranslation(
    seg.from[0] + sin * lz + cos * lx,
    y + seg.yOffset,
    seg.from[1] + cos * lz - sin * lx,
  );
  m.multiply(new THREE.Matrix4().makeRotationY(angle));
  if (flip) m.multiply(new THREE.Matrix4().makeRotationZ(Math.PI));
  m.multiply(new THREE.Matrix4().makeRotationX(-Math.PI / 2));
  return m;
}

/** 建一个已把 UV 平铺倍率烘焙进去的平面几何（调用方用后 dispose）。 */
function tiledPlane(w: number, len: number, uvScaleY: number): THREE.BufferGeometry {
  const g = new THREE.PlaneGeometry(w, len);
  const uv = g.attributes.uv;
  for (let i = 0; i < uv.count; i++) uv.setY(i, uv.getY(i) * uvScaleY);
  return g;
}

/** 合并并回收部件几何（mergeParts 内部已克隆，调用方持有所有权）。 */
function mergeAndDispose(parts: MergePart[]): THREE.BufferGeometry | null {
  if (!parts.length) return null;
  const out = mergeParts(parts);
  for (const p of parts) p.geo.dispose();
  return out;
}

export function RoadMarkings({ segments, junctions }: Props) {
  // ── 全城合并几何（segments 为模块级静态 network，实际只算一次）──
  const merged = useMemo(() => {
    const crosswalk: MergePart[] = [];
    const stopline: MergePart[] = [];
    const arrow: MergePart[] = [];
    const sidewalkMain: MergePart[] = [];
    const sidewalkSide: MergePart[] = [];

    // ── ① 人行道：沿段连续，但在每个交叉口留出「垂直道路半宽」的缺口 ──
    for (const seg of segments) {
      const dx = seg.to[0] - seg.from[0];
      const dz = seg.to[1] - seg.from[1];
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len < 0.05) continue;
      const roadWidth = seg.kind === 'main' ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE;
      const vertical = Math.abs(dx) < 1e-6; // 沿 z 跑（南北向街道）
      // 该段上所有交叉口（按沿段距离排序；记录需让开的半宽）
      const gaps: Array<{ d: number; half: number }> = [];
      for (const j of junctions) {
        if (vertical) {
          if (Math.abs(j.x - seg.from[0]) > 1e-6) continue;
          const d = (j.z - seg.from[1]) * Math.sign(dz);
          if (d <= 0 || d >= len) continue;
          gaps.push({ d, half: ewWidthAt(j) / 2 });
        } else {
          if (Math.abs(j.z - seg.from[1]) > 1e-6) continue;
          const d = (j.x - seg.from[0]) * Math.sign(dx);
          if (d <= 0 || d >= len) continue;
          gaps.push({ d, half: nsWidthAt(j) / 2 });
        }
      }
      gaps.sort((a, b) => a.d - b.d);
      // 切出可铺人行道的区间
      const intervals: Array<[number, number]> = [];
      let cur = 0;
      for (const g of gaps) {
        intervals.push([cur, g.d - g.half]);
        cur = g.d + g.half;
      }
      intervals.push([cur, len]);

      const targets = seg.kind === 'main' ? sidewalkMain : sidewalkSide;
      for (const [a, b] of intervals) {
        const L = b - a;
        if (L <= 0.05) continue;
        const mid = (a + b) / 2;
        const repeatY = Math.max(1, Math.round(L / SIDEWALK_TILE));
        for (const side of [-1, 1] as const) {
          const g = tiledPlane(SIDEWALK_WIDTH, L, repeatY);
          targets.push({
            geo: g,
            matrix: partMatrix(seg, side * (roadWidth / 2 + SIDEWALK_WIDTH / 2), SIDEWALK_Y, mid, false),
          });
        }
      }
    }

    // ── ② 交叉口标线：每个口 4 个进口道 ──
    for (const j of junctions) {
      const nsW = nsWidthAt(j);
      const ewW = ewWidthAt(j);
      // axis: 进口道所在道路的走向；sgn: 进口道所在的侧（+1 = +轴侧）。
      // width: 该进口道所在道路的路幅；perp: 垂直道路的路幅（决定外移距离）。
      const approaches: Array<{
        axis: 'z' | 'x';
        sgn: 1 | -1;
        width: number;
        perp: number;
        main: boolean;
      }> = [
        { axis: 'z', sgn: -1, width: nsW, perp: ewW, main: isMainStreetLine(j.x) },
        { axis: 'z', sgn: 1, width: nsW, perp: ewW, main: isMainStreetLine(j.x) },
        { axis: 'x', sgn: -1, width: ewW, perp: nsW, main: isMainStreetLine(j.z) },
        { axis: 'x', sgn: 1, width: ewW, perp: nsW, main: isMainStreetLine(j.z) },
      ];

      for (const a of approaches) {
        // 斑马线中心：距路口中心 = 垂直道路半宽 + 斑马线半深 + 外移余量
        const d = a.perp / 2 + CROSSWALK_DEPTH / 2 + CROSSWALK_GAP;
        const cxx = a.axis === 'z' ? j.x : j.x + a.sgn * d;
        const czz = a.axis === 'z' ? j.z + a.sgn * d : j.z;
        const half = CROSSWALK_DEPTH / 2;
        // 合成一段「假路段」：沿进口道方向，长 = 斑马线深
        const fake: { from: [number, number]; to: [number, number]; yOffset: number } =
          a.axis === 'z'
            ? { from: [cxx, czz - half], to: [cxx, czz + half], yOffset: 0 }
            : { from: [cxx - half, czz], to: [cxx + half, czz], yOffset: 0 };
        crosswalk.push({
          geo: tiledPlane(a.width, CROSSWALK_DEPTH, 1),
          matrix: partMatrix(fake, 0, ROAD_SURFACE_Y + 0.002, half, false),
        });

        if (!a.main) continue; // 次干道进口道只设斑马线（让行标）
        // 车道偏移（右行制）：南北向 fake 段的 local x → 世界 +x；东西向 → 世界 −z
        const laneX = a.axis === 'z' ? -a.sgn * STOPLINE_X : a.sgn * STOPLINE_X;
        // 停止线 / 箭头再往路口外推一段
        const stopD = a.perp / 2 + CROSSWALK_DEPTH + CROSSWALK_GAP + STOPLINE_GAP + STOPLINE_D / 2;
        const arrowD = stopD + STOPLINE_D / 2 + ARROW_GAP + ARROW_L / 2;
        const mk = (dist: number): { from: [number, number]; to: [number, number]; yOffset: number } => {
          const px = a.axis === 'z' ? cxx : cxx + a.sgn * (dist - d);
          const pz = a.axis === 'z' ? czz + a.sgn * (dist - d) : czz;
          return a.axis === 'z'
            ? { from: [px, pz - half], to: [px, pz + half], yOffset: 0 }
            : { from: [px - half, pz], to: [px + half, pz], yOffset: 0 };
        };
        // 箭头朝向：进口道在 −sgn 侧时车流朝 +轴行驶（箭头指 +轴）。
        // fake 段沿 +轴构造 ⇒ 局部 +y（贴图上方）映射到世界 −轴 ⇒ 需 flip 才指 +轴。
        const flip = a.sgn < 0;
        stopline.push({
          geo: tiledPlane(STOPLINE_W, STOPLINE_D, 1),
          matrix: partMatrix(mk(stopD), laneX, ROAD_SURFACE_Y + 0.003, half, false),
        });
        arrow.push({
          geo: tiledPlane(ARROW_W, ARROW_L, 1),
          matrix: partMatrix(mk(arrowD), laneX, ROAD_SURFACE_Y + 0.004, half, flip),
        });
      }
    }

    return {
      crosswalk: mergeAndDispose(crosswalk),
      stopline: mergeAndDispose(stopline),
      arrow: mergeAndDispose(arrow),
      sidewalkMain: mergeAndDispose(sidewalkMain),
      sidewalkSide: mergeAndDispose(sidewalkSide),
    };
  }, [segments, junctions]);

  // ── 共享贴图（各一份实例；UV 平铺已烘焙进几何，repeat 恒 [1,1]）──
  const crosswalkTex = useSharedTexture(streetTileUrl('crosswalk'), { wrap: 'repeat', repeat: [1, 1] });
  const stoplineTex = useSharedTexture(streetTileUrl('stopline'), { wrap: 'repeat', repeat: [1, 1] });
  const arrowTex = useSharedTexture(streetTileUrl('arrow_straight'), { wrap: 'repeat', repeat: [1, 1] });
  const sidewalkMainPbr = useSharedPBR(
    streetTileUrl('sidewalk_main'),
    pbrNormalUrl('streets', 'sidewalk_main'),
    pbrRoughUrl('streets', 'sidewalk_main'),
    { wrap: 'repeat', repeat: [1, 1], normalScale: [0.8, 0.8] },
  );
  const sidewalkSidePbr = useSharedPBR(
    streetTileUrl('sidewalk_side'),
    pbrNormalUrl('streets', 'sidewalk_side'),
    pbrRoughUrl('streets', 'sidewalk_side'),
    { wrap: 'repeat', repeat: [1, 1], normalScale: [0.8, 0.8] },
  );

  const sidewalkBase = (pbr: typeof sidewalkMainPbr) =>
    withPBR(
      { color: pbr.matProps.map ? '#ffffff' : '#2a3340', roughness: 0.85 },
      pbr,
    );

  return (
    <group>
      {/* ① 全城斑马线 → 1 draw call（贴图缺失整层跳过，同旧降级语义） */}
      {merged.crosswalk && crosswalkTex && (
        <mesh geometry={merged.crosswalk}>
          <meshStandardMaterial map={crosswalkTex} color="#ffffff" roughness={0.9} transparent opacity={0.95} />
        </mesh>
      )}
      {/* ② 全城停止线 → 1 draw call */}
      {merged.stopline && stoplineTex && (
        <mesh geometry={merged.stopline}>
          <meshStandardMaterial map={stoplineTex} color="#ffffff" roughness={0.9} transparent opacity={0.95} />
        </mesh>
      )}
      {/* ③ 全城直行箭头 → 1 draw call */}
      {merged.arrow && arrowTex && (
        <mesh geometry={merged.arrow}>
          <meshStandardMaterial map={arrowTex} color="#ffffff" roughness={0.9} transparent />
        </mesh>
      )}
      {/* ④ 主城人行道 → 1 draw call（PBR 砖纹） */}
      {merged.sidewalkMain && (
        <mesh geometry={merged.sidewalkMain} receiveShadow>
          <meshStandardMaterial {...sidewalkBase(sidewalkMainPbr)} />
        </mesh>
      )}
      {/* ⑤ 次城人行道 → 1 draw call */}
      {merged.sidewalkSide && (
        <mesh geometry={merged.sidewalkSide} receiveShadow>
          <meshStandardMaterial {...sidewalkBase(sidewalkSidePbr)} />
        </mesh>
      )}
    </group>
  );
}
