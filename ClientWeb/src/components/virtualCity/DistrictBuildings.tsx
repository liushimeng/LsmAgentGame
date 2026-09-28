/**
 * DistrictBuildings — 城区楼群**合并渲染**（批次 30 A2 DC 攻坚）。
 *
 * 取代「逐栋 <BuildingMesh> + 逐栋 2 mesh 多材质组」（旧口径：160 栋 ×
 * 5~8 材质组 ≈ 1100 draw call）。同城区楼栋共享同一套立面/屋顶贴图与材质规格
 * （matSpecs 同签名）⇒ 全楼几何按材质分桶合并：
 *   - **每签名组 1 个墙体 mesh**（多 group，DC = 材质组数 5~8）；
 *   - **全区 1 个点缀 mesh**（顶点色单材质，DC = 1）；
 *   ⇒ 32 区合计 ≈ 200~300 draw call，为「每区 8~14 栋」的肌理加密腾出预算
 *   （目标：默认视角 DC ≤ 2397 基线，理想 ≤1800）。
 *
 * 逐栋 hover/信息卡保留：每栋 1 个**不可见代理盒**（visible=false，three Raycaster
 * 照常命中 —— 实测验证；不产生 draw call），挂 BuildingMesh 同款
 * useObjectInfoProps（building.<archetype> + 楼层/编号动态行）。
 *
 * 视觉取舍（实施记录有记）：
 *   - 同城区共享 buildingTint（原逐栋 w/d hash 0.92~1.08 灰度微分化 ⇒ 区内统一）；
 *   - 广告牌辉光 pointLight 不再逐栋挂（合并后光源数会爆炸；灯带 emissive 保留）。
 *
 * 窗灯昼夜（批次 30 B2）：材质建好后记 userData.baseEmissive，useFrame 读
 * cityTimeStore.getDayNight().dayFactor01 逐帧缩放（昼 ×0.25 / 夜 ×2.85）。
 */

import { memo, useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  districtFacadeUrl,
  districtRoofUrl,
  districtTextureStem,
  pbrNormalUrl,
  pbrRoughUrl,
} from '@/assets/images/virtualCity';
import type { VirtualCityDistrictDef } from '@/types/virtualCity';
import { DISTRICT_FLOORS, buildingHeight } from './cityScale';
import {
  buildBuildingParts,
  buildWallMaterial,
  buildingTint,
  tagBaseEmissive,
  ACCENT_ROUGH,
  ACCENT_METAL,
  DISTRICT_ARCHETYPE,
  type BuildingArchetype,
  type WallCtx,
  type WallMatSpec,
} from './building_shapes';
import type { BuildingSpec } from './building_layout';
import {
  mergeGrouped,
  mergeParts,
  useSharedTexture,
  useSharedPBR,
  type GroupedMergePart,
  type MergePart,
} from '@/engine3d';
import { useSynthPBR } from './cityPbr';
import { getDayNight } from './cityTimeStore';
import { useObjectInfoProps } from './objectInfo/useObjectInfoProps';

/** 部件平移（区中心局部系 → 楼栋落位）。 */
function offsetPart<T extends MergePart>(p: T, dx: number, dz: number): T {
  if (p.matrix) {
    const m = p.matrix.clone();
    const pos = new THREE.Vector3().setFromMatrixPosition(m);
    m.setPosition(pos.x + dx, pos.y, pos.z + dz);
    return { ...p, matrix: m };
  }
  return { ...p, x: (p.x ?? 0) + dx, z: (p.z ?? 0) + dz };
}

/** matSpecs 签名（分组合并键：kind 序列；glow 强度随 emissive 全区一致）。 */
function specsSignature(specs: WallMatSpec[]): string {
  return specs.map((s) => s.kind).join('|');
}

/** 逐栋不可见代理盒（hover/信息卡；不产生 draw call）。 */
function BuildingProxy({
  spec,
  h,
  archetype,
  def,
  floors,
}: {
  spec: BuildingSpec;
  h: number;
  archetype: BuildingArchetype;
  def: VirtualCityDistrictDef;
  floors: number;
}) {
  const t = useT();
  const info = useObjectInfoProps(`building.${archetype}`, {
    anchorY: 0.6,
    extra: [
      { label: 'district', value: t(`virtualCity.district.${def.id}` as TKey) },
      { label: 'floors', value: String(floors) },
      { label: 'serial', value: `b-${def.id}-${spec.idx ?? 0}` },
    ],
  });
  return (
    <mesh {...info} visible={false} position={[spec.x, h / 2, spec.z]}>
      <boxGeometry args={[spec.w, h, spec.d]} />
      <meshBasicMaterial />
    </mesh>
  );
}

interface Props {
  /** 楼群布局（building_layout.buildingsFor 产出）。 */
  specs: BuildingSpec[];
  def: VirtualCityDistrictDef;
  /** 当前房价指数（0.8–1.6 → prosperity 0–1）。 */
  prosperity: number;
}

