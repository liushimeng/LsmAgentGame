/**
 * engine3d/WeatherFX — 雨/雪粒子（游戏无关，单 draw call Points）。
 *
 * 批次 27「时间比例与昼夜季节天气」抽象入引擎层（原始契约见
 * lag_docs/虚拟城市/已实现/27-时间比例与昼夜季节天气/01-方案设计-v1.md §4.2）：
 *   - sample() 每帧读 { kind, intensity01, windX }；kind=null → visible=false 零开销。
 *   - 预分配 maxCount 粒子 BufferAttribute，逐帧原地更新位置，出盒 wrap 回顶
 *     （无 GC 压力；2000 粒子 ≈ 6k float/帧，可忽略）。
 *   - 数量随 intensity01 三档（600/1200/2000，setDrawRange 切换，不重建缓冲）；
 *     low 质量档减半。
 *   - 雨：竖条 sprite（调用方可传 URL，缺省引擎自绘 canvas 竖条）+ 快速下落；
 *     雪：柔光圆点 + 慢降 + 正弦横摆，可被 windX 平推。
 *   - 分层契约：只依赖 three / @react-three/fiber / 本目录 quality，
 *     **禁止 import 游戏私有模块**（CLAUDE.md §2.1 规则 5）。
 */

import { useEffect, useMemo, useRef } from 'react';
import type { MutableRefObject } from 'react';
import { useFrame } from '@react-three/fiber';
import * as THREE from 'three';
import type { QualityTier } from './quality';

/** 采样契约（每帧调用）。 */
export interface WeatherSample {
  /** 粒子类型；null = 无天气（隐藏整层）。 */
  kind: 'rain' | 'snow' | null;
  /** 强度 0..1（数量档位 + 尺寸/速度微调）。 */
  intensity01: number;
  /** 水平风力（世界单位/秒；雪的横摆漂移 + 暴雨/暴雪平推）。 */
  windX?: number;
}

export interface WeatherFXProps {
  /** 天气采样（每帧调用，必须廉价）。 */
  sample: () => WeatherSample;
  /** 渲染质量档（low → 粒子上限减半；缺省 high）。 */
  quality?: QualityTier;
  /** 粒子盒尺寸 [w, h, d]（世界单位）。 */
  area?: [number, number, number];
  /** 粒子盒中心。 */
  center?: [number, number, number];
  /** 雨滴 sprite URL（缺省引擎自绘竖条贴图）。 */
  sprite?: string;
  /** 雪花 sprite URL（缺省引擎自绘柔光圆点）。 */
  snowSprite?: string;
}

const MAX_COUNT_HIGH = 2000;
const MAX_COUNT_LOW = 1000;

/** 引擎自绘雨滴竖条（16×64 canvas，上下渐隐）。 */
function makeRainTexture(): THREE.Texture | null {
  if (typeof document === 'undefined') return null;
  const c = document.createElement('canvas');
  c.width = 16;
  c.height = 64;
  const ctx = c.getContext('2d');
  if (!ctx) return null;
  const grad = ctx.createLinearGradient(0, 0, 0, 64);
  grad.addColorStop(0, 'rgba(210,228,246,0)');
  grad.addColorStop(0.45, 'rgba(210,228,246,0.95)');
  grad.addColorStop(1, 'rgba(210,228,246,0)');
  ctx.fillStyle = grad;
  ctx.fillRect(6, 0, 4, 64);
  const tex = new THREE.CanvasTexture(c);
  tex.needsUpdate = true;
  return tex;
}

/** 引擎自绘雪花柔光点（64×64 radial）。 */
function makeSnowTexture(): THREE.Texture | null {
  if (typeof document === 'undefined') return null;
  const c = document.createElement('canvas');
  c.width = 64;
  c.height = 64;
  const ctx = c.getContext('2d');
  if (!ctx) return null;
  const grad = ctx.createRadialGradient(32, 32, 2, 32, 32, 30);
  grad.addColorStop(0, 'rgba(255,255,255,1)');
  grad.addColorStop(0.55, 'rgba(255,255,255,0.85)');
  grad.addColorStop(1, 'rgba(255,255,255,0)');
  ctx.fillStyle = grad;
  ctx.fillRect(0, 0, 64, 64);
  const tex = new THREE.CanvasTexture(c);
  tex.needsUpdate = true;
  return tex;
}

/** 可选外部 sprite：加载成功写入 target ref（替换自绘默认），失败静默保留默认。 */
function useOptionalSprite(
  url: string | undefined,
  target: MutableRefObject<THREE.Texture | null>,
): void {
  useEffect(() => {
    if (!url) return;
    const loader = new THREE.TextureLoader();
    let disposed = false;
    let loaded: THREE.Texture | null = null;
    loader.load(url, (tex) => {
      if (disposed) {
        tex.dispose();
        return;
      }
      loaded = tex;
      target.current = tex;
    });
    return () => {
      disposed = true;
      loaded?.dispose();
    };
  }, [url, target]);
}

