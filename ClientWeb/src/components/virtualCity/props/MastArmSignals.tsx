/**
 * MastArmSignals — 悬臂式（横杆式）交通信号灯（**批次 43 新增**）。
 *
 * 与立杆式（`TrafficSignals`）的分工：
 *   · **立杆式**布在一般路口（灯头挂在杆顶，仅服务本进口道）；
 *   · **悬臂式**布在**宽路口**（方格骨干互交点）—— 立柱在路口外缘，横臂 4 m
 *     水平伸过路口上空，灯头悬于**对向车道上方**，让远处驾驶员提前看到信号
 *     （GB 14886 §4.3 的核心用意，也是立杆式做不到的）。
 *
 * 渲染：GLB `road/mast_arm_signal.glb` 优先（混凝土基础 + 8 锚栓 + ⌀0.20 立柱 +
 * 柱顶防雨帽 + 4 m 悬臂 + 斜拉撑 + 检修爬梯 + 悬挂三色灯头），材质名
 * `LEDRed`/`LEDYellow`/`LEDGreen` 逐色随相位调制（与立杆式同一 `signalLitColor`，
 * 全城同相位 ⇒ 共享材质一次调制生效）。
 *
 * 数量少（方格骨干 4 交点 × 2 = 8 座），故走 `<Model>` 逐座克隆而非实例化 ——
 * 实例化需要按材质名逐组调光，而克隆同样共享材质，代价仅 8 次 scene clone。
 * 缺失时回退程序化立柱 + 悬臂 + 灯头三件套。
 */

import { useMemo } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { modelUrl } from '@/assets/models';
import { blenderModelsEnabled, useSharedGLTF } from '@/engine3d';
import { collectGlbPairs, GlbInstances, type GlbRenderPair } from '../glbInstances';
import { u, sizeTargetFor } from '../cityScale';
import { signalLitColor, type SignalColor } from './TrafficSignals';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';
import type { MastArmSignalSpot } from '../StreetPropsLayer';

/** GLB 尺寸/落地校验目标（dev 态）；表值 = REAL_DIMS_M.mastArmSignal（米制）。 */
const MAST_ARM_SIZE_TARGET = sizeTargetFor('mastArmSignal', { label: 'road/mast_arm_signal' });

/** GLB 三色 LED 材质名键（与 build_mast_arm_signal.py 的 `MastSignal_LED{Color}_Mat` 对齐）。 */
const LED_MAT_KEY: Record<SignalColor, string> = {
  red: 'LEDRed',
  yellow: 'LEDYellow',
  green: 'LEDGreen',
};
/** 熄灭态 = base × 0.06（与立杆式同口径）。 */
const LED_OFF_FACTOR = 0.06;

// ── 程序化 fallback 几何（米制经 u()）──────────────────────────────────
const COL_GEOM: [number, number, number, number] = [u(0.1), u(0.1), u(6.0), 10];
const COL_Y = u(3.2);
const ARM_GEOM: [number, number, number] = [u(4.0), u(0.15), u(0.15)];
const ARM_Y = u(6.1);
const ARM_X = u(2.0);          // 悬臂中心 x（0→4.0 的中点）
const HEAD_GEOM: [number, number, number] = [u(0.35), u(0.25), u(1.0)];
const HEAD_Y = u(5.1);
const HEAD_X = u(4.0);         // 灯头悬挂于臂端
const HANG_GEOM: [number, number, number, number] = [u(0.05), u(0.05), u(0.22), 6];
const HANG_Y = u(5.71);
const BULB_R = u(0.15);
/** 三色灯泡世界 y（红上/黄中/绿下，间隔 0.35 m）。 */
const BULB_Y: Record<SignalColor, number> = {
  red: u(5.45),
  yellow: u(5.1),
  green: u(4.75),
};
/** 灯泡相对臂端灯头的 local +Z 外凸（灯面朝来车）。 */
const BULB_Z_OFF = u(0.21);

/** 收集 GLB 三色 LED 共享材质。 */
function collectMastLedMaterials(
  root: THREE.Object3D | null,
): Record<SignalColor, Array<{ mat: THREE.MeshStandardMaterial; base: number }>> {
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
      for (const color of ['red', 'yellow', 'green'] as const) {
        if (std.name.includes(LED_MAT_KEY[color])) {
          out[color].push({ mat: std, base: std.emissiveIntensity });
          break;
        }
      }
    }
  });
  return out;
}

