/**
 * DistrictBlock — 单城区：8×8 底板（纹理缺失降级主色）+ 确定性伪随机楼群
 * （central_park 1-2 栋 pavilion，其余城区 4–6 栋）+ hover 信息卡（区名 / 房价 / 租金 / beta / 在区玩家数）。
 *
 * P1-B 改造：楼群渲染段由 inline boxGeometry 改为 <BuildingMesh />；
 * 13-3D优化 阶段 B：BuildingMesh 内部按 DISTRICT_ARCHETYPE 分发体块组合
 * （tower/slab/house/shed/pavilion，见 building_shapes.tsx），缺失 → 退色。
 *
 * 楼群高度映射 price_index（0.8–1.6 → 繁荣度 0–1，在城区楼层区间内插值，
 * 见 cityScale.ts DISTRICT_FLOORS）：繁荣期楼变高、萧条期变矮
 * = 可视化市场周期（前端架构文档 §3）。伪随机 **不用 Math.random**——seed 由
 * district id hash，重渲染布局稳定。
 */

import { useMemo, useState } from 'react';
import * as THREE from 'three';
import { Html } from '@react-three/drei';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { districtTexture, groundTileUrl } from '@/assets/images/wealth';
import { BuildingMesh, type BuildingSpec } from './BuildingMesh';
import { DISTRICT_FLOORS, buildingHeight, u } from './cityScale';
import { useSharedTexture } from './textureCache';
import {
  WEALTH_DISTRICTS,
  formatCny,
  type WealthDistrictDef,
  type WealthDistrictId,
} from '@/types/wealth';

// ── 确定性伪随机（FNV-1a hash + mulberry32；重渲染布局稳定）──────

