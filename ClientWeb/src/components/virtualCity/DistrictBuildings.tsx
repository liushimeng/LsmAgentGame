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
  facadeTilePbrUrl,
  facadeTileUrl,
  facadeTileLitUrl,
  districtFacadeUrl,
  districtRoofUrl,
  districtTextureStem,
  pbrNormalUrl,
  pbrRoughUrl,
} from '@/assets/images/virtualCity';
import type { VirtualCityDistrictDef } from '@/types/virtualCity';
import { buildingFloorsOf, buildingTopY } from './cityScale';
import {
  buildBuildingParts,
  buildWallMaterial,
  buildingTint,
  districtFacadeUvOffset,
  tagBaseEmissive,
  ACCENT_ROUGH,
  ACCENT_METAL,
  DISTRICT_ARCHETYPE,
  type BuildingArchetype,
  type WallCtx,
  type WallMatSpec,
  warnFacadeDegradeOnce,
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

/**
 * 批次 39 B1 · 降级态 3 告警（dev-only，每城区打印**一次**；范式同
 * `building_shapes::warnFacadeDegradeOnce` 的模块级 Set 去重）。
 *
 * 场景：开间贴图 `facade_tiles/<family>_{base,mid}.png` 在位（物理 UV 管线正常），
 * 但**亮窗遮罩 `<family>_lit.png` 缺失** ⇒ 侧墙 `emissiveIntensity = 0`，
 * 该城区夜间**没有窗灯**。这是有记录的安全降级（宁可无灯，不可整墙发光），
 * 但必须可见 —— 静默降级正是批次 37 起要根除的失效模式。
 */
const LIT_MASK_DEGRADE_WARNED = new Set<string>();

function warnLitMaskDegradeOnce(districtId: string): void {
  if (!import.meta.env.DEV) return;
  if (LIT_MASK_DEGRADE_WARNED.has(districtId)) return;
  LIT_MASK_DEGRADE_WARNED.add(districtId);
  console.warn(
    `[DistrictBuildings] 城区 ${districtId}：facade_tiles/<family>_lit.png 缺失 ⇒ ` +
    '该区侧墙 emissiveIntensity = 0，夜间无窗灯（安全降级：不回退到 albedo，' +
    '否则整个墙面连窗间墙一起发光 = 批次 39 要根除的缺陷 B3）。' +
    '补齐亮窗遮罩后自动恢复，无需改代码。',
  );
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
  //
  // 批次 37 P1 · 立面贴图三级降级链（任一级缺失都只影响该级、不产生新变形）：
  //   1) facade_tiles/<stem>_{base,mid}.png 存在 → 物理尺寸 UV + wrap:'repeat'（零变形）；
  //   2) 回落 facades/<stem>_{base,mid}.png（整栋立面图）→ 沿用 0..1 UV
  //      （批次 37 §1.3 的 D1 变形仍在，但这是**已知且被记录**的降级态，并 dev 告警一次）；
  //   3) 颜色图也缺失 → 纯色 fallbackColor（零回归）。
  // 开关 `facadeTiles` 同时统辖几何 UV（wallBox）与贴图包裹（wrap），二者必须同源切换。
  const stem = districtTextureStem(def.id);
  // 开间贴图按**材质族**出图（16 族复用 32 城区），故传原始城区 id def.id ——
  // 若先经 districtTextureStem 别名（旧 16 区→新区的复用表）会把 mountain_resort
  // 提前改写成 suburb，丢掉 resort_wood 的材质族归属。映射见 FACADE_TILE_STEM。
  const tileBaseUrl = facadeTileUrl(def.id, 'base');
  const tileMidUrl = facadeTileUrl(def.id, 'mid');
  const legacyBaseUrl = districtFacadeUrl(stem, 'base');
  const legacyMidUrl = districtFacadeUrl(stem, 'mid');
  const facadeTiles = tileBaseUrl !== '' && tileMidUrl !== '';
  // 降级态 2 告警（dev-only，每 stem×variant 一次）。
  useEffect(() => {
    if (facadeTiles) return;
    warnFacadeDegradeOnce(stem, 'base', legacyBaseUrl);
    warnFacadeDegradeOnce(stem, 'mid', legacyMidUrl);
  }, [facadeTiles, stem, legacyBaseUrl, legacyMidUrl]);

  const facadePbrOpts = facadeTiles
    ? { wrap: 'repeat' as const, normalScale: [0.8, 0.8] as [number, number] }
    : { normalScale: [0.8, 0.8] as [number, number] };
  const pbrBase = useSharedPBR(
    facadeTiles ? tileBaseUrl : legacyBaseUrl,
    facadeTiles ? facadeTilePbrUrl('n', def.id, 'base') : pbrNormalUrl('facades', `${stem}_base`),
    facadeTiles ? facadeTilePbrUrl('r', def.id, 'base') : pbrRoughUrl('facades', `${stem}_base`),
    facadePbrOpts,
  );
  const pbrMid = useSharedPBR(
    facadeTiles ? tileMidUrl : legacyMidUrl,
    facadeTiles ? facadeTilePbrUrl('n', def.id, 'mid') : pbrNormalUrl('facades', `${stem}_mid`),
    facadeTiles ? facadeTilePbrUrl('r', def.id, 'mid') : pbrRoughUrl('facades', `${stem}_mid`),
    facadePbrOpts,
  );
  // 立面颜色图统一取自 PBR 三件套的 map（保证 map 与 normalMap/roughnessMap 包裹一致）。
  const facadeBaseTex = pbrBase.map;
  const facadeMidTex = pbrMid.map;
  const roofUrl = districtRoofUrl(stem);
  const roofTex = useSharedTexture(roofUrl, facadeTiles ? { wrap: 'repeat' } : undefined);
  const roofPbrOpts = facadeTiles
    ? { wrap: 'repeat' as const, normalScale: [0.7, 0.7] as [number, number] }
    : { normalScale: [0.7, 0.7] as [number, number] };
  const roofPbr = useSharedPBR(
    roofUrl,
    pbrNormalUrl('roofs', stem),
    pbrRoughUrl('roofs', stem),
    roofPbrOpts,
  );
  // house/shed 屋面 synth 兜底（原 HouseShape/ShedShape 内 useSynthPBR 上移至区级）。
  const tilePbr = useSynthPBR('tile_roof', { wrap: 'repeat', repeat: [1, 1], normalScale: [1.0, 1.0] });
  const metalPbr = useSynthPBR('metal_deck', { wrap: 'repeat', repeat: [1, 1], normalScale: [0.9, 0.9] });

  // 批次 39 B1：立面**亮窗遮罩**（`facade_tiles/<family>_lit.png`，只有窗玻璃非黑）。
  // 侧墙 emissiveMap 的唯一来源 —— 批次 39 之前挂的是立面 albedo（`pbrBase.map`），
  // 整个墙面（窗间墙 / 贴图里画好的空调外机）一起发光。
  // 缺失 ⇒ null ⇒ 材质 emissiveIntensity = 0（夜间无窗灯），**不回退 albedo**。
  // 包裹必须 repeat：与 albedo 共用同一套几何 UV（值可 > 1，批次 37 P1 口径）。
  const litUrl = facadeTiles ? facadeTileLitUrl(def.id) : '';
  const litMap = useSharedTexture(litUrl, facadeTiles ? { wrap: 'repeat' } : undefined);
  // 降级态 3 告警：开间贴图在、亮窗遮罩缺 ⇒ 夜间无窗灯（dev-only，每区一次）。
  useEffect(() => {
    if (!facadeTiles || litUrl !== '') return;
    warnLitMaskDegradeOnce(def.id);
  }, [facadeTiles, litUrl, def.id]);

  const archetype = DISTRICT_ARCHETYPE[def.id] ?? 'slab';
  // 批次 39 B1.4：**入住率下限**。原式 `min(0.35, prosperity*0.35)` 在 prosperity = 0
  // 时基准为 0 ⇒ 萧条城区夜全黑（B5）。真实城市从不全黑，取 0.06 作地板：
  // 繁荣度 0 的城区仍有零星灯光（少，但不是没有）。
  const emissive = Math.max(0.06, Math.min(0.35, prosperity * 0.35));
  // 区内统一 tint（原逐栋 hash；合并取舍见文件头）。
  const tint = useMemo(() => buildingTint(1.7, 1.35), []);
  // 批次 39 B1.3：逐城区**立面 UV 相位**（整数周期 ⇒ 平移后无缝，纹素密度不变）。
  // 打在几何 UV 上而非 `texture.offset`（理由见 building_shapes::districtFacadeUvOffset）。
  const uvOffset = useMemo(() => districtFacadeUvOffset(def.id), [def.id]);
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
      litMap,
      facadeTiles,
    }),
    [facadeBaseTex, facadeMidTex, roofTex, def.color, emissive, tint, pbrBase, pbrMid, roofPbr, tilePbr, metalPbr, litMap, facadeTiles],
  );

  // ── 几何合并（按 matSpecs 签名分组；同签名 ⇒ 单墙体 mesh 多 group）──
  const merged = useMemo(() => {
    const groups = new Map<
      string,
      { matSpecs: WallMatSpec[]; wall: GroupedMergePart[] }
    >();
    const accents: MergePart[] = [];
    const proxyData: Array<{ spec: BuildingSpec; h: number; floors: number }> = [];
    for (const spec of specs) {
      // 批次 32：改走 cityScale.buildingTopY（渲染与相机碰撞体共用单一事实来源）。
      const h = buildingTopY(def.id, prosperity, spec.factor);
      // 批次 39 C3/C4：透传临街面（法向轴 + 朝向）—— 决定 facadeBase 挂哪一面、
      // 以及门 / 门框 / 雨棚 / 底商雨棚 / 灯带挂在哪一侧（缺省 'z'/+1 = 旧行为）。
      const parts = buildBuildingParts(
        archetype, spec.w, spec.d, h, emissive, facadeTiles, uvOffset,
        spec.streetAxis, spec.streetSign,
      );
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
        // 批次 39 C2：楼层数改走 `cityScale.buildingFloorsOf`（与 buildingTopY 同源）。
        // 旧口径 `round(minF + (maxF-minF)*prosperity)` **不含 factor**，与真实楼高
        // 最多差 span×FLOOR_SPREAD 层（finance ±3 层）⇒ 悬停提示的楼层数对不上楼。
        floors: buildingFloorsOf(def.id, prosperity, spec.factor),
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
  }, [specs, def.id, prosperity, archetype, emissive, facadeTiles, uvOffset]);

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
      {/* 批次 39 B2：墙 mesh 补 `receiveShadow` —— 此前只有 `castShadow`，
          楼与楼之间无遮蔽、楼自身无自遮挡、晨昏无长影（B14，几何明明是对的）。
          点缀 mesh 由 `receiveShadow={false}` 改 `true`：点缀件（女儿墙/雨棚/空调外机/
          楼层线）已随墙体 castShadow，收阴影后楼角与雨棚会正确压暗。 */}
      {merged.wallGroups.map((g, i) => (
        <mesh key={`wall-${i}`} geometry={g.geo} material={materialGroups[i]} castShadow receiveShadow />
      ))}
      {merged.hasAccent && (
        <mesh geometry={merged.accentGeo} castShadow={false} receiveShadow>
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
