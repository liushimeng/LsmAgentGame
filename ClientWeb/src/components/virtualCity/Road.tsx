/**
 * Road — 3D 城市场景道路组件（批次 24「真实马路与交通设施」重排）：
 *
 * 单段道路分层组合（局部系：group 原点 = from，local +z = from→to）：
 *   1. 路面（road_main / road_side 整幅贴图，标线已烘焙：中心黄虚线 + 白色边缘线；
 *      贴图覆盖 roadWidth×roadWidth，沿路长 repeat；缺失 → 纯色 #1f2733）
 *   2. 双端斑马线（crosswalk.png，t=0.08 / t=0.92 全宽；置于停止线外侧更靠路端）
 *   3. 半幅停止线（stopline.png，仅主干道；原点端铺 local x>0、城区端铺 x<0，
 *      见 §6.1 车道对齐推导——正向车 district→origin 占 local x>0 车道）
 *   4. 直行导向箭头（arrow_straight.png，每方向 1 枚，位于该方向停止线后方，
 *      原点端朝 local +z（指向 to）、城区端旋转 180° 朝 -z）
 *   5. 两侧人行道（sidewalk_main/side.png 砖纹，贴图覆盖 0.25×0.25 方块）
 *
 * y 抬高节奏：路面 0.015 → 斑马线 +0.002 → 停止线 +0.003 → 箭头 +0.004
 * （逐层微抬避免 z-fighting，总量 ≤ 0.019 仍低于人行道 0.035 与环路 0.017 上层）。
 *
 * 批次 24 变更：
 *   - 删除中心线独立面片（centerline.png）与程序化箭头组 / 程序化停止线——
 *     标线已烘焙进 road_main / road_side 整幅贴图（文档 24 §4）。
 *   - 斑马线由单端（t=0.08）扩为双端（t=0.08 / t=0.92）。
 *   - PBR 沿用 asphalt_main / asphalt_side 的 _n/_r 对（标线平坦无需独立法线）。
 *
 * 路灯不再逐盏渲染在本组件内（批次 20 §3.3）：点位生成通式 lampsForRoad
 * 由 VirtualCityCityMap 汇总后交 <StreetLightsInstanced> 全局实例化。
 */

import type { Texture } from 'three';
import { streetTileUrl, pbrNormalUrl, pbrRoughUrl, type StreetTileName } from '@/assets/images/virtualCity';
import type { InstancedLamp } from './props/StreetLightsInstanced';
import { useSharedPBR, useSharedTexture, withPBR } from '@/engine3d';

