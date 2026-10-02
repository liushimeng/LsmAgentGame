/**
 * TrafficSignals — 全城机动车红绿灯（批次 24 §6.2 起始；**批次 43 真实感重制**）。
 *
 * 批次 43 之前：纯程序化（立杆 + 灯箱 + 3 个纯色球灯泡），无遮光罩、无 LED 点阵。
 * 批次 43 改为 **GLB 优先 + 程序化 fallback**：
 *   - GLB `road/traffic_signal.glb`（3d_script/build_traffic_signal.py）含
 *     ⌀300 mm 灯盘 ×3（半筒遮光罩 + 每盘 12 颗 LED 环形点阵）、近黑灯箱、
 *     顶沿出檐檐口、法兰底座 + 4 膨胀螺栓；单灯头直立 0.35×5.50×0.53 m；
 *   - **相位驱动的逐色调光**（真实性核心）：真实信号灯同一时刻只点亮一色，
 *     另两色为熄灭暗态。GLB 三色 LED 分材质名 `LEDRed`/`LEDYellow`/`LEDGreen`，
 *     本组件按 `signalLitColor()` 的结果对三者分别调制 emissiveIntensity
 *     （亮 = base×1.0，熄灭 = base×0.06），**同材质共享 ⇒ 全城一次遍历即生效**；
 *   - 保留原程序化三色球 fallback（GLB 缺失 / `blenderModelsEnabled()=false`），
 *     其相位切换仍走原生 instancedMesh setColorAt（与 GLB 分支互斥，不双渲染）。
 *
 * 相位周期 16s：绿 6s → 黄 2s → 红 8s；A 组 0s 起，B 组 +8s 偏移
 * （布点见 StreetPropsLayer::trafficSignalsForCity §6.2）。
 *
 * ⚠️ 与旧版的接口差异：旧版用 drei `<Instances>` 把杆/灯箱/檐口各合 1 draw call
 *   （静态件 3 DC + 灯泡 3 DC = 6 DC）。GLB 分支下每座灯 7 个材质组，44 座 clone
 *   会爆 DC —— 故 GLB 分支改走 `GlbPairInstances` 实例化（与 RoadsideBins 同款
 *   范式，见 glbInstances.tsx），**全城 44 座 = 7 draw call**（材质组数，非座数）。
 */

import { useLayoutEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Instances, Instance } from '@react-three/drei';
import { modelUrl } from '@/assets/models';
import { blenderModelsEnabled, useSharedGLTF } from '@/engine3d';
import { collectGlbPairs, GlbInstances, type GlbRenderPair } from '../glbInstances';
import { u, sizeTargetFor } from '../cityScale';
import type { TrafficSignalSpot } from '../StreetPropsLayer';
import {
  useObjectInfoProps,
  instancedEventsRaycast,
} from '../objectInfo/useObjectInfoProps';

// ── 相位时序（§6.2：16s 周期 绿 6 → 黄 2 → 红 8；B 组 +8s）──────────
/** 相位周期（秒）。 */
const SIGNAL_CYCLE = 16;
/** 绿灯时长（秒）。 */
const GREEN_S = 6;
/** 黄灯时长（秒）。 */
const YELLOW_S = 2;
/** B 组相位偏移（秒）。 */
const PHASE_B_OFFSET = 8;

/** 灯色字面量（红上 / 黄中 / 绿下）。 */
export type SignalColor = 'red' | 'yellow' | 'green';

/**
 * 相位纯函数：某组灯在 tSec 时刻应亮的颜色。
 * 导出供单测锚定（绿 [0,6) / 黄 [6,8) / 红 [8,16)，B 组整体 +8s）。
 */
export function signalLitColor(phase: 'A' | 'B', tSec: number): SignalColor {
  const local = (tSec + (phase === 'B' ? PHASE_B_OFFSET : 0)) % SIGNAL_CYCLE;
  if (local < GREEN_S) return 'green';
  if (local < GREEN_S + YELLOW_S) return 'yellow';
  return 'red';
}

// ── 灯色（亮色 = §6.2 指定；暗色 = 亮色 15% 亮度）──────────────────
const ON_COLOR: Record<SignalColor, THREE.Color> = {
  red: new THREE.Color('#ff2d2d'),
  yellow: new THREE.Color('#ffc40f'),
  green: new THREE.Color('#2ecc71'),
};
const OFF_COLOR: Record<SignalColor, THREE.Color> = {
  red: ON_COLOR.red.clone().multiplyScalar(0.15),
  yellow: ON_COLOR.yellow.clone().multiplyScalar(0.15),
  green: ON_COLOR.green.clone().multiplyScalar(0.15),
};

