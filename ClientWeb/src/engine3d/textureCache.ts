/**
 * engine3d/textureCache — 进程级共享贴图缓存 + 通用 PBR 三件套拼装。
 *
 * 自 components/virtualCity/textureCache.ts 迁入（22-3D世界升级与引擎模块化，
 * 14-3D城市渲染深化 · 阶段 H 的原始契约不变）：
 *   - key = `${url}|${wrap}|${rx}|${ry}|${srgb}|${aniso}`；命中同步复用。
 *   - **禁止组件侧 dispose 共享纹理** —— 缓存随页面生命周期存活（页面卸载即整页销毁）。
 *   - url === ''（资产缺失）→ 返回 null，零副作用（降级链由调用方处理）。
 *   - 加载失败缓存哨兵：tex=null, done=true，后续调用直接返回 null（不反复重试）。
 *
 * 通用模块：只接受 URL 参数，不 import 任何游戏的资产路径模块。
 * 游戏私有的「stem → URL」拼接（如虚拟城市 synth 合成材质）由各游戏侧薄适配层完成
 * （虚拟城市：components/virtualCity/cityPbr.ts）。
 */

import { useEffect, useMemo, useState } from 'react';
import * as THREE from 'three';
import { QUALITY_PRESETS, knownQualityTier } from './quality';

