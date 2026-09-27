/**
 * engine3d/modelCache — 进程级共享 GLTFLoader 缓存。
 *
 * 自 components/virtualCity/modelCache.ts 迁入（22-3D世界升级与引擎模块化，
 * 19-Blender3D模型集成 · 阶段 A 的原始契约不变）：
 *   - 同 url 多组件只发一次网络请求 / 只占一份 GPU 几何 + 材质。
 *   - 命中同步复用 GLTFData，但调用方拿到 scene 后**必须 .clone(true)** 再挂载
 *     （共享 scene 会让多实例 transform 互相污染）。
 *   - **禁止组件侧 dispose 共享 scene** —— 缓存随页面生命周期存活。
 *   - url === ''（资产缺失）→ 返回 null，零副作用（降级链由调用方处理）。
 *   - 加载失败缓存哨兵：gltf=null, done=true，后续调用直接返回 null（不反复重试）。
 *
 * 通用模块：不依赖任何游戏业务代码，任何 3D 游戏/程序可直接复用。
 */

import { useEffect, useState } from 'react';
import * as THREE from 'three';
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js';
import { mergeParts } from './geoMerge';
import {
  checkModelSize,
  registerModelSizeTarget,
  type ModelSizeTarget,
} from './glbSizeGuard';

/**
 * 批次 28 二轮：GLB 等材质归并（加载时一次性，缓存后全部实例受益）。
 *
 * Blender 导出的 .glb 常见「每部件一材质」——例如 sedan 18 材质中仅 7 种
 * 视觉属性互异（4 轮胎同黑、4 轮毂同灰、4 面车窗同玻璃……）。GLTFLoader 对
 * 多 primitive 网格产出「Group + 每 primitive 一个独立 Mesh」（非单 mesh 多 group），
 * 视觉重复的兄弟 mesh 各自成 draw call。此处做两层归并，**渲染属性完全一致**
 * （颜色/粗糙度/金属度/emissive/透明/side/贴图实例）才合并：
 *
 *   a) 兄弟 mesh 归并：Group 子级全为 Mesh → 按材质签名分桶，把桶内几何经
 *      各自局部矩阵烘焙进合并 mesh（几何全等，像素级零回归）。
 *   b) 多 group 单 mesh 归并：材质数组 + 连续 group 布局 → 只重排 index 段顺序
 *      使同材质 group 连续（不透明几何三角形次序不影响 Z-buffer 结果；桶按
 *      首次出现次序输出，跨材质透明混合次序不变）。
 *
 * 跳过：SkinnedMesh / morph 几何 / vertexColors（COLOR_0 顶点色无法经部件级
 * 填充保真）/ 属性集超出 position+normal+uv 的部件（保守，本仓 GLB 均满足）。
 */
function materialSignature(m: THREE.Material): string {
  const s = m as THREE.MeshStandardMaterial;
  return JSON.stringify([
    s.type,
    s.color?.getHexString?.() ?? null,
    s.roughness, s.metalness,
    s.emissive?.getHexString?.() ?? null, s.emissiveIntensity,
    s.transparent, s.opacity, s.alphaTest, s.side, s.flatShading, s.vertexColors,
    s.map?.uuid ?? null,
    s.normalMap?.uuid ?? null,
    s.roughnessMap?.uuid ?? null,
    s.metalnessMap?.uuid ?? null,
    s.emissiveMap?.uuid ?? null,
    s.aoMap?.uuid ?? null,
  ]);
}

/** 部件可安全参与兄弟归并的判定（属性/蒙皮/morph/顶点色守卫）。 */
function siblingMergeable(mesh: THREE.Mesh): boolean {
  const m = mesh as unknown as { isSkinnedMesh?: boolean };
  if (m.isSkinnedMesh) return false;
  const g = mesh.geometry;
  if (!g) return false;
  if (Object.keys(g.morphAttributes ?? {}).length > 0) return false;
  const mat = mesh.material as THREE.MeshStandardMaterial;
  if (Array.isArray(mat) || mat.vertexColors) return false;
  for (const name of Object.keys(g.attributes)) {
    if (name !== 'position' && name !== 'normal' && name !== 'uv') return false;
  }
  return true;
}

