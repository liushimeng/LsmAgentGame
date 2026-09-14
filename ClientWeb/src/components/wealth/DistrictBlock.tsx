/**
 * DistrictBlock — 单城区：8×8 底板（纹理缺失降级主色）+ 4–6 栋确定性伪随机楼群
 * + hover 信息卡（区名 / 房价 / 租金 / beta / 在区玩家数）。
 *
 * 楼群高度映射 price_index（0.8–1.6 → 1–5 单位）：繁荣期楼变高、萧条期变矮
 * = 可视化市场周期（前端架构文档 §3）。伪随机 **不用 Math.random**——seed 由
 * district id hash，重渲染布局稳定。
 */

import { useEffect, useMemo, useState } from 'react';
import * as THREE from 'three';
import { Html } from '@react-three/drei';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { districtTexture } from '@/assets/images/wealth';
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

interface BuildingSpec {
  x: number;        // 相对区中心偏移
  z: number;
  w: number;
  d: number;
  factor: number;   // 0.6–1.0 楼高系数
}

/** 楼群布局（seed = district id；与 price_index 无关，仅高度随行情缩放）。 */
function buildingsFor(def: WealthDistrictDef): BuildingSpec[] {
  const rnd = mulberry32(hashStr(def.id));
  const count = 4 + Math.floor(rnd() * 3); // 4–6 栋
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
  const [texture, setTexture] = useState<THREE.Texture | null>(null);
  const [hovered, setHovered] = useState(false);
  const texUrl = districtTexture(def.id);
  const buildings = useMemo(() => buildingsFor(def), [def]);

  // 纹理加载（失败静默降级主色底板 —— 降级策略 §9）。
  useEffect(() => {
    if (!texUrl) return;
    let disposed = false;
    const loader = new THREE.TextureLoader();
    loader.load(
      texUrl,
      (tex) => {
        if (disposed) {
          tex.dispose();
          return;
        }
        tex.colorSpace = THREE.SRGBColorSpace;
        setTexture(tex);
      },
      undefined,
      () => {
        // onError：保持 null → 纯色底板。
      },
    );
    return () => {
      disposed = true;
    };
  }, [texUrl]);

  // 繁荣度 → 楼高（price_index 0.8–1.6 → 1–5 单位）。
  const prosperity = Math.min(1, Math.max(0, (priceIndex - 0.8) / 0.8));
  const heightBase = 1 + prosperity * 4;

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
      {/* 楼群 */}
      {buildings.map((b, i) => {
        const h = heightBase * b.factor;
        return (
          <mesh
            key={i}
            castShadow
            position={[b.x, h / 2, b.z]}
          >
            <boxGeometry args={[b.w, h, b.d]} />
            <meshStandardMaterial
              color={def.color}
              roughness={0.65}
              metalness={0.15}
              emissive={def.color}
              emissiveIntensity={prosperity * 0.25}
            />
          </mesh>
        );
      })}
      {/* hover 信息卡 */}
      {hovered && (
        <Html position={[0, heightBase + 1.6, 0]} center distanceFactor={16}>
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
