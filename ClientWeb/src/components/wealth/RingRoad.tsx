/**
 * RingRoad — CBD 环形路（16-3D城市WebGL质感与城市补全 · 阶段 S）：
 *
 * 16 边形近似圆环路，半径 5.6（CBD 底板外缘 4.05 之外），路宽 1.0；
 * 沥青贴图（缺失 → 纯色）+ 每段中央白色虚线条；与 len>12 放射主干道交点
 * 外侧画停止线（确定性 ≤ 12 处）。
 *
 * 分层：RING_Y = 0.017（主干道 0.015 之上），交叉处不 z-fighting。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §5。
 */

import { useMemo } from 'react';
import { streetTileUrl, pbrNormalUrl, pbrRoughUrl } from '@/assets/images/wealth';
import { districtCenter, WEALTH_DISTRICTS } from '@/types/wealth';
import { useSharedTexture, useSharedPBR, withPBR } from './textureCache';

/** 环路半径（世界单位；CBD 8×8 底板半宽 4.05 + curb，环内缘 5.1 不压底板）。 */
const RING_RADIUS = 5.6;
/** 环路宽度。 */
const RING_WIDTH = 1.0;
/** 环路 y（主干道 ROAD_Y=0.015 之上）。 */
const RING_Y = 0.017;
/** 边数（16 边形在该尺度下读作圆）。 */
const SEGMENTS = 16;
/** 每段搭接系数（防缝）。 */
const OVERLAP = 1.08;

interface Props {
  /** 贴图缺失时的兜底色。 */
  fallbackColor?: string;
}

export function RingRoad({ fallbackColor = '#232b38' }: Props) {
  const asphalt = useSharedTexture(streetTileUrl('asphalt_main'), {
    wrap: 'repeat',
    repeat: [2, 1],
  });
  // 18-X：路面 PBR（02 §2.3 行 8：asphalt_main，normalScale [0.5,0.5]）。
  const asphaltPbr = useSharedPBR(
    streetTileUrl('asphalt_main'),
    pbrNormalUrl('streets', 'asphalt_main'),
    pbrRoughUrl('streets', 'asphalt_main'),
    { wrap: 'repeat', repeat: [2, 1], normalScale: [0.5, 0.5] },
  );


  // 段参数：θ 取段中心角；segLen 加 8% 搭接
  const segs = useMemo(() => {
    const segLen = (2 * Math.PI * RING_RADIUS) / SEGMENTS * OVERLAP;
    return Array.from({ length: SEGMENTS }, (_, i) => {
      const theta = ((i + 0.5) / SEGMENTS) * Math.PI * 2;
      return {
        key: i,
        cx: Math.cos(theta) * RING_RADIUS,
        cz: Math.sin(theta) * RING_RADIUS,
        rotY: theta,
        len: segLen,
      };
    });
  }, []);

  // 放射主干道（len > 12）与环的交点角（确定性；交点停止线）
  const junctionAngles = useMemo(() => {
    return WEALTH_DISTRICTS
      .filter((d) => d.id !== 'finance')
      .map((d) => districtCenter(d.id))
      .filter((c) => Math.sqrt(c.x * c.x + c.z * c.z) > 12)
      .map((c) => Math.atan2(c.z, c.x));
  }, []);

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

  return (
    <group>
      {segs.map((s) => (
        <group key={`ring-seg-${s.key}`} position={[s.cx, 0, s.cz]} rotation={[0, s.rotY, 0]}>
          {/* 路面（plane X=宽，Y=长 → rotation.x=-π/2 后长沿局部 Z） */}
          <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, RING_Y, 0]} receiveShadow>
            <planeGeometry args={[RING_WIDTH, s.len]} />
            {roadMat}
          </mesh>
          {/* 中央虚线（每段 1 条，全环成虚线环） */}
          <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, RING_Y + 0.001, 0]}>
            <planeGeometry args={[0.08, s.len * 0.5]} />
            <meshStandardMaterial color="#e8eaee" roughness={0.85} />
          </mesh>
        </group>
      ))}
      {/* 交点停止线（道路外侧：半径 R + RING_WIDTH/2 + 0.15） */}
      {junctionAngles.map((ang, i) => {
        const r = RING_RADIUS + RING_WIDTH / 2 + 0.15;
        return (
          <group key={`ring-stop-${i}`} position={[Math.cos(ang) * r, 0, Math.sin(ang) * r]} rotation={[0, ang, 0]}>
            <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, RING_Y + 0.002, 0]}>
              <planeGeometry args={[0.9, 0.14]} />
              <meshStandardMaterial color="#ffffff" roughness={0.85} />
            </mesh>
          </group>
        );
      })}
    </group>
  );
}
