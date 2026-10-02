/**
 * PedestrianSignals — 全城行人过街信号灯（**批次 43 新增**）。
 *
 * 真实语义（GB 14886）：行人信号灯与机动车信号灯**反相联动** ——
 *   · 机动车绿灯 ⇒ 行人**红人**亮（禁止通行，车辆优先）；
 *   · 机动车红灯 ⇒ 行人**绿人**亮（允许通行）；
 *   · 机动车黄灯 ⇒ 行人保持红人（清空路口的过渡态，行人不动）。
 * 本组件直接复用 `signalLitColor()` 求机动车相位，再取**反色**驱动行人灯，
 * 保证两者**永不同时放行**（这是交通安全的核心，也是旧实现完全没有的语义）。
 *
 * 渲染：GLB `road/pedestrian_signal.glb` 优先（方盘灯箱 + U 形遮光罩 + 红人/绿人
 * 剪影 + 倒计时屏 + 侧挂黄色过街按钮盒），材质名 `LEDRed`/`LEDGreen` 分组逐色调制；
 * 缺失时回退程序化几何（立杆 + 方灯箱 + 红/绿人面片 + 倒计时条）。
 *
 * 布点见 `StreetPropsLayer::pedestrianSignalsForCity`（路口四角，面向人行道）。
 * 本组件只负责渲染，不含布点逻辑。
 */

import { useLayoutEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { modelUrl } from '@/assets/models';
import { blenderModelsEnabled, useSharedGLTF } from '@/engine3d';
import { collectGlbPairs, GlbInstances, type GlbRenderPair } from '../glbInstances';
import { u, sizeTargetFor } from '../cityScale';
import { signalLitColor } from './TrafficSignals';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

/** GLB 尺寸/落地校验目标（dev 态）；表值 = REAL_DIMS_M.pedestrianSignal（米制）。 */
const PED_SIGNAL_SIZE_TARGET = sizeTargetFor('pedestrianSignal', {
  label: 'road/pedestrian_signal',
});

/** GLB 双色 LED 材质名键（与 build_pedestrian_signal.py 的 `PedSignal_LED{Color}_Mat` 对齐）。 */
const PED_LED_MAT_KEY: Record<'red' | 'green', string> = {
  red: 'LEDRed',
  green: 'LEDGreen',
};
/** 熄灭态 = base × 0.06（与机动车灯同口径：灯盘罩体仍在，仅 LED 近乎不发光）。 */
const LED_OFF_FACTOR = 0.06;

/**
 * 行人灯相位：**机动车相位的反色**。
 * 绿 → 红人（车辆放行，行人止步）；红 → 绿人（可通行）；黄 → 红人（清空过渡）。
 * 导出供单测锚定 —— 「行人红 ⇔ 车绿」是本函数存在的唯一理由。
 */
export function pedestrianLitColor(vehiclePhase: 'A' | 'B', tSec: number): 'red' | 'green' {
  const v = signalLitColor(vehiclePhase, tSec);
  if (v === 'red') return 'green';
  return 'red'; // 车绿 / 车黄 ⇒ 行人红（黄灯期间不与车辆同时变更）
}

/** 行人灯布点规格（与机动车灯同构，但面向人行道）。 */
export interface PedestrianSignalSpot {
  x: number;
  z: number;
  /** 绕 Y 旋转（弧度）：local +Z = 灯面朝向（面向等待过街的人行道侧）。 */
  rotation: number;
  /** 相位组：与对向机动车灯同组（本路口的 A/B）。 */
  phase: 'A' | 'B';
}

// ── 程序化 fallback 几何（米制经 u()；GLB 缺失时的最低保真观感）─────────
/** 立杆：⌀0.08 × 高 2.50。 */
const POLE_GEOM: [number, number, number, number] = [u(0.04), u(0.04), u(2.5), 8];
const POLE_Y = u(1.25);
/** 方盘灯箱：0.30 宽 × 0.16 深 × 0.30 高，中心高 2.05。 */
const HEAD_GEOM: [number, number, number] = [u(0.3), u(0.16), u(0.3)];
const HEAD_Y = u(2.05);
/** 灯面 local +Z 偏移（灯箱前面之外）。 */
const FACE_OFF = u(0.09);
/** 红人 / 绿人面片：人形剪影的极简近似（0.13 宽 × 0.20 高）。 */
const FIG_GEOM: [number, number, number] = [u(0.13), u(0.01), u(0.2)];
/** 倒计时条（灯箱下方）：0.20 × 0.01 × 0.07。 */
const CD_GEOM: [number, number, number] = [u(0.2), u(0.01), u(0.07)];
const CD_Y = u(1.85);
/** instanceColor 初始化色（仅用于触发 instanceColor buffer 建立）。 */
const BLACK = new THREE.Color(0x000000);

/** 收集 GLB 双色 LED 共享材质（同名材质全城共享 ⇒ 一次调制即全城生效）。 */
function collectPedLedMaterials(
  root: THREE.Object3D | null,
): Record<'red' | 'green', Array<{ mat: THREE.MeshStandardMaterial; base: number }>> {
  const out: Record<'red' | 'green', Array<{ mat: THREE.MeshStandardMaterial; base: number }>> = {
    red: [],
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
      for (const color of ['red', 'green'] as const) {
        if (std.name.includes(PED_LED_MAT_KEY[color])) {
          out[color].push({ mat: std, base: std.emissiveIntensity });
          break;
        }
      }
    }
  });
  return out;
}

