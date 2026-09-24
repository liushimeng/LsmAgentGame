/**
 * BuildingMesh — 单栋楼（13-3D城市渲染优化 · 阶段 B 重构）：
 *
 * 历史演进：
 *   v1   boxGeometry + 单色 meshStandardMaterial（贴图被城区主色覆盖，已修复）。
 *   P1-B 5 个独立 plane 拼盒子 + facade/roof 贴图（§1.2 贴图修复）。
 *   阶段 B（本版） 按 DISTRICT_ARCHETYPE 分发到 building_shapes.tsx 的
 *        体块组合渲染器（tower/slab/house/shed/pavilion），贴图加载、
 *        高度公式、prosperity 语义不变。
 *   18-X  按 def.id 拼 stem（facades/<id>_<v> / roofs/<id>），用 useSharedPBR
 *        加载 PBR 三件套传给 BuildingShape（02 §3.3 接线契约）。
 *
 * 职责边界：本文件只负责「贴图加载 + 高度/繁荣度计算 + archetype 分发」，
 * 体块几何与材质细节全部在 ./building_shapes.tsx（契约：
 * lag_docs/虚拟城市/已实现/13-3D城市渲染优化/02-架构设计-WebGL渲染管线优化-v1.md §2）。
 *
 * emissive：统一暖窗光 EMISSIVE_WINDOW(#ffd9a0)，强度 prosperity × 0.35 上限，
 * 有贴图时 emissiveMap 复用立面贴图（夜景窗灯近似），无贴图回退城区主色。
 */

import {
  districtFacadeUrl,
  districtRoofUrl,
  districtTextureStem,
  pbrNormalUrl,
  pbrRoughUrl,
} from '@/assets/images/virtualCity';
import type { VirtualCityDistrictDef } from '@/types/virtualCity';
import { DISTRICT_FLOORS, buildingHeight } from './cityScale';
import { BuildingShape, DISTRICT_ARCHETYPE } from './building_shapes';
import { useSharedTexture, useSharedPBR } from './textureCache';

export interface BuildingSpec {
  /** 相对区中心偏移（x, z）。 */
  x: number;
  z: number;
  /** 楼栋占地（宽 / 深）。 */
  w: number;
  d: number;
  /** 楼高系数 0.6–1.0。 */
  factor: number;
}

interface Props {
  spec: BuildingSpec;
  def: VirtualCityDistrictDef;
  /** 当前房价指数（0.8–1.6 → prosperity 0–1）。 */
  prosperity: number;
}

export function BuildingMesh({ spec, def, prosperity }: Props) {
  // 14-3D渲染深化：共享贴图缓存（区数 × 楼数同源贴图只上传一次 GPU）
  // 批次 20 §3.5：先查贴图别名再取 def.id（16 新区复用旧 stem，零新增图片）。
  const stem = districtTextureStem(def.id);
  const facadeBaseTex = useSharedTexture(districtFacadeUrl(stem, 'base'));
  const facadeMidTex = useSharedTexture(districtFacadeUrl(stem, 'mid'));
  const roofTex = useSharedTexture(districtRoofUrl(stem));

  // 18-X PBR 三件套（02 §2.3 接线表 1/2/3 行）。
  // stem 拼接仅在此处发生（def.id 由父层注入，符合 02 §3.3「stem 拼接只允许在 BuildingMesh」）；
  // 法线/粗糙度 useSharedTexture 内部固定 srgb:false → NoColorSpace。
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

  // 楼高：分城区楼层区间 [minF, maxF] × 繁荣度插值 × factor 抖动（0.85~1.0，
  // 保留确定性伪随机但避免 0.6 倍把楼压扁）。层高 3m，见 cityScale.ts。
  const [minF, maxF] = DISTRICT_FLOORS[def.id];
  const h = buildingHeight(minF + (maxF - minF) * prosperity) * (0.85 + spec.factor * 0.15);
  // 暖窗光强度（契约 §2.3：prosperity × 0.35，上限 0.35 防 ACES 过曝）
  const emissive = Math.min(0.35, prosperity * 0.35);

  const archetype = DISTRICT_ARCHETYPE[def.id] ?? 'slab';

  // 注意 facadeBase/Mid/roofTex 与 pbrBase/Mid/roofPbr.map 指向同一缓存纹理（cacheKey 一致），
  // 这里复用 .map 即可——避免双重 load 与状态不一致。
  return (
    <group position={[spec.x, 0, spec.z]}>
      <BuildingShape
        archetype={archetype}
        w={spec.w}
        d={spec.d}
        h={h}
        facadeBase={facadeBaseTex}
        facadeMid={facadeMidTex}
        roofMap={roofTex}
        fallbackColor={def.color}
        emissive={emissive}
        pbrBase={pbrBase}
        pbrMid={pbrMid}
        roofPbr={roofPbr}
      />
    </group>
  );
}