export function MastArmSignals({ spots }: { spots: MastArmSignalSpot[] }) {
  const glbUrl = blenderModelsEnabled() ? modelUrl('road', 'mast_arm_signal') : '';
  const { scene: glbScene } = useSharedGLTF(glbUrl, MAST_ARM_SIZE_TARGET);
  const useGlb = Boolean(glbUrl && glbScene);

  const pairs = useMemo<GlbRenderPair[]>(() => (useGlb ? collectGlbPairs(glbScene) : []), [useGlb, glbScene]);
  const ledMats = useMemo(
    () => (useGlb ? collectMastLedMaterials(glbScene) : { red: [], yellow: [], green: [] }),
    [useGlb, glbScene],
  );
  const hasLed = ledMats.red.length + ledMats.yellow.length + ledMats.green.length > 0;

  // 实例世界矩阵：GLB 节点 identity 且资产已 X 居中 ⇒ 直接用布点位姿。
  const worldMatrices = useMemo(
    () =>
      spots.map((s) => {
        const m = new THREE.Matrix4();
        m.makeRotationY(s.rotation);
        m.setPosition(s.x, 0, s.z);
        return m;
      }),
    [spots],
  );

  // 与立杆式同相位语义（真实路口两组灯同相位）。
  useFrame(({ clock }) => {
    if (!useGlb || !hasLed) return;
    const t = clock.elapsedTime;
    for (const color of ['red', 'yellow', 'green'] as const) {
      const list = ledMats[color];
      if (!list.length) continue;
      const lit = signalLitColor('A', t) === color;
      const target = lit ? 1.0 : LED_OFF_FACTOR;
      for (const e of list) e.mat.emissiveIntensity = e.base * target;
    }
  });

  const info = useObjectInfoProps('road.mast-arm-signal', { anchorY: 2.2 });

  if (!spots.length) return null;

  if (useGlb && pairs.length) {
    return <GlbInstances pairs={pairs} worldMatrices={worldMatrices} castShadow={false} receiveShadow={false} />;
  }

  return <ProceduralMastArms spots={spots} info={info} />;
}

/** 程序化 fallback：立柱 + 悬臂 + 吊杆 + 灯头 + 三色灯泡。 */
function ProceduralMastArms({
  spots,
  info,
}: {
  spots: MastArmSignalSpot[];
  info: ReturnType<typeof useObjectInfoProps>;
}) {
  return (
    <group>
      {spots.map((s, i) => (
        <group key={`mast-${i}`} position={[s.x, 0, s.z]} rotation={[0, s.rotation, 0]}>
          {/* 立柱 */}
          <mesh position={[0, COL_Y, 0]} castShadow={false} {...info}>
            <cylinderGeometry args={COL_GEOM} />
            <meshStandardMaterial color="#6b7280" roughness={0.6} metalness={0.5} />
          </mesh>
          {/* 悬臂（沿 local +X 挑出 4 m） */}
          <mesh position={[ARM_X, ARM_Y, 0]} castShadow={false}>
            <boxGeometry args={ARM_GEOM} />
            <meshStandardMaterial color="#6b7280" roughness={0.6} metalness={0.5} />
          </mesh>
          {/* 吊杆（臂端 → 灯箱顶） */}
          <mesh position={[HEAD_X, HANG_Y, 0]} castShadow={false}>
            <cylinderGeometry args={HANG_GEOM} />
            <meshStandardMaterial color="#6b7280" roughness={0.6} metalness={0.5} />
          </mesh>
          {/* 灯头（近黑箱体） */}
          <mesh position={[HEAD_X, HEAD_Y, 0]} castShadow={false}>
            <boxGeometry args={HEAD_GEOM} />
            <meshStandardMaterial color="#17191d" roughness={0.7} metalness={0.2} />
          </mesh>
          {/* 三色灯泡（emissive 常亮色；fallback 不做逐座相位切换，全城同色同亮） */}
          {(['red', 'yellow', 'green'] as SignalColor[]).map((c) => (
            <mesh key={`bulb-${c}`} position={[HEAD_X, BULB_Y[c], BULB_Z_OFF]} castShadow={false}>
              <sphereGeometry args={[BULB_R, 10, 8]} />
              <meshBasicMaterial
                color={c === 'red' ? '#ff2d2d' : c === 'yellow' ? '#ffc40f' : '#2ecc71'}
                toneMapped={false}
              />
            </mesh>
          ))}
        </group>
      ))}
    </group>
  );
}

// re-export 便于调用方统一从本模块取类型
export type { MastArmSignalSpot };