function hashStr(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// BuildingSpec 由 ./BuildingMesh 统一导出，此处不再重复定义

/** 楼群布局（seed = district id；与 price_index 无关，仅高度随行情缩放）。
 *  13-3D优化 阶段 B：central_park 楼数 1-2 栋（pavilion 景观建筑，
 *  绿化交给 StreetPropsLayer 树群），其余城区不变 4-6 栋。 */
function buildingsFor(def: WealthDistrictDef): BuildingSpec[] {
  const rnd = mulberry32(hashStr(def.id));
  const isPark = def.id === 'central_park';
  const count = isPark ? 1 + (rnd() < 0.5 ? 1 : 0) : 4 + Math.floor(rnd() * 3);
  const out: BuildingSpec[] = [];
  for (let i = 0; i < count; i++) {
    // 3×2 网格 + 抖动，保证楼间留缝不重叠。
    const col = i % 3;
    const row = Math.floor(i / 3);
    const gx = (col - 1) * 2.4 + (rnd() - 0.5) * 0.7;
    const gz = (row - 0.5) * 2.6 + (rnd() - 0.5) * 0.7;
    const w = 1.1 + rnd() * 0.8;
    const d = 1.1 + rnd() * 0.8;
    out.push({ x: gx, z: gz, w, d, factor: 0.6 + rnd() * 0.4 });
  }
  return out;
}

interface Props {
  def: WealthDistrictDef;
  /** 当前房价指数（缺省 1.0）。 */
  priceIndex: number;
  /** 在区玩家数（hover 卡展示）。 */
  playerCount: number;
  /** 是否被选中（小地图 / 面板点击）。 */
  selected: boolean;
  onSelect: (id: WealthDistrictId) => void;
}

export function DistrictBlock({ def, priceIndex, playerCount, selected, onSelect }: Props) {
  const t = useT();
  const [hovered, setHovered] = useState(false);
  const texUrl = districtTexture(def.id);
  const buildings = useMemo(() => buildingsFor(def), [def]);

  // 14-3D渲染深化：共享贴图缓存（失败静默降级主色底板 —— 降级策略 §9）。
  const texture = useSharedTexture(texUrl);
  // 地表覆盖层：中央公园草地 / 交通枢纽+金融广场（缺失降级纯色）
  const isPark = def.id === 'central_park';
  const isPlaza = def.id === 'transport_hub' || def.id === 'finance';
  const grassTex = useSharedTexture(isPark ? groundTileUrl('grass_tile') : '', {
    wrap: 'repeat', repeat: [4, 4],
  });
  const plazaTex = useSharedTexture(isPlaza ? groundTileUrl('plaza_tile') : '', {
    wrap: 'repeat', repeat: [3, 3],
  });

  // 繁荣度 → 楼高（price_index 0.8–1.6 → 0–1；实际楼高公式在 BuildingMesh，
  // 按 cityScale.DISTRICT_FLOORS 分城区楼层区间插值）。
  const prosperity = Math.min(1, Math.max(0, (priceIndex - 0.8) / 0.8));
  // hover 卡定位基准：本城区最高楼层对应的世界单位楼高（2026-09-21 高度系统）。
  const heightBase = buildingHeight(DISTRICT_FLOORS[def.id][1]);

  // hover 信息卡数据：当前房价（万元）= 基准 × beta × 指数（《后端架构》§4 公式）。
  const housePriceWan = Math.round(def.basePriceWan * def.houseBeta * priceIndex);
  const rentCny = Math.round(def.basePriceWan * 10000 * def.houseBeta * priceIndex * 0.0016);

  const idx = WEALTH_DISTRICTS.findIndex((d) => d.id === def.id);

  return (
    <group
      position={[def.x, 0, def.z]}
      onPointerOver={(e) => {
        e.stopPropagation();
        setHovered(true);
      }}
      onPointerOut={() => setHovered(false)}
      onClick={(e) => {
        e.stopPropagation();
        onSelect(def.id);
      }}
    >
      {/* 底板 8×8（纹理缺失降级主色） */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.02, 0]} receiveShadow>
        <planeGeometry args={[8, 8]} />
        {texture ? (
          <meshStandardMaterial map={texture} />
        ) : (
          <meshStandardMaterial color={def.color} roughness={0.9} />
        )}
      </mesh>
      {/* 14-3D渲染深化：中央公园草地覆盖层（y=0.026 防 z-fighting） */}
      {isPark && (
        <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.026, 0]} receiveShadow>
          <planeGeometry args={[7.8, 7.8]} />
          <meshStandardMaterial
            map={grassTex ?? undefined}
            color={grassTex ? '#ffffff' : '#3f7a3a'}
            roughness={0.95}
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
            map={plazaTex ?? undefined}
            color={plazaTex ? '#ffffff' : '#9aa1ab'}
            roughness={0.85}
          />
        </mesh>
      )}
      {/* 14-3D渲染深化：马路牙子 curb（底板四边窄条，模拟真实城区路缘） */}
      {([
        [0, -3.95, 8.1, 0.18],
        [0, 3.95, 8.1, 0.18],
        [-3.95, 0, 0.18, 8.1],
        [3.95, 0, 0.18, 8.1],
      ] as Array<[number, number, number, number]>).map(([cx, cz, cw, cd], i) => (
        <mesh key={`curb-${i}`} position={[cx, 0.03, cz]}>
          <boxGeometry args={[cw, u(0.5), cd]} />
          <meshStandardMaterial color="#3a414c" roughness={0.9} />
        </mesh>
      ))}
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
      {/* 楼群（P1-B：BuildingMesh 接管，单 box → 4 侧面 + 顶面） */}
      {buildings.map((b, i) => (
        <BuildingMesh
          key={i}
          spec={b}
          def={def}
          prosperity={prosperity}
        />
      ))}
      {/* hover 信息卡（阶段 E：zIndexRange [30,0] 封顶 —— 不盖小地图 z40 /
          error banner z50；默认 16777271 会压住一切，契约 04 文档 §4） */}
      {hovered && (
        <Html position={[0, heightBase + 1.0, 0]} center distanceFactor={16} zIndexRange={[30, 0]}>
          <div className="wealth-district-card">
            <div className="wealth-district-card__name">
              {t(`wealth.district.${def.id}` as TKey)} · #{idx + 1}
            </div>
            <div className="wealth-district-card__row">
              <span>{t('wealth.housePrice' as TKey)}</span>
              <b>{housePriceWan} 万</b>
            </div>
            <div className="wealth-district-card__row">
              <span>{t('wealth.map.rent' as TKey)}</span>
              <b>{formatCny(rentCny)}/月</b>
            </div>
            <div className="wealth-district-card__row">
              <span>β</span>
              <b>{def.houseBeta.toFixed(2)}</b>
            </div>
            <div className="wealth-district-card__row">
              <span>{t('wealth.map.players' as TKey)}</span>
              <b>{playerCount}</b>
            </div>
          </div>
        </Html>
      )}
    </group>
  );
}
