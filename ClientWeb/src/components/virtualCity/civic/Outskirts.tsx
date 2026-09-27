/**
 * 外围腹地（18-AA · §5.2 Outskirts）
 *   农田 4 块（批次 26 起仅四角象限）+ 丘陵 4 个 + 环城高速 + 风机 2 座。
 *   批次 20（文档 1 §3.4）：半径带整体 ×1.33 → **r∈[50,77]**，与外圈新区
 *   底板（最远 |x|=50）留 ≥6 单位缓冲（WORLD_GROUND_SIZE=160 半幅 80 内）：
 *     农田 6×4.5 @ r∈[56,73]（= 旧 42..55 ×1.33）
 *     丘陵 r=66（旧 50），sphereGeometry r=u(80) + scale[1,0.3,1] + position.y=u(-9)
 *     环城高速 r=58（旧 44）、32 段（旧 24，等弧长加密）
 *     风机 @ r=64（旧 48）
 *   批次 26（26-坐标系统与城市边缘环境 · 方案 §2.4）：农田 8 块均布 →
 *     **仅四角象限 4 块**（角 45°±15° 抖动、r ∈ [52,58]），保证农田边缘
 *     |x|,|z| ≤ 58+3 = 61 不伸进四缘环境带（|x| 或 |z| > 62 为雪山/沙漠/
 *     森林/海洋带域）；丘陵 / 风机 / 环城高速不动（45° 对角 r=64~66 不与
 *     边缘带冲突）。
 *
 * 批次 28 二轮（DC 攻坚）：86 mesh → 6 mesh（几何全等合并，engine3d/geoMerge）：
 *   1) 农田+丘陵 mesh（顶点色）：4 块 plane + 4 颗压扁 sphere 合并。
 *   2) 环城高速 mesh：32 段路面 plane 合并（原共享同材质）。
 *   3) 车道标线 mesh：32 段白色半透明 plane 合并（原共享同 basic 材质）。
 *   4) 长途车+风机静件 mesh（顶点色）：2 车 + 2 塔 + 2 机舱合并。
 *   5/6) 风机叶轮 ×2：useFrame 旋转必须独立 mesh —— 每座叶轮
 *      （轮毂 + 3 叶）内部合 1 mesh，随动画组整体旋转。
 * 批次 28 二轮取舍：农田(0.95)/丘陵(0.9) 粗糙度统一 0.95（丘陵略更哑光，
 *   远景不可辨）；车(0.5/0.3)与塔舱(0.5/0.4)统一 0.5/0.35；叶轮轮毂(1.0/0.6)
 *   与叶片(0.5/0)统一 0.75/0.3；caster 裁剪——外围静物不投影（shadow pass
 *   实测 1044 DC 超 500 阈值，只留楼体+树干）。
 */
import React, { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { hashStr, mulberry32 } from './rand';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const FIELD_COLORS = ['#6b8e23', '#c4a945', '#8b7355', '#556b2f'];
const HILL_COLOR = '#3f5a3a';
const ROAD_COLOR = '#1f2733';
const WHITE = '#e8e3dc';
const BUS_RED = '#c0392b';
const TURBINE_WHITE = '#e8e8e8';
const HUB_GREY = '#7a8290';

/** 农田+丘陵统一粗糙度（原 0.95 / 0.9 取 0.95）。 */
const GROUND_ROUGH = 0.95;
/** 车辆+风机静件统一参数（原 车 0.5/0.3、塔舱 0.5/0.4 取中）。 */
const MACHINE_ROUGH = 0.5;
const MACHINE_METAL = 0.35;
/** 叶轮统一参数（原 轮毂 1.0/0.6、叶片 0.5/0 取中）。 */
const BLADE_ROUGH = 0.75;
const BLADE_METAL = 0.3;

/** 欧拉旋转 + 平移（+ 可选缩放）的部件矩阵（等价原 JSX 的 rotation/position/scale）。 */
function partMatrix(
  x: number, y: number, z: number,
  rx: number, ry: number, rz: number,
  sx = 1, sy = 1, sz = 1,
): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(rx, ry, rz)),
    new THREE.Vector3(sx, sy, sz),
  );
}

