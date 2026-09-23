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

import { useEffect, useMemo, useState } from 'react';
import * as THREE from 'three';
import { pbrNormalUrl, pbrRoughUrl } from '@/assets/images/wealth';

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

// ── 18-3D城市PBR材质与真实城市冲刺 · 阶段 X（PBR 三件套）────────────────

/** synth 合成材质固定 stem（与 3d_script/procedural_pbr_maps.py::synth_jobs 对齐）。
 *  由 useSynthPBR 内部消费，building_shapes 不感知具体 URL 拼接。 */
export type SynthStem =
  | 'water'
  | 'concrete'
  | 'brick'
  | 'metal_deck'
  | 'tile_roof'
  | 'asphalt_wear'
  | 'foliage';

export interface SharedPBROpts {
  /** 三张贴图共用同一 wrap（颜色图走 sRGB，法线/粗糙度在 useSharedPBR 内固定 srgb:false）。 */
  wrap?: 'repeat' | 'clamp';
  /** repeat 平铺次数。 */
  repeat?: [number, number];
  /** 法线强度（three `normalScale`，默认 [1, 1]）。 */
  normalScale?: [number, number];
}

export interface SharedPBR {
  /** 颜色贴图。 */
  map: THREE.Texture | null;
  /** 法线贴图（已设 NoColorSpace）。 */
  normalMap: THREE.Texture | null;
  /** 粗糙度贴图（已设 NoColorSpace）。 */
  roughnessMap: THREE.Texture | null;
  /** 拼好的材质 props（map/normalMap/roughnessMap/normalScale）；贴图缺失的键自动省略。 */
  matProps: {
    map?: THREE.Texture;
    normalMap?: THREE.Texture;
    roughnessMap?: THREE.Texture;
    normalScale?: THREE.Vector2;
  };
}

/**
 * 共享 PBR 三件套（18-3D城市PBR材质与真实城市冲刺 · 02 架构设计 §2.2）。
 * 三张贴图共用同一 wrap/repeat（同一套 UV），
 * 法线/粗糙度固定 `srgb: false`（colorSpace = NoColorSpace，线性空间）——这是常踩的
 * 「法线过弱」根因：法线是方向向量、粗糙度是标量，走 sRGB 会被色彩空间扭曲。
 * 全部走 useSharedTexture → 进程级缓存，禁组件自建 loader。
 *
 * @param colorUrl  颜色贴图 URL；缺失时 map=null（材质回退纯色 / 硬编码属性）。
 * @param normalUrl 法线贴图 URL；缺失时 normalMap=null（降级 → 无凹凸）。
 * @param roughUrl  粗糙度贴图 URL；缺失时 roughnessMap=null（降级 → 用材质硬编码 roughness）。
 * @param opts      wrap / repeat / normalScale；normalScale 仅当 normalMap 存在时拼入 matProps。
 */
export function useSharedPBR(
  colorUrl: string,
  normalUrl: string,
  roughUrl: string,
  opts?: SharedPBROpts,
): SharedPBR {
  const map = useSharedTexture(colorUrl, opts ? { wrap: opts.wrap, repeat: opts.repeat, srgb: true } : { srgb: true });
  const normalMap = useSharedTexture(normalUrl, opts ? { wrap: opts.wrap, repeat: opts.repeat, srgb: false } : { srgb: false });
  const roughnessMap = useSharedTexture(roughUrl, opts ? { wrap: opts.wrap, repeat: opts.repeat, srgb: false } : { srgb: false });

  // 法线强度 Vector2 —— 仅当 normalMap 存在时拼入 matProps（无 normalMap 时 normalScale 无意义，
  // three 不会告警但「声明却从不接线」违反 §130 接线纪律）。
  const normalScale = useMemo(() => {
    const [nx = 1, ny = 1] = opts?.normalScale ?? [1, 1];
    return new THREE.Vector2(nx, ny);
  }, [opts?.normalScale?.[0], opts?.normalScale?.[1]]);

  const matProps: SharedPBR['matProps'] = {};
  if (map) matProps.map = map;
  if (normalMap) {
    matProps.normalMap = normalMap;
    matProps.normalScale = normalScale;
  }
  if (roughnessMap) matProps.roughnessMap = roughnessMap;

  return { map, normalMap, roughnessMap, matProps };
}

/**
 * synth 合成材质 PBR（02 架构设计 §2.3 表内第 11-14 行：concrete / brick / metal_deck /
 * tile_roof / foliage 等）。颜色图为空 → 材质保持纯色 + 硬编码属性，仅由法线/粗糙度贴图
 * 贡献凹凸与光泽差。building_shapes 用此 hook 而非直接 import pbr 路径，
 * 满足 §3.3「stem 拼接只允许在 BuildingMesh.tsx」的约束。
 */
export function useSynthPBR(name: SynthStem, opts?: SharedPBROpts): SharedPBR {
  return useSharedPBR('', pbrNormalUrl('synth', name), pbrRoughUrl('synth', name), opts);
}

/**
 * 统一材质规则（02 架构设计 §2.3 withPBR）：
 *   - 有 roughnessMap 时不传 roughness（由贴图全权决定；three 用 roughnessMap.g × 1.0）。
 *   - 无 roughnessMap 时保留 base.roughness 硬编码值。
 * 贴图 map/normalMap 缺失时 matProps 自动不含对应键。
 */
export function withPBR(
  base: Record<string, unknown>,
  pbr: SharedPBR,
): Record<string, unknown> {
  const { roughness: _omitRoughness, ...rest } = base;
  const merged: Record<string, unknown> = { ...rest, ...pbr.matProps };
  if (!pbr.matProps.roughnessMap) merged.roughness = base.roughness;
  return merged;
}
