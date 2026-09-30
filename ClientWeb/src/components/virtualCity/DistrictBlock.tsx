/**
 * DistrictBlock — 单城区：8×8 底板（纹理缺失降级主色）+ 街墙楼群 + hover 信息卡
 * （区名 / 房价 / 租金 / beta / 在区玩家数）。
 *
 * P1-B 改造：楼群渲染段由 inline boxGeometry 改为 <BuildingMesh />；
 * 13-3D优化 阶段 B：BuildingMesh 内部按 DISTRICT_ARCHETYPE 分发体块组合
 * （tower/slab/house/shed/pavilion，见 building_shapes.tsx），缺失 → 退色。
 *
 * 批次 30 A2：楼群布局升格「街墙」（building_layout.buildingsFor：每区 8~14 栋、
 * 贴街区边界留 3.5~6 m 退线），渲染改 <DistrictBuildings> **区级合并**
 * （每区 1 墙体 mesh 多 group + 1 点缀 mesh，逐栋 hover 由不可见代理盒保留）。
 * 楼群高度映射 price_index（0.8–1.6 → 繁荣度 0–1，在城区楼层区间内插值，
 * 见 cityScale.ts DISTRICT_FLOORS）：繁荣期楼变高、萧条期变矮
 * = 可视化市场周期（前端架构文档 §3）。伪随机 **不用 Math.random**——seed 由
 * district id hash，重渲染布局稳定。
 *
 * 批次 27 §4.3：中央公园草地覆盖层按季节换贴图（seasonAssets 适配：
 * 夏 grass_tile / 春秋冬季节贴图，缺失回退 grass_tile），低频订阅不抖动布局。
 * 批次 30 A3/P1-10：马路牙子 u(0.5)=50cm「黑框」→ 真实 15 cm 浅色路缘，
 * 且 4 条经 building_shapes.mergeBoxes 合为 1 mesh（每区省 3 draw call）。
 */

import { memo, useCallback, useEffect, useMemo, useState } from 'react';
import * as THREE from 'three';
import { Html } from '@react-three/drei';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { districtTexture, districtTextureStem, groundTileUrl, pbrNormalUrl, pbrRoughUrl } from '@/assets/images/virtualCity';
import { buildingsFor } from './building_layout';
import { DistrictBuildings } from './DistrictBuildings';
import { mergeBoxes, type BoxSpec } from './building_shapes';
import { DISTRICT_FLOORS, DISTRICT_SURFACE_Y, buildingHeight, prosperityOf, u } from './cityScale';
import { useSharedPBR, useSharedTexture, withPBR } from '@/engine3d';
import { currentSeason, subscribeSeason } from './cityTimeStore';
import type { CitySeason } from './cityTimeStore';
import { seasonGrassTileUrl } from './seasonAssets';
import { useObjectInfoProps } from './objectInfo/useObjectInfoProps';
import {
  VIRTUAL_CITY_DISTRICTS,
  formatCny,
  type VirtualCityDistrictDef,
  type VirtualCityDistrictId,
} from '@/types/virtualCity';

interface Props {
  def: VirtualCityDistrictDef;
  /** 当前房价指数（缺省 1.0）。 */
  priceIndex: number;
  /** 在区玩家数（hover 卡展示）。 */
  playerCount: number;
  /** 是否被选中（小地图 / 面板点击）。 */
  selected: boolean;
  onSelect: (id: VirtualCityDistrictId) => void;
}

