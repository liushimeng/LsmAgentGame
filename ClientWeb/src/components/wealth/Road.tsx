/**
 * Road — 2.5D 城市场景道路组件（P1-A）：
 *
 * 把单 plane 道路升级为 4 层组合 ——
 *   1. 主车道（asphalt_main.png 平铺；缺失 → 纯色 #1f2733）
 *   2. 中央分隔线（centerline.png；缺失 → 跳过绘制）
 *   3. 两侧人行道（sidewalk_main.png 平铺；缺失 → 纯色 #2a3340）
 *   4. 路灯阵列（每 2.5 单位 1 根；由 props/StreetLight 提供）
 *
 * 道路方向：用 from→to 向量旋转 group，每段独立渲染（不强制连到原点中心）。
 * 主干道 vs 次干道：根据 kind 决定纹理细节 + 路灯密度。
 */

import { useEffect, useMemo, useState } from 'react';
import * as THREE from 'three';
import { streetTileUrl, type StreetTileName } from '@/assets/images/wealth';
import { StreetLight } from './props/StreetLight';

interface Props {
  /** 道路起点世界坐标（城区中心）。 */
  from: [number, number];
  /** 道路终点世界坐标（金融 CBD 原点）。 */
  to: [number, number];
  /** 'main' = 主干道（有路灯 + 中线）；'side' = 次干道（仅车道 + 偶发路灯）。 */
  kind: 'main' | 'side';
}