/** a) 兄弟 mesh 按材质签名归并（GLTFLoader 多 primitive Group 的主路径）。 */
function mergeSiblingMeshesByMaterial(group: THREE.Group): void {
  const kids = group.children as THREE.Object3D[];
  if (kids.length < 2) return;
  const meshes: THREE.Mesh[] = [];
  for (const k of kids) {
    if (!(k as unknown as { isMesh?: boolean }).isMesh) return; // 混有非 Mesh 子级 → 整组跳过
    meshes.push(k as THREE.Mesh);
  }
  // 桶：签名 → mesh 列表（保持首次出现次序）
  const bucketOrder: string[] = [];
  const buckets = new Map<string, THREE.Mesh[]>();
  for (const mesh of meshes) {
    if (!siblingMergeable(mesh)) return; // 任一部件不可并 → 整组跳过（保守）
    const sig = materialSignature(mesh.material as THREE.Material);
    let list = buckets.get(sig);
    if (!list) {
      list = [];
      buckets.set(sig, list);
      bucketOrder.push(sig);
    }
    list.push(mesh);
  }
  const mergeableBuckets = bucketOrder.filter((sig) => (buckets.get(sig)!.length > 1));
  if (mergeableBuckets.length === 0) return; // 无重复材质，无可并

  for (const sig of mergeableBuckets) {
    const list = buckets.get(sig)!;
    const parts = list.map((mesh) => ({
      geo: mesh.geometry,
      matrix: mesh.matrix.clone(), // 烘焙各自局部变换 → 几何全等
    }));
    const merged = mergeParts(parts);
    const first = list[0];
    const mergedMesh = new THREE.Mesh(merged, first.material);
    mergedMesh.name = `${first.name || 'mesh'}-merged-${list.length}`;
    // cast/receive 取「任一原部件为真」；可见性/剔除跟随原组语义
    mergedMesh.castShadow = list.some((m) => m.castShadow);
    mergedMesh.receiveShadow = list.some((m) => m.receiveShadow);
    group.add(mergedMesh);
    for (const mesh of list) {
      group.remove(mesh);
      mesh.geometry.dispose(); // 原几何已被合并副本取代（材质共享不 dispose）
    }
  }
}

/** b) 多 group 单 mesh 等材质归并（手工构造/特殊 GLB 兜底路径）。 */
function mergeEqualMaterialGroups(root: THREE.Object3D): void {
  root.traverse((o) => {
    const mesh = o as THREE.Mesh;
    if (!(mesh as unknown as { isMesh?: boolean }).isMesh) return;
    if ((mesh as unknown as { isSkinnedMesh?: boolean }).isSkinnedMesh) return;
    if (!Array.isArray(mesh.material) || !mesh.geometry) return;
    const geo = mesh.geometry;
    const idx = geo.getIndex();
    const groups = geo.groups;
    if (!idx || groups.length <= 1 || (mesh.material as THREE.Material[]).length !== groups.length) return;
    // 只处理 loader 产出的「连续无缝覆盖」group 布局，非常规布局保守跳过
    let cursor = 0;
    for (const g of groups) {
      if (g.start !== cursor) return;
      cursor += g.count;
    }
    if (cursor !== idx.count) return;

    // 签名分桶（保持首次出现次序，跨材质透明混合次序不变）
    const bucketOrder: string[] = [];
    const buckets = new Map<string, Array<{ start: number; count: number; mat: THREE.Material }>>();
    groups.forEach((g, i) => {
      const mat = (mesh.material as THREE.Material[])[i];
      const sig = materialSignature(mat);
      let list = buckets.get(sig);
      if (!list) {
        list = [];
        buckets.set(sig, list);
        bucketOrder.push(sig);
      }
      list.push({ start: g.start, count: g.count, mat });
    });
    if (bucketOrder.length === groups.length) return; // 无可合并重复

    // 重排 index：按桶连续拼接（顶点不动，只动三角形提交次序）
    const src = idx.array as ArrayLike<number>;
    const reordered = new (src instanceof Uint32Array ? Uint32Array : Uint16Array)(idx.count);
    const newGroups: Array<{ start: number; count: number; matIndex: number }> = [];
    const newMats: THREE.Material[] = [];
    let w = 0;
    for (const sig of bucketOrder) {
      const list = buckets.get(sig)!;
      const start = w;
      for (const seg of list) {
        for (let k = 0; k < seg.count; k++) reordered[w++] = src[seg.start + k];
      }
      newGroups.push({ start, count: w - start, matIndex: newMats.length });
      newMats.push(list[0].mat);
    }
    geo.setIndex(new THREE.BufferAttribute(reordered, 1));
    geo.clearGroups();
    for (const g of newGroups) geo.addGroup(g.start, g.count, g.matIndex);
    mesh.material = newMats;
  });
}