interface Props {
  /** 道路起点世界坐标（城区中心）。 */
  from: [number, number];
  /** 道路终点世界坐标（金融 CBD 原点）。 */
  to: [number, number];
  /** 'main' = 主干道（双车道标线 + 停止线/箭头）；'side' = 次干道（仅边缘线）。 */
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

/** 主干道路面宽度（世界单位）。 */
const ROAD_WIDTH_MAIN = 1.4;
/** 次干道路面宽度。 */
const ROAD_WIDTH_SIDE = 0.9;
/** 人行道宽度（每侧）。 */
const SIDEWALK_WIDTH = 0.25;
/** 人行道砖纹贴图覆盖边长（0.25×0.25u 一块砖区）。 */
const SIDEWALK_TILE = 0.25;
/** 主干道路灯间距。 */
const LAMP_SPACING_MAIN = 2.5;
/** 次干道路灯间距（更稀）。 */
const LAMP_SPACING_SIDE = 5.0;
/** 路面 y 抬高（避免 z-fighting with ground）。 */
const ROAD_Y = 0.015;
/** 人行道 y 抬高（再高一点点）。 */
const SIDEWALK_Y = 0.035;

// ── 批次 24 标线几何常量（文档 24 §4 / §6.1）─────────────────────
/** 双端斑马线中心 t（0=城区端，1=原点端）。 */
const CROSSWALK_T = [0.08, 0.92] as const;
/** 斑马线深度（沿路向，= crosswalk.png 覆盖 1.4×0.5）。 */
const CROSSWALK_DEPTH = 0.5;
/** 停止线与斑马线内边缘间距。 */
const STOPLINE_GAP = 0.04;
/** 停止线尺寸（= stopline.png 覆盖 0.6×0.08）。 */
const STOPLINE_W = 0.6;
const STOPLINE_D = 0.08;
/** 停止线中心横向偏移（半幅中心：|x|=0.35，覆盖 [0.05,0.65] 半幅）。 */
const STOPLINE_X = 0.35;
/** 直行箭头尺寸（= arrow_straight.png 覆盖 0.28×0.56）。 */
const ARROW_W = 0.28;
const ARROW_L = 0.56;
/** 箭头与停止线后沿间距。 */
const ARROW_GAP = 0.1;

/**
 * 路灯阵列点位（批次 20 §3.3 从 Road 组件抽出为纯函数，世界坐标；
 * 由 VirtualCityCityMap 汇总全部道路后交给 <StreetLightsInstanced> 实例化渲染）。
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

  const roadWidth = kind === 'main' ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE;

  // ── ① 路面贴图（批次 24：标线烘焙进整幅贴图；贴图覆盖 roadWidth×roadWidth，
  //    沿路长 repeat = round(len / roadWidth)。缺失 → 纯色 #1f2733 降级）。
  const surfaceName = kind === 'main' ? 'road_main' : 'road_side';
  const pbrName = kind === 'main' ? 'asphalt_main' : 'asphalt_side';
  const roadRepeatY = Math.max(1, Math.round(len / roadWidth));
  const roadTex = useStreetTile(surfaceName, 1, roadRepeatY);
  // PBR 复用 asphalt 对（normalScale [0.5,0.5]；标线平坦无需独立法线，文档 24 §4）。
  const asphaltPbr = useSharedPBR(
    streetTileUrl(pbrName),
    pbrNormalUrl('streets', pbrName),
    pbrRoughUrl('streets', pbrName),
    { wrap: 'repeat', repeat: [1, roadRepeatY], normalScale: [0.5, 0.5] },
  );

  // ── ② 双端斑马线（贴图缺失静默跳过，路面本身仍有降级色）
  const crosswalkTex = useStreetTile('crosswalk', 1, 1);
  // ── ③ 半幅停止线（仅主干道；贴图缺失跳过）
  const stoplineTex = useStreetTile('stopline', 1, 1);
  // ── ④ 直行导向箭头（贴图缺失跳过）
  const arrowTex = useStreetTile('arrow_straight', 1, 1);

  // ── ⑤ 人行道砖纹（贴图覆盖 0.25×0.25 方块 → repeat = round(len / 0.25)）
  const sidewalkRepeatY = Math.max(1, Math.round(len / SIDEWALK_TILE));
  const sidewalkName = kind === 'main' ? 'sidewalk_main' : 'sidewalk_side';
  const sidewalkTex = useStreetTile(sidewalkName, 1, sidewalkRepeatY);
  const sidewalkPbr = useSharedPBR(
    streetTileUrl(sidewalkName),
    pbrNormalUrl('streets', sidewalkName),
    pbrRoughUrl('streets', sidewalkName),
    { wrap: 'repeat', repeat: [1, sidewalkRepeatY], normalScale: [0.8, 0.8] },
  );

  // 标线纵向定位（§6.1）：斑马线中心 → 内边缘 → 停止线中心 → 箭头中心
  const stopZOrigin = CROSSWALK_T[1] * len - CROSSWALK_DEPTH / 2 - STOPLINE_GAP - STOPLINE_D / 2;
  const stopZDistrict = CROSSWALK_T[0] * len + CROSSWALK_DEPTH / 2 + STOPLINE_GAP + STOPLINE_D / 2;
  const arrowZOrigin = stopZOrigin - STOPLINE_D / 2 - ARROW_GAP - ARROW_L / 2;
  const arrowZDistrict = stopZDistrict + STOPLINE_D / 2 + ARROW_GAP + ARROW_L / 2;

  return (
    <group rotation={[0, angle, 0]} position={[from[0], 0, from[1]]}>
      {/* ① 主车道（z ∈ [-w/2, w/2]；标线烘焙贴图 / 纯色降级） */}
      <mesh
        rotation={[-Math.PI / 2, 0, 0]}
        position={[0, ROAD_Y, 0]}
        receiveShadow
      >
        <planeGeometry args={[roadWidth, len]} />
        <meshStandardMaterial
          {...withPBR(
            {
              map: roadTex ?? undefined,
              color: roadTex ? '#ffffff' : '#1f2733',
              roughness: 0.92,
              metalness: 0.05,
            },
            asphaltPbr,
          )}
        />
      </mesh>

      {/* ② 双端斑马线（全宽；置于停止线外侧更靠路端） */}
      {crosswalkTex &&
        CROSSWALK_T.map((t) => (
          <mesh
            key={`crosswalk-${t}`}
            rotation={[-Math.PI / 2, 0, 0]}
            position={[0, ROAD_Y + 0.002, t * len]}
          >
            <planeGeometry args={[roadWidth, CROSSWALK_DEPTH]} />
            <meshStandardMaterial
              map={crosswalkTex}
              color="#ffffff"
              roughness={0.9}
              transparent
              opacity={0.95}
            />
          </mesh>
        ))}

      {/* ③ 半幅停止线（仅主干道；原点端 local x>0、城区端 x<0，§6.1 右行推导） */}
      {kind === 'main' && stoplineTex && (
        <>
          <mesh
            rotation={[-Math.PI / 2, 0, 0]}
            position={[STOPLINE_X, ROAD_Y + 0.003, stopZOrigin]}
          >
            <planeGeometry args={[STOPLINE_W, STOPLINE_D]} />
            <meshStandardMaterial
              map={stoplineTex}
              color="#ffffff"
              roughness={0.9}
              transparent
              opacity={0.95}
            />
          </mesh>
          <mesh
            rotation={[-Math.PI / 2, 0, 0]}
            position={[-STOPLINE_X, ROAD_Y + 0.003, stopZDistrict]}
          >
            <planeGeometry args={[STOPLINE_W, STOPLINE_D]} />
            <meshStandardMaterial
              map={stoplineTex}
              color="#ffffff"
              roughness={0.9}
              transparent
              opacity={0.95}
            />
          </mesh>
        </>
      )}

      {/* ④ 直行导向箭头（每方向 1 枚，位于该方向停止线后方；
          plane +y 经 rotation.x=-π/2 映射到 local -z，故原点端（朝 +z）再绕
          plane 法线转 π 翻转贴图，城区端（朝 -z）保持默认） */}
      {kind === 'main' && arrowTex && (
        <>
          <mesh
            rotation={[-Math.PI / 2, 0, Math.PI]}
            position={[STOPLINE_X, ROAD_Y + 0.004, arrowZOrigin]}
          >
            <planeGeometry args={[ARROW_W, ARROW_L]} />
            <meshStandardMaterial
              map={arrowTex}
              color="#ffffff"
              roughness={0.9}
              transparent
            />
          </mesh>
          <mesh
            rotation={[-Math.PI / 2, 0, 0]}
            position={[-STOPLINE_X, ROAD_Y + 0.004, arrowZDistrict]}
          >
            <planeGeometry args={[ARROW_W, ARROW_L]} />
            <meshStandardMaterial
              map={arrowTex}
              color="#ffffff"
              roughness={0.9}
              transparent
            />
          </mesh>
        </>
      )}

      {/* ⑤ 两侧人行道（砖纹 0.25×0.25 平铺） */}
      {/* 左侧（local -x 侧）*/}
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
