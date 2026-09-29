/**
 * SelectionMarker — 通用物体选中特效（批次 32 v2）。
 *
 * 挂在 VirtualCityCityMap 的 Canvas 内（单例，不挂在任何物体组件里），订阅
 * useObjectInfoStore.selected，按 target.pos 定位。
 *
 * 设计为**通用**：不只服务建筑。所有被 objectInfo 接线的物体（建筑/街具/树/车/行人/
 * 道路/市政/地标/水系/城缘）左键点中后都出现同一组「选中环」。半径 r 由调用方
 * （VirtualCityCityMap）经 selectedTargetFor 检索得出 —— 建筑用盒半径，其它物体用
 * fallback —— 因此环只是「一个中心点 + 一个半径」，与物体形状无关。
 *
 * 特效分两层：
 *   1. 地面环：RingGeometry 躺平（X 轴 -90°），y = groundY + 0.06 略浮于地面/水面；
 *   2. 竖环：外接圆柱的 3/4 截面（顶点在 shader 内按 theta ∈ [0, 3π/2] 弧形扫掠），
 *      作为「包裹物体」的立面指示；
 * 两者都走 AdditiveBlending + 呼吸脉动（scale + opacity 按 sin(t·2.6) 调制），
 * 让「选中」一眼可辨。
 *
 * 为什么不包半透明盒：353 栋楼的合并 mesh 是多楼共享一个 mesh，单独给某一栋加材质
 * 覆盖会破坏合并（draw call 退化）；非建筑物体形状各异，「包盒」观感差。
 * 环 + 竖环是通用几何，不依赖物体本身。
 */

import { useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { useObjectInfoStore } from './objectInfo/objectInfoStore';

/** 地面环与竖环的共享材质参数（亮青，叠加混合，暗色主题下对比足够）。 */
const RING_COLOR = '#38bdf8';
const RING_OPACITY = 0.30;
/** 呼吸脉动周期（秒）。 */
const PULSE_SPEED = 2.6;
/** 环距地面高度（世界单位；略浮于地面/水面防 z-fight）。 */
const LIFT = 0.06;

/**
 * 竖环几何：外接圆柱的 3/4 截面。
 * 在 BufferGeometry 里直接生成「弧形墙」：半径 r、高 h、圆心角 270°、径向厚度 0。
 */
function buildArcWallGeometry(radius: number, height: number, arc = Math.PI * 1.5): THREE.BufferGeometry {
  const seg = 48;
  const positions: number[] = [];
  const indices: number[] = [];
  for (let i = 0; i <= seg; i += 1) {
    const a = (i / seg) * arc;
    const x = Math.cos(a) * radius;
    const z = Math.sin(a) * radius;
    positions.push(x, 0, z);
    positions.push(x, height, z);
  }
  for (let i = 0; i < seg; i += 1) {
    const a = i * 2;
    indices.push(a, a + 1, a + 2, a + 1, a + 3, a + 2);
  }
  const geo = new THREE.BufferGeometry();
  geo.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
  geo.setIndex(indices);
  geo.computeVertexNormals();
  return geo;
}

export interface SelectionMarkerProps {
  /** 选中物体的聚焦点（含环绕半径）。由 selectedTargetFor 算出。 */
  focus: { x: number; y: number; z: number; radius: number } | null;
  /** 地面/水面安全高度（环浮于此高）。 */
  groundY: number;
}

export function SelectionMarker({ focus, groundY }: SelectionMarkerProps) {
  const selected = useObjectInfoStore((s) => s.selected);
  const groupRef = useRef<THREE.Group>(null);

  // 半径：focus 给了就用（建筑盒半径），否则用一个保守默认
  const radius = focus?.radius ?? 2.0;
  const arcGeo = useMemo(() => buildArcWallGeometry(radius, radius * 1.6), [radius]);

  useFrame(({ clock }) => {
    const g = groupRef.current;
    if (!g) return;
    const t = clock.getElapsedTime();
    const pulse = 1 + 0.05 * Math.sin(t * PULSE_SPEED);
    g.scale.setScalar(pulse);
    g.traverse((obj) => {
      const mat = (obj as THREE.Mesh).material as THREE.MeshBasicMaterial | undefined;
      if (mat && 'opacity' in mat) {
        mat.opacity = RING_OPACITY * (0.85 + 0.15 * Math.sin(t * PULSE_SPEED));
      }
    });
  });

  if (!selected || !focus) return null;

  const cx = focus.x;
  const cz = focus.z;

  return (
    <group ref={groupRef} position={[cx, groundY + LIFT, cz]}>
      {/* 地面环 */}
      <mesh rotation={[-Math.PI / 2, 0, 0]}>
        <ringGeometry args={[radius * 0.85, radius, 48]} />
        <meshBasicMaterial
          color={RING_COLOR}
          transparent
          opacity={RING_OPACITY}
          blending={THREE.AdditiveBlending}
          depthWrite={false}
          side={THREE.DoubleSide}
        />
      </mesh>
      {/* 竖环：外接圆柱的 3/4 截面，立在环上 */}
      <mesh geometry={arcGeo} position={[0, 0, 0]}>
        <meshBasicMaterial
          color={RING_COLOR}
          transparent
          opacity={RING_OPACITY * 0.7}
          blending={THREE.AdditiveBlending}
          depthWrite={false}
          side={THREE.DoubleSide}
        />
      </mesh>
    </group>
  );
}
