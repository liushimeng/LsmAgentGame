/**
 * 批次 26 · 南缘海洋带（z ∈ [+62,+150]，x ∈ [−150,+150]）。
 *
 * 内容（方案文档 §2.1 坐标带契约）：
 *   - 海面大 plane：300×88 @ (0,106)，**独立于地面 plane**（地面仅 ±80），
 *     ocean_tile 贴图 → water_tile 复用 → 纯色 #1a3a52 二级降级链；
 *   - 沙滩过渡带 z ∈ [58,64]：sand_tile 贴图（缺失降级纯色），CityEdgeLayer
 *     的西沙漠 patch 同贴图不同 repeat（textureCache 按 repeat 分 key，无冲突）；
 *   - 南港 z ∈ [62,70]：码头面 + 岸吊 ×2 + 集装箱堆 + 系缆桩（照
 *     civic/PortTerminal.tsx 模式；岸吊全部箱梁走单位 box Instances → 1 draw call）；
 *   - lighthouse.glb ×1 @ 防波堤端（+75, +72）；fallback 程序化红白条纹塔；
 *   - cargo_ship.glb ×1：useFrame 慢速巡航 x −120→+120 循环（~1.5 单位/秒），
 *     z=95；fallback 程序化船体；
 *   - sailboat.glb ×2 静态泊位；fallback 程序化帆船；
 *   - 浮标 ×4（sphere+cylinder 两个 Instances）。
 *
 * GLB 朝向约定（art-designer build_cargo_ship.py / build_sailboat.py 头注释）：
 * Blender Z-up → glTF/three.js 中 **+x 为船头**（舰桥在 −x 船尾），与车辆 GLB
 * 的 front=+Z 约定不同；货船向 +x 巡航 → rotationY = 0。帆船两处静态
 * rotationY（0.6 / −0.9）为装饰性泊位朝向，视觉无害。
 * 布点确定性（hashStr + mulberry32）。罗盘契约：南 = +Z（方案文档 §1.1）。
 * prefers-reduced-motion 下货船静止于 x=0。
 *
 * 契约：lag_docs/虚拟城市/已实现/26-坐标系统与城市边缘环境/01-现状分析与方案设计.md §2.4。
 */

import { useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Instances, Instance } from '@react-three/drei';
import { Model, useSharedTexture } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { groundTileUrl } from '@/assets/images/virtualCity';
import { u } from '../cityScale';
import { hashStr, mulberry32 } from '../civic/rand';
import { GlbInstanced } from './glbInstanced';

// ── 坐标带常量（方案 §2.1）─────────────────────────────────────
/** 沙滩：z ∈ [58,64]，x ∈ [−120,+120]。 */
const BEACH = { cx: 0, cz: 61, w: 240, d: 6 };
/** 海面：z ∈ [62,150]，x ∈ [−150,150]（越出地面 plane ±80 无碍，方案明示）。 */
const OCEAN = { cx: 0, cz: 106, w: 300, d: 88 };
/** 南港码头面：x ∈ [−35,15]，z ∈ [62,68]（海侧沿 +z）。 */
const QUAY = { cx: -10, cz: 65, w: 50, d: 6 };
/** 货船巡航参数：x −120→+120 循环，z=95，速度 ~1.5 单位/秒（=15 m/s 观光船速）。 */
const SHIP_Z = 95;
const SHIP_RANGE = 120;
const SHIP_SPEED = 1.5;

const SAND_COLOR = '#d9b36c';
const WATER_FALLBACK = '#1a3a52';
const CONCRETE_COLOR = '#6b7280';
const STEEL_COLOR = '#8a8d96';
const STEEL_DARK = '#5a6270';
const CONTAINER_COLORS = ['#c0392b', '#2471a3', '#e67e22', '#27ae60'];
/** 岸吊 x 位（码头面内，海侧沿 z=67）。 */
const CRANE_CENTERS: number[] = [-25, 5];

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export function SouthOcean() {
  return (
    <group>
      <Beach />
      <OceanSurface />
      <SouthPort />
      {/* 灯塔 @ 防波堤端（GLB；children fallback 红白条纹塔，§27.3 契约） */}
      <Model url={modelUrl('ocean', 'lighthouse')} position={[75, 0, 72]}>
        <ProceduralLighthouse />
      </Model>
      <CargoShip />
      {/* 帆船 ×2 静态泊位（GlbInstanced：2 实例共享子网格 draw call） */}
      <GlbInstanced
        url={modelUrl('ocean', 'sailboat')}
        instances={[
          { position: [-60, 0.02, 80], rotationY: 0.6, scale: 1 },
          { position: [45, 0.02, 88], rotationY: -0.9, scale: 1.1 },
        ]}
        fallback={
          <group>
            <ProceduralSailboat x={-60} z={80} rotY={0.6} />
            <ProceduralSailboat x={45} z={88} rotY={-0.9} />
          </group>
        }
      />
      <Buoys />
    </group>
  );
}