/** GLB 尺寸/落地校验目标（dev 态）；表值 = REAL_DIMS_M.trafficSignal（米制）。 */
const SIGNAL_SIZE_TARGET = sizeTargetFor('trafficSignal', { label: 'road/traffic_signal' });

/**
 * GLB 三色 LED 的材质名键（3d_script/build_traffic_signal.py 写死 `Signal_LED{Color}_Mat`）。
 * 前端按这些键收集共享材质，逐色调制 emissiveIntensity。
 */
const LED_MAT_KEY: Record<SignalColor, string> = {
  red: 'LEDRed',
  yellow: 'LEDYellow',
  green: 'LEDGreen',
};
/** 熄灭态 = base × 0.06（灯盘罩体仍在，仅 LED 近乎不发光 —— 真实暗态观感）。 */
const LED_OFF_FACTOR = 0.06;

// ── 程序化 fallback 几何尺寸（米制经 u()；批次 30 P1-8 口径保留）──────
/** 立杆：⌀0.15 × 高 5.50（= 表值总高，灯头不再越出）。 */
const POLE_GEOM: [number, number, number, number] = [u(0.075), u(0.075), u(5.5), 8];
const POLE_Y = u(2.75);
/** 灯箱背板：宽 × 高 × 深（近黑箱体，挂杆前侧 local +z 偏移 FACE_OFF）。 */
const HOUSING_GEOM: [number, number, number] = [u(0.35), u(1.0), u(0.25)];
const HOUSING_Y = u(4.7);
/** 出檐小檐口（灯箱顶沿微前出）。 */
const VISOR_GEOM: [number, number, number] = [u(0.4), u(0.05), u(0.32)];
const VISOR_Y = u(5.26);
/** 灯箱/檐口 local +z 偏移（杆前侧，灯面朝 local +z）。 */
const FACE_OFF = u(0.2);
/** 灯泡半径 + 三灯竖排 y 偏移（红上 +0.35 / 黄中 0 / 绿下 −0.35，间隔 u(0.35)）。 */
const BULB_R = u(0.15);
const BULB_Y: Record<SignalColor, number> = {
  red: HOUSING_Y + u(0.35),
  yellow: HOUSING_Y,
  green: HOUSING_Y - u(0.35),
};
/** 灯泡 local +z 偏移（灯箱前面 u(0.125) 之外微凸）。 */
const BULB_Z_OFF = u(0.345);

/** 信号（x, z, rotation=灯面朝向）→ 任意 local +z 偏移件的落点世界坐标。 */
function facePoint(s: TrafficSignalSpot, zOff: number): [number, number] {
  return [s.x + Math.sin(s.rotation) * zOff, s.z + Math.cos(s.rotation) * zOff];
}

interface Props {
  /** 布点（StreetPropsLayer::trafficSignalsForCity 产出）。 */
  signals: TrafficSignalSpot[];
}

const BULB_ORDER: SignalColor[] = ['red', 'yellow', 'green'];

/**
 * 收集 GLB 三色 LED 共享材质（同名材质在 GLTFLoader 后是同一引用，全城共享 ⇒
 * 一次调制即全城生效）。返回 { color → {mat, base}[] }。
 */
function collectLedMaterials(root: THREE.Object3D | null): Record<SignalColor, Array<{ mat: THREE.MeshStandardMaterial; base: number }>> {
  const out: Record<SignalColor, Array<{ mat: THREE.MeshStandardMaterial; base: number }>> = {
    red: [],
    yellow: [],
    green: [],
  };
  if (!root) return out;
  root.traverse((o) => {
    const mesh = o as THREE.Mesh;
    if (!mesh.isMesh) return;
    const mats = Array.isArray(mesh.material) ? mesh.material : [mesh.material];
    for (const m of mats) {
      const std = m as THREE.MeshStandardMaterial;
      if (!std?.name || !('emissiveIntensity' in std)) continue;
      for (const color of BULB_ORDER) {
        if (std.name.includes(LED_MAT_KEY[color])) {
          out[color].push({ mat: std, base: std.emissiveIntensity });
          break;
        }
      }
    }
  });
  return out;
}

