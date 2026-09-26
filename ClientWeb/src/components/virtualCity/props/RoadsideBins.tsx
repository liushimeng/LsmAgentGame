/**
 * RoadsideBins — 全城路侧垃圾桶（批次 24 §5/§6）：
 *
 *   - 模型：modelUrl('road', 'trash_can')（3d_script/build_trash_can.py 产出）；
 *     加载后按对象名取 TrashCan_Green / TrashCan_Blue 两变体节点，**遍历子树**
 *     收集 (geometry, material) 对去重（GLTFLoader 可能把多 primitive 拆成
 *     子 Mesh，不假设单 mesh），每对建一个全城 instancedMesh（预计 2~6 draw call）；
 *   - 实例矩阵 = 桶位世界矩阵 × mesh 在变体子树内的局部矩阵（局部矩阵为
 *     identity 时即「所有 pair 共享同一变换序列」的常规情形）；桶底贴地
 *     偏移由子树包围盒 minY 动态推导（不依赖 GLB 原点在桶底的约定）；
 *   - 降级链：GLB 缺失 / 变体名找不到 / blenderModelsEnabled()=false →
 *     回退 props/TrashCan.tsx 程序化几何，密度减半（隔一取一，保底不空）。
 *
 * 布点在 StreetPropsLayer::roadsideBinsForCity（主干道 ≈6u 两侧交替 +
 * 公交站台旁），本组件只负责渲染，不含布点逻辑。
 */

import { useLayoutEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { modelUrl } from '@/assets/models';
import { blenderModelsEnabled, useSharedGLTF } from '@/engine3d';
import type { RoadsideBinSpot } from '../StreetPropsLayer';
import { TrashCan } from './TrashCan';

/** GLB 内两变体对象名（与 3d_script/build_trash_can.py join_objects 命名对齐）。 */
const VARIANT_OBJECT_NAMES: Record<RoadsideBinSpot['variant'], string> = {
  green: 'TrashCan_Green',
  blue: 'TrashCan_Blue',
};

/** GLB 子树内抽出的一个 (geometry, material) 渲染对。 */
interface BinPair {
  geometry: THREE.BufferGeometry;
  material: THREE.Material;
  /** mesh 相对变体根节点的局部矩阵（多 primitive 拆分时非平凡）。 */
  localMatrix: THREE.Matrix4;
}

/** 遍历子树收集去重 (geometry, material) 对（§5 调用约定）。 */
function collectPairs(root: THREE.Object3D | null): BinPair[] {
  if (!root) return [];
  const seen = new Set<string>();
  const pairs: BinPair[] = [];
  root.updateWorldMatrix(true, true);
  const rootInv = new THREE.Matrix4().copy(root.matrixWorld).invert();
  root.traverse((o) => {
    const mesh = o as THREE.Mesh;
    if (!mesh.isMesh || !mesh.geometry || !mesh.material) return;
    const mat = Array.isArray(mesh.material) ? mesh.material[0] : mesh.material;
    const key = `${mesh.geometry.uuid}|${mat.uuid}`;
    if (seen.has(key)) return;
    seen.add(key);
    pairs.push({
      geometry: mesh.geometry,
      material: mat,
      localMatrix: rootInv.clone().multiply(mesh.matrixWorld),
    });
  });
  return pairs;
}

/** 桶位 → 世界矩阵（绕 Y 旋转；yLift 把桶底贴地，见 variantYlift）。 */
function binWorldMatrix(b: RoadsideBinSpot, yLift: number): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(b.x, yLift, b.z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(0, b.rotation, 0)),
    new THREE.Vector3(1, 1, 1),
  );
}

/**
 * 变体子树在根局部系下的 minY（正数 = 原点在桶底上方，需抬升贴地）。
 * 不假设 GLB 原点在桶底（build_trash_can.py 现版原点在桶身中心）——
 * 由包围盒动态求落地偏移，art 侧将来改原点约定也不受影响。
 */