/** 沙滩过渡带：sand_tile 贴图（缺失 → 纯色沙色）。 */
function Beach() {
  const tex = useSharedTexture(groundTileUrl('sand_tile'), {
    wrap: 'repeat',
    repeat: [Math.max(1, Math.round(BEACH.w / 8)), 1],
  });
  return (
    <mesh rotation={[-Math.PI / 2, 0, 0]} position={[BEACH.cx, 0.02, BEACH.cz]} receiveShadow>
      <planeGeometry args={[BEACH.w, BEACH.d]} />
      <meshStandardMaterial
        map={tex ?? undefined}
        color={tex ? '#ffffff' : SAND_COLOR}
        roughness={0.95}
      />
    </mesh>
  );
}

/** 海面大 plane：ocean_tile → water_tile 复用 → 纯色 #1a3a52（二级降级链）。 */
function OceanSurface() {
  const oceanTex = useSharedTexture(groundTileUrl('ocean_tile'), {
    wrap: 'repeat',
    repeat: [Math.max(1, Math.round(OCEAN.w / 8)), Math.max(1, Math.round(OCEAN.d / 8))],
  });
  const waterTex = useSharedTexture(groundTileUrl('water_tile'), {
    wrap: 'repeat',
    repeat: [Math.max(1, Math.round(OCEAN.w / 8)), Math.max(1, Math.round(OCEAN.d / 8))],
  });
  const tex = oceanTex ?? waterTex;
  return (
    <mesh rotation={[-Math.PI / 2, 0, 0]} position={[OCEAN.cx, 0.03, OCEAN.cz]} receiveShadow>
      <planeGeometry args={[OCEAN.w, OCEAN.d]} />
      <meshStandardMaterial
        map={tex ?? undefined}
        color={tex ? '#ffffff' : WATER_FALLBACK}
        transparent
        opacity={0.94}
        roughness={0.08}
        metalness={0.3}
        envMapIntensity={0.9}
      />
    </mesh>
  );
}

