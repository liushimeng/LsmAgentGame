/**
 * textureCache — 进程级共享贴图缓存（14-3D城市渲染深化 · 阶段 H）。
 *
 * 动机（契约 02-架构设计 §1）：BuildingMesh / Road / DistrictBlock / Vehicle /
 * Pedestrian / Tree / Sign / RooftopAcc / StreetLight / Ground 各自 new TextureLoader
 * → 同源 PNG 重复 GPU 上传（约 240 次 vs 约 30 张唯一贴图），且组件卸载各自 dispose()
 * 有误销毁共享源的风险。
 *
 * 规约：
 *   - key = `${url}|${wrap}|${rx}|${ry}|${srgb}`；命中同步复用。
 *   - **禁止组件侧 dispose 共享纹理** —— 缓存随页面生命周期存活（游戏页卸载即整页销毁）。
 *   - url === ''（资产缺失）→ 返回 null，零副作用（§9 降级链由调用方处理）。
 *   - 加载失败缓存哨兵 FAILED，后续调用直接返回 null（不反复重试）。
 */

import { useEffect, useState } from 'react';
import * as THREE from 'three';

export interface SharedTextureOpts {
  /** repeat = RepeatWrapping（路面/地面/水面）；clamp = ClampToEdge（立面/道具 sprite）。 */
  wrap?: 'repeat' | 'clamp';
  /** repeat 平铺次数（仅 repeat 模式生效；默认 [1, 1]）。 */
  repeat?: [number, number];
  /** 默认 true（颜色贴图走 sRGB）。 */
  srgb?: boolean;
  /**
   * 16 · 阶段 R：各向异性过滤等级。repeat 模式默认 8（路面/地面掠射角清晰），
   * clamp 模式默认 1。three 上传时自动 clamp 到 GPU 上限，无需读 renderer。
   */
  anisotropy?: number;
}

interface CacheEntry {
  tex: THREE.Texture | null; // null = 加载失败哨兵
  done: boolean;
  listeners: Set<(t: THREE.Texture | null) => void>;
}

const CACHE = new Map<string, CacheEntry>();
const LOADER = new THREE.TextureLoader();

function cacheKey(url: string, opts: SharedTextureOpts): string {
  const wrap = opts.wrap ?? 'clamp';
  const [rx, ry] = opts.repeat ?? [1, 1];
  const srgb = opts.srgb !== false;
  const aniso = opts.anisotropy ?? (opts.wrap === 'repeat' ? 8 : 1);
  return `${url}|${wrap}|${rx}|${ry}|${srgb}|${aniso}`;
}

function startLoad(url: string, key: string, opts: SharedTextureOpts): CacheEntry {
  const entry: CacheEntry = { tex: null, done: false, listeners: new Set() };
  CACHE.set(key, entry);
  LOADER.load(
    url,
    (loaded) => {
      loaded.colorSpace = (opts.srgb !== false) ? THREE.SRGBColorSpace : THREE.NoColorSpace;
      loaded.magFilter = THREE.LinearFilter;
      loaded.minFilter = THREE.LinearMipmapLinearFilter;
      // 16 · 阶段 R：各向异性过滤（GPU 上限由 three 内部 clamp，设置值过大安全）
      loaded.anisotropy = opts.anisotropy ?? (opts.wrap === 'repeat' ? 8 : 1);
      if (opts.wrap === 'repeat') {
        loaded.wrapS = loaded.wrapT = THREE.RepeatWrapping;
        const [rx, ry] = opts.repeat ?? [1, 1];
        loaded.repeat.set(rx, ry);
      } else {
        loaded.wrapS = loaded.wrapT = THREE.ClampToEdgeWrapping;
      }
      entry.tex = loaded;
      entry.done = true;
      entry.listeners.forEach((fn) => fn(loaded));
      entry.listeners.clear();
    },
    undefined,
    () => {
      // 失败哨兵：tex 保持 null，done=true，后续直接走降级
      entry.done = true;
      entry.listeners.forEach((fn) => fn(null));
      entry.listeners.clear();
    },
  );
  return entry;
}

/**
 * 共享贴图 hook：同 key 多组件只发一次网络请求 / 只占一份 GPU 纹理。
 * 未加载完成或失败返回 null；url 为空直接返回 null。
 */
export function useSharedTexture(
  url: string,
  opts?: SharedTextureOpts,
): THREE.Texture | null {
  const wrap = opts?.wrap ?? 'clamp';
  const rx = opts?.repeat?.[0] ?? 1;
  const ry = opts?.repeat?.[1] ?? 1;
  const srgb = opts?.srgb !== false;
  const aniso = opts?.anisotropy;
  const key = url ? cacheKey(url, { wrap, repeat: [rx, ry], srgb, anisotropy: aniso }) : '';

  const [tex, setTex] = useState<THREE.Texture | null>(() => {
    if (!key) return null;
    const hit = CACHE.get(key);
    return hit?.done ? hit.tex : null;
  });

  useEffect(() => {
    if (!key) {
      setTex(null);
      return;
    }
    let entry = CACHE.get(key);
    if (!entry) entry = startLoad(url, key, { wrap, repeat: [rx, ry], srgb, anisotropy: aniso });
    if (entry.done) {
      setTex(entry.tex);
      return;
    }
    const listener = (t: THREE.Texture | null) => setTex(t);
    entry.listeners.add(listener);
    return () => {
      entry?.listeners.delete(listener);
    };
  }, [key, url, wrap, rx, ry, srgb]);

  return tex;
}

/** 测试/热更新用：清空缓存（正常运行时不需要）。 */
export function clearTextureCache(): void {
  CACHE.forEach((e) => e.tex?.dispose());
  CACHE.clear();
}
