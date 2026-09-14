/**
 * WealthCityMap — r3f 主场景（首个正式 R3F 游戏场景，规范见前端架构文档 §3）：
 * Canvas（shadows, dpr 1–2, 相机 [14,12,14] fov45）+ 40×40 地面 + gridHelper +
 * 8 城区 DistrictBlock + 道路薄板（各城区 → finance）+ AgentToken +
 * ambient/directional 灯光 + OrbitControls（pan/zoom/rotate, maxPolarAngle 1.2）。
 *
 * 相机联动：viewRef 由场景内 CameraReporter 每帧回写（target + distance），
 * 小地图据此画视野框；focusRef 由小地图 / 面板写入目标区中心，
 * FocusController 平滑移动 OrbitControls.target。
 */

import { useMemo, useRef } from 'react';
import * as THREE from 'three';
import { Canvas, useFrame, useThree } from '@react-three/fiber';
import { OrbitControls } from '@react-three/drei';
import type { OrbitControls as OrbitControlsImpl } from 'three-stdlib';
import { DistrictBlock } from './DistrictBlock';
import { AgentToken } from './AgentToken';
import {
  WEALTH_DISTRICTS,
  districtCenter,
  type WealthDistrictId,
  type WealthGameState,
} from '@/types/wealth';

/** 主场景 → 小地图的相机视野快照（ref 每帧覆写，不触发 React 渲染）。 */
export interface WealthCameraView {
  x: number;
  z: number;
  dist: number;
}

/** 小地图 / 面板 → 主场景的聚焦目标（null = 无聚焦请求）。 */
export interface WealthFocusTarget {
  x: number;
  z: number;
}

interface Props {
  gameState: WealthGameState | null;
  /** 小地图画视野框用（每帧覆写）。 */
  viewRef: React.MutableRefObject<WealthCameraView>;
  /** 聚焦目标（写入后场景平滑移 target，到位自动清空）。 */
  focusRef: React.MutableRefObject<WealthFocusTarget | null>;
  selectedDistrict: WealthDistrictId | null;
  onSelectDistrict: (id: WealthDistrictId) => void;
}

function Ground() {
  return (
    <>
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0, 0]} receiveShadow>
        <planeGeometry args={[40, 40]} />
        <meshStandardMaterial color="#141a24" roughness={1} />
      </mesh>
      <gridHelper args={[40, 20, '#2a3342', '#1d2530']} position={[0, 0.01, 0]} />
    </>
  );
}

/** 深色薄板道路：各城区中心 → finance(0,0)，宽 0.8，y=0.02。 */
function Roads() {
  const roads = useMemo(() => {
    return WEALTH_DISTRICTS.filter((d) => d.id !== 'finance').map((d) => {
      const dx = 0 - d.x;
      const dz = 0 - d.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      return {
        key: d.id,
        midX: d.x + dx / 2,
        midZ: d.z + dz / 2,
        len,
        angle: Math.atan2(dx, dz),
      };
    });
  }, []);
  return (
    <>
      {roads.map((r) => (
        <group key={r.key} position={[r.midX, 0, r.midZ]} rotation={[0, r.angle, 0]}>
          <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.02, 0]}>
            <planeGeometry args={[0.8, r.len]} />
            <meshStandardMaterial color="#232b38" roughness={1} />
          </mesh>
        </group>
      ))}
    </>
  );
}

/** useFrame 内读 controls.target + 相机距离 → 写 viewRef（小地图视野框）。 */
function CameraReporter({
  controlsRef,
  viewRef,
}: {
  controlsRef: React.MutableRefObject<OrbitControlsImpl | null>;
  viewRef: React.MutableRefObject<WealthCameraView>;
}) {
  const camera = useThree((s) => s.camera);
  useFrame(() => {
    const c = controlsRef.current;
    if (!c) return;
    viewRef.current.x = c.target.x;
    viewRef.current.z = c.target.z;
    viewRef.current.dist = camera.position.distanceTo(c.target);
  });
  return null;
}

