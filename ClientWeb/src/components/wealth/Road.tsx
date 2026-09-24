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
 *
 * v2.13 阶段 D（13-3D城市渲染优化）：
 *   - crosswalk.png 斑马线接线（修复「生成却从不接线」§130）——from 端（城区
 *     入口侧）t=0.08 处铺设，主/次干道均有。
 *
 * 批次 20 性能专项（文档 1 §3.3）：路灯不再逐盏渲染在本组件旋转组内，
 *   - 点位生成通式抽为导出函数 lampsForRoad(from, to, kind)（世界坐标）；
 *   - WealthCityMap 汇总全部道路点位后交 <StreetLightsInstanced> 全局实例化
 *     （三段 3 draw call），props/StreetLight 组件文件保留可回退。
 */

import type { Texture } from 'three';
import { streetTileUrl, pbrNormalUrl, pbrRoughUrl, type StreetTileName } from '@/assets/images/wealth';
import { u } from './cityScale';
import type { InstancedLamp } from './props/StreetLightsInstanced';
import { useSharedPBR, useSharedTexture, withPBR } from './textureCache';

interface Props {
  /** 道路起点世界坐标（城区中心）。 */
  from: [number, number];
  /** 道路终点世界坐标（金融 CBD 原点）。 */
  to: [number, number];
  /** 'main' = 主干道（有路灯 + 中线）；'side' = 次干道（仅车道 + 偶发路灯）。 */
  kind: 'main' | 'side';
}

