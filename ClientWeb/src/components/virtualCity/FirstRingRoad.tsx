/**
 * FirstRingRoad — 批次 31「混合式路网与一环路」：城市核心区域一环路。
 *
 * 32 段折线近似圆环，半径 20（内圈含 5 个内城区：tech/industry/oldtown/
 * commerce/residential），路宽 1.4u=14m（主干道断面）；asphalt_main 贴图 +
 * 每段中央白色虚线条 + 放射主路交点外侧停止线（几何口径照 RingRoad.tsx）。
 *
 * 批次 31 二轮（draw call 攻坚）：32 段路面 + 32 虚线 + 24 停止线合并为
 * 3 个 mesh（mergeParts；段内 UV u×3 平铺烘焙进几何，替代旧 texture.repeat
 * [3,1]；虚线/停止线白色走顶点色单材质）。232 个 mesh → 3 draw call。
 *
 * 分层（批次 31 §2.4 平面交叉微抬最高层）：Y = ROAD_SURFACE_Y + 0.007，
 * 压 spoke(0) / connector(+0.002) / arterial(+0.004) 下层，交叉无 z-fighting。
 *
 * 常量单一事实来源：roadNetwork.ts FIRST_RING_*（§130 防双写漂移）。
 * 契约：lag_docs/虚拟城市/已实现/31-混合式路网与一环路/01-方案设计.md §2.1/§4.2。
 */

import { useMemo } from 'react';
import * as THREE from 'three';
import { streetTileUrl, pbrNormalUrl, pbrRoughUrl } from '@/assets/images/virtualCity';
import { useSharedTexture, useSharedPBR, withPBR, mergeParts, type MergePart } from '@/engine3d';
import { useObjectInfoProps } from './objectInfo/useObjectInfoProps';
import { ROAD_SURFACE_Y } from './cityScale';
import { segCrossesCanal } from './cityObstacles';
import {
  FIRST_RING_RADIUS,
  FIRST_RING_WIDTH,
  FIRST_RING_SEGMENTS,
  FIRST_RING_Y_OFFSET,
} from './roadNetwork';

/** 每段搭接系数（防缝，照 RingRoad）。 */
const OVERLAP = 1.06;

interface Props {
  /** 放射主路与环交点角（atan2(z,x) 口径；roadNetwork.buildRoadNetwork 下发）。 */
  junctionAngles: number[];
  /** 贴图缺失时的兜底色。 */
  fallbackColor?: string;
}

/** 环段 matrix：段中心 (cx,cz)、绕 Y 转角 theta，plane 躺平后长沿切向。 */
function segMatrix(cx: number, cz: number, y: number, theta: number): THREE.Matrix4 {
  const m = new THREE.Matrix4().makeTranslation(cx, y, cz);
  m.multiply(new THREE.Matrix4().makeRotationY(theta));
  m.multiply(new THREE.Matrix4().makeRotationX(-Math.PI / 2));
  return m;
}

/** 建 UV u 方向带平铺倍率的平面（调用方用后 dispose）。 */
function uTiledPlane(w: number, len: number, uvScaleX: number): THREE.BufferGeometry {
  const g = new THREE.PlaneGeometry(w, len);
  const uv = g.attributes.uv;
  for (let i = 0; i < uv.count; i++) uv.setX(i, uv.getX(i) * uvScaleX);
  return g;
}

function mergeAndDispose(parts: MergePart[]): THREE.BufferGeometry | null {
  if (!parts.length) return null;
  const out = mergeParts(parts);
  for (const p of parts) p.geo.dispose();
  return out;
}

