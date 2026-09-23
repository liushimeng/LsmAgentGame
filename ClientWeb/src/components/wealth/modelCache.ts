/**
 * modelCache — 进程级共享 GLTFLoader 缓存（19-Blender3D模型集成 · 阶段 A）。
 *
 * 动机（与 textureCache.ts 同源，契约见 lag_docs/虚拟城市/已实现/19-Blender3D模型集成/02-架构设计 §3）：
 *   - 56 个行人 / N 车 / N 市政厅若各自 new GLTFLoader → 同一 .glb 被 N 次 fetch + parse
 *   - 同源 scene 必须 .clone(true) per instance（共享 scene 会让多组件 transform 互相污染）
 *   - 缓存 key = url（GLB 不像贴图有 wrap/repeat 参数，材质贴图由 mesh 内 material slot 自带）
 *
 * 规约（与 textureCache.ts 完全对齐）：
 *   - 命中同步复用 GLTFData，但 scene.clone(true) per useSharedGLTF 调用
 *   - **禁止组件侧 dispose 共享 scene** —— 缓存随页面生命周期存活（游戏页卸载即整页销毁）
 *   - url === ''（资产缺失）→ 返回 null，零副作用（§9 降级链由调用方处理）
 *   - 加载失败缓存哨兵：gltf=null, done=true，后续调用直接返回 null（不反复重试）
 */

import { useEffect, useState } from 'react';
import * as THREE from 'three';
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js';

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
 * ⚠️ 调用方拿到的 scene 必须 .clone(true) 后再挂载，否则多实例 transform 会互相污染。
 * 详见 <Model /> 组件（ClientWeb/src/components/wealth/Model.tsx）的 useMemo clone 模式。
 */
export function useSharedGLTF(url: string): SharedGLTF {
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
  }, [url]);

  // 始终返回对象结构（即便空），调用方写 { scene, animations } 不需要空判
  if (!gltf) return { scene: null, animations: [] };
  return gltf;
}

/** 测试/热更新用：清空缓存（正常运行时不需要；与 textureCache::clearTextureCache 对齐）。 */
export function clearModelCache(): void {
  // 不主动 dispose scene/clips：scene 是 shallow copy，materials/geometries 可能被其他实例共享
  CACHE.clear();
}