/** 加载单张贴图（14-3D渲染深化：走共享缓存；缺失返回 null）。 */
function useStreetTile(
  name: StreetTileName,
  repeatX: number,
  repeatY: number,
): Texture | null {
  return useSharedTexture(streetTileUrl(name), {
    wrap: 'repeat',
    repeat: [repeatX, repeatY],
  });
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

/**
 * 路灯阵列点位（批次 20 §3.3 从 Road 组件抽出为纯函数，世界坐标；
 * 由 WealthCityMap 汇总全部道路后交给 <StreetLightsInstanced> 实例化渲染）。
 * 规则不变：沿 from→to 等距 lampSpacing，道路右侧 sideOffset 摆放；
 * 次干道 len < 8 不画。
 */
export function lampsForRoad(
  from: [number, number],
  to: [number, number],
  kind: 'main' | 'side',
): InstancedLamp[] {
  const dx = to[0] - from[0];
  const dz = to[1] - from[1];
  const len = Math.sqrt(dx * dx + dz * dz);
  const angle = Math.atan2(dx, dz);
  const roadWidth = kind === 'main' ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE;
  const lampSpacing = kind === 'main' ? LAMP_SPACING_MAIN : LAMP_SPACING_SIDE;
  if (kind === 'side' && len < 8) return []; // 次干道太短不画
  const count = Math.max(1, Math.floor(len / lampSpacing));
  const out: InstancedLamp[] = [];
  for (let i = 1; i <= count; i++) {
    const t = i / (count + 1); // 0..1 之间，避免落在端点
    const x = from[0] + dx * t;
    const z = from[1] + dz * t;
    // 侧偏移：right-hand 侧（向 -z 旋转 90° 方向）
    const nx = -dz / len; // normalized perpendicular
    const nz = dx / len;
    const sideOffset = roadWidth / 2 + 0.08;
    out.push({ x: x + nx * sideOffset, z: z + nz * sideOffset, rot: angle, kind });
  }
  return out;
}

export function Road({ from, to, kind }: Props) {
  // from → to 向量
  const dx = to[0] - from[0];
  const dz = to[1] - from[1];
  const len = Math.sqrt(dx * dx + dz * dz);
  const angle = Math.atan2(dx, dz); // around Y

  // 道路几何参数（路灯点位已移出本组件，见 lampsForRoad + StreetLightsInstanced）
  const roadWidth = kind === 'main' ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE;

  // 路面 / 中线 / 人行道 贴图（按长度 repeat）
  // 路面 repeat = (len / 2, 1)；人行道 repeat = (len / 2, 1) ；中线 repeat = (len / 4, 1)
  const repeatX = Math.max(1, Math.round(len / 2));
  const asphaltTex = useStreetTile('asphalt_main', repeatX, 1);
  // 18-X：路面 PBR（02 §2.3 行 8：streets/asphalt_main，normalScale [0.5,0.5]）。
  const asphaltPbr = useSharedPBR(
    streetTileUrl('asphalt_main'),
    pbrNormalUrl('streets', 'asphalt_main'),
    pbrRoughUrl('streets', 'asphalt_main'),
    { wrap: 'repeat', repeat: [repeatX, 1], normalScale: [0.5, 0.5] },
  );
  const sidewalkTex = useStreetTile(
    kind === 'main' ? 'sidewalk_main' : 'sidewalk_side',
    repeatX,
    1,
  );
  // 18-X：人行道 PBR（02 §2.3 行 9：sidewalk_main/sidewalk_side，normalScale [0.8,0.8]）。
  const sidewalkName = kind === 'main' ? 'sidewalk_main' : 'sidewalk_side';
  const sidewalkPbr = useSharedPBR(
    streetTileUrl(sidewalkName),
    pbrNormalUrl('streets', sidewalkName),
    pbrRoughUrl('streets', sidewalkName),
    { wrap: 'repeat', repeat: [repeatX, 1], normalScale: [0.8, 0.8] },
  );
  const centerlineTex = useStreetTile('centerline', repeatX, 1);
  // v2.13 阶段 D：斑马线贴图（城区入口 t=0.08 处；不随路长平铺，repeat 1:1）
  const crosswalkTex = useStreetTile('crosswalk', 1, 1);


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
          {...withPBR(
            {
              map: asphaltTex ?? undefined,
              color: asphaltTex ? '#ffffff' : '#1f2733',
              roughness: 0.92,
              metalness: 0.05,
            },
            asphaltPbr,
          )}
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

      {/* 15 阶段 O：车道直行箭头（主干道 t=0.5，local +z = to 方向） */}
      {kind === 'main' && (
        <group position={[0, ROAD_Y + 0.003, 0.5 * len]}>
          {/* 箭头杆（朝向 to 方向 = local +z） */}
          <mesh position={[0, 0, -0.2]}>
            <boxGeometry args={[0.08, 0.005, 0.4]} />
            <meshStandardMaterial color="#ffffff" roughness={0.85} />
          </mesh>
          {/* 箭头三角头（cone 3 段近三角） */}
          <mesh position={[0, 0, 0.12]} rotation={[-Math.PI / 2, 0, 0]}>
            <coneGeometry args={[0.18, 0.2, 3]} />
            <meshStandardMaterial color="#ffffff" roughness={0.85} />
          </mesh>
        </group>
      )}

      {/* 15 阶段 O：停止线（主干道 t=0.95，路口前） */}
      {kind === 'main' && (
        <mesh
          rotation={[-Math.PI / 2, 0, 0]}
          position={[0, ROAD_Y + 0.003, 0.95 * len]}
        >
          <planeGeometry args={[roadWidth, u(0.4)]} />
          <meshStandardMaterial color="#ffffff" roughness={0.85} />
        </mesh>
      )}

      {/* v2.13 阶段 D：斑马线（城区入口侧 t=0.08；local +z 指向 to，
          贴图缺失时静默跳过，路面本身仍有降级色） */}
      {crosswalkTex && (
        <mesh
          rotation={[-Math.PI / 2, 0, 0]}
          position={[0, ROAD_Y + 0.002, 0.08 * len]}
        >
          <planeGeometry args={[roadWidth, 0.5]} />
          <meshStandardMaterial
            map={crosswalkTex}
            color="#ffffff"
            roughness={0.9}
            transparent
            opacity={0.9}
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
          {...withPBR(
            {
              map: sidewalkTex ?? undefined,
              color: sidewalkTex ? '#ffffff' : '#2a3340',
              roughness: 0.85,
            },
            sidewalkPbr,
          )}
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
          {...withPBR(
            {
              map: sidewalkTex ?? undefined,
              color: sidewalkTex ? '#ffffff' : '#2a3340',
              roughness: 0.85,
            },
            sidewalkPbr,
          )}
        />
      </mesh>

    </group>
  );
}