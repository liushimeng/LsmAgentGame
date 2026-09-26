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
 */
import React, { useMemo } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { u } from '../cityScale';
import { hashStr, mulberry32 } from './rand';

const FIELD_COLORS = ['#6b8e23', '#c4a945', '#8b7355', '#556b2f'];
const HILL_COLOR = '#3f5a3a';
const ROAD_COLOR = '#1f2733';
const WHITE = '#e8e3dc';

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
  const highway = useMemo(() => {
    const out: Array<{ cx: number; cz: number; rot: number }> = [];
    for (let i = 0; i < segments; i++) {
      const a = (i * Math.PI * 2) / segments;
      out.push({ cx: 0, cz: 0, rot: a });
    }
    return out;
  }, []);
  // s 参数保留兼容（高速段元组展开时用）
  void highway;
  const segmentWidth = 1.6;
  const segmentLen = ((58 * Math.PI * 2) / segments) * 1.06; // 6% 搭接
  return (
    <group>
      {/* 农田（6×4.5 世界单位） */}
      {fields.map((f) => (
        <mesh
          key={`field-${f.idx}`}
          position={[f.cx, 0.018, f.cz]}
          rotation={[-Math.PI / 2, 0, -f.rot]}
          receiveShadow
        >
          <planeGeometry args={[6, 4.5]} />
          <meshStandardMaterial color={f.color} roughness={0.95} />
        </mesh>
      ))}
      {/* 丘陵：sphereGeometry r=u(80) + scale[1,0.3,1] + position.y=u(-9) */}
      {hills.map((h, i) => (
        <mesh
          key={`hill-${i}`}
          position={[h.cx, u(-9), h.cz]}
          rotation={[0, -h.rot, 0]}
          scale={[1, 0.3, 1]}
          castShadow
          receiveShadow
        >
          <sphereGeometry args={[u(80), 24, 16]} />
          <meshStandardMaterial color={HILL_COLOR} roughness={0.9} />
        </mesh>
      ))}
      {/* 环城高速（24 段） */}
      {highway.map((_s, i) => {
        const a = (i * Math.PI * 2) / segments;
        const cx = Math.cos(a) * 58;
        const cz = Math.sin(a) * 58;
        return (
          <mesh
            key={`hw-${i}`}
            position={[cx, 0.018, cz]}
            rotation={[0, -a + Math.PI / 2, 0]}
            receiveShadow
          >
            <planeGeometry args={[segmentWidth, segmentLen]} />
            <meshStandardMaterial color={ROAD_COLOR} roughness={0.85} />
          </mesh>
        );
      })}
      {/* 高速护栏（外侧 24 短柱 + 顶部连梁 太密，省略 mesh 计数控制；用 plane 标线替代） */}
      {highway.map((_s, i) => {
        const a = (i * Math.PI * 2) / segments;
        const cx = Math.cos(a) * 58;
        const cz = Math.sin(a) * 58;
        return (
          <mesh
            key={`hwl-${i}`}
            position={[cx, 0.02, cz]}
            rotation={[0, -a + Math.PI / 2, 0]}
          >
            <planeGeometry args={[0.08, segmentLen * 0.95]} />
            <meshBasicMaterial color={WHITE} transparent opacity={0.7} />
          </mesh>
        );
      })}
      {/* 长途车 ×2（环城高速上随机位置） */}
      {turbines.slice(0, 2).map((t, i) => (
        <mesh
          key={`bus-${i}`}
          position={[t.cx * 0.92, u(0.5), t.cz * 0.92]}
          rotation={[0, Math.atan2(-t.cz, -t.cx), 0]}
          castShadow
        >
          <boxGeometry args={[u(2.5), u(1.6), u(1)]} />
          <meshStandardMaterial color="#c0392b" roughness={0.5} metalness={0.3} />
        </mesh>
      ))}
      {/* 风机 ×2：塔 + 机舱 + 3 叶（叶轮旋转） */}
      {turbines.map((t, i) => (
        <WindTurbine key={`turb-${i}`} cx={t.cx} cz={t.cz} phase={t.phase} />
      ))}
    </group>
  );
}

function WindTurbine({ cx, cz, phase }: { cx: number; cz: number; phase: number }) {
  return (
    <group position={[cx, 0, cz]}>
      {/* 塔 */}
      <mesh position={[0, u(15), 0]} castShadow>
        <cylinderGeometry args={[u(0.3), u(0.5), u(30), 8]} />
        <meshStandardMaterial color="#e8e8e8" roughness={0.5} metalness={0.4} />
      </mesh>
      {/* 机舱 */}
      <mesh position={[0, u(30.5), 0]} castShadow>
        <boxGeometry args={[u(2), u(1), u(1.2)]} />
        <meshStandardMaterial color="#e8e8e8" roughness={0.5} metalness={0.4} />
      </mesh>
      {/* 轮毂（旋转头）在机舱前端 */}
      <RotatingBlades phase={phase} />
    </group>
  );
}

function RotatingBlades({ phase }: { phase: number }) {
  const ref = React.useRef<THREE.Group>(null);
  useFrame((_, delta) => {
    if (ref.current) ref.current.rotation.z += delta * 0.4;
  });
  return (
    <group ref={ref} position={[u(1.2), u(30.5), 0]} rotation={[0, 0, phase]}>
      <mesh>
        <sphereGeometry args={[u(0.35), 12, 12]} />
        <meshStandardMaterial color="#7a8290" metalness={0.6} />
      </mesh>
      {[0, (2 * Math.PI) / 3, (4 * Math.PI) / 3].map((a, i) => (
        <mesh key={`blade-${i}`} rotation={[0, 0, a]} position={[u(4), 0, 0]}>
          <boxGeometry args={[u(8), u(0.3), u(0.05)]} />
          <meshStandardMaterial color="#e8e8e8" roughness={0.5} />
        </mesh>
      ))}
    </group>
  );
}