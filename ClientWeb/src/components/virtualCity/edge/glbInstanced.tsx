/**
 * 批次 26 · edge 共享：GLB 多子网格实例化渲染。
 *
 * 背景：边缘带需要几十~上百个同型 GLB 实例（雪山 / 针叶树 / 橡树 / 仙人掌 /
 * 帆船），若走 <Model> 逐实例 clone 会产生 N×子网格数 的 draw call，远超
 * 方案文档 §2.4「四缘带合计 ≤ 30 draw calls」的性能预算。
 *
 * 做法：经 useSharedGLTF 取共享 scene，收集全部子网格
 * （geometry / material / 相对根的局部矩阵），每个子网格渲染一个
 * <instancedMesh>（draw call 数 = GLB 子网格数，与实例数无关）；
 * 逐实例矩阵 = 实例 TRS × 子网格局部矩阵。
 *
 * 降级链（§27.3 契约）：url 缺失 / 加载失败 / 加载中 → 渲染 fallback
 * （调用方提供的程序化几何），零代码分支。
 *
 * 契约：lag_docs/虚拟城市/已实现/26-坐标系统与城市边缘环境/01-现状分析与方案设计.md §2.4。
 */

import { useLayoutEffect, useMemo, useRef } from 'react';
import type { ReactNode } from 'react';
import * as THREE from 'three';
import { useSharedGLTF } from '@/engine3d';

/** GLB 单个子网格的可实例化描述。 */
export interface GlbMeshPart {
  geometry: THREE.BufferGeometry;
  material: THREE.Material | THREE.Material[];
  /** 子网格相对 GLB 根节点的局部矩阵。 */
  local: THREE.Matrix4;
}

/** 单个实例的 TRS（scale 为等比缩放；边缘带布点仅需绕 Y 旋转）。 */
export interface GlbInstanceTRS {
  position: [number, number, number];
  rotationY?: number;
  scale?: number;
}

/** 从共享 GLTF scene 收集全部子网格（scene 需保持根节点单位变换）。 */
export function collectGlbMeshParts(scene: THREE.Group): GlbMeshPart[] {
  scene.updateMatrixWorld(true);
  const parts: GlbMeshPart[] = [];
  scene.traverse((o) => {
    const mesh = o as THREE.Mesh;
    if (mesh.isMesh && mesh.geometry && mesh.material) {
      parts.push({
        geometry: mesh.geometry,
        material: mesh.material,
        local: mesh.matrixWorld.clone(),
      });
    }
  });
  return parts;
}

interface Props {
  /** .glb URL（'' → 直接走 fallback，不发请求）。 */
  url: string;
  /** 实例 TRS 列表（调用方 useMemo 缓存，避免每帧重建矩阵）。 */
  instances: GlbInstanceTRS[];
  /** GLB 缺失 / 加载中 / 失败时的程序化 fallback（必传）。 */
  fallback: ReactNode;
}

export function GlbInstanced({ url, instances, fallback }: Props) {
  const { scene } = useSharedGLTF(url);
  const parts = useMemo(() => (scene ? collectGlbMeshParts(scene) : null), [scene]);

  // 实例 TRS → Matrix4 列表（HOOK 必须在条件 return 之前，见 hook-after-return 教训）
  const matrices = useMemo(() => {
    const q = new THREE.Quaternion();
    const eu = new THREE.Euler();
    const p = new THREE.Vector3();
    const s = new THREE.Vector3();
    return instances.map((it) => {
      eu.set(0, it.rotationY ?? 0, 0);
      q.setFromEuler(eu);
      p.set(it.position[0], it.position[1], it.position[2]);
      s.setScalar(it.scale ?? 1);
      return new THREE.Matrix4().compose(p, q, s);
    });
  }, [instances]);

  if (!parts || parts.length === 0) return <>{fallback}</>;
  return (
    <group>
      {parts.map((part, i) => (
        <InstancedPart key={i} part={part} matrices={matrices} />
      ))}
    </group>
  );
}

/** 单个子网格的 instancedMesh（逐实例矩阵 = 实例 TRS × 子网格局部矩阵）。 */
function InstancedPart({ part, matrices }: { part: GlbMeshPart; matrices: THREE.Matrix4[] }) {
  const ref = useRef<THREE.InstancedMesh>(null);

  useLayoutEffect(() => {
    const im = ref.current;
    if (!im) return;
    const tmp = new THREE.Matrix4();
    matrices.forEach((m, i) => {
      tmp.multiplyMatrices(m, part.local);
      im.setMatrixAt(i, tmp);
    });
    im.count = matrices.length;
    im.instanceMatrix.needsUpdate = true;
    // 让包围球覆盖全部实例（实例散布广，默认单位包围球会被视锥误剔除）
    im.computeBoundingSphere();
  }, [part, matrices]);

  return (
    <instancedMesh
      ref={ref}
      args={[part.geometry, part.material, Math.max(1, matrices.length)]}
    />
  );
}