function subtreeLift(pairs: BinPair[]): number {
  const box = new THREE.Box3();
  const tmp = new THREE.Box3();
  for (const p of pairs) {
    if (!p.geometry.boundingBox) p.geometry.computeBoundingBox();
    if (p.geometry.boundingBox) box.union(tmp.copy(p.geometry.boundingBox).applyMatrix4(p.localMatrix));
  }
  return box.isEmpty() ? 0 : -box.min.y;
}

/** 单个 (geometry, material) 对的全城 instancedMesh（矩阵一次性写入）。 */
function BinPairMesh({
  pair,
  worldMatrices,
}: {
  pair: BinPair;
  worldMatrices: THREE.Matrix4[];
}) {
  const ref = useRef<THREE.InstancedMesh>(null);
  useLayoutEffect(() => {
    const mesh = ref.current;
    if (!mesh) return;
    const m = new THREE.Matrix4();
    worldMatrices.forEach((wm, i) => {
      m.copy(wm).multiply(pair.localMatrix);
      mesh.setMatrixAt(i, m);
    });
    mesh.instanceMatrix.needsUpdate = true;
    mesh.computeBoundingSphere(); // 实例级视锥剔除包围球
  }, [pair, worldMatrices]);
  return (
    <instancedMesh
      ref={ref}
      args={[pair.geometry, pair.material, Math.max(1, worldMatrices.length)]}
      castShadow
    />
  );
}

interface Props {
  /** 布点（StreetPropsLayer::roadsideBinsForCity 产出）。 */
  bins: RoadsideBinSpot[];
}

export function RoadsideBins({ bins }: Props) {
  const url = modelUrl('road', 'trash_can');
  const { scene } = useSharedGLTF(url);
  // feature flag 一次性判定（CLAUDE.md §27.5：disable-blender-models=1 强制回退）。
  const blenderOn = useMemo(() => blenderModelsEnabled(), []);

  /** GLB 路径数据：两变体各自的渲染对 + 实例世界矩阵（任一变体缺失 → null 走回退）。 */
  const glbData = useMemo(() => {
    if (!blenderOn || !scene) return null;
    const greenPairs = collectPairs(scene.getObjectByName(VARIANT_OBJECT_NAMES.green) ?? null);
    const bluePairs = collectPairs(scene.getObjectByName(VARIANT_OBJECT_NAMES.blue) ?? null);
    if (!greenPairs.length || !bluePairs.length) return null;
    const world = (variant: RoadsideBinSpot['variant'], lift: number) =>
      bins.filter((b) => b.variant === variant).map((b) => binWorldMatrix(b, lift));
    const greenLift = subtreeLift(greenPairs);
    const blueLift = subtreeLift(bluePairs);
    return {
      greenPairs,
      bluePairs,
      greenMatrices: world('green', greenLift),
      blueMatrices: world('blue', blueLift),
    };
  }, [blenderOn, scene, bins]);

  // 降级：程序化 TrashCan，密度减半（隔一取一）。
  if (!glbData) {
    return (
      <group>
        {bins
          .filter((_, i) => i % 2 === 0)
          .map((b, i) => (
            <TrashCan
              key={`bin-fallback-${i}`}
              x={b.x}
              z={b.z}
              rotation={b.rotation}
              // 程序化桶无绿变体：green→灰(1) / blue→蓝(0) 就近映射
              variant={b.variant === 'blue' ? 0 : 1}
            />
          ))}
      </group>
    );
  }

  return (
    <group>
      {glbData.greenPairs.map((pair, i) =>
        glbData.greenMatrices.length ? (
          <BinPairMesh key={`bin-green-${i}`} pair={pair} worldMatrices={glbData.greenMatrices} />
        ) : null,
      )}
      {glbData.bluePairs.map((pair, i) =>
        glbData.blueMatrices.length ? (
          <BinPairMesh key={`bin-blue-${i}`} pair={pair} worldMatrices={glbData.blueMatrices} />
        ) : null,
      )}
    </group>
  );
}