/** 入口：a) 兄弟 mesh 归并 + b) 多 group 归并（遍历中收集待处理 Group，避免边遍历边改树）。 */
function mergeMeshesByMaterial(root: THREE.Object3D): void {
  const groups: THREE.Group[] = [];
  root.traverse((o) => {
    if ((o as THREE.Group).isGroup && o.children.length >= 2) groups.push(o as THREE.Group);
  });
  for (const g of groups) mergeSiblingMeshesByMaterial(g);
  mergeEqualMaterialGroups(root);
}

export interface SharedGLTF {
  /** 场景根（共享，调用方必须 .clone(true) 后再使用） */
  scene: THREE.Group | null;
  /** 动画剪辑（如有；行人 walk 等） */
  animations: THREE.AnimationClip[];
}

interface CacheEntry {
  gltf: SharedGLTF | null; // null = 加载失败哨兵
  done: boolean;
  listeners: Set<(g: SharedGLTF | null) => void>;
}

const CACHE = new Map<string, CacheEntry>();
const LOADER = new GLTFLoader();

function startLoad(url: string): CacheEntry {
  const entry: CacheEntry = { gltf: null, done: false, listeners: new Set() };
  CACHE.set(url, entry);
  LOADER.load(
    url,
    (g) => {
      // 批次 28 二轮：等材质归并（一次加载一次合并，全部克隆实例共享收益）
      mergeMeshesByMaterial(g.scene);
      // 批次 29：尺寸/落地/变换规约校验（dev-only，只告警不改几何；目标尺寸由调用方注册）。
      // 置于归并之后：合并只重排/烘焙几何，不改包围盒，量测口径与渲染一致。
      // animations 传入以豁免"被 clip 驱动的节点"的 rotation（rig 姿态，如行人四肢翻转）。
      checkModelSize(g.scene, url, g.animations ?? []);
      // GLTFLoader 默认加载的所有 texture colorSpace 保留（不强改）
      entry.gltf = {
        scene: g.scene,
        animations: g.animations ?? [],
      };
      entry.done = true;
      entry.listeners.forEach((fn) => fn(entry.gltf));
      entry.listeners.clear();
    },
    undefined,
    () => {
      // 失败哨兵：gltf 保持 null，done=true，后续直接走降级
      entry.done = true;
      entry.listeners.forEach((fn) => fn(null));
      entry.listeners.clear();
    },
  );
  return entry;
}

/**
 * 共享 GLTF hook：同 url 多组件只发一次网络请求 / 只占一份 GPU 几何 + 材质。
 * 未加载完成或失败返回 { scene: null, animations: [] }；url 为空直接返回 null scene。
 *
 * `sizeTarget`（可选，批次 29）：本 url 的**期望尺寸 / 落地**声明，用于 dev 态
 * glbSizeGuard 校验（见 engine3d/glbSizeGuard.ts）。在起加载的同一个 effect 里注册，
 * 故首次加载成功回调前注册必已完成；加载完成的缓存命中路径无需重注册。
 * ⚠️ 调用方应传**稳定引用**（模块级常量 / useMemo），否则每次渲染重跑 effect。
 *
 * ⚠️ 调用方拿到的 scene 必须 .clone(true) 后再挂载，否则多实例 transform 会互相污染。
 * 详见 engine3d/Model.tsx 的 useMemo clone 模式。
 */
export function useSharedGLTF(url: string, sizeTarget?: ModelSizeTarget): SharedGLTF {
  const [gltf, setGltf] = useState<SharedGLTF | null>(() => {
    if (!url) return null;
    const hit = CACHE.get(url);
    return hit?.done ? hit.gltf : null;
  });

  useEffect(() => {
    if (!url) {
      setGltf(null);
      return;
    }
    // 批次 29：目标尺寸贴近加载点注册（dev-only；生产构建静态折叠为 no-op）
    if (sizeTarget) registerModelSizeTarget(url, sizeTarget);
    let entry = CACHE.get(url);
    if (!entry) entry = startLoad(url);
    if (entry.done) {
      setGltf(entry.gltf);
      return;
    }
    const listener = (g: SharedGLTF | null) => setGltf(g);
    entry.listeners.add(listener);
    return () => {
      entry?.listeners.delete(listener);
    };
  }, [url, sizeTarget]);

  // 始终返回对象结构（即便空），调用方写 { scene, animations } 不需要空判
  if (!gltf) return { scene: null, animations: [] };
  return gltf;
}

/** 测试/热更新用：清空缓存（正常运行时不需要；与 textureCache::clearTextureCache 对齐）。 */
export function clearModelCache(): void {
  // 不主动 dispose scene/clips：scene 是 shallow copy，materials/geometries 可能被其他实例共享
  CACHE.clear();
}
