/**
 * BuildingMesh — 单栋楼（P1-B）：
 *
 * 把 v1 DistrictBlock 中的 boxGeometry + 单色 meshStandardMaterial 升级为：
 *   - 4 个侧面：facade 贴图（按 box 面材质索引 [0,1,2,3]）
 *   - 顶面：roof 贴图（box 面索引 5）
 *   - 底面：DistrictDefs 主色（box 面索引 4，玩家看不到但材质完整）
 *
 * 贴图加载：复用 DistrictBlock 的 useEffect + TextureLoader + dispose 模式；
 * 单组件缓存 2 张 facade（base + mid，避免接缝）与 1 张 roof。
 *
 * 材质数组顺序（boxGeometry 默认 6 面顺序）：
 *   [0] +X, [1] -X, [2] +Y, [3] -Y, [4] +Z, [5] -Z
 *   —— 但 R3F 中 boxGeometry 的实际顺序取决于 THREE 版本；
 *   我们用 6 个独立 <mesh> 子节点更直观：4 个侧面共享同 facade 贴图，1 顶面 roof，1 底面。
 *
 * prosperity（繁荣度）→ emissiveIntensity 仍保留（繁荣期楼顶暖光）。
 */

import { useEffect, useState } from 'react';
import * as THREE from 'three';
import {
  districtFacadeUrl,
  districtRoofUrl,
} from '@/assets/images/wealth';
import type { WealthDistrictDef } from '@/types/wealth';

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
  def: WealthDistrictDef;
  /** 当前房价指数（0.8–1.6 → prosperity 0–1）。 */
  prosperity: number;
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

export function BuildingMesh({ spec, def, prosperity }: Props) {
  const facadeBaseUrl = districtFacadeUrl(def.id, 'base');
  const facadeMidUrl = districtFacadeUrl(def.id, 'mid');
  const roofUrl = districtRoofUrl(def.id);

  const facadeBase = useTexture(facadeBaseUrl);
  const facadeMid = useTexture(facadeMidUrl);
  const roof = useTexture(roofUrl);

  // 楼高（与 DistrictBlock.tsx 一致：1 + prosperity*4 范围 × factor）
  const h = (1 + prosperity * 4) * spec.factor;
  // emissive 强度（v1 一致：prosperity * 0.25）
  const emissive = prosperity * 0.25;

  return (
    <group position={[spec.x, 0, spec.z]}>
      {/* 4 个侧面：横向用 facadeBase，纵向用 facadeMid（避免完全镜像接缝）。
          实际实现：每面单独 mesh，材质用 4 张独立的 facade 贴图 variant（base 横向, mid 纵向）。
          因为楼是轴对齐的 box，我们让 +X / -X 共享 base，+Z / -Z 共享 mid（不同方向不同贴图）。 */}
      {/* +X（右面） */}
      <mesh castShadow position={[spec.w / 2, h / 2, 0]} rotation={[0, Math.PI / 2, 0]}>
        <planeGeometry args={[spec.d, h]} />
        <meshStandardMaterial
          map={facadeBase ?? facadeMid ?? undefined}
          color={def.color}
          emissive={def.color}
          emissiveIntensity={emissive}
          roughness={0.7}
          metalness={0.1}
          side={THREE.DoubleSide}
        />
      </mesh>
      {/* -X（左面） */}
      <mesh castShadow position={[-spec.w / 2, h / 2, 0]} rotation={[0, -Math.PI / 2, 0]}>
        <planeGeometry args={[spec.d, h]} />
        <meshStandardMaterial
          map={facadeMid ?? facadeBase ?? undefined}
          color={def.color}
          emissive={def.color}
          emissiveIntensity={emissive}
          roughness={0.7}
          metalness={0.1}
          side={THREE.DoubleSide}
        />
      </mesh>
      {/* +Z（前面） */}
      <mesh castShadow position={[0, h / 2, spec.d / 2]}>
        <planeGeometry args={[spec.w, h]} />
        <meshStandardMaterial
          map={facadeMid ?? facadeBase ?? undefined}
          color={def.color}
          emissive={def.color}
          emissiveIntensity={emissive}
          roughness={0.7}
          metalness={0.1}
          side={THREE.DoubleSide}
        />
      </mesh>
      {/* -Z（后面） */}
      <mesh castShadow position={[0, h / 2, -spec.d / 2]} rotation={[0, Math.PI, 0]}>
        <planeGeometry args={[spec.w, h]} />
        <meshStandardMaterial
          map={facadeBase ?? facadeMid ?? undefined}
          color={def.color}
          emissive={def.color}
          emissiveIntensity={emissive}
          roughness={0.7}
          metalness={0.1}
          side={THREE.DoubleSide}
        />
      </mesh>

      {/* 顶面：roof 贴图（缺失回退主色） */}
      <mesh position={[0, h + 0.001, 0]} rotation={[-Math.PI / 2, 0, 0]}>
        <planeGeometry args={[spec.w, spec.d]} />
        <meshStandardMaterial
          map={roof ?? undefined}
          color={def.color}
          emissive={def.color}
          emissiveIntensity={emissive * 0.6}
          roughness={0.8}
        />
      </mesh>
    </group>
  );
}