export function TrafficSignals({ signals }: Props) {
  // 批次 43：GLB 优先（`blenderModelsEnabled()` 覆盖 disable-blender-models 开关）。
  const glbUrl = blenderModelsEnabled() ? modelUrl('road', 'traffic_signal') : '';
  const { scene: glbScene } = useSharedGLTF(glbUrl, SIGNAL_SIZE_TARGET);
  const useGlb = Boolean(glbUrl && glbScene);

  // GLB 子树的 (geometry, material) 对（实例化用）+ 三色 LED 材质引用。
  const pairs = useMemo<GlbRenderPair[]>(() => (useGlb ? collectGlbPairs(glbScene) : []), [useGlb, glbScene]);
  const ledMats = useMemo(
    () => (useGlb ? collectLedMaterials(glbScene) : { red: [], yellow: [], green: [] }),
    [useGlb, glbScene],
  );
  /** GLB 是否真的带三色 LED（资产缺失时退回 fallback 观感）。 */
  const hasLed = ledMats.red.length + ledMats.yellow.length + ledMats.green.length > 0;

  // 实例世界矩阵（每座灯一个位姿；GLB 节点 identity ⇒ 直接用布点位姿）。
  const worldMatrices = useMemo(
    () =>
      signals.map((s) => {
        const m = new THREE.Matrix4();
        m.makeRotationY(s.rotation);
        m.setPosition(s.x, 0, s.z);
        return m;
      }),
    [signals],
  );

  // 批次 28 B2：信号灯信息交互（objectInfo）。
  const info = useObjectInfoProps('road.traffic-signal', { anchorY: 2.2 });

  // GLB 分支：逐色 emissive 随全局相位。与 fallback 的 setColorAt 走同一
  // `signalLitColor`，两分支共用相位语义。
  useFrame(({ clock }) => {
    if (!useGlb || !hasLed) return;
    const t = clock.elapsedTime;
    for (const color of BULB_ORDER) {
      const list = ledMats[color];
      if (!list.length) continue;
      // 该色本帧是否点亮：真实路口对角双灯同相位同色，故按 A 组主相位驱动 GLB
      // 共享材质（A/B 的差异体现在灯面朝向不同来车方向上）。
      const lit = signalLitColor('A', t) === color;
      const target = lit ? 1.0 : LED_OFF_FACTOR;
      for (const e of list) e.mat.emissiveIntensity = e.base * target;
    }
  });

  // 空（无主干道的极端地图）不渲染 —— 置于全部 hook 之后，防 Instances limit=0 崩溃。
  if (!signals.length) return null;

  // ── GLB 分支渲染：全城 instancedMesh（材质组数 = draw call，与座数无关）──
  if (useGlb && pairs.length) {
    return (
      <group>
        <GlbInstances
          pairs={pairs}
          worldMatrices={worldMatrices}
          castShadow={false}
          receiveShadow={false}
        />
      </group>
    );
  }

  // ── fallback 分支：drei <Instances> 静态件 3 DC + 灯泡 3 DC ─────────────
  return <ProceduralSignals signals={signals} info={info} />;
}

/**
 * 程序化 fallback（GLB 缺失 / blenderModelsEnabled()=false 时的降级链）。
 * 相位切换走原生 instancedMesh setColorAt —— 与 GLB 分支互斥，**不双渲染**。
 */
