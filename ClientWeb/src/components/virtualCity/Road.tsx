/**
 * Road — 3D 城市场景**单段路面**组件（批次 24 重排 → 批次 31 二轮精简）：
 *
 * 批次 31 二轮（draw call 攻坚，02-实施记录二轮）：只渲染 ① 路面
 * （road_main / road_side 整幅贴图，标线已烘焙：中心黄虚线 + 白色边缘线；
 * 贴图覆盖 roadWidth×roadWidth，沿路长 repeat；缺失 → 纯色 #1f2733）。
 * 斑马线/停止线/箭头/人行道已全城合并至 <RoadMarkings>（5 draw call）。
 *
 * 批次 31 一轮新增 yOffset（§2.4 平面交叉分层：spoke 0 / connector 0.002 /
 * arterial 0.004；一环路 FirstRingRoad 0.007 不在本组件）。
 *
 * 批次 24 变更（保留）：
 *   - 删除中心线独立面片（centerline.png）与程序化箭头组 / 程序化停止线——
 *     标线已烘焙进 road_main / road_side 整幅贴图（文档 24 §4）。
 *   - PBR 沿用 asphalt_main / asphalt_side 的 _n/_r 对（标线平坦无需独立法线）。
 *
 * 路灯不逐盏渲染在本组件内（批次 20 §3.3）：点位生成通式 lampsForRoad
 * 由 VirtualCityCityMap 汇总后交 <StreetLightsInstanced> 全局实例化。
 */

import type { Texture } from 'three';
import { streetTileUrl, pbrNormalUrl, pbrRoughUrl, type StreetTileName } from '@/assets/images/virtualCity';
import type { InstancedLamp } from './props/StreetLightsInstanced';
import { useSharedPBR, useSharedTexture, withPBR } from '@/engine3d';
import { ROAD_SURFACE_Y, spacing } from './cityScale';
import { useObjectInfoProps } from './objectInfo/useObjectInfoProps';

interface Props {
  /** 道路起点世界坐标（城区中心）。 */
  from: [number, number];
  /** 道路终点世界坐标（金融 CBD 原点）。 */
  to: [number, number];
  /** 'main' = 主干道（双车道标线 + 停止线/箭头）；'side' = 次干道（仅边缘线）。 */
  kind: 'main' | 'side';
  /**
   * 平面交叉微抬（批次 31 §2.4 分层：spoke/edgeLink 0、connector 0.002、
   * arterial 0.004），防同 Y 路面 z-fighting；缺省 0。
   */
  yOffset?: number;
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
export const ROAD_WIDTH_MAIN = 1.4;
/** 次干道路面宽度。 */
export const ROAD_WIDTH_SIDE = 0.9;
/** 主干道路灯间距（批次 30 A4：cityScale.REAL_SPACING_M.lampMain = 37 m，两侧交替）。 */
const LAMP_SPACING_MAIN = spacing('lampMain');
/** 次干道路灯间距（REAL_SPACING_M.lampSide = 50 m，两侧交替）。 */
const LAMP_SPACING_SIDE = spacing('lampSide');
/** 路面 y 抬高（避免 z-fighting with ground）；唯一事实来源在 cityScale.ROAD_SURFACE_Y
 *  （车辆/行人落地基准与之同源）。 */
const ROAD_Y = ROAD_SURFACE_Y;
/**
 * 路灯阵列点位（批次 20 §3.3 从 Road 组件抽出为纯函数，世界坐标；
 * 由 VirtualCityCityMap 汇总全部道路后交给 <StreetLightsInstanced> 实例化渲染）。
 * 批次 30 A4：间距取 cityScale.REAL_SPACING_M（主 37 m / 次 50 m），**两侧交替**
 * （side = ±1 随 i 奇偶翻转，灯臂朝路面），消除旧「25 m 单侧密排」杆林；
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
    const side: 1 | -1 = i % 2 === 0 ? 1 : -1; // 两侧交替
    out.push({ x: x + nx * sideOffset * side, z: z + nz * sideOffset * side, rot: angle, kind, side });
  }
  return out;
}

export function Road({ from, to, kind, yOffset = 0 }: Props) {
  // from → to 向量
  const dx = to[0] - from[0];
  const dz = to[1] - from[1];
  const len = Math.sqrt(dx * dx + dz * dz);
  const angle = Math.atan2(dx, dz); // around Y

  const roadWidth = kind === 'main' ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE;
  // 批次 31：分层 y（§2.4 平面交叉微抬）
  const roadY = ROAD_Y + yOffset;
  // 批次 28 B2：道路信息交互（主/次干道共用 road.surface 文案，kind 进动态行）。
  // 批次 31 二轮：每段仅路面一 mesh，按路 hover 语义不变。
  const info = useObjectInfoProps('road.surface', {
    anchorY: 0.3,
    extra: [{ label: 'variant', value: kind }],
  });

  // ── ① 路面贴图（批次 24：标线烘焙进整幅贴图；贴图覆盖 roadWidth×roadWidth，
  //    沿路长 repeat = round(len / roadWidth)。缺失 → 纯色 #1f2733 降级）。
  //    批次 31 二轮：人行道 PBR 对随合并迁往 RoadMarkings，此处仅 asphalt 对。
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

  return (
    <group {...info} rotation={[0, angle, 0]} position={[from[0], 0, from[1]]}>
      {/* ① 主车道（z ∈ [-w/2, w/2]；标线烘焙贴图 / 纯色降级） */}
      <mesh
        rotation={[-Math.PI / 2, 0, 0]}
        position={[0, roadY, 0]}
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
    </group>
  );
}