export function PedestrianSignals({ signals }: { signals: PedestrianSignalSpot[] }) {
  const glbUrl = blenderModelsEnabled() ? modelUrl('road', 'pedestrian_signal') : '';
  const { scene: glbScene } = useSharedGLTF(glbUrl, PED_SIGNAL_SIZE_TARGET);
  const useGlb = Boolean(glbUrl && glbScene);

  const pairs = useMemo<GlbRenderPair[]>(() => (useGlb ? collectGlbPairs(glbScene) : []), [useGlb, glbScene]);
  const ledMats = useMemo(
    () => (useGlb ? collectPedLedMaterials(glbScene) : { red: [], green: [] }),
    [useGlb, glbScene],
  );
  const hasLed = ledMats.red.length + ledMats.green.length > 0;

  // 实例世界矩阵（GLB 节点 identity ⇒ 直接用布点位姿）。
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

  // 相位反色驱动：绿灯禁行 / 红灯放行。
  useFrame(({ clock }) => {
    if (!useGlb || !hasLed) return;
    const t = clock.elapsedTime;
    const lit = pedestrianLitColor('A', t);
    for (const color of ['red', 'green'] as const) {
      const list = ledMats[color];
      if (!list.length) continue;
      const target = lit === color ? 1.0 : LED_OFF_FACTOR;
      for (const e of list) e.mat.emissiveIntensity = e.base * target;
    }
  });

  const info = useObjectInfoProps('road.pedestrian-signal', { anchorY: 1.6 });

  // 空点位不渲染 —— 置于全部 hook 之后（§React hook-after-return）。
  if (!signals.length) return null;

  if (useGlb && pairs.length) {
    return <GlbInstances pairs={pairs} worldMatrices={worldMatrices} castShadow={false} receiveShadow={false} />;
  }

  return <ProceduralPedSignals signals={signals} info={info} />;
}