export function FirstRingRoad({ junctionAngles, fallbackColor = '#232b38' }: Props) {
  // UV u×3 平铺已烘焙进合并几何 → 贴图 repeat 恒 [1,1]（防 ×3 叠加）
  const asphalt = useSharedTexture(streetTileUrl('asphalt_main'), {
    wrap: 'repeat',
    repeat: [1, 1],
  });
  const asphaltPbr = useSharedPBR(
    streetTileUrl('asphalt_main'),
    pbrNormalUrl('streets', 'asphalt_main'),
    pbrRoughUrl('streets', 'asphalt_main'),
    { wrap: 'repeat', repeat: [1, 1], normalScale: [0.5, 0.5] },
  );

  const ringY = ROAD_SURFACE_Y + FIRST_RING_Y_OFFSET;

  // ── 合并几何：32 路面（UV u×3 烘焙）+ 32 虚线 + 交点停止线（顶点色白）──
  const merged = useMemo(() => {
    const segLen = ((2 * Math.PI * FIRST_RING_RADIUS) / FIRST_RING_SEGMENTS) * OVERLAP;
    const pavements: MergePart[] = [];
    const dashes: MergePart[] = [];
    for (let i = 0; i < FIRST_RING_SEGMENTS; i++) {
      const theta = ((i + 0.5) / FIRST_RING_SEGMENTS) * Math.PI * 2;
      const cx = Math.cos(theta) * FIRST_RING_RADIUS;
      const cz = Math.sin(theta) * FIRST_RING_RADIUS;
      // 批次 38 §4.6：**真过河**段不渲染路面（由 canalBridgeSpots 的桥面承载，
      // 视觉「桥就是路的延续」；否则一环路过河处整段沉入水面下）
      const a0 = (i / FIRST_RING_SEGMENTS) * Math.PI * 2;
      const a1 = ((i + 1) / FIRST_RING_SEGMENTS) * Math.PI * 2;
      const p0: [number, number] = [Math.cos(a0) * FIRST_RING_RADIUS, Math.sin(a0) * FIRST_RING_RADIUS];
      const p1: [number, number] = [Math.cos(a1) * FIRST_RING_RADIUS, Math.sin(a1) * FIRST_RING_RADIUS];
      if (segCrossesCanal(p0, p1)) continue;
      pavements.push({
        geo: uTiledPlane(FIRST_RING_WIDTH, segLen, 3),
        matrix: segMatrix(cx, cz, ringY, theta),
      });
      dashes.push({
        geo: new THREE.PlaneGeometry(0.08, segLen * 0.5),
        matrix: segMatrix(cx, cz, ringY + 0.001, theta),
        color: '#e8eaee',
      });
    }
    const stoplines: MergePart[] = junctionAngles.map((ang) => {
      const r = FIRST_RING_RADIUS + FIRST_RING_WIDTH / 2 + 0.15;
      return {
        geo: new THREE.PlaneGeometry(0.9, 0.14),
        matrix: segMatrix(Math.cos(ang) * r, Math.sin(ang) * r, ringY + 0.002, ang),
        color: '#ffffff',
      };
    });
    return {
      pavements: mergeAndDispose(pavements),
      dashes: mergeAndDispose(dashes),
      stoplines: mergeAndDispose(stoplines),
    };
  }, [junctionAngles, ringY]);

  // 批次 28 B2 同风格：环路信息交互。
  const info = useObjectInfoProps('road.first-ring', { anchorY: 0.3 });

  return (
    <group {...info}>
      {/* ① 环路面（32 段合并）→ 1 draw call */}
      {merged.pavements && (
        <mesh geometry={merged.pavements} receiveShadow>
          <meshStandardMaterial
            {...withPBR(
              {
                map: asphalt ?? undefined,
                color: asphalt ? '#ffffff' : fallbackColor,
                roughness: 0.92,
                metalness: 0.05,
              },
              asphaltPbr,
            )}
          />
        </mesh>
      )}
      {/* ② 中央虚线（32 段合并，顶点色白）→ 1 draw call */}
      {merged.dashes && (
        <mesh geometry={merged.dashes}>
          <meshStandardMaterial vertexColors roughness={0.85} />
        </mesh>
      )}
      {/* ③ 交点停止线（合并，顶点色白）→ 1 draw call */}
      {merged.stoplines && (
        <mesh geometry={merged.stoplines}>
          <meshStandardMaterial vertexColors roughness={0.85} />
        </mesh>
      )}
    </group>
  );
}