export function Outskirts() {
  // 批次 26：农田 8 块均布 → 仅四角象限 4 块（角 45°+k·90° ±15° 抖动、
  // r ∈ [52,58]，确定性 mulberry32），保证农田（6×4.5）边缘不越过
  // |x|,|z| ≤ 61，不伸进四缘环境带（>62）。
  const fields = useMemo(() => {
    const rnd = mulberry32(hashStr('outskirts:fields26'));
    const out: Array<{ cx: number; cz: number; rot: number; color: string; idx: number }> = [];
    for (let i = 0; i < 4; i++) {
      const baseAngle = Math.PI / 4 + (i * Math.PI) / 2 + (rnd() - 0.5) * (Math.PI / 6); // 45°±15°
      const r = 52 + rnd() * 6; // r ∈ [52,58]
      out.push({
        cx: Math.cos(baseAngle) * r,
        cz: Math.sin(baseAngle) * r,
        rot: baseAngle,
        color: FIELD_COLORS[i % FIELD_COLORS.length],
        idx: i,
      });
    }
    return out;
  }, []);

  // 丘陵 4 个：4 个等角槽
  const hills = useMemo(() => {
    const out: Array<{ cx: number; cz: number; rot: number }> = [];
    for (let i = 0; i < 4; i++) {
      const a = (i * Math.PI * 2) / 4 + Math.PI / 4;
      out.push({ cx: Math.cos(a) * 66, cz: Math.sin(a) * 66, rot: a }); // 批次 20 ×1.33：50→66
    }
    return out;
  }, []);

  // 风机 2 座
  const turbines = useMemo(() => {
    const rnd = mulberry32(hashStr('outskirts:turbines'));
    return [
      { cx: Math.cos(Math.PI / 4) * 64, cz: Math.sin(Math.PI / 4) * 64, phase: rnd() * Math.PI * 2 },
      { cx: Math.cos((Math.PI * 5) / 4) * 64, cz: Math.sin((Math.PI * 5) / 4) * 64, phase: rnd() * Math.PI * 2 },
    ];
  }, []);

  // 环城高速：32 段 `plane` 拼圆环，半径 58，宽 1.6（批次 20 ×1.33 + 等弧长加密）
  const segments = 32;
  const segmentWidth = 1.6;
  const segmentLen = ((58 * Math.PI * 2) / segments) * 1.06; // 6% 搭接

  // 1) 农田 + 丘陵（顶点色合并）
  const groundGeo = useMemo(() => {
    const parts: MergePart[] = fields.map((f) => ({
      geo: new THREE.PlaneGeometry(6, 4.5),
      matrix: partMatrix(f.cx, 0.018, f.cz, -Math.PI / 2, 0, -f.rot),
      color: f.color,
    }));
    for (const h of hills) {
      parts.push({
        geo: new THREE.SphereGeometry(u(80), 24, 16),
        matrix: partMatrix(h.cx, u(-9), h.cz, 0, -h.rot, 0, 1, 0.3, 1),
        color: HILL_COLOR,
      });
    }
    return mergeParts(parts);
  }, [fields, hills]);
  useEffect(() => () => groundGeo.dispose(), [groundGeo]);

  // 2) 环城高速 32 段（共享同材质 → 合 1 mesh）
  const highwayGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (let i = 0; i < segments; i++) {
      const a = (i * Math.PI * 2) / segments;
      parts.push({
        geo: new THREE.PlaneGeometry(segmentWidth, segmentLen),
        matrix: partMatrix(Math.cos(a) * 58, 0.018, Math.sin(a) * 58, 0, -a + Math.PI / 2, 0),
      });
    }
    return mergeParts(parts);
  }, []);
  useEffect(() => () => highwayGeo.dispose(), [highwayGeo]);

  // 3) 车道标线 32 段（basic 半透明 → 合 1 mesh）
  const laneGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (let i = 0; i < segments; i++) {
      const a = (i * Math.PI * 2) / segments;
      parts.push({
        geo: new THREE.PlaneGeometry(0.08, segmentLen * 0.95),
        matrix: partMatrix(Math.cos(a) * 58, 0.02, Math.sin(a) * 58, 0, -a + Math.PI / 2, 0),
      });
    }
    return mergeParts(parts);
  }, []);
  useEffect(() => () => laneGeo.dispose(), [laneGeo]);

  // 4) 长途车 ×2 + 风机塔/机舱 ×2（顶点色合并）
  const machineGeo = useMemo(() => {
    const parts: MergePart[] = turbines.slice(0, 2).map((t) => ({
      geo: new THREE.BoxGeometry(u(2.5), u(1.6), u(1)),
      matrix: partMatrix(t.cx * 0.92, u(0.5), t.cz * 0.92, 0, Math.atan2(-t.cz, -t.cx), 0),
      color: BUS_RED,
    }));
    for (const t of turbines) {
      parts.push({ geo: new THREE.CylinderGeometry(u(0.3), u(0.5), u(30), 8), x: t.cx, y: u(15), z: t.cz, color: TURBINE_WHITE });
      parts.push({ geo: new THREE.BoxGeometry(u(2), u(1), u(1.2)), x: t.cx, y: u(30.5), z: t.cz, color: TURBINE_WHITE });
    }
    return mergeParts(parts);
  }, [turbines]);
  useEffect(() => () => machineGeo.dispose(), [machineGeo]);

  // 批次 28 B2：外围腹地信息交互（农田/风机/高速事件冒泡至根组）。
  const info = useObjectInfoProps('civic.outskirts', { anchorY: 1.0 });
  return (
    <group {...info}>
      {/* 农田 + 丘陵（caster 裁剪不投影） */}
      <mesh geometry={groundGeo} receiveShadow>
        <meshStandardMaterial vertexColors roughness={GROUND_ROUGH} />
      </mesh>
      {/* 环城高速（32 段合 1） */}
      <mesh geometry={highwayGeo} receiveShadow>
        <meshStandardMaterial color={ROAD_COLOR} roughness={0.85} />
      </mesh>
      {/* 高速车道标线（32 段合 1；basic 半透明） */}
      <mesh geometry={laneGeo}>
        <meshBasicMaterial color={WHITE} transparent opacity={0.7} />
      </mesh>
      {/* 长途车 + 风机塔/机舱（caster 裁剪不投影） */}
      <mesh geometry={machineGeo}>
        <meshStandardMaterial vertexColors roughness={MACHINE_ROUGH} metalness={MACHINE_METAL} />
      </mesh>
      {/* 风机 ×2：叶轮为 useFrame 旋转件，各自独立 mesh */}
      {turbines.map((t, i) => (
        <RotatingBlades key={`turb-${i}`} cx={t.cx} cz={t.cz} phase={t.phase} />
      ))}
    </group>
  );
}