export function WeatherFX({
  sample,
  quality = 'high',
  area = [150, 55, 150],
  center = [0, 26, 0],
  sprite,
  snowSprite,
}: WeatherFXProps) {
  const maxCount = quality === 'low' ? MAX_COUNT_LOW : MAX_COUNT_HIGH;

  // 粒子预分配：位置 + 逐粒子速度系数 / 摆动相位（确定性无需——纯装饰随机即可）。
  const { geometry, material, speeds, phases } = useMemo(() => {
    const positions = new Float32Array(maxCount * 3);
    const sp = new Float32Array(maxCount);
    const ph = new Float32Array(maxCount);
    const [w, h, d] = area;
    for (let i = 0; i < maxCount; i++) {
      positions[i * 3] = center[0] + (Math.random() - 0.5) * w;
      positions[i * 3 + 1] = center[1] + (Math.random() - 0.5) * h;
      positions[i * 3 + 2] = center[2] + (Math.random() - 0.5) * d;
      sp[i] = 0.7 + Math.random() * 0.6;
      ph[i] = Math.random() * Math.PI * 2;
    }
    const geo = new THREE.BufferGeometry();
    geo.setAttribute('position', new THREE.BufferAttribute(positions, 3));
    geo.setDrawRange(0, 0);
    const mat = new THREE.PointsMaterial({
      size: 1.2,
      sizeAttenuation: true,
      transparent: true,
      opacity: 0.55,
      depthWrite: false,
      color: new THREE.Color('#c6d8ec'),
    });
    return { geometry: geo, material: mat, speeds: sp, phases: ph };
    // area/center 视为静态配置（挂载定盒）；maxCount 随质量档固定。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [maxCount]);

  // 自绘默认贴图（雨竖条 / 雪柔光点）+ 可选外部 sprite（异步加载成功后替换）。
  const rainTexRef = useRef<THREE.Texture | null>(null);
  const snowTexRef = useRef<THREE.Texture | null>(null);
  useEffect(() => {
    rainTexRef.current = makeRainTexture();
    snowTexRef.current = makeSnowTexture();
    return () => {
      rainTexRef.current?.dispose();
      rainTexRef.current = null;
      snowTexRef.current?.dispose();
      snowTexRef.current = null;
    };
  }, []);
  useOptionalSprite(sprite, rainTexRef);
  useOptionalSprite(snowSprite, snowTexRef);

  const pointsRef = useRef<THREE.Points>(null);
  const lastKindRef = useRef<'rain' | 'snow' | null>(null);
  const lastCountRef = useRef(0);

  // 卸载释放（自建 geometry / material 不归 R3F 托管）。
  useEffect(
    () => () => {
      geometry.dispose();
      material.dispose();
    },
    [geometry, material],
  );

  useFrame((state, delta) => {
    const pts = pointsRef.current;
    if (!pts) return;
    const s = sample();
    const kind = s.kind;
    pts.visible = kind !== null;
    if (!kind) {
      lastKindRef.current = null;
      return;
    }
    const inten = Math.min(1, Math.max(0, s.intensity01));
    const wind = s.windX ?? 0;
    const t = state.clock.elapsedTime;

    // 类型切换 → 换颜色/尺寸/透明度（低频分支）。
    if (kind !== lastKindRef.current) {
      lastKindRef.current = kind;
      if (kind === 'rain') {
        material.color.set('#c6d8ec');
        material.size = 1.3;
        material.opacity = 0.5;
      } else {
        material.color.set('#ffffff');
        material.size = 0.6;
        material.opacity = 0.85;
      }
      material.needsUpdate = true;
    }
    // 贴图同步：覆盖「类型切换」与「外部 sprite 异步加载完成」两种时序（指针比较，零开销）。
    const wantMap = kind === 'rain' ? rainTexRef.current : snowTexRef.current;
    if (material.map !== (wantMap ?? null)) {
      material.map = wantMap ?? null;
      material.needsUpdate = true;
    }

    // 数量三档：600 / 1200 / 上限（low 减半），setDrawRange 切换。
    let count = inten < 0.4 ? 600 : inten < 0.75 ? 1200 : maxCount;
    if (quality === 'low') count = Math.floor(count / 2);
    if (count !== lastCountRef.current) {
      lastCountRef.current = count;
      geometry.setDrawRange(0, count);
    }

    // 逐粒子推进：雨 28 u/s 快落；雪 6 u/s 慢降 + 正弦横摆 + 风推。
    const pos = geometry.getAttribute('position') as THREE.BufferAttribute;
    const arr = pos.array as Float32Array;
    const [w, h, d] = area;
    const xMin = center[0] - w / 2;
    const zMin = center[2] - d / 2;
    const yMin = center[1] - h / 2;
    const fall = (kind === 'rain' ? 28 : 6) * (0.6 + 0.6 * inten);
    const dt = Math.min(delta, 0.1); // 后台标签页回来时防巨步穿盒
    const swayAmp = kind === 'snow' ? 1.6 : 0;
    for (let i = 0; i < count; i++) {
      const i3 = i * 3;
      arr[i3 + 1] -= fall * speeds[i] * dt;
      arr[i3] += (swayAmp * Math.sin(t * 1.4 + phases[i]) + wind) * dt;
      // wrap：落出盒底 → 回顶；横移出盒 → 环回对侧。
      if (arr[i3 + 1] < yMin) arr[i3 + 1] += h;
      if (arr[i3] < xMin) arr[i3] += w;
      else if (arr[i3] > xMin + w) arr[i3] -= w;
      if (arr[i3 + 2] < zMin) arr[i3 + 2] += d;
      else if (arr[i3 + 2] > zMin + d) arr[i3 + 2] -= d;
    }
    pos.needsUpdate = true;
  });

  return (
    <points
      ref={pointsRef}
      geometry={geometry}
      material={material}
      frustumCulled={false}
      renderOrder={5}
    />
  );
}

export default WeatherFX;
