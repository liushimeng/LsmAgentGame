/**
 * RoadMarkings — 批次 31 二轮：全路网标线/人行道**合并渲染**（draw call 攻坚）。
 *
 * 背景：批次 31 首轮把路网扩到 88 段后 renderer.info 实测 drawCalls 2316
 * （批次 30 终态 1654，红线 ≤1500）——瓶颈是每段 Road 的标线/人行道小面片
 * （crosswalk ×2 + stopline ×2 + arrow ×2 + sidewalk ×2 ≈ 8 mesh/段）。
 *
 * 方案（设计 31 §5 / 02-实施记录二轮）：全部道路**每段只留 1 个路面 mesh**（保
 * objectInfo 按路 hover；见 Road.tsx 精简），标线与人行道按贴图**全城合并成
 * 5 个 mesh**：
 *
 *   1. crosswalk（透明贴图，全部线段 × 双端）
 *   2. stopline（透明贴图，main 段 × 双端半幅）
 *   3. arrow_straight（透明贴图，main 段 × 双向）
 *   4. sidewalk_main（PBR 砖纹，main 段 × 双侧）
 *   5. sidewalk_side（PBR 砖纹，side 段 × 双侧）
 *
 * 平铺不再用 texture.repeat（合并后只有一份贴图实例），改为**烘焙进几何 UV**
 * （repeat = round(len/tile) 乘进 planeGeometry 的 uv 属性）；部件摆放用
 * matrix（T(world) · RotY(路向) · [RotZ(π)] · RotX(-π/2)），与旧 JSX 分层
 * rotation 同构；合并走 engine3d mergeParts（内部克隆拼接，调用方几何用后自弃）。
 *
 * 布局常量与旧 Road.tsx 逐字一致（批次 24 §6.1 / 批次 30 P1-9 口径）；
 * y 分层沿用设计 §2.4（yOffset 由 segment 携带）。
 */

import { useMemo } from 'react';
import * as THREE from 'three';
import { streetTileUrl, pbrNormalUrl, pbrRoughUrl } from '@/assets/images/virtualCity';
import { useSharedTexture, useSharedPBR, withPBR, mergeParts, type MergePart } from '@/engine3d';
import { ROAD_SURFACE_Y } from './cityScale';
import { ROAD_WIDTH_MAIN, ROAD_WIDTH_SIDE } from './Road';
import { segIntersectsWater } from './cityObstacles';
import type { RoadSegment } from './roadNetwork';

// ── 布局常量（旧 Road.tsx 同源逐字搬迁）────────────────────────────
/** 双端斑马线中心 t（0=from 端，1=to 端）。 */
const CROSSWALK_T = [0.08, 0.92] as const;
/** 斑马线深度（3.5 m）。 */
const CROSSWALK_DEPTH = 0.35;
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
}

/**
 * 路段局部系 → 世界 matrix：local +z = from→to（照 Road.tsx group 语义；
 * plane 中心落在 lz），plane 先 RotX(-π/2) 躺平（宽沿 local x、长沿 local z），
 * flip 时追加 RotZ(π)（旧箭头 to 端 rotation [-π/2, 0, π] 同构）。
 */
function partMatrix(seg: RoadSegment, lx: number, y: number, lz: number, flip: boolean): THREE.Matrix4 {
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

export function RoadMarkings({ segments }: Props) {
  // ── 全城合并几何（segments 为模块级静态 network，实际只算一次）──
  const merged = useMemo(() => {
    const crosswalk: MergePart[] = [];
    const stopline: MergePart[] = [];
    const arrow: MergePart[] = [];
    const sidewalkMain: MergePart[] = [];
    const sidewalkSide: MergePart[] = [];

    for (const seg of segments) {
      const dx = seg.to[0] - seg.from[0];
      const dz = seg.to[1] - seg.from[1];
      const len = Math.sqrt(dx * dx + dz * dz);
      const roadWidth = seg.kind === 'main' ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE;

      // 批次 38 R3/§4.3：与水域矩形相交的路段**整段跳过**人行道/斑马线/停止线
      // 铺装（桥面自身标线由 CanalBridge 承担，不在此列）。
      if (segIntersectsWater(seg.from, seg.to, 0)) continue;

      // ② 双端斑马线（全部线段）
      for (const t of CROSSWALK_T) {
        const g = tiledPlane(roadWidth, CROSSWALK_DEPTH, 1);
        crosswalk.push({ geo: g, matrix: partMatrix(seg, 0, ROAD_SURFACE_Y + 0.002, t * len, false) });
      }

      // ③④ 仅 main：半幅停止线 + 直行箭头（批次 24 §6.1 右行推导：
      //    to 端 local x>0、from 端 x<0；箭头 to 端 flip 对齐行进方向）
      if (seg.kind === 'main') {
        const stopZTo = CROSSWALK_T[1] * len - CROSSWALK_DEPTH / 2 - STOPLINE_GAP - STOPLINE_D / 2;
        const stopZFrom = CROSSWALK_T[0] * len + CROSSWALK_DEPTH / 2 + STOPLINE_GAP + STOPLINE_D / 2;
        const arrowZTo = stopZTo - STOPLINE_D / 2 - ARROW_GAP - ARROW_L / 2;
        const arrowZFrom = stopZFrom + STOPLINE_D / 2 + ARROW_GAP + ARROW_L / 2;
        stopline.push({ geo: tiledPlane(STOPLINE_W, STOPLINE_D, 1), matrix: partMatrix(seg, STOPLINE_X, ROAD_SURFACE_Y + 0.003, stopZTo, false) });
        stopline.push({ geo: tiledPlane(STOPLINE_W, STOPLINE_D, 1), matrix: partMatrix(seg, -STOPLINE_X, ROAD_SURFACE_Y + 0.003, stopZFrom, false) });
        arrow.push({ geo: tiledPlane(ARROW_W, ARROW_L, 1), matrix: partMatrix(seg, STOPLINE_X, ROAD_SURFACE_Y + 0.004, arrowZTo, true) });
        arrow.push({ geo: tiledPlane(ARROW_W, ARROW_L, 1), matrix: partMatrix(seg, -STOPLINE_X, ROAD_SURFACE_Y + 0.004, arrowZFrom, false) });
      }

      // ⑤ 双侧人行道（UV 平铺烘焙：repeat = round(len / 0.25)，plane 中心 = len/2）
      const targets = seg.kind === 'main' ? sidewalkMain : sidewalkSide;
      const repeatY = Math.max(1, Math.round(len / SIDEWALK_TILE));
      for (const side of [-1, 1] as const) {
        const g = tiledPlane(SIDEWALK_WIDTH, len, repeatY);
        targets.push({
          geo: g,
          matrix: partMatrix(seg, side * (roadWidth / 2 + SIDEWALK_WIDTH / 2), SIDEWALK_Y, len / 2, false),
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
  }, [segments]);

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
