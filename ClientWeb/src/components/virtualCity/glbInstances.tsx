/**
 * glbInstances — GLB `(geometry, material)` 对的**实例化**渲染（批次 38）。
 *
 * 背景：`<Model url>{children}</Model>` 的降级链适合「一物一实例」；批量平铺
 * （桥栏杆 2m/段 × N、驳岸 8m/段 × M）逐段 mount React 组件会 clone N 次场景
 * 并翻倍 draw call。本模块按 `props/RoadsideBins` 的既定范式：遍历 GLB 子树
 * 收集去重 (geometry, material) 对，每对建一个 `THREE.InstancedMesh`，
 * 矩阵一次性写入（全城平铺 = 1 draw call/对）。
 *
 * 自 `props/RoadsideBins.tsx` 抽出（共享工具只有一处；RoadsideBins / CanalBridge /
 * CanalExtras 三处共用）。
 *
 * ⚠️ 前置契约（批次 29，engine3d/glbSizeGuard dev 告警）：GLB 节点链
 * scale / rotation 必须 identity（位移允许）——`collectGlbPairs` 用
 * `rootInv × mesh.matrixWorld` 重算相对矩阵，会**静默抵消**根节点自身 scale
 * （trash_can.glb 旧版「单位几何 + 节点 scale 0.0375」即全城桶放大 20 倍）。
 */

import { useLayoutEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';

/** GLB 子树内抽出的一个 (geometry, material) 渲染对。 */
export interface GlbRenderPair {
  geometry: THREE.BufferGeometry;
  material: THREE.Material;
  /** mesh 相对根节点的局部矩阵（多 primitive 拆分时非平凡）。 */
  localMatrix: THREE.Matrix4;
}

/** 遍历子树收集去重 (geometry, material) 对。 */
export function collectGlbPairs(root: THREE.Object3D | null): GlbRenderPair[] {
  if (!root) return [];
  const seen = new Set<string>();
  const pairs: GlbRenderPair[] = [];
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

/**
 * 内容包围盒归一化位移（根节点局部系）：x/z 取内容盒中心、y 取 −min
 * ⇒ 抗美术侧在 GLB 内平移内容，且**底面贴地**不依赖原点在底面的约定。
 */
export function pairPivotOffset(pairs: GlbRenderPair[]): THREE.Vector3 {
  const box = new THREE.Box3();
  const tmp = new THREE.Box3();
  for (const p of pairs) {
    if (!p.geometry.boundingBox) p.geometry.computeBoundingBox();
    if (p.geometry.boundingBox) box.union(tmp.copy(p.geometry.boundingBox).applyMatrix4(p.localMatrix));
  }
  if (box.isEmpty()) return new THREE.Vector3(0, 0, 0);
  const center = box.getCenter(new THREE.Vector3());
  return new THREE.Vector3(-center.x, -box.min.y, -center.z);
}

/** 单个 (geometry, material) 对的 instancedMesh（矩阵一次性写入）。 */
export function GlbPairInstances({
  pair,
  worldMatrices,
  pivot,
  castShadow = false,
  receiveShadow = false,
}: {
  pair: GlbRenderPair;
  worldMatrices: THREE.Matrix4[];
  /** 内容盒归一化位移（根局部系，见 pairPivotOffset）。 */
  pivot: THREE.Matrix4;
  castShadow?: boolean;
  receiveShadow?: boolean;
}) {
  const ref = useRef<THREE.InstancedMesh>(null);
  useLayoutEffect(() => {
    const mesh = ref.current;
    if (!mesh) return;
    const m = new THREE.Matrix4();
    worldMatrices.forEach((wm, i) => {
      // 世界矩阵 × 归一化位移 × 子网格局部矩阵（位移在旋转之后、局部系内生效）
      m.copy(wm).multiply(pivot).multiply(pair.localMatrix);
      mesh.setMatrixAt(i, m);
    });
    mesh.instanceMatrix.needsUpdate = true;
    mesh.computeBoundingSphere(); // 实例级视锥剔除包围球
  }, [pair, worldMatrices, pivot]);
  if (!worldMatrices.length) return null;
  return (
    <instancedMesh
      ref={ref}
      args={[pair.geometry, pair.material, worldMatrices.length]}
      castShadow={castShadow}
      receiveShadow={receiveShadow}
    />
  );
}

/** 批量渲染一对多实例（任一对为空自动跳过）。 */
export function GlbInstances({
  pairs,
  worldMatrices,
  castShadow,
  receiveShadow,
}: {
  pairs: GlbRenderPair[];
  worldMatrices: THREE.Matrix4[];
  castShadow?: boolean;
  receiveShadow?: boolean;
}) {
  const pivot = useMemo(() => {
    const o = pairPivotOffset(pairs);
    return new THREE.Matrix4().makeTranslation(o.x, o.y, o.z);
  }, [pairs]);
  if (!worldMatrices.length) return null;
  return (
    <>
      {pairs.map((pair, i) => (
        <GlbPairInstances
          key={`glb-i-${i}`}
          pair={pair}
          worldMatrices={worldMatrices}
          pivot={pivot}
          castShadow={castShadow}
          receiveShadow={receiveShadow}
        />
      ))}
    </>
  );
}