export interface SharedTextureOpts {
  /** repeat = RepeatWrapping（路面/地面/水面）；clamp = ClampToEdge（立面/道具 sprite）。 */
  wrap?: 'repeat' | 'clamp';
  /** repeat 平铺次数（仅 repeat 模式生效；默认 [1, 1]）。 */
  repeat?: [number, number];
  /** 默认 true（颜色贴图走 sRGB）。 */
  srgb?: boolean;
  /**
   * 各向异性过滤等级。repeat 模式默认取质量档 `QUALITY_PRESETS[].anisotropy`
   * （批次 28 A5：high 8 / low 4），clamp 模式默认 1。
   * three 上传时自动 clamp 到 GPU 上限，无需读 renderer。
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

/** repeat 模式默认 AF 等级按质量档（批次 28 A5）；clamp 模式 1。 */
function defaultAnisotropy(wrap: 'repeat' | 'clamp' | undefined): number {
  return wrap === 'repeat' ? QUALITY_PRESETS[knownQualityTier()].anisotropy : 1;
}

function cacheKey(url: string, opts: SharedTextureOpts): string {
  const wrap = opts.wrap ?? 'clamp';
  const [rx, ry] = opts.repeat ?? [1, 1];
  const srgb = opts.srgb !== false;
  const aniso = opts.anisotropy ?? defaultAnisotropy(opts.wrap);
  return `${url}|${wrap}|${rx}|${ry}|${srgb}|${aniso}`;
}

/** 给纹理对象应用缓存 key 对应的采样参数（创建时同步设置一次）。 */
function applyTextureOpts(tex: THREE.Texture, opts: SharedTextureOpts): void {
  tex.colorSpace = (opts.srgb !== false) ? THREE.SRGBColorSpace : THREE.NoColorSpace;
  tex.magFilter = THREE.LinearFilter;
  tex.minFilter = THREE.LinearMipmapLinearFilter;
  // 各向异性过滤（GPU 上限由 three 内部 clamp，设置值过大安全）
  tex.anisotropy = opts.anisotropy ?? defaultAnisotropy(opts.wrap);
  if (opts.wrap === 'repeat') {
    tex.wrapS = tex.wrapT = THREE.RepeatWrapping;
    const [rx, ry] = opts.repeat ?? [1, 1];
    tex.repeat.set(rx, ry);
  } else {
    tex.wrapS = tex.wrapT = THREE.ClampToEdgeWrapping;
  }
}

function startLoad(url: string, key: string, opts: SharedTextureOpts): CacheEntry {
  // 幂等：并发/StrictMode 双调返回既有 entry（占位纹理对象唯一）。
  const existing = CACHE.get(key);
  if (existing) return existing;
  const entry: CacheEntry = { tex: null, done: false, listeners: new Set() };
  CACHE.set(key, entry);
  // 批次 54（地表全白根因修复）：占位纹理对象。TextureLoader.load 同步返回纹理
  // 对象（image=null 占位、参数创建时同步施加），组件首帧即持稳定引用 ——
  // `map` 在材质首编译时即入 program key（USE_MAP），图片到达后由 loader 内部
  // needsUpdate 重上传。规避 r3f 8.17 applyProps 不设 material.needsUpdate 导致
  // 「贴图到达但着色器不重编译」（map 被静默忽略 → 白 color 直通 → 全城地表全白）。
  // 加载窗口内未上传纹理按 WebGL 规范采样 (0,0,0,1) 黑 —— 与多数降级色同为暗色，
  // 本地资产毫秒级到达，验收以最终帧为准。
  const tex = LOADER.load(
    url,
    (loaded) => {
      // loaded === tex（TextureLoader 同步返回的对象即回调入参），参数已施加。
      entry.done = true;
      entry.listeners.forEach((fn) => fn(loaded));
      entry.listeners.clear();
    },
    undefined,
    () => {
      // 失败哨兵：tex=null, done=true，后续直接走降级
      entry.done = true;
      entry.tex = null;
      entry.listeners.forEach((fn) => fn(null));
      entry.listeners.clear();
    },
  );
  applyTextureOpts(tex, opts);
  entry.tex = tex;
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
    // 批次 54：渲染期 get-or-create —— 首个消费者的首帧即持占位纹理对象
    //（useEffect 在首帧编译之后才跑，等它再 setTex 就错过了 program 首编译），
    // StrictMode/并发双调由 startLoad 内部幂等挡掉。命中未完成 entry 同理返回
    // 占位纹理，保证 map 引用稳定、材质首编译即带 USE_MAP。
    const hit = startLoad(url, key, { wrap, repeat: [rx, ry], srgb, anisotropy: aniso });
    return hit.tex;
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

// ── 通用 PBR 三件套（18-3D城市PBR材质与真实城市冲刺 · 02 架构设计 §2.2）─────

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
 * 共享 PBR 三件套。三张贴图共用同一 wrap/repeat（同一套 UV），
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

  // 批次 28 A4：matProps / 返回对象按纹理实例稳定化（useMemo）——
  // 此前每次 render 新建对象，叠加页面级 re-render 会逐帧重 diff 数千材质。
  return useMemo(() => {
    const matProps: SharedPBR['matProps'] = {};
    if (map) matProps.map = map;
    if (normalMap) {
      matProps.normalMap = normalMap;
      matProps.normalScale = normalScale;
    }
    if (roughnessMap) matProps.roughnessMap = roughnessMap;
    return { map, normalMap, roughnessMap, matProps };
  }, [map, normalMap, roughnessMap, normalScale]);
}

/**
 * 统一材质规则（withPBR）：
 *   - **pbr 提供缺省，base 可覆盖**（批次 38 R6 P0 修复：原 `{...rest, ...mp}`
 *     让 `mp.map` 覆盖 `base.map`，导致 road_main/road_side 烘焙标线永远不上屏）；
 *   - base 中 `undefined` 值**不算显式覆盖**（`{ map: tex ?? undefined }` 的惯用写法
 *     在 tex 缺失时应回落 pbr 的缺省 map，而不是把 map 清掉）；
 *   - 有 roughnessMap 时不传 roughness（由贴图全权决定；three 用 roughnessMap.g × 1.0）。
 *   - 无 roughnessMap 时保留 base.roughness 硬编码值。
 * 贴图 map/normalMap 缺失时 matProps 自动不含对应键。
 */
export function withPBR(
  base: Record<string, unknown>,
  pbr: SharedPBR,
): Record<string, unknown> {
  // 批次 28 A4：无 PBR 贴图可拼时直接返回 base —— 与原 spread 逻辑结果逐键一致
  // （rest = base 去 roughness，再回填 base.roughness），但省掉每 render 一次
  // 新对象分配（贴图缺失 / 加载失败的降级路径是高频路径）。
  const mp = pbr.matProps;
  if (!mp.map && !mp.normalMap && !mp.roughnessMap) return base;
  const { roughness: _omitRoughness, ...rest } = base;
  // 只收集显式（非 undefined）的 base 键 —— 调用方传入的 map 优先，pbr 只补缺
  const overrides: Record<string, unknown> = {};
  for (const key of Object.keys(rest)) {
    const v = rest[key];
    if (v !== undefined) overrides[key] = v;
  }
  const merged: Record<string, unknown> = { ...mp, ...overrides };
  if (!mp.roughnessMap) merged.roughness = base.roughness;
  return merged;
}