// 批次 28 A1 memo 约定延续：specs（useMemo 布局）/ def（常量表）/ prosperity（原语）稳定。
export const DistrictBuildings = memo(function DistrictBuildings({ specs, def, prosperity }: Props) {
  // 18-X PBR 三件套 + 立面/屋顶贴图（原 BuildingMesh 同款；stem 拼接只在本文件发生）。
  const stem = districtTextureStem(def.id);
  const facadeBaseTex = useSharedTexture(districtFacadeUrl(stem, 'base'));
  const facadeMidTex = useSharedTexture(districtFacadeUrl(stem, 'mid'));
  const roofTex = useSharedTexture(districtRoofUrl(stem));
  const pbrBase = useSharedPBR(
    districtFacadeUrl(stem, 'base'),
    pbrNormalUrl('facades', `${stem}_base`),
    pbrRoughUrl('facades', `${stem}_base`),
    { normalScale: [0.8, 0.8] },
  );
  const pbrMid = useSharedPBR(
    districtFacadeUrl(stem, 'mid'),
    pbrNormalUrl('facades', `${stem}_mid`),
    pbrRoughUrl('facades', `${stem}_mid`),
    { normalScale: [0.8, 0.8] },
  );
  const roofPbr = useSharedPBR(
    districtRoofUrl(stem),
    pbrNormalUrl('roofs', stem),
    pbrRoughUrl('roofs', stem),
    { normalScale: [0.7, 0.7] },
  );
  // house/shed 屋面 synth 兜底（原 HouseShape/ShedShape 内 useSynthPBR 上移至区级）。
  const tilePbr = useSynthPBR('tile_roof', { wrap: 'repeat', repeat: [1, 1], normalScale: [1.0, 1.0] });
  const metalPbr = useSynthPBR('metal_deck', { wrap: 'repeat', repeat: [1, 1], normalScale: [0.9, 0.9] });

  const archetype = DISTRICT_ARCHETYPE[def.id] ?? 'slab';
  const emissive = Math.min(0.35, prosperity * 0.35);
  // 区内统一 tint（原逐栋 hash；合并取舍见文件头）。
  const tint = useMemo(() => buildingTint(1.7, 1.35), []);
  const ctx = useMemo<WallCtx>(
    () => ({
      facadeBase: facadeBaseTex,
      facadeMid: facadeMidTex,
      roofMap: roofTex,
      fallbackColor: def.color,
      emissive,
      tint,
      pbrBase,
      pbrMid,
      roofPbr,
      tilePbr,
      metalPbr,
    }),
    [facadeBaseTex, facadeMidTex, roofTex, def.color, emissive, tint, pbrBase, pbrMid, roofPbr, tilePbr, metalPbr],
  );

  // ── 几何合并（按 matSpecs 签名分组；同签名 ⇒ 单墙体 mesh 多 group）──
  const merged = useMemo(() => {
    const [minF, maxF] = DISTRICT_FLOORS[def.id];
    const groups = new Map<
      string,
      { matSpecs: WallMatSpec[]; wall: GroupedMergePart[] }
    >();
    const accents: MergePart[] = [];
    const proxyData: Array<{ spec: BuildingSpec; h: number; floors: number }> = [];
    for (const spec of specs) {
      const h = buildingHeight(minF + (maxF - minF) * prosperity) * (0.85 + spec.factor * 0.15);
      const parts = buildBuildingParts(archetype, spec.w, spec.d, h, emissive);
      const sig = specsSignature(parts.matSpecs);
      let g = groups.get(sig);
      if (!g) {
        g = { matSpecs: parts.matSpecs, wall: [] };
        groups.set(sig, g);
      }
      for (const p of parts.wallParts) g.wall.push(offsetPart(p, spec.x, spec.z));
      for (const p of parts.accentParts) accents.push(offsetPart(p, spec.x, spec.z));
      proxyData.push({
        spec,
        h,
        floors: Math.round(minF + (maxF - minF) * prosperity),
      });
    }
    return {
      wallGroups: [...groups.values()].map((g) => ({
        matSpecs: g.matSpecs,
        geo: mergeGrouped(g.wall),
      })),
      accentGeo: mergeParts(accents),
      hasAccent: accents.length > 0,
      proxyData,
    };
  }, [specs, def.id, prosperity, archetype, emissive]);

  // 几何 dispose（§92a；依赖变化即释放）。
  useEffect(
    () => () => {
      merged.wallGroups.forEach((g) => g.geo.dispose());
      merged.accentGeo.dispose();
    },
    [merged],
  );

  // 材质：每签名组一套（tagBaseEmissive 记基准，供窗灯昼夜缩放）。
  const materialGroups = useMemo(
    () =>
      merged.wallGroups.map((g) =>
        tagBaseEmissive(g.matSpecs.map((s) => buildWallMaterial(s, ctx))),
      ),
    [merged, ctx],
  );
  useEffect(
    () => () => materialGroups.forEach((ms) => ms.forEach((m) => m.dispose())),
    [materialGroups],
  );

  // ── 批次 30 B2：窗灯随昼夜（§130「声明了却从不接线」修复；昼 ×0.25 / 夜 ×2.85）──
  const matsRef = useRef(materialGroups);
  matsRef.current = materialGroups;
  useFrame(() => {
    const day = getDayNight()?.dayFactor01 ?? 1;
    const k = 0.25 + 2.6 * (1 - day);
    for (const ms of matsRef.current) {
      for (const m of ms) {
        const base = m.userData.baseEmissive as number | undefined;
        if (base !== undefined && m.emissive.getHex() !== 0) {
          m.emissiveIntensity = base * k;
        }
      }
    }
  });

  return (
    <group userData={{ bucket: 'buildings' }}>
      {merged.wallGroups.map((g, i) => (
        <mesh key={`wall-${i}`} geometry={g.geo} material={materialGroups[i]} castShadow />
      ))}
      {merged.hasAccent && (
        <mesh geometry={merged.accentGeo} castShadow={false} receiveShadow={false}>
          <meshStandardMaterial vertexColors roughness={ACCENT_ROUGH} metalness={ACCENT_METAL} />
        </mesh>
      )}
      {merged.proxyData.map((p) => (
        <BuildingProxy
          key={`proxy-${p.spec.idx ?? 0}`}
          spec={p.spec}
          h={p.h}
          archetype={archetype}
          def={def}
          floors={p.floors}
        />
      ))}
    </group>
  );
});

export default DistrictBuildings;
