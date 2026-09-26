/**
 * TrafficSignals — 全城红绿灯（批次 24 §6.2，取代 props/TrafficLight.tsx 与
 * civic/IntersectionSignals.tsx 的渲染职责）：
 *
 *   - 静态件（杆 / 灯箱背板 / 出檐檐口）走 drei <Instances> ×3 → 3 draw call；
 *   - 三色灯泡走原生 instancedMesh ×3（meshBasicMaterial toneMapped={false}，
 *     亮色 #ff2d2d/#ffc40f/#2ecc71、暗色 = 亮色 15% 亮度）→ 3 draw call；
 *     灯泡矩阵只在 useLayoutEffect 设置一次，颜色在 useFrame 按全局相位纯函数
 *     切换（无 React state，仅状态变化帧写 setColorAt + needsUpdate）。
 *   - 相位周期 16s：绿 6s → 黄 2s → 红 8s；A 组 0s 起，B 组 +8s 偏移
 *     （布点见 StreetPropsLayer::trafficSignalsForCity §6.2）。
 *
 * 竖式三色灯头：红上 / 黄中 / 绿下，灯泡半径 u(0.28)，灯头中心高 ≈ u(5.5)。
 * 本组件纯程序化几何，不依赖 GLB —— blenderModelsEnabled()=false 时仍渲染
 * （相位色切换是交通语义状态而非装饰运动，不受 prefers-reduced-motion 门控）。
 */

import { useLayoutEffect, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Instances, Instance } from '@react-three/drei';
import { u } from '../cityScale';
import type { TrafficSignalSpot } from '../StreetPropsLayer';

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

// ── 几何尺寸（米制经 u()；灯头中心高 ≈ u(5.5)）─────────────────────
/** 立杆：半径 / 高。 */
const POLE_GEOM: [number, number, number, number] = [u(0.14), u(0.14), u(7.0), 8];
const POLE_Y = u(3.5);
/** 灯箱背板：宽 × 高 × 深（近黑箱体，挂杆前侧 local +z 偏移 FACE_OFF）。 */
const HOUSING_GEOM: [number, number, number] = [u(1.0), u(2.6), u(0.5)];
const HOUSING_Y = u(5.5);
/** 出檐小檐口（灯箱顶沿微前出）。 */
const VISOR_GEOM: [number, number, number] = [u(1.1), u(0.1), u(0.7)];
const VISOR_Y = u(6.85);
/** 灯箱/檐口 local +z 偏移（杆前侧，灯面朝 local +z）。 */
const FACE_OFF = u(0.28);
/** 灯泡半径 + 三灯竖排 y 偏移（红上 +0.8 / 黄中 0 / 绿下 −0.8，间隔 u(0.8)）。 */
const BULB_R = u(0.28);
const BULB_Y: Record<SignalColor, number> = {
  red: HOUSING_Y + u(0.8),
  yellow: HOUSING_Y,
  green: HOUSING_Y - u(0.8),
};
/** 灯泡 local +z 偏移（灯箱前面 u(0.53) 之外微凸）。 */
const BULB_Z_OFF = u(0.59);

/** 信号（x, z, rotation=灯面朝向）→ 任意 local +z 偏移件的落点世界坐标。 */
function facePoint(s: TrafficSignalSpot, zOff: number): [number, number] {
  return [s.x + Math.sin(s.rotation) * zOff, s.z + Math.cos(s.rotation) * zOff];
}

interface Props {
  /** 布点（StreetPropsLayer::trafficSignalsForCity 产出）。 */
  signals: TrafficSignalSpot[];
}

const BULB_ORDER: SignalColor[] = ['red', 'yellow', 'green'];

export function TrafficSignals({ signals }: Props) {
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
        mesh.setColorAt(i, OFF_COLOR[color]); // 触发 instanceColor 建buffer
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
    <Instances key={key} limit={items.length} range={items.length}>
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

  // 空（无主干道的极端地图）不渲染 —— 置于全部 hook 之后，防 Instances limit=0 崩溃。
  if (!signals.length) return null;

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