/** 加载单张贴图 → 设置 wrap/repeat/colorSpace（缺失返回 null）。 */
function useStreetTile(
  name: StreetTileName,
  repeatX: number,
  repeatY: number,
): THREE.Texture | null {
  const url = streetTileUrl(name);
  const [tex, setTex] = useState<THREE.Texture | null>(null);
  useEffect(() => {
    if (!url) {
      setTex(null);
      return;
    }
    let disposed = false;
    const loader = new THREE.TextureLoader();
    loader.load(
      url,
      (loaded) => {
        if (disposed) {
          loaded.dispose();
          return;
        }
        loaded.colorSpace = THREE.SRGBColorSpace;
        loaded.wrapS = loaded.wrapT = THREE.RepeatWrapping;
        loaded.repeat.set(repeatX, repeatY);
        setTex(tex_ => {
          if (tex_) tex_.dispose();
          return loaded;
        });
      },
      undefined,
      () => {
        setTex(null);
      },
    );
    return () => {
      disposed = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, repeatX, repeatY]);
  return tex;
}

/** 主干道路面宽度（世界坐标）。 */
const ROAD_WIDTH_MAIN = 1.4;
/** 次干道路面宽度。 */
const ROAD_WIDTH_SIDE = 0.9;
/** 人行道宽度（每侧）。 */
const SIDEWALK_WIDTH = 0.25;
/** 主干道路灯间距。 */
const LAMP_SPACING_MAIN = 2.5;
/** 次干道路灯间距（更稀）。 */
const LAMP_SPACING_SIDE = 5.0;
/** 路面 y 抬高（避免 z-fighting with ground）。 */
const ROAD_Y = 0.015;
/** 人行道 y 抬高（再高一点点）。 */
const SIDEWALK_Y = 0.035;

export function Road({ from, to, kind }: Props) {
  // from → to 向量
  const dx = to[0] - from[0];
  const dz = to[1] - from[1];
  const len = Math.sqrt(dx * dx + dz * dz);
  const angle = Math.atan2(dx, dz); // around Y

  // 道路几何参数
  const roadWidth = kind === 'main' ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE;
  const lampSpacing = kind === 'main' ? LAMP_SPACING_MAIN : LAMP_SPACING_SIDE;

  // 路面 / 中线 / 人行道 贴图（按长度 repeat）
  // 路面 repeat = (len / 2, 1)；人行道 repeat = (len / 2, 1) ；中线 repeat = (len / 4, 1)
  const repeatX = Math.max(1, Math.round(len / 2));
  const asphaltTex = useStreetTile('asphalt_main', repeatX, 1);
  const sidewalkTex = useStreetTile(
    kind === 'main' ? 'sidewalk_main' : 'sidewalk_side',
    repeatX,
    1,
  );
  const centerlineTex = useStreetTile('centerline', repeatX, 1);

  // 路灯阵列点位（沿 from→to 等距，置于道路右侧）
  const lampPositions = useMemo(() => {
    if (kind === 'side' && len < 8) return []; // 次干道太短不画
    const count = Math.max(1, Math.floor(len / lampSpacing));
    const out: Array<{ x: number; z: number; rot: number; variant: 'a' | 'b' | 'c' }> = [];
    for (let i = 1; i <= count; i++) {
      const t = i / (count + 1); // 0..1 之间，避免落在端点
      const x = from[0] + dx * t;
      const z = from[1] + dz * t;
      // 侧偏移：right-hand 侧（向 -z 旋转 90° 方向）
      const nx = -dz / len; // normalized perpendicular
      const nz = dx / len;
      const sideOffset = roadWidth / 2 + 0.08;
      out.push({
        x: x + nx * sideOffset,
        z: z + nz * sideOffset,
        rot: angle,
        variant: i % 3 === 0 ? 'c' : (i % 2 === 0 ? 'b' : 'a'),
      });
    }
    return out;
  }, [from, dx, dz, len, angle, roadWidth, lampSpacing, kind]);

  return (
    <group rotation={[0, angle, 0]} position={[from[0], 0, from[1]]}>
      {/* 主车道（z ∈ [-w/2, w/2]） */}
      <mesh
        rotation={[-Math.PI / 2, 0, 0]}
        position={[0, ROAD_Y, 0]}
        receiveShadow
      >
        <planeGeometry args={[roadWidth, len]} />
        <meshStandardMaterial
          map={asphaltTex ?? undefined}
          color={asphaltTex ? '#ffffff' : '#1f2733'}
          roughness={0.92}
          metalness={0.05}
        />
      </mesh>

      {/* 中央分隔线（仅主干道且有 centerline 贴图） */}
      {kind === 'main' && centerlineTex && (
        <mesh
          rotation={[-Math.PI / 2, 0, 0]}
          position={[0, ROAD_Y + 0.001, 0]}
        >
          <planeGeometry args={[0.12, len]} />
          <meshStandardMaterial
            map={centerlineTex}
            color="#ffffff"
            roughness={0.9}
            transparent
            opacity={0.85}
          />
        </mesh>
      )}

      {/* 两侧人行道 */}
      {/* 左侧（local +z 方向取决于 group 旋转；这里用 -z = "左侧"）*/}
      <mesh
        rotation={[-Math.PI / 2, 0, 0]}
        position={[0, SIDEWALK_Y, -(roadWidth / 2 + SIDEWALK_WIDTH / 2)]}
        receiveShadow
      >
        <planeGeometry args={[SIDEWALK_WIDTH, len]} />
        <meshStandardMaterial
          map={sidewalkTex ?? undefined}
          color={sidewalkTex ? '#ffffff' : '#2a3340'}
          roughness={0.85}
        />
      </mesh>
      {/* 右侧 */}
      <mesh
        rotation={[-Math.PI / 2, 0, 0]}
        position={[0, SIDEWALK_Y, roadWidth / 2 + SIDEWALK_WIDTH / 2]}
        receiveShadow
      >
        <planeGeometry args={[SIDEWALK_WIDTH, len]} />
        <meshStandardMaterial
          map={sidewalkTex ?? undefined}
          color={sidewalkTex ? '#ffffff' : '#2a3340'}
          roughness={0.85}
        />
      </mesh>

      {/* 路灯阵列（世界坐标） */}
      {lampPositions.map((lp, i) => (
        <StreetLight
          key={`lamp-${i}`}
          x={lp.x}
          z={lp.z}
          rotation={lp.rot}
          variant={lp.variant}
          kind={kind}
        />
      ))}
    </group>
  );
}