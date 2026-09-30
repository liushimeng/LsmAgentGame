/**
 * RoadsideBins — 全城路侧垃圾桶（批次 24 §5/§6）：
 *
 *   - 模型：modelUrl('road', 'trash_can')（3d_script/build_trash_can.py 产出）；
 *     加载后按对象名取 TrashCan_Green / TrashCan_Blue 两变体节点，**遍历子树**
 *     收集 (geometry, material) 对去重（GLTFLoader 可能把多 primitive 拆成
 *     子 Mesh，不假设单 mesh），每对建一个全城 instancedMesh（预计 2~6 draw call）；
 *   - 实例矩阵 = 桶位世界矩阵 × **变体内容归一化位移** × mesh 在变体子树内的局部矩阵；
 *     归一化位移 = 变体内容包围盒（x/z 取中心、y 取 −min）——桶底贴地不依赖 GLB
 *     原点在桶底的约定，且美术侧在 GLB 内平移变体（如 Blue 烘焙在 +1.2 m 处）时
 *     桶身仍落在桶位点上；
 *   - 降级链：GLB 缺失 / 变体名找不到 / blenderModelsEnabled()=false →
 *     回退 props/TrashCan.tsx 程序化几何，密度减半（隔一取一，保底不空）。
 *
 * ⚠️ 前置契约（批次 29 明示，由 engine3d/glbSizeGuard 在 dev 态告警）：
 *   变体根节点及其子节点的 **scale / rotation 必须 identity**（位移允许）。
 *   本组件的 `collectPairs()` 用 `rootInv × mesh.matrixWorld` 重算相对矩阵，
 *   会**静默抵消变体根自身的 scale** —— trash_can.glb 旧版正是「单位几何 +
 *   节点 scale 0.0375」，于是 instancedMesh 按单位几何渲染（全城桶放大 20 倍，
 *   用户所述"垃圾桶比汽车还大"的真因）。尺寸必须烘焙进顶点。
 *
 * 布点在 StreetPropsLayer::roadsideBinsForNetwork（主干道 ≈6u 两侧交替 +
 * 公交站台旁），本组件只负责渲染，不含布点逻辑。
 */

import { memo, useMemo } from 'react';
import * as THREE from 'three';
import { modelUrl } from '@/assets/models';
import { blenderModelsEnabled, useSharedGLTF } from '@/engine3d';
import {
  collectGlbPairs,
  pairPivotOffset,
  GlbPairInstances,
  type GlbRenderPair,
} from '../glbInstances';
import { useI18nStore } from '@/store/i18n.store';
import type { Lang } from '@/i18n';
import type { RoadsideBinSpot } from '../StreetPropsLayer';
import { sizeTargetFor } from '../cityScale';
import { TrashCan } from './TrashCan';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

/** GLB 内两变体对象名（与 3d_script/build_trash_can.py join_objects 命名对齐）。 */
const VARIANT_OBJECT_NAMES: Record<RoadsideBinSpot['variant'], string> = {
  green: 'TrashCan_Green',
  blue: 'TrashCan_Blue',
};

/**
 * 尺寸/落地校验目标（批次 29，dev 态）：表值取 cityScale.REAL_DIMS_M.trashCan
 * （⌀0.50 × H1.00），**量测 Green 变体子树**（场景包围盒是两变体并集，不是单桶尺寸）。
 * 桶底贴地由 subtreeLift() 按包围盒 minY 动态推导 —— 重导出后 minY=0 时 lift=0，
 * 逻辑与表值口径一致，无需改动。
 */
const BIN_SIZE_TARGET = sizeTargetFor('trashCan', {
  label: 'road/trash_can',
  measureNode: VARIANT_OBJECT_NAMES.green,
});

/** 路侧桶分类行（三语与 catalog 同策略不进 i18n/Dict）。 */
const ROADSIDE_CATEGORY: Record<RoadsideBinSpot['variant'], Record<Lang, string>> = {
  green: { 'zh-CN': '厨余垃圾（绿）', en: 'Kitchen waste (green)', ja: '生ごみ（緑）' },
  blue: { 'zh-CN': '可回收物（蓝）', en: 'Recyclable (blue)', ja: 'リサイクル（青）' },
};

/** GLB 子树内抽出的一个 (geometry, material) 渲染对（共享实现在 ../glbInstances）。 */
type BinPair = GlbRenderPair;
const collectPairs = collectGlbPairs;
const variantPivotOffset = pairPivotOffset;
const BinPairMesh = GlbPairInstances;