/** 南港：码头面 + 岸吊 ×2 + 集装箱 + 系缆桩（照 civic/PortTerminal.tsx 模式）。 */
function SouthPort() {
  // 岸吊 ×2：全部箱梁单位 box Instances → 1 draw call
  const boxes = useMemo(() => {
    const out: Array<{
      position: [number, number, number];
      scale: [number, number, number];
      color: string;
    }> = [];
    for (const cx of CRANE_CENTERS) {
      // 4 腿（u(0.5)×u(22)×u(0.5) @ ±u(9)/±u(4)，顶高 u(22)）
      for (const sx of [-1, 1]) {
        for (const sz of [-1, 1]) {
          out.push({
            position: [cx + sx * u(9), u(11), 67 + sz * u(4)],
            scale: [u(0.5), u(22), u(0.5)],
            color: STEEL_COLOR,
          });
        }
      }
      // 横梁 u(22)×u(1.2)×u(1.0)
      out.push({ position: [cx, u(22.5), 67], scale: [u(22), u(1.2), u(1.0)], color: STEEL_DARK });
      // 海侧悬臂（伸向 +z 海面）
      out.push({ position: [cx, u(22.5), 67 + u(5.5)], scale: [u(0.8), u(0.8), u(10)], color: STEEL_COLOR });
      // 小车
      out.push({ position: [cx + u(3), u(22.5), 67 + u(5.5)], scale: [u(1.6), u(1.0), u(1.4)], color: STEEL_DARK });
      // 吊具
      out.push({ position: [cx + u(3), u(13.5), 67 + u(5.5)], scale: [u(2.4), u(0.35), u(1.6)], color: '#c0392b' });
    }
    return out;
  }, []);

  // 钢缆（小车 → 吊具；单位 cylinder Instances → 1 draw call）
  const cables = useMemo(
    () =>
      CRANE_CENTERS.map((cx) => ({
        position: [cx + u(3), u(17.5), 67 + u(5.5)] as [number, number, number],
        len: u(8),
      })),
    [],
  );

  // 集装箱堆：码头面陆侧（z 62.5~65.5），3 色确定性轮转
  const containers = useMemo(() => {
    const rnd = mulberry32(hashStr('edge26:south:containers'));
    const out: Array<{ x: number; z: number; layer: 0 | 1; color: string }> = [];
    for (let cx = 0; cx < 5; cx++) {
      for (let cz = 0; cz < 2; cz++) {
        const layer = (rnd() < 0.4 ? 1 : 0) as 0 | 1;
        out.push({
          x: -30 + cx * u(7),
          z: 62.8 + cz * u(2.6),
          layer,
          color: CONTAINER_COLORS[Math.floor(rnd() * CONTAINER_COLORS.length)],
        });
      }
    }
    return out;
  }, []);

  return (
    <group>
      {/* 码头面（混凝土色；y 高于海面 0.03，低于街区 curb） */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[QUAY.cx, 0.06, QUAY.cz]} receiveShadow>
        <planeGeometry args={[QUAY.w, QUAY.d]} />
        <meshStandardMaterial color={CONCRETE_COLOR} roughness={0.85} metalness={0.05} />
      </mesh>
      {/* 码头面海侧边沿抬升（挡浪沿，单位 box） */}
      <mesh position={[QUAY.cx, 0.12, 68]}>
        <boxGeometry args={[QUAY.w, 0.12, 0.3]} />
        <meshStandardMaterial color={STEEL_DARK} roughness={0.7} />
      </mesh>
      {/* 岸吊 ×2 全部箱梁 → 1 draw call */}
      <Instances limit={boxes.length} range={boxes.length} castShadow>
        <boxGeometry args={[1, 1, 1]} />
        <meshStandardMaterial color="#ffffff" metalness={0.5} roughness={0.45} />
        {boxes.map((b, i) => (
          <Instance key={`cb-${i}`} position={b.position} scale={b.scale} color={b.color} />
        ))}
      </Instances>
      {/* 钢缆 → 1 draw call */}
      <Instances limit={cables.length} range={cables.length}>
        <cylinderGeometry args={[0.008, 0.008, 1, 5]} />
        <meshStandardMaterial color="#3a3f4a" />
        {cables.map((c, i) => (
          <Instance key={`cc-${i}`} position={c.position} scale={[1, c.len, 1]} />
        ))}
      </Instances>
      {/* 集装箱堆 → 1 draw call */}
      <Instances limit={containers.length} range={containers.length} castShadow>
        <boxGeometry args={[u(6), u(2.6), u(2.4)]} />
        <meshStandardMaterial color="#ffffff" roughness={0.55} metalness={0.25} />
        {containers.map((c, i) => (
          <Instance
            key={`ct-${i}`}
            position={[c.x, 0.06 + (c.layer ? u(2.8) : u(1.4)), c.z]}
            color={c.color}
          />
        ))}
      </Instances>
      {/* 系缆桩 ×5 → 1 draw call */}
      <Instances limit={5} range={5} castShadow>
        <cylinderGeometry args={[u(0.15), u(0.15), u(0.5), 8]} />
        <meshStandardMaterial color={STEEL_DARK} metalness={0.5} roughness={0.6} />
        {[-2, -1, 0, 1, 2].map((k, i) => (
          <Instance key={`mg-${i}`} position={[QUAY.cx + k * u(12), u(0.25) + 0.06, 67.6]} />
        ))}
      </Instances>
    </group>
  );
}

/** 货船：GLB + useFrame 慢速巡航（x −120→+120 循环；reduced-motion 静止于 x=0）。 */
function CargoShip() {
  const ref = useRef<THREE.Group>(null);
  useFrame((state) => {
    if (!ref.current || REDUCED_MOTION) return;
    // 三角波太慢会倒车感，用线性回绕：t 映射到 [−RANGE, +RANGE]
    const t = (state.clock.elapsedTime * SHIP_SPEED) % (SHIP_RANGE * 2);
    ref.current.position.x = t - SHIP_RANGE;
  });
  return (
    <group ref={ref} position={[0, 0.02, SHIP_Z]}>
      {/* 船向 +x 巡航；cargo_ship.glb 船头 = +x（build_cargo_ship.py 头注释，
          与车辆 front=+Z 约定不同）→ rotationY = 0 */}
      <Model url={modelUrl('ocean', 'cargo_ship')}>
        <ProceduralCargoShip />
      </Model>
    </group>
  );
}

