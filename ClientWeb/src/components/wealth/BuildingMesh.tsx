/**
 * BuildingMesh — 单栋楼（13-3D城市渲染优化 · 阶段 B 重构）：
 *
 * 历史演进：
 *   v1   boxGeometry + 单色 meshStandardMaterial（贴图被城区主色覆盖，已修复）。
 *   P1-B 5 个独立 plane 拼盒子 + facade/roof 贴图（§1.2 贴图修复）。
 *   阶段 B（本版） 按 DISTRICT_ARCHETYPE 分发到 building_shapes.tsx 的
 *        体块组合渲染器（tower/slab/house/shed/pavilion），贴图加载、
 *        高度公式、prosperity 语义不变。
 *
 * 职责边界：本文件只负责「贴图加载 + 高度/繁荣度计算 + archetype 分发」，
 * 体块几何与材质细节全部在 ./building_shapes.tsx（契约：
 * lag_docs/虚拟城市/已实现/13-3D城市渲染优化/02-架构设计-WebGL渲染管线优化-v1.md §2）。
 *
 * emissive：统一暖窗光 EMISSIVE_WINDOW(#ffd9a0)，强度 prosperity × 0.35 上限，
 * 有贴图时 emissiveMap 复用立面贴图（夜景窗灯近似），无贴图回退城区主色。
 */

import { useEffect, useState } from 'react';
import * as THREE from 'three';
import {
  districtFacadeUrl,
  districtRoofUrl,
} from '@/assets/images/wealth';
import type { WealthDistrictDef } from '@/types/wealth';
import { DISTRICT_FLOORS, buildingHeight } from './cityScale';
import { BuildingShape, DISTRICT_ARCHETYPE } from './building_shapes';

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

/** 加载单张贴图（缺失 → null）。带 dispose 清理。 */
function useTexture(url: string): THREE.Texture | null {
  const [tex, setTex] = useState<THREE.Texture | null>(null);
  useEffect(() => {
    if (!url) {
      setTex(null);
      return;
    }
    let disposed = false;
    const loader = new THREE.TextureLoader();
    loader.load(
      url,
      (loaded) => {
        if (disposed) {
          loaded.dispose();
          return;
        }
        loaded.colorSpace = THREE.SRGBColorSpace;
        loaded.magFilter = THREE.LinearFilter;
        loaded.minFilter = THREE.LinearMipmapLinearFilter;
        // 立面/屋顶不需要 tile（每楼独立贴图）
        setTex(prev => {
          if (prev) prev.dispose();
          return loaded;
        });
      },
      undefined,
      () => setTex(null),
    );
    return () => {
      disposed = true;
    };
  }, [url]);
  return tex;
}

interface Props {
  spec: BuildingSpec;
  def: WealthDistrictDef;
  /** 当前房价指数（0.8–1.6 → prosperity 0–1）。 */
  prosperity: number;
}

export function BuildingMesh({ spec, def, prosperity }: Props) {
  const facadeBase = useTexture(districtFacadeUrl(def.id, 'base'));
  const facadeMid = useTexture(districtFacadeUrl(def.id, 'mid'));
  const roof = useTexture(districtRoofUrl(def.id));

  // 楼高：分城区楼层区间 [minF, maxF] × 繁荣度插值 × factor 抖动（0.85~1.0，
  // 保留确定性伪随机但避免 0.6 倍把楼压扁）。层高 3m，见 cityScale.ts。
  const [minF, maxF] = DISTRICT_FLOORS[def.id];
  const h = buildingHeight(minF + (maxF - minF) * prosperity) * (0.85 + spec.factor * 0.15);
  // 暖窗光强度（契约 §2.3：prosperity × 0.35，上限 0.35 防 ACES 过曝）
  const emissive = Math.min(0.35, prosperity * 0.35);

  const archetype = DISTRICT_ARCHETYPE[def.id] ?? 'slab';

  return (
    <group position={[spec.x, 0, spec.z]}>
      <BuildingShape
        archetype={archetype}
        w={spec.w}
        d={spec.d}
        h={h}
        facadeBase={facadeBase}
        facadeMid={facadeMid}
        roofMap={roof}
        fallbackColor={def.color}
        emissive={emissive}
      />
    </group>
  );
}