/** 桶位 → 世界矩阵（绕 Y 旋转；variant 归一化位移见 variantPivotOffset）。 */
function binWorldMatrix(b: RoadsideBinSpot): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(b.x, 0, b.z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(0, b.rotation, 0)),
    new THREE.Vector3(1, 1, 1),
  );
}

interface Props {
  /** 布点（StreetPropsLayer::roadsideBinsForNetwork 产出）。 */
  bins: RoadsideBinSpot[];
}

// 批次 28 A1/A3：memo + 街具默认不投影（阴影 pass caster 裁剪）。
export const RoadsideBins = memo(function RoadsideBins({ bins }: Props) {
  const url = modelUrl('road', 'trash_can');
  const { scene } = useSharedGLTF(url, BIN_SIZE_TARGET);
  // feature flag 一次性判定（CLAUDE.md §27.5：disable-blender-models=1 强制回退）。
  const blenderOn = useMemo(() => blenderModelsEnabled(), []);

  /** GLB 路径数据：两变体各自的渲染对 + 实例世界矩阵（任一变体缺失 → null 走回退）。 */
  const glbData = useMemo(() => {
    if (!blenderOn || !scene) return null;
    const greenPairs = collectPairs(scene.getObjectByName(VARIANT_OBJECT_NAMES.green) ?? null);
    const bluePairs = collectPairs(scene.getObjectByName(VARIANT_OBJECT_NAMES.blue) ?? null);
    if (!greenPairs.length || !bluePairs.length) return null;
    const world = (variant: RoadsideBinSpot['variant']) =>
      bins.filter((b) => b.variant === variant).map((b) => binWorldMatrix(b));
    // 变体内容盒归一化（重心 x/z + 桶底 y）—— 抗美术侧在 GLB 内平移变体（批次 29）
    const pivotMatrix = (pairs: BinPair[]) => {
      const o = variantPivotOffset(pairs);
      return new THREE.Matrix4().makeTranslation(o.x, o.y, o.z);
    };
    const greenPivot = pivotMatrix(greenPairs);
    const bluePivot = pivotMatrix(bluePairs);
    return {
      greenPairs,
      bluePairs,
      greenPivot,
      bluePivot,
      greenMatrices: world('green'),
      blueMatrices: world('blue'),
    };
  }, [blenderOn, scene, bins]);

  // 批次 28 B2：路侧垃圾桶信息交互（GLB instancedMesh 路径按绿/蓝变体分组挂接，
  // 各自带「分类」动态行——厨余（绿）/可回收（蓝）；
  // 降级路径里 TrashCan 自带同 id 接线，子级 stopPropagation 不会双触发）。
  const lang = useI18nStore((s) => s.lang);
  const greenInfo = useObjectInfoProps('prop.trash-can', {
    anchorY: 0.2,
    extra: [{ label: 'category', value: ROADSIDE_CATEGORY.green[lang] }],
  });
  const blueInfo = useObjectInfoProps('prop.trash-can', {
    anchorY: 0.2,
    extra: [{ label: 'category', value: ROADSIDE_CATEGORY.blue[lang] }],
  });
  // 降级路径包裹组：绿蓝混布（程序化桶无绿变体），不挂「分类」行以免误导。
  const fallbackInfo = useObjectInfoProps('prop.trash-can', { anchorY: 0.2 });

  // 降级：程序化 TrashCan，密度减半（隔一取一）。
  if (!glbData) {
    return (
      <group {...fallbackInfo}>
        {bins
          .filter((_, i) => i % 2 === 0)
          .map((b, i) => (
            <TrashCan
              key={`bin-fallback-${i}`}
              x={b.x}
              z={b.z}
              rotation={b.rotation}
              // 批次 30 P1-14 语义统一：green→绿·厨余(2) / blue→蓝·可回收(0)
              variant={b.variant === 'blue' ? 0 : 2}
            />
          ))}
      </group>
    );
  }

  return (
    <>
      <group {...greenInfo}>
        {glbData.greenPairs.map((pair, i) =>
          glbData.greenMatrices.length ? (
            <BinPairMesh
              key={`bin-green-${i}`}
              pair={pair}
              worldMatrices={glbData.greenMatrices}
              pivot={glbData.greenPivot}
            />
          ) : null,
        )}
      </group>
      <group {...blueInfo}>
        {glbData.bluePairs.map((pair, i) =>
          glbData.blueMatrices.length ? (
            <BinPairMesh
              key={`bin-blue-${i}`}
              pair={pair}
              worldMatrices={glbData.blueMatrices}
              pivot={glbData.bluePivot}
            />
          ) : null,
        )}
      </group>
    </>
  );
});