// 批次 28 A1：memo —— props 全为稳定引用（def 常量表 / 原语 / useCallback），
// 静止时父层 re-render 直接 bail out，消除 4Hz 全树 reconcile 的逐块开销。
export const DistrictBlock = memo(function DistrictBlock({ def, priceIndex, playerCount, selected, onSelect }: Props) {
  const t = useT();
  const [hovered, setHovered] = useState(false);
  const texUrl = districtTexture(def.id);
  const buildings = useMemo(() => buildingsFor(def), [def]);
  // 批次 30 A3/P1-10：四边路缘 mergeBoxes 合 1 mesh（真实 15 cm 高浅色路缘）。
  const curbGeo = useMemo(() => {
    const specs: BoxSpec[] = [
      { x: 0, y: u(0.075), z: -3.95, w: 8.1, h: u(0.15), d: 0.18 },
      { x: 0, y: u(0.075), z: 3.95, w: 8.1, h: u(0.15), d: 0.18 },
      { x: -3.95, y: u(0.075), z: 0, w: 0.18, h: u(0.15), d: 8.1 },
      { x: 3.95, y: u(0.075), z: 0, w: 0.18, h: u(0.15), d: 8.1 },
    ];
    return mergeBoxes(specs);
  }, []);
  useEffect(() => () => curbGeo.dispose(), [curbGeo]);

  // 14-3D渲染深化：共享贴图缓存（失败静默降级主色底板 —— 降级策略 §9）。
  const texture = useSharedTexture(texUrl);
  // 18-X：底板 PBR（02 §2.3 行 4：districts/<id>，normalScale [0.5,0.5]）。
  // 批次 20 §3.5：16 新区贴图 stem 先查别名（districtTexture 内部同源已解析，
  // PBR 名需显式走 districtTextureStem）。
  const stem = districtTextureStem(def.id);
  const boardPbr = useSharedPBR(
    texUrl,
    pbrNormalUrl('districts', stem),
    pbrRoughUrl('districts', stem),
    { normalScale: [0.5, 0.5] },
  );
  // 地表覆盖层：中央公园草地 / 交通枢纽+金融广场（缺失降级纯色）
  const isPark = def.id === 'central_park';
  const isPlaza = def.id === 'transport_hub' || def.id === 'finance';
  // 批次 27：草地贴图随季节（seasonAssets 缺键回退 grass_tile；非公园城区传 '' 不加载）。
  const [season, setSeason] = useState<CitySeason>(currentSeason);
  useEffect(() => subscribeSeason(setSeason), []);
  const grassTileUrl = isPark ? seasonGrassTileUrl(season) : '';
  const grassTex = useSharedTexture(grassTileUrl, {
    wrap: 'repeat', repeat: [4, 4],
  });
  // 18-X：草地 PBR（02 §2.3 行 5：ground/grass_tile，normalScale [0.7,0.7]；
  // 季节色图沿用夏季法线/粗糙度——法线不带色相，视觉无损）。
  const grassPbr = useSharedPBR(
    grassTileUrl,
    pbrNormalUrl('ground', 'grass_tile'),
    pbrRoughUrl('ground', 'grass_tile'),
    { wrap: 'repeat', repeat: [4, 4], normalScale: [0.7, 0.7] },
  );
  const plazaTex = useSharedTexture(isPlaza ? groundTileUrl('plaza_tile') : '', {
    wrap: 'repeat', repeat: [3, 3],
  });
  // 18-X：广场 PBR（02 §2.3 行 6：ground/plaza_tile，normalScale [0.8,0.8]）。
  const plazaPbr = useSharedPBR(
    isPlaza ? groundTileUrl('plaza_tile') : '',
    pbrNormalUrl('ground', 'plaza_tile'),
    pbrRoughUrl('ground', 'plaza_tile'),
    { wrap: 'repeat', repeat: [3, 3], normalScale: [0.8, 0.8] },
  );

  // 繁荣度 → 楼高（price_index 0.8–1.6 → 0–1；实际楼高公式在 BuildingMesh，
  // 按 cityScale.DISTRICT_FLOORS 分城区楼层区间插值）。
  // 批次 32：公式上提到 cityScale.prosperityOf（渲染与相机碰撞体共用单一事实来源）。
  const prosperity = prosperityOf(priceIndex);
  // hover 卡定位基准：本城区最高楼层对应的世界单位楼高（2026-09-21 高度系统）。
  const heightBase = buildingHeight(DISTRICT_FLOORS[def.id][1]);

  // hover 信息卡数据：当前房价（万元）= 基准 × beta × 指数（《后端架构》§4 公式）。
  const housePriceWan = Math.round(def.basePriceWan * def.houseBeta * priceIndex);
  const rentCny = Math.round(def.basePriceWan * 10000 * def.houseBeta * priceIndex * 0.0016);

  const idx = VIRTUAL_CITY_DISTRICTS.findIndex((d) => d.id === def.id);

  // 批次 28 B2：保留既有 hover 卡（房价/租金/β 不动），只补 click 详情卡
  // （hover:false = 不写全局悬浮卡，避免与本卡叠卡）。extra 动态行注入行情快照。
  const info = useObjectInfoProps(`district.${def.id}`, {
    anchorY: heightBase + 1.0,
    hover: false,
    onSelected: () => onSelect(def.id),
    extra: [
      { label: 'housePrice', value: `${housePriceWan} 万` },
      { label: 'rent', value: `${formatCny(rentCny)}/月` },
      { label: 'beta', value: def.houseBeta.toFixed(2) },
      { label: 'players', value: String(playerCount) },
      { label: 'serial', value: `#${idx + 1}` },
    ],
  });
  // hover 卡 + 描边金环的本地态（既有行为），叠加 hook 的 cursor/stopPropagation。
  const handleOver = useCallback(
    (e: Parameters<typeof info.onPointerOver>[0]) => {
      info.onPointerOver(e);
      setHovered(true);
    },
    [info],
  );
  const handleOut = useCallback(
    (e: Parameters<typeof info.onPointerOut>[0]) => {
      info.onPointerOut(e);
      setHovered(false);
    },
    [info],
  );

  return (
    <group
      position={[def.x, 0, def.z]}
      onPointerOver={handleOver}
      onPointerOut={handleOut}
      onClick={info.onClick}
    >
      {/* 底板 8×8（纹理缺失降级主色）。
          批次 38 §4.6：y 0.02 → DISTRICT_SURFACE_Y(0.010)，低于路面 0.015 ⇒
          「路在板上」—— 街区内部道路不再被底板吞没（降级方案优先，零几何开口）。 */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, DISTRICT_SURFACE_Y, 0]} receiveShadow>
        <planeGeometry args={[8, 8]} />
        {texture ? (
          <meshStandardMaterial {...withPBR({ map: texture, roughness: 0.9 }, boardPbr)} />
        ) : (
          <meshStandardMaterial color={def.color} roughness={0.9} />
        )}
      </mesh>
      {/* 14-3D渲染深化：中央公园草地覆盖层（y=0.026 防 z-fighting） */}
      {isPark && (
        <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.026, 0]} receiveShadow>
          <planeGeometry args={[7.8, 7.8]} />
          <meshStandardMaterial
            {...withPBR(
              { map: grassTex ?? undefined, color: grassTex ? '#ffffff' : '#3f7a3a', roughness: 0.95 },
              grassPbr,
            )}
          />
        </mesh>
      )}
      {/* 14-3D渲染深化：广场铺装（交通枢纽南半 6×4 / 金融 CBD 中心 3×3） */}
      {isPlaza && (
        <mesh
          rotation={[-Math.PI / 2, 0, 0]}
          position={[0, 0.025, def.id === 'transport_hub' ? 2.0 : 0]}
          receiveShadow
        >
          <planeGeometry args={def.id === 'transport_hub' ? [6, 4] : [3, 3]} />
          <meshStandardMaterial
            {...withPBR(
              { map: plazaTex ?? undefined, color: plazaTex ? '#ffffff' : '#9aa1ab', roughness: 0.85 },
              plazaPbr,
            )}
          />
        </mesh>
      )}
      {/* 14-3D渲染深化：马路牙子 curb（底板四边窄条）。
          批次 30 A3/P1-10：原 u(0.5)=50 cm 高深色条读作「黑框棋盘格」⇒
          真实路缘 15 cm + 浅色；4 条经 mergeBoxes 合 1 mesh（×32 区省 3 DC/区）。 */}
      <mesh geometry={curbGeo} position={[0, 0.03, 0]}>
        <meshStandardMaterial color="#7a828e" roughness={0.9} />
      </mesh>
      {/* 选中 / 悬停描边 */}
      {(selected || hovered) && (
        <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.04, 0]}>
          <ringGeometry args={[3.9, 4.15, 4, 1, Math.PI / 4]} />
          <meshBasicMaterial
            color={selected ? '#d4a017' : '#e5e7eb'}
            transparent
            opacity={0.9}
            side={THREE.DoubleSide}
          />
        </mesh>
      )}
      {/* 楼群（批次 30 A2：街墙 8~14 栋 + DistrictBuildings 区级合并） */}
      <DistrictBuildings specs={buildings} def={def} prosperity={prosperity} />
      {/* hover 信息卡（阶段 E：zIndexRange [30,0] 封顶 —— 不盖小地图 z40 /
          error banner z50；默认 16777271 会压住一切，契约 04 文档 §4） */}
      {hovered && (
        <Html position={[0, heightBase + 1.0, 0]} center distanceFactor={16} zIndexRange={[30, 0]}>
          <div className="virtualCity-district-card">
            <div className="virtualCity-district-card__name">
              {t(`virtualCity.district.${def.id}` as TKey)} · #{idx + 1}
            </div>
            <div className="virtualCity-district-card__row">
              <span>{t('virtualCity.housePrice' as TKey)}</span>
              <b>{housePriceWan} 万</b>
            </div>
            <div className="virtualCity-district-card__row">
              <span>{t('virtualCity.map.rent' as TKey)}</span>
              <b>{formatCny(rentCny)}/月</b>
            </div>
            <div className="virtualCity-district-card__row">
              <span>β</span>
              <b>{def.houseBeta.toFixed(2)}</b>
            </div>
            <div className="virtualCity-district-card__row">
              <span>{t('virtualCity.map.players' as TKey)}</span>
              <b>{playerCount}</b>
            </div>
          </div>
        </Html>
      )}
    </group>
  );
});