/** 货船程序化 fallback：黑底红舷船体 + 舰桥 + 集装箱堆 3 色（长 ~60m）。 */
function ProceduralCargoShip() {
  return (
    <group>
      {/* 船体（黑） */}
      <mesh position={[0, u(1.5), 0]}>
        <boxGeometry args={[u(60), u(3), u(12)]} />
        <meshStandardMaterial color="#20242c" roughness={0.6} metalness={0.3} />
      </mesh>
      {/* 红舷水线 */}
      <mesh position={[0, u(0.4), 0]}>
        <boxGeometry args={[u(60.5), u(0.8), u(12.5)]} />
        <meshStandardMaterial color="#8a2f23" roughness={0.7} />
      </mesh>
      {/* 舰桥（船尾） */}
      <mesh position={[-u(24), u(4.5), 0]}>
        <boxGeometry args={[u(6), u(6), u(10)]} />
        <meshStandardMaterial color="#e8e3dc" roughness={0.6} />
      </mesh>
      {/* 集装箱堆 3 色 */}
      {CONTAINER_COLORS.slice(0, 3).map((c, i) => (
        <mesh key={i} position={[-u(8) + i * u(9), u(3.6), 0]}>
          <boxGeometry args={[u(8), u(2.6), u(9)]} />
          <meshStandardMaterial color={c} roughness={0.55} metalness={0.25} />
        </mesh>
      ))}
    </group>
  );
}

/** 帆船程序化 fallback：白船体 + 桅杆 + 三角帆（长 ~8m）。 */
function ProceduralSailboat({ x, z, rotY }: { x: number; z: number; rotY: number }) {
  return (
    <group position={[x, 0.02, z]} rotation={[0, rotY, 0]}>
      <mesh position={[0, u(0.5), 0]}>
        <boxGeometry args={[u(2.2), u(0.8), u(8)]} />
        <meshStandardMaterial color="#f2f2f0" roughness={0.5} />
      </mesh>
      <mesh position={[0, u(3.5), 0]}>
        <cylinderGeometry args={[u(0.06), u(0.08), u(6), 6]} />
        <meshStandardMaterial color="#8a6f4d" roughness={0.7} />
      </mesh>
      {/* 三角帆（cone 3 段压扁） */}
      <mesh position={[u(0.05), u(3.2), u(0.8)]} rotation={[0, 0, 0]}>
        <coneGeometry args={[u(1.6), u(4.5), 3]} />
        <meshStandardMaterial color="#ffffff" roughness={0.6} side={THREE.DoubleSide} />
      </mesh>
    </group>
  );
}

/** 灯塔程序化 fallback：红白相间圆塔 + 灯室 + 基座（高 ~15m）。 */
function ProceduralLighthouse() {
  return (
    <group>
      {/* 基座（防波堤石台） */}
      <mesh position={[0, u(0.5), 0]}>
        <cylinderGeometry args={[u(3), u(3.5), u(1), 10]} />
        <meshStandardMaterial color="#7a7f88" roughness={0.9} />
      </mesh>
      {/* 红白条纹塔身 4 段 */}
      {[0, 1, 2, 3].map((i) => (
        <mesh key={i} position={[0, u(1) + u(3) * (i + 0.5), 0]}>
          <cylinderGeometry args={[u(1.1) - i * u(0.1), u(1.2) - i * u(0.1), u(3), 12]} />
          <meshStandardMaterial color={i % 2 === 0 ? '#e8e3dc' : '#c0392b'} roughness={0.7} />
        </mesh>
      ))}
      {/* 灯室 */}
      <mesh position={[0, u(13.6), 0]}>
        <cylinderGeometry args={[u(0.9), u(0.9), u(1.4), 10]} />
        <meshStandardMaterial color="#2c313a" roughness={0.4} metalness={0.4} />
      </mesh>
      <mesh position={[0, u(14.6), 0]}>
        <coneGeometry args={[u(1.1), u(1.2), 10]} />
        <meshStandardMaterial color="#c0392b" roughness={0.7} />
      </mesh>
    </group>
  );
}

/** 浮标 ×4：红球 + 顶杆（两个 Instances，主航道标记 z=75 一线）。 */
function Buoys() {
  const buoys = useMemo(() => {
    const rnd = mulberry32(hashStr('edge26:south:buoys'));
    return [-30, -10, 10, 30].map((x) => ({
      x: x + (rnd() - 0.5) * 3,
      z: 75 + (rnd() - 0.5) * 2,
    }));
  }, []);
  return (
    <group>
      <Instances limit={buoys.length} range={buoys.length}>
        <sphereGeometry args={[u(0.5), 10, 8]} />
        <meshStandardMaterial color="#c0392b" roughness={0.6} />
        {buoys.map((b, i) => (
          <Instance key={`buoy-s-${i}`} position={[b.x, u(0.3), b.z]} />
        ))}
      </Instances>
      <Instances limit={buoys.length} range={buoys.length}>
        <cylinderGeometry args={[u(0.05), u(0.05), u(1.2), 6]} />
        <meshStandardMaterial color="#e8e3dc" roughness={0.6} />
        {buoys.map((b, i) => (
          <Instance key={`buoy-c-${i}`} position={[b.x, u(1.1), b.z]} />
        ))}
      </Instances>
    </group>
  );
}

export default SouthOcean;
