/**
 * StreetLightsInstanced — 批次 20 渲染性能专项（文档 1 §3.3）：
 *
 * 全城主干/次干道路灯合并为 4 个 drei <Instances>（底座 / 主杆 / 灯臂 / 灯头）
 * → 全部路灯总计 **4 个 draw call** + 夜间发光层。
 *
 * 批次 30 P0-2 重修（「杆林 + 奶白方块」真因）：
 *   - 横截面原为**裸字面量世界单位**（基座 ⌀2.0 m / 杆 ⌀0.7~1.0 m / 灯头 1.6 m 立方）
 *     ⇒ 全部改走米制 u()：杆 ⌀0.16~0.24 m、基座 ⌀0.44~0.56 m、灯头 0.6×0.3×0.15 m；
 *   - 灯头由奶白方块改为「灯臂 + 长条灯头」（悬臂朝向路面，随所在侧翻转）；
 *   - 总高唯一事实来源 = cityScale.REAL_DIMS_M.streetLight（12.00 m）/
 *     streetLightSide（7.00 m）；**props/StreetLight.tsx 已删除**（零引用死文件，
 *     且自带另一套 0.08/0.12 口径与本文件差 40%）——本文件是路灯唯一渲染源。
 *
 * 批次 27 §4.3：灯头 emissiveIntensity 由 useFrame 读 cityTimeStore 的
 * **dayFactor01** 联动（昼 0.15 → 夜 2.2 平滑渐变，~0.5s 时间常数）。
 * 批次 30 B2：夜间地面暖色光斑（radial 贴图面片，additive，1 draw call）。
 *
 * 批次 34「路灯夜间发亮」：
 *   - 灯头 emissive 夜间上限 2.2 → **3.4**，色温偏暖（#ffd98a），白天回落到 0.12
 *     （旧 0.15 在正午仍有一点自发光，看成"白盒子"）；
 *   - 新增 ⑥ **灯头辉光**（THREE.Points + radial 贴图，additive，永远朝向相机）
 *     —— 这是"灯在发光"最直接的视觉信号：光源本体有一团可见的暖晕；
 *   - 新增 ⑦ **光锥**（开口圆台 additive 面片，自灯头向下张开，纵向渐隐贴图）
 *     —— 夜间空气中的光柱，把地面光斑与灯头连起来，读作"被照亮"而非"贴了张贴纸"；
 *   - ⑤ 地面光斑夜间上限 0.55 → 0.72，直径 8 m → 10 m（真实路灯地面光池约 2~3 倍杆高）。
 *
 * 发光层成本：辉光 Points = **1 draw call**（全部灯头合批）；光锥 Instances = 1 draw call。
 * 合计 7 DC / 全城 261 盏灯，仍在批次 28 的 DC 预算内（路灯属 540 DC 街具桶）。
 *
 * ⚠️ 为什么不做「真实点光源」：261 盏 PointLight 会立刻击穿光照预算（three 前向渲染
 * 每光源每材质都要过一遍），且 shadow 不可用；additive 伪光（辉光 + 光锥 + 光斑）
 * 是本项目 30 批次已验证的等效做法（窗灯 / 车灯同款）。
 */

import { useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Instances, Instance } from '@react-three/drei';
import { getDayNight } from '../cityTimeStore';
import { u, worldDims, ROAD_SURFACE_Y } from '../cityScale';
import {
  useObjectInfoProps,
  instancedEventsRaycast,
} from '../objectInfo/useObjectInfoProps';

export interface InstancedLamp {
  x: number;
  z: number;
  /** 朝向（弧度，绕 Y；随所在道路方向，local +Z = 路向）。 */
  rot: number;
  kind: 'main' | 'side';
  /** 批次 30 A4：所在侧（+1/−1，路向法线）；灯臂朝路面（−side）悬挑。 */
  side: 1 | -1;
}

/**
 * 灯具分段尺寸（米制经 u()；批次 30 P0-2）：
 *   基座 ⌀0.56/0.44 × H0.30 / 主杆 ⌀0.24→0.16（真实灯杆 ⌀0.15~0.30）/
 *   灯臂 1.2 m 悬挑 / 灯头 0.6×0.15×0.30（长条朝路面）。
 */