/** focusRef 有值 → lerp OrbitControls.target 到目标区，到位清空。 */
function FocusController({
  controlsRef,
  focusRef,
}: {
  controlsRef: React.MutableRefObject<OrbitControlsImpl | null>;
  focusRef: React.MutableRefObject<WealthFocusTarget | null>;
}) {
  const tmpRef = useRef(new THREE.Vector3());
  useFrame(() => {
    const c = controlsRef.current;
    const f = focusRef.current;
    if (!c || !f) return;
    const tmp = tmpRef.current;
    tmp.x = f.x;
    tmp.y = 0;
    tmp.z = f.z;
    c.target.lerp(tmp, 0.08);
    if (c.target.distanceTo(tmp) < 0.05) {
      focusRef.current = null;
    }
  });
  return null;
}

/** 按城区聚合玩家，产出 token 布点参数。 */
function tokenLayout(gameState: WealthGameState | null) {
  const players = gameState?.players ?? [];
  const byDistrict = new Map<string, number[]>(); // districtId → player indexes
  players.forEach((p, i) => {
    const list = byDistrict.get(p.district) ?? [];
    list.push(i);
    byDistrict.set(p.district, list);
  });
  const counts = new Map<string, number>();
  players.forEach((p) => {
    counts.set(p.district, (counts.get(p.district) ?? 0) + 1);
  });
  return { players, byDistrict, counts };
}

export function WealthCityMap({
  gameState,
  viewRef,
  focusRef,
  selectedDistrict,
  onSelectDistrict,
}: Props) {
  const controlsRef = useRef<OrbitControlsImpl | null>(null);
  const { players, byDistrict, counts } = useMemo(() => tokenLayout(gameState), [gameState]);
  const mySeat = gameState?.my_seat ?? -1;
  const marketById = useMemo(() => {
    const m = new Map<string, number>();
    // 2026-09-14 §财商流P0-bugfix: 半截可选链 `?.market.districts` 在
    // gameState 已到达但 market/districts 尚未填充(占位帧/竞态)时整页崩溃
    // (TypeError: null.forEach → ErrorBoundary)。双层防御。
    (gameState?.market?.districts ?? []).forEach((d) => m.set(d.id, d.price_index));
    return m;
  }, [gameState]);

  return (
    <div className="wealth-map-host">
      <Canvas
        shadows
        dpr={[1, 2]}
        camera={{ position: [14, 12, 14], fov: 45 }}
      >
        <color attach="background" args={['#0b0f16']} />
        <ambientLight intensity={0.7} />
        <directionalLight
          castShadow
          position={[10, 16, 8]}
          intensity={1.15}
          shadow-mapSize-width={1024}
          shadow-mapSize-height={1024}
        />
        <Ground />
        {WEALTH_DISTRICTS.map((d) => (
          <DistrictBlock
            key={d.id}
            def={d}
            priceIndex={marketById.get(d.id) ?? 1}
            playerCount={counts.get(d.id) ?? 0}
            selected={selectedDistrict === d.id}
            onSelect={onSelectDistrict}
          />
        ))}
        <Roads />
        {players.map((p, i) => {
          const inDistrict = byDistrict.get(p.district) ?? [];
          const index = inDistrict.indexOf(i);
          const c = districtCenter(p.district);
          return (
            <AgentToken
              key={`${p.seat}-${p.account}`}
              player={p}
              cx={c.x}
              cz={c.z}
              index={index < 0 ? 0 : index}
              total={inDistrict.length}
              isMe={p.seat === mySeat}
            />
          );
        })}
        <OrbitControls
          ref={controlsRef}
          enablePan
          enableZoom
          enableRotate
          maxPolarAngle={1.2}
          minDistance={8}
          maxDistance={40}
          target={[0, 0, 0]}
        />
        <CameraReporter controlsRef={controlsRef} viewRef={viewRef} />
        <FocusController controlsRef={controlsRef} focusRef={focusRef} />
      </Canvas>
      {/* 小地图由 WealthGamePage 以绝对定位叠加（左上角） */}
    </div>
  );
}