/**
 * 风机叶轮（批次 28 二轮：轮毂 + 3 叶合 1 mesh，随 useFrame 旋转组整体转动；
 * 塔与机舱已并入根部的静件 mesh，此组件只保留动画子装配）。
 */
function RotatingBlades({ cx, cz, phase }: { cx: number; cz: number; phase: number }) {
  const ref = React.useRef<THREE.Group>(null);
  useFrame((_, delta) => {
    if (ref.current) ref.current.rotation.z += delta * 0.4;
  });
  // 叶轮合并几何（轮毂球 + 3 叶 box；位置即原 WindTurbine 组内叶轮组局部坐标）
  const bladesGeo = useMemo(() => {
    const parts: MergePart[] = [
      { geo: new THREE.SphereGeometry(u(0.35), 12, 12), x: 0, y: 0, z: 0, color: HUB_GREY },
    ];
    for (const a of [0, (2 * Math.PI) / 3, (4 * Math.PI) / 3]) {
      parts.push({ geo: new THREE.BoxGeometry(u(8), u(0.3), u(0.05)), rotZ: a, x: u(4), y: 0, z: 0, color: TURBINE_WHITE });
    }
    return mergeParts(parts);
  }, []);
  useEffect(() => () => bladesGeo.dispose(), [bladesGeo]);
  return (
    <group ref={ref} position={[cx + u(1.2), u(30.5), cz]} rotation={[0, 0, phase]}>
      <mesh geometry={bladesGeo}>
        <meshStandardMaterial vertexColors roughness={BLADE_ROUGH} metalness={BLADE_METAL} />
      </mesh>
    </group>
  );
}