const BASE_H = u(0.3);
const HEAD_H = u(0.15);
const MAIN_TOTAL = worldDims('streetLight').y;
const POLE_H = MAIN_TOTAL - BASE_H - HEAD_H;
const BASE_GEOM: [number, number, number, number] = [u(0.22), u(0.28), BASE_H, 8];
const POLE_GEOM: [number, number, number, number] = [u(0.08), u(0.12), POLE_H, 6];
/** 灯臂（盒体沿 local +X 悬挑）：长 1.2 / 截面 0.08×0.10。 */
const ARM_GEOM: [number, number, number] = [u(1.2), u(0.1), u(0.08)];
const HEAD_GEOM: [number, number, number] = [u(0.6), HEAD_H, u(0.3)];
const BASE_Y = BASE_H / 2;
const POLE_Y = BASE_H + POLE_H / 2;
/** 灯臂中心：杆顶下 0.2 m（灯头底与臂顶贴合）。 */
const ARM_Y = BASE_H + POLE_H - u(0.2);
const HEAD_Y = BASE_H + POLE_H + HEAD_H / 2;
/** 次干道整体缩放（表值总高 side / main；横截面两档同源，不再各写一套）。 */
const SIDE_SCALE = worldDims('streetLightSide').y / MAIN_TOTAL;
/** 灯臂悬挑半长（世界单位；灯头中心 x = 臂半长，随 side 翻转）。 */
const ARM_HALF = u(0.6);
/** 夜间地面光斑直径（批次 34：8 m → 10 m，真实地面光池约 2~3 倍杆高）。 */
const POOL_SIZE = u(10);
/** 批次 34 ⑥ 灯头辉光面径（世界单位；视觉上约为灯头 3 倍）。 */
const HALO_SIZE = u(1.8);
/** 批次 34 ⑦ 光锥：顶半径（= 灯头半宽）/ 底半径 / 高（= 灯总高）。 */
const CONE_TOP_R = u(0.28);
const CONE_BOT_R = u(3.0);

/** 夜间光斑 radial 贴图（CanvasTexture，模块级一次性；additive 面片用）。 */
function makePoolTexture(): THREE.CanvasTexture {
  const size = 64;
  const canvas = document.createElement('canvas');
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext('2d');
  if (ctx) {
    const g = ctx.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
    g.addColorStop(0, 'rgba(255,228,170,1)');
    g.addColorStop(0.45, 'rgba(255,210,140,0.35)');
    g.addColorStop(1, 'rgba(255,200,120,0)');
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, size, size);
  }
  const tex = new THREE.CanvasTexture(canvas);
  tex.colorSpace = THREE.SRGBColorSpace;
  return tex;
}

/**
 * 批次 34 ⑥ 灯头辉光贴图：径向高斯衰减，中心近乎实心、外缘完全透明。
 * 比 pool 贴图更「聚」（alpha 衰减更快），否则 261 个光球叠在一起会糊成一片。
 */
function makeHaloTexture(): THREE.CanvasTexture {
  const size = 64;
  const canvas = document.createElement('canvas');
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext('2d');
  if (ctx) {
    const g = ctx.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
    g.addColorStop(0, 'rgba(255,246,214,1)');
    g.addColorStop(0.18, 'rgba(255,232,178,0.62)');
    g.addColorStop(0.45, 'rgba(255,214,140,0.22)');
    g.addColorStop(1, 'rgba(255,200,120,0)');
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, size, size);
  }
  const tex = new THREE.CanvasTexture(canvas);
  tex.colorSpace = THREE.SRGBColorSpace;
  return tex;
}

/**
 * 批次 34 ⑦ 光锥渐隐贴图（沿 V 方向）：顶部（灯头处）最亮，向下线性衰减到 0。
 * 圆台 UV 的 V 轴自顶向底 ⇒ 用线性渐变即可，无需逐顶点色。
 */
function makeConeTexture(): THREE.CanvasTexture {
  const size = 64;
  const canvas = document.createElement('canvas');
  canvas.width = 1;
  canvas.height = size;
  const ctx = canvas.getContext('2d');
  if (ctx) {
    // canvas y 向下 = V 向下（glTF 默认）；顶亮底灭
    const g = ctx.createLinearGradient(0, 0, 0, size);
    g.addColorStop(0, 'rgba(255,226,176,0.95)');
    g.addColorStop(0.35, 'rgba(255,214,150,0.32)');
    g.addColorStop(1, 'rgba(255,200,120,0)');
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, 1, size);
  }
  const tex = new THREE.CanvasTexture(canvas);
  tex.colorSpace = THREE.SRGBColorSpace;
  return tex;
}