function ProceduralSignals({ signals, info }: { signals: TrafficSignalSpot[]; info: ReturnType<typeof useObjectInfoProps> }) {
  // 三色灯泡原生 instancedMesh refs（固定 3 个 hook，顺序稳定）。
  const bulbRefs = [
    useRef<THREE.InstancedMesh>(null),
    useRef<THREE.InstancedMesh>(null),
    useRef<THREE.InstancedMesh>(null),
  ];
  /** 上一帧各信号亮色（null = 尚未初始化，强制全量刷新）。 */
  const lastLitRef = useRef<Array<SignalColor | null>>([]);

  // 灯泡矩阵只设一次（§6：矩阵 useLayoutEffect、颜色 useFrame）。
  useLayoutEffect(() => {
    const m = new THREE.Matrix4();
    const q = new THREE.Quaternion();
    const e = new THREE.Euler();
    const pos = new THREE.Vector3();
    const one = new THREE.Vector3(1, 1, 1);
    BULB_ORDER.forEach((color, ci) => {
      const mesh = bulbRefs[ci].current;
      if (!mesh) return;
      signals.forEach((s, i) => {
        const [x, z] = facePoint(s, BULB_Z_OFF);
        pos.set(x, BULB_Y[color], z);
        e.set(0, s.rotation, 0);
        q.setFromEuler(e);
        m.compose(pos, q, one);
        mesh.setMatrixAt(i, m);
        mesh.setColorAt(i, OFF_COLOR[color]); // 触发 instanceColor 建 buffer
      });
      mesh.instanceMatrix.needsUpdate = true;
      if (mesh.instanceColor) mesh.instanceColor.needsUpdate = true;
      mesh.computeBoundingSphere();
    });
    lastLitRef.current = signals.map(() => null);
  }, [signals, bulbRefs]);

  // 相位动画：只在本帧状态变化的实例上写色（全城 ≤ ~100 实例 ×3，代价可忽略）。
  useFrame(({ clock }) => {
    const t = clock.elapsedTime;
    let dirty = false;
    signals.forEach((s, i) => {
      const lit = signalLitColor(s.phase, t);
      if (lastLitRef.current[i] === lit) return;
      lastLitRef.current[i] = lit;
      dirty = true;
      BULB_ORDER.forEach((color, ci) => {
        const mesh = bulbRefs[ci].current;
        if (!mesh) return;
        mesh.setColorAt(i, lit === color ? ON_COLOR[color] : OFF_COLOR[color]);
      });
    });
    if (dirty) {
      bulbRefs.forEach(({ current }) => {
        if (current?.instanceColor) current.instanceColor.needsUpdate = true;
      });
    }
  });

  /** 静态件通用渲染（三段 = 3 draw call；Instance 世界位姿在布点层算好）。 */
  const renderPart = (
    key: string,
    geometry: JSX.Element,
    material: JSX.Element,
    items: Array<{ key: string; pos: [number, number, number]; rot: number }>,
  ) => (
    <Instances
      {...info}
      raycast={instancedEventsRaycast}
      key={key}
      limit={items.length}
      range={items.length}
    >
      {geometry}
      {material}
      {items.map((it) => (
        <Instance key={it.key} position={it.pos} rotation={[0, it.rot, 0]} />
      ))}
    </Instances>
  );

  const poles = signals.map((s, i) => ({
    key: `sig-pole-${i}`,
    pos: [s.x, POLE_Y, s.z] as [number, number, number],
    rot: s.rotation,
  }));
  const faceItems = signals.map((s, i) => {
    const [hx, hz] = facePoint(s, FACE_OFF);
    return { s, i, hx, hz };
  });
  const housings = faceItems.map(({ i, hx, hz, s }) => ({
    key: `sig-housing-${i}`,
    pos: [hx, HOUSING_Y, hz] as [number, number, number],
    rot: s.rotation,
  }));
  const visors = faceItems.map(({ i, hx, hz, s }) => ({
    key: `sig-visor-${i}`,
    pos: [hx, VISOR_Y, hz] as [number, number, number],
    rot: s.rotation,
  }));

  return (
    <group>
      {/* ① 立杆 ×N → 1 draw call */}
      {renderPart(
        'sig-pole',
        <cylinderGeometry args={POLE_GEOM} />,
        <meshStandardMaterial color="#3a3f46" roughness={0.6} metalness={0.4} />,
        poles,
      )}
      {/* ② 灯箱背板 ×N → 1 draw call（近黑箱体） */}
      {renderPart(
        'sig-housing',
        <boxGeometry args={HOUSING_GEOM} />,
        <meshStandardMaterial color="#17191d" roughness={0.7} metalness={0.2} />,
        housings,
      )}
      {/* ③ 出檐檐口 ×N → 1 draw call */}
      {renderPart(
        'sig-visor',
        <boxGeometry args={VISOR_GEOM} />,
        <meshStandardMaterial color="#101216" roughness={0.75} metalness={0.15} />,
        visors,
      )}
      {/* ④ 三色灯泡 ×3 instancedMesh → 3 draw call（toneMapped=false 保饱和色） */}
      {BULB_ORDER.map((color, ci) => (
        <instancedMesh
          {...info}
          key={`sig-bulb-${color}`}
          ref={bulbRefs[ci]}
          args={[undefined, undefined, signals.length]}
        >
          <sphereGeometry args={[BULB_R, 10, 8]} />
          <meshBasicMaterial toneMapped={false} />
        </instancedMesh>
      ))}
    </group>
  );
}