/** 程序化 fallback（GLB 缺失 / blenderModelsEnabled()=false 时的降级链）。 */
function ProceduralPedSignals({
  signals,
  info,
}: {
  signals: PedestrianSignalSpot[];
  info: ReturnType<typeof useObjectInfoProps>;
}) {
  const redRef = useRef<THREE.InstancedMesh>(null);
  const greenRef = useRef<THREE.InstancedMesh>(null);
  /** 上一帧的亮色（null = 未初始化）。 */
  const lastLit = useRef<'red' | 'green' | null>(null);

  // 静态件矩阵只写一次（§TrafficSignals 同款：矩阵 useLayoutEffect、颜色 useFrame）。
  useLayoutEffect(() => {
    const m = new THREE.Matrix4();
    const q = new THREE.Quaternion();
    const e = new THREE.Euler();
    const pos = new THREE.Vector3();
    const one = new THREE.Vector3(1, 1, 1);
    for (const ref of [redRef, greenRef]) {
      const mesh = ref.current;
      if (!mesh) continue;
      signals.forEach((s, i) => {
        // 人形面片贴在灯箱前面（local +Z），与灯面同向。
        pos.set(s.x + Math.sin(s.rotation) * FACE_OFF, HEAD_Y, s.z + Math.cos(s.rotation) * FACE_OFF);
        e.set(0, s.rotation, 0);
        q.setFromEuler(e);
        m.compose(pos, q, one);
        mesh.setMatrixAt(i, m);
        mesh.setColorAt(i, BLACK); // 触发 instanceColor 建 buffer
      });
      mesh.instanceMatrix.needsUpdate = true;
      if (mesh.instanceColor) mesh.instanceColor.needsUpdate = true;
      mesh.computeBoundingSphere();
    }
  }, [signals]);

  // 相位反色：全城同相位 ⇒ 只在状态变化帧写色。
  useFrame(({ clock }) => {
    const lit = pedestrianLitColor('A', clock.elapsedTime);
    if (lastLit.current === lit) return;
    lastLit.current = lit;
    const on = new THREE.Color(lit === 'red' ? '#ff2d2d' : '#2ecc71');
    const off = on.clone().multiplyScalar(0.12);
    const red = redRef.current;
    const green = greenRef.current;
    if (red) {
      for (let i = 0; i < signals.length; i++) red.setColorAt(i, lit === 'red' ? on : off);
      if (red.instanceColor) red.instanceColor.needsUpdate = true;
    }
    if (green) {
      for (let i = 0; i < signals.length; i++) green.setColorAt(i, lit === 'green' ? on : off);
      if (green.instanceColor) green.instanceColor.needsUpdate = true;
    }
  });

  return (
    <group>
      {/* ① 立杆（静态，逐个 mesh 挂载 —— 行人灯数量少，几十个 mesh 可接受） */}
      {signals.map((s, i) => (
        <mesh key={`ped-pole-${i}`} position={[s.x, POLE_Y, s.z]} castShadow={false} {...info}>
          <cylinderGeometry args={POLE_GEOM} />
          <meshStandardMaterial color="#5a6270" roughness={0.6} metalness={0.5} />
        </mesh>
      ))}
      {/* ② 方盘灯箱 + 倒计时条 */}
      {signals.map((s, i) => (
        <group key={`ped-head-${i}`} position={[s.x, 0, s.z]} rotation={[0, s.rotation, 0]}>
          <mesh position={[0, HEAD_Y, 0]}>
            <boxGeometry args={HEAD_GEOM} />
            <meshStandardMaterial color="#17191d" roughness={0.7} metalness={0.2} />
          </mesh>
          <mesh position={[0, CD_Y, FACE_OFF * 0.55]}>
            <boxGeometry args={CD_GEOM} />
            <meshStandardMaterial color="#ffb43a" emissive="#ffb43a" emissiveIntensity={0.6} roughness={0.3} />
          </mesh>
        </group>
      ))}
      {/* ③ 红人 / 绿人（各 1 draw call，颜色按反相位切换） */}
      <instancedMesh
        ref={redRef}
        args={[undefined, undefined, signals.length]}
        raycast={() => null}
        castShadow={false}
      >
        <boxGeometry args={FIG_GEOM} />
        <meshBasicMaterial toneMapped={false} />
      </instancedMesh>
      <instancedMesh
        ref={greenRef}
        args={[undefined, undefined, signals.length]}
        raycast={() => null}
        castShadow={false}
      >
        <boxGeometry args={FIG_GEOM} />
        <meshBasicMaterial toneMapped={false} />
      </instancedMesh>
    </group>
  );
}
