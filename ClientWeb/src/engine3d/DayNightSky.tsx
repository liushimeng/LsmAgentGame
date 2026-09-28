/**
 * engine3d/DayNightSky — 昼夜渐变天空穹（游戏无关，批次 30 B1）。
 *
 * 背景：drei `<Sky>`（Preetham）在本机实测「夜间天带亮度 229 vs 正午 233」几乎
 * 不随昼夜变化（恒为近白），一整条白带把暗城市拉成白天观感（批次 30 审计实测，
 * gl.readPixels 采样）。Preetham 的 sunfade 依赖 450000 量级 sunPosition，
 * 标准用法压不到目标亮度 ⇒ 换自写渐变穹（主 Agent 方案 (b) 明确授权，仍放 engine3d/
 * 通用组件，游戏侧只传采样/调色，遵守 §2.1 硬约束 5）。
 *
 * 渲染契约：
 *   - 单 mesh（BackSide 球穹，radius=1 由父层 scale），1 draw call；
 *   - ShaderMaterial 四段色带（顶 / 地平线 双段插值 + 日出日落暖色带）+
 *     太阳光晕/日盘 + 程序化星空（hash 阈值，无额外 mesh）+ 月盘/月晕；
 *   - 输出走 `tonemapping_fragment` + `colorspace_fragment`（与 renderer 的
 *     ACES / toneMappingExposure 同步 —— 夜间曝光下降同样压暗天穹）；
 *   - 颜色由调用方每帧经 `update()` 写入（无 React 重渲染）；
 *   - `scene.background` 与 `scene.fog.color` 由调用方取**同一 horizon 色**注入
 *     （同源同色，杜绝「白天空 + 深色雾」），本组件不管 fog 生命周期。
 *
 * 契约目标（批次 30 验收不变量）：夜（timeOfDay01≈0.02）天带亮度 ≤60；
 * 黄昏/黎明 ≤120 且带暖/冷粉偏色；正午 ~230。
 */

import { useMemo, useRef } from 'react';
import { useFrame } from '@react-three/fiber';
import * as THREE from 'three';

/** 每帧写入的天空参数（全部线性空间颜色可直接喂 THREE.Color）。 */
export interface DayNightSkyState {
  /** 天顶色。 */
  top: THREE.Color;
  /** 地平线色（= scene.fog.color 同源值）。 */
  horizon: THREE.Color;
  /** 太阳方向（归一化；低于地平线时 y<0）。 */
  sunDir: THREE.Vector3;
  /** 太阳光晕强度 0..1（日出日落最高，夜间 0）。 */
  sunGlow: number;
  /** 星光强度 0..1（夜间 1）。 */
  stars: number;
  /** 月亮方向（归一化；仅夜间有意义）。 */
  moonDir: THREE.Vector3;
  /** 月亮强度 0..1。 */
  moon: number;
}

export interface DayNightSkyProps {
  /** 每帧采样（返回值可复用调用方对象；调用方自己写入后本组件只读 uniform）。 */
  state: React.MutableRefObject<DayNightSkyState | null>;
  /** 球穹半径（世界单位；须 < camera.far）。 */
  radius?: number;
}

const VERT = /* glsl */ `
  varying vec3 vDir;
  void main() {
    vDir = position;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
  }
`;

const FRAG = /* glsl */ `
  uniform vec3 uTop;
  uniform vec3 uHorizon;
  uniform vec3 uSunDir;
  uniform vec3 uSunColor;
  uniform float uSunGlow;
  uniform float uStars;
  uniform vec3 uMoonDir;
  uniform float uMoon;
  varying vec3 vDir;

  float hash13(vec3 p) {
    p = fract(p * 0.1031);
    p += dot(p, p.yzx + 33.33);
    return fract((p.x + p.y) * p.z);
  }

  void main() {
    vec3 d = normalize(vDir);
    float h = d.y;

    // ── 双段色带：地平线 → 天顶（上段渐变）；地平线下微暗（地面反射近似）──
    float t = smoothstep(-0.02, 0.55, h);
    vec3 col = mix(uHorizon, uTop, t);
    col = mix(col, uHorizon * 0.5, smoothstep(0.0, -0.3, h));

    // ── 太阳光晕 + 日盘（y<0 自然熄灭）──
    if (uSunGlow > 0.001) {
      float sunAmt = max(dot(d, uSunDir), 0.0);
      col += uSunColor * pow(sunAmt, 8.0) * uSunGlow * 0.35;
      col += uSunColor * pow(sunAmt, 400.0) * uSunGlow * 1.4;
    }

    // ── 星空（hash 点阵，靠近天顶更密；无额外 draw call）──
    if (uStars > 0.001 && h > 0.0) {
      vec3 sp = floor(d * 180.0);
      float n = hash13(sp);
      float twinkle = 0.55 + 0.45 * hash13(sp + 1.7);
      float star = step(0.9972, n) * twinkle;
      col += vec3(0.85, 0.9, 1.0) * star * uStars * smoothstep(0.0, 0.18, h);
    }

    // ── 月盘 + 月晕 ──
    if (uMoon > 0.001) {
      float moonAmt = max(dot(d, uMoonDir), 0.0);
      float disc = smoothstep(0.99935, 0.99965, moonAmt);
      float glow = pow(moonAmt, 300.0) * 0.5 + pow(moonAmt, 28.0) * 0.08;
      col += vec3(0.88, 0.92, 1.0) * (disc * 1.3 + glow) * uMoon;
    }

    gl_FragColor = vec4(col, 1.0);
    #include <tonemapping_fragment>
    #include <colorspace_fragment>
  }
`;

/**
 * 天穹组件：只持有着色器与几何；逐帧参数由调用方 ref 写入（useFrame 里
 * 读 ref → 写 uniforms，零分配）。
 */
export function DayNightSky({ state, radius = 500 }: DayNightSkyProps) {
  const matRef = useRef<THREE.ShaderMaterial>(null);

  const uniforms = useMemo(
    () => ({
      uTop: { value: new THREE.Color('#4a7fc8') },
      uHorizon: { value: new THREE.Color('#aeb8c6') },
      uSunDir: { value: new THREE.Vector3(0, 1, 0) },
      uSunColor: { value: new THREE.Color('#fff2e0') },
      uSunGlow: { value: 1 },
      uStars: { value: 0 },
      uMoonDir: { value: new THREE.Vector3(0, 1, 0) },
      uMoon: { value: 0 },
    }),
    [],
  );

  useFrame(() => {
    const s = state.current;
    const m = matRef.current;
    if (!s || !m) return;
    m.uniforms.uTop.value.copy(s.top);
    m.uniforms.uHorizon.value.copy(s.horizon);
    m.uniforms.uSunDir.value.copy(s.sunDir);
    m.uniforms.uSunGlow.value = s.sunGlow;
    m.uniforms.uStars.value = s.stars;
    m.uniforms.uMoonDir.value.copy(s.moonDir);
    m.uniforms.uMoon.value = s.moon;
  });

  return (
    <mesh renderOrder={-10} frustumCulled={false}>
      <sphereGeometry args={[radius, 32, 16]} />
      <shaderMaterial
        ref={matRef}
        vertexShader={VERT}
        fragmentShader={FRAG}
        uniforms={uniforms}
        side={THREE.BackSide}
        depthWrite={false}
        fog={false}
      />
    </mesh>
  );
}

export default DayNightSky;