export function StreetLightsInstanced({ lamps }: { lamps: InstancedLamp[] }) {
  // 批次 27：灯头材质 ref（emissiveIntensity 逐帧随 dayFactor01 渐变）。
  const headMatRef = useRef<THREE.MeshStandardMaterial>(null);
  // 批次 30 B2：夜间地面光斑材质 ref（opacity 逐帧随 dayFactor01 渐变）。
  const poolMatRef = useRef<THREE.MeshBasicMaterial>(null);
  // 批次 34：灯头辉光 / 光锥 材质 ref（同源 dayFactor01，见文件头「四层共用同一 k」）。
  const haloMatRef = useRef<THREE.PointsMaterial>(null);
  const coneMatRef = useRef<THREE.MeshBasicMaterial>(null);

  useFrame((_state, delta) => {
    const day = getDayNight()?.dayFactor01 ?? 1;
    // 四层共用同一时间常数 ⇒ 辉光/光锥/光斑/自发光 同相位渐变，不出现层次错开。
    const k = Math.min(1, delta * 2);
    // 夜间强度因子：day=1 → 0，day=0 → 1（晨昏 0.5 时约 0.5，平滑过渡）。
    const night = 1 - day;

    const m = headMatRef.current;
    if (m) {
      // 昼 0.12 → 夜 3.4（批次 34 提亮：旧 2.2 在深夜里仍偏暗，看成"灰盒子"）。
      const target = 0.12 + (3.4 - 0.12) * night;
      m.emissiveIntensity += (target - m.emissiveIntensity) * k;
    }
    const h = haloMatRef.current;
    if (h) {
      // 辉光只在夜间出现（day 0.5 → 0 渐入）。
      const target = Math.max(0, 1 - day * 2) * 0.85;
      h.opacity += (target - h.opacity) * k;
    }
    const c = coneMatRef.current;
    if (c) {
      // 光锥更克制：夜里 0.16，否则 261 根光柱会把夜空洗白。
      const target = Math.max(0, 1 - day * 2) * 0.16;
      c.opacity += (target - c.opacity) * k;
    }
    const p = poolMatRef.current;
    if (p) {
      // 光斑只在夜间出现（day 0.5 → 0 渐入）。
      const target = Math.max(0, 1 - day * 2) * 0.72;
      p.opacity += (target - p.opacity) * k;
    }
  });

  const poolTexture = useMemo(() => makePoolTexture(), []);
  const haloTexture = useMemo(() => makeHaloTexture(), []);
  const coneTexture = useMemo(() => makeConeTexture(), []);

  /**
   * 批次 34 ⑥ 辉光点云：全部灯头合批为 **1 个 draw call**。
   * 位置与 renderPart 的灯头分支严格同式（x = 灯头悬挑位、y = HEAD_Y×sideScale），
   * 否则辉光会和灯头错位。
   */
  const haloGeo = useMemo(() => {
    const n = Math.max(1, lamps.length);
    const arr = new Float32Array(n * 3);
    for (let i = 0; i < lamps.length; i++) {
      const l = lamps[i];
      const s = l.kind === 'side' ? SIDE_SCALE : 1;
      arr[i * 3 + 0] = l.x + -l.side * ARM_HALF * 2 * s;
      arr[i * 3 + 1] = HEAD_Y * s;
      arr[i * 3 + 2] = l.z;
    }
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.BufferAttribute(arr, 3));
    return g;
  }, [lamps]);
  useEffect(() => () => haloGeo.dispose(), [haloGeo]);
  useEffect(() => () => poolTexture.dispose(), [poolTexture]);
  useEffect(() => () => haloTexture.dispose(), [haloTexture]);
  useEffect(() => () => coneTexture.dispose(), [coneTexture]);

  // 批次 28 B2：路灯信息交互（分段实例层共享 lamps 序 → instanceId 一致）。
  const info = useObjectInfoProps('road.street-light', { anchorY: 1.2 });

  const renderPart = (
    key: string,
    geometry: JSX.Element,
    material: JSX.Element,
    list: InstancedLamp[],
    yOf: (s: number) => number,
    xOf?: (l: InstancedLamp) => number,
  ) => (
    <Instances
      {...info}
      raycast={instancedEventsRaycast}
      key={key}
      limit={Math.max(1, list.length)}
      range={list.length}
    >
      {geometry}
      {material}
      {list.map((l, i) => {
        const s = l.kind === 'side' ? SIDE_SCALE : 1;
        return (
          <Instance
            key={`${key}-${i}`}
            position={[l.x + (xOf ? xOf(l) * s : 0), yOf(s), l.z]}
            rotation={[0, l.rot, 0]}
            scale={s}
          />
        );
      })}
    </Instances>
  );

  return (
    <group>
      {/* ① 底座 ×N → 1 draw call */}
      {renderPart(
        'lamp-base',
        <cylinderGeometry args={BASE_GEOM} />,
        <meshStandardMaterial color="#4a4f5a" roughness={0.7} metalness={0.4} />,
        lamps,
        (s) => BASE_Y * s,
      )}
      {/* ② 主杆 ×N → 1 draw call */}
      {renderPart(
        'lamp-pole',
        <cylinderGeometry args={POLE_GEOM} />,
        <meshStandardMaterial color="#6b7280" roughness={0.55} metalness={0.6} />,
        lamps,
        (s) => POLE_Y * s,
      )}
      {/* ③ 灯臂 ×N → 1 draw call（悬挑朝路面：local +X × −side） */}
      {renderPart(
        'lamp-arm',
        <boxGeometry args={ARM_GEOM} />,
        <meshStandardMaterial color="#6b7280" roughness={0.55} metalness={0.6} />,
        lamps,
        (s) => ARM_Y * s,
        (l) => -l.side * ARM_HALF,
      )}
      {/* ④ 灯头 ×N → 1 draw call（emissive 暖光随昼夜：昼 0.12 → 夜 3.4，批次 34） */}
      {renderPart(
        'lamp-head',
        <boxGeometry args={HEAD_GEOM} />,
        <meshStandardMaterial
          ref={headMatRef}
          color="#8a8d92"
          emissive="#ffd98a"
          emissiveIntensity={0.4}
          roughness={0.4}
        />,
        lamps,
        (s) => HEAD_Y * s,
        (l) => -l.side * ARM_HALF * 2,
      )}
      {/* ⑤ 夜间地面暖光斑 ×N → 1 draw call（批次 30 B2；additive，opacity 随昼夜）
          raycast=null：纯视觉层，不参与物件信息点选（否则 10 m 面片会抢 hover） */}
      <Instances
        key="lamp-pool"
        limit={Math.max(1, lamps.length)}
        range={lamps.length}
        castShadow={false}
        receiveShadow={false}
        raycast={() => null}
      >
        <planeGeometry args={[POOL_SIZE, POOL_SIZE]} />
        <meshBasicMaterial
          ref={poolMatRef}
          map={poolTexture}
          transparent
          opacity={0}
          depthWrite={false}
          blending={THREE.AdditiveBlending}
        />
        {lamps.map((l, i) => (
          <Instance
            key={`lamp-pool-${i}`}
            position={[l.x, ROAD_SURFACE_Y + 0.002, l.z]}
            rotation={[-Math.PI / 2, 0, 0]}
          />
        ))}
      </Instances>
      {/* ⑥ 灯头辉光 ×全部灯头 → **1 draw call**（批次 34；Points 天然朝向相机） */}
      <points key="lamp-halo" geometry={haloGeo} raycast={() => null} frustumCulled={false}>
        <pointsMaterial
          ref={haloMatRef}
          map={haloTexture}
          size={HALO_SIZE}
          sizeAttenuation
          transparent
          opacity={0}
          depthWrite={false}
          blending={THREE.AdditiveBlending}
          toneMapped={false}
        />
      </points>
      {/* ⑦ 光锥 ×N → 1 draw call（批次 34；开口圆台，自灯头向下张开，纵向渐隐） */}
      <Instances
        key="lamp-cone"
        limit={Math.max(1, lamps.length)}
        range={lamps.length}
        castShadow={false}
        receiveShadow={false}
        raycast={() => null}
      >
        {/* 开口圆台：rTop=灯头半宽、rBot=地面光池半径、高=灯总高（经 scale 随 side 缩放） */}
        <cylinderGeometry args={[CONE_TOP_R, CONE_BOT_R, MAIN_TOTAL, 14, 1, true]} />
        <meshBasicMaterial
          ref={coneMatRef}
          map={coneTexture}
          transparent
          opacity={0}
          depthWrite={false}
          side={THREE.DoubleSide}
          blending={THREE.AdditiveBlending}
          toneMapped={false}
        />
        {lamps.map((l, i) => {
          const s = l.kind === 'side' ? SIDE_SCALE : 1;
          return (
            <Instance
              key={`lamp-cone-${i}`}
              position={[l.x + -l.side * ARM_HALF * 2 * s, (MAIN_TOTAL / 2) * s, l.z]}
              rotation={[0, l.rot, 0]}
              scale={s}
            />
          );
        })}
      </Instances>
    </group>
  );
}
