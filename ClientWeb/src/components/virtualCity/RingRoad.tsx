/**
 * RingRoad — CBD 环形路（16-3D城市WebGL质感与城市补全 · 阶段 S）：
 *
 * 16 边形近似圆环路，半径 5.6（CBD 底板外缘 4.05 之外），路宽 1.0；
 * 沥青贴图（缺失 → 纯色）+ 每段中央白色虚线条；与 len>MAIN_ROAD_MIN_LEN
 * 放射主干道交点外侧画停止线（批次 20 §3.2：阈值从写死 12 改为派生常量）。
 *
 * 分层：RING_Y = ROAD_SURFACE_Y + 0.002（主干道 0.015 之上），交叉处不 z-fighting。
 *
 * 批次 31 二轮（draw call 攻坚）：16 段路面 + 16 虚线 + 交点停止线合并为
 * 3 个 mesh（mergeParts；UV u×2 平铺烘焙进几何，替代旧 texture.repeat [2,1]；
 * 虚线/停止线白色走顶点色单材质）。原 ~66 mesh → 3 draw call。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §5。
 */

import { useMemo } from 'react';
import * as THREE from 'three';
import { streetTileUrl, pbrNormalUrl, pbrRoughUrl } from '@/assets/images/virtualCity';
import { useSharedTexture, useSharedPBR, withPBR, mergeParts, type MergePart } from '@/engine3d';
import { useObjectInfoProps } from './objectInfo/useObjectInfoProps';
import { ROAD_SURFACE_Y } from './cityScale';

/** 环路半径（世界单位；CBD 8×8 底板半宽 4.05 + curb，环内缘 5.1 不压底板）。 */
const RING_RADIUS = 5.6;
/** 环路宽度。 */
const RING_WIDTH = 1.0;
/** 环路 y（主干道 ROAD_SURFACE_Y 之上；批次 30 随路面基准派生）。 */
const RING_Y = ROAD_SURFACE_Y + 0.002;
/** 边数（16 边形在该尺度下读作圆）。 */
const SEGMENTS = 16;
/** 每段搭接系数（防缝）。 */
const OVERLAP = 1.08;

interface Props {
  /** 环与线段的交点角（atan2(z,x) 口径；roadNetwork.buildRoadNetwork 下发。
   *  放射路删除后 CBD 环通常零交点 = 纯环岛，无交点停止线）。 */
  junctionAngles: number[];
  /** 贴图缺失时的兜底色。 */
  fallbackColor?: string;
}

/** 环段 matrix（照 FirstRingRoad 同构）。 */
function segMatrix(cx: number, cz: number, y: number, theta: number): THREE.Matrix4 {
  const m = new THREE.Matrix4().makeTranslation(cx, y, cz);
  m.multiply(new THREE.Matrix4().makeRotationY(theta));
  m.multiply(new THREE.Matrix4().makeRotationX(-Math.PI / 2));
  return m;
}

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

export function RingRoad({ junctionAngles, fallbackColor = '#232b38' }: Props) {
  // UV u×2 平铺已烘焙进合并几何 → 贴图 repeat 恒 [1,1]（防 ×2 叠加）
  const asphalt = useSharedTexture(streetTileUrl('asphalt_main'), {
    wrap: 'repeat',
    repeat: [1, 1],
  });
  // 18-X：路面 PBR（02 §2.3 行 8：asphalt_main，normalScale [0.5,0.5]）。
  const asphaltPbr = useSharedPBR(
    streetTileUrl('asphalt_main'),
    pbrNormalUrl('streets', 'asphalt_main'),
    pbrRoughUrl('streets', 'asphalt_main'),
    { wrap: 'repeat', repeat: [1, 1], normalScale: [0.5, 0.5] },
  );

  // ── 合并几何：16 路面（UV u×2 烘焙）+ 16 虚线 + 交点停止线（顶点色白）──
  const merged = useMemo(() => {
    const segLen = ((2 * Math.PI * RING_RADIUS) / SEGMENTS) * OVERLAP;
    const pavements: MergePart[] = [];
    const dashes: MergePart[] = [];
    for (let i = 0; i < SEGMENTS; i++) {
      const theta = ((i + 0.5) / SEGMENTS) * Math.PI * 2;
      const cx = Math.cos(theta) * RING_RADIUS;
      const cz = Math.sin(theta) * RING_RADIUS;
      pavements.push({
        geo: uTiledPlane(RING_WIDTH, segLen, 2),
        matrix: segMatrix(cx, cz, RING_Y, theta),
      });
      dashes.push({
        geo: new THREE.PlaneGeometry(0.08, segLen * 0.5),
        matrix: segMatrix(cx, cz, RING_Y + 0.001, theta),
        color: '#e8eaee',
      });
    }
    // 交点停止线：线段与环圆的真实交点（roadNetwork 求交下发；放射路删除后
    // 通常为空数组 = 纯环岛无停止线，符合真实语义）
    const stoplines: MergePart[] = junctionAngles.map((ang) => {
      const r = RING_RADIUS + RING_WIDTH / 2 + 0.15;
      return {
        geo: new THREE.PlaneGeometry(0.9, 0.14),
        matrix: segMatrix(Math.cos(ang) * r, Math.sin(ang) * r, RING_Y + 0.002, ang),
        color: '#ffffff',
      };
    });
    return {
      pavements: mergeAndDispose(pavements),
      dashes: mergeAndDispose(dashes),
      stoplines: mergeAndDispose(stoplines),
    };
  }, [junctionAngles]);

  const roadMat = (
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
  );
  // 批次 28 B2：环路信息交互。
  const info = useObjectInfoProps('road.ring', { anchorY: 0.3 });

  return (
    <group {...info}>
      {/* ① 环路面（16 段合并）→ 1 draw call */}
      {merged.pavements && (
        <mesh geometry={merged.pavements} receiveShadow>
          {roadMat}
        </mesh>
      )}
      {/* ② 中央虚线（16 段合并，顶点色白）→ 1 draw call */}
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
