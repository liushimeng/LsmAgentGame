/**
 * WealthCityMap — r3f 主场景（08-UI优化 v2）：
 *
 * P1-A 改造：
 *   - 删除 gridHelper 黑线（line 56 of v1），地面改用 RepeatWrapping 沥青贴图。
 *   - 单 plane Roads() 替换为分层 <Road />（4 层组合 + 路灯阵列）。
 *   - 追加 hemisphereLight（天/地反弹）+ <fog />（远景雾化）。
 *
 * P1-C 追加：
 *   - <StreetPropsLayer /> 在 Token 之前注入，绘制路灯、树、车辆、行人、屋顶杂物。
 *
 * v2.12 阶段 2（16 城区扩展）：
 *   - 地图 40×40 → 80×80（WORLD_SIZE 常量，地面贴图 / 雾化 / 相机 / 光照等比派生）。
 *   - <StreetPropsLayer districts={...} /> props 化（内部不再 import 城区静态表）。
 *
 * v2.13 阶段 C（13-3D城市渲染优化 · 环境氛围，02-架构设计 §1）：
 *   - drei <Sky /> 天空穹顶接管背景（删除纯色 <color>），sunPosition 与主方向光同向。
 *   - ACESFilmicToneMapping + exposure 1.05；阴影 PCFSoft + 2048 + bias/normalBias，
 *     显式阴影相机 ±WORLD_SIZE*0.6 覆盖全城（默认 ±5 曾导致大部分楼无阴影）。
 *   - 雾色随天际线改 #aeb8c6；ambient 0.45 / hemisphere 0.5 / 日光暖白 #fff2e0。
 *
 * 相机 / OrbitControls / CameraReporter / FocusController 行为不变（与 v1 完全兼容）。
 */

import { useMemo, useRef } from 'react';
import * as THREE from 'three';
import { Canvas, useFrame, useThree } from '@react-three/fiber';
import { OrbitControls, Sky } from '@react-three/drei';
import type { OrbitControls as OrbitControlsImpl } from 'three-stdlib';
import { DistrictBlock } from './DistrictBlock';
import { AgentToken } from './AgentToken';
import { Road } from './Road';
import { StreetPropsLayer } from './StreetPropsLayer';
import { WaterPlane } from './props/WaterPlane';
import { WaterMist } from './props/WaterMist';
import { AtmosphereLayer } from './AtmosphereLayer';
import { useSharedTexture } from './textureCache';
import { streetTileUrl } from '@/assets/images/wealth';
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

// ── v2.12 阶段 2 世界尺寸常量（地图 40×40 → 80×80，面积 ×4）──────────
// 所有 40 相关魔法数收敛于此；地面贴图 / 雾化 / 相机 / 光照按比例派生。

/** 世界边长（世界单位；1 单位 = 10 米，见 cityScale.ts）。 */
export const WORLD_SIZE = 80;
/** 地面贴图每 8 单位平铺一次（与城区底板 8×8 同标尺）。 */
export const GROUND_TILE = 8;
/** 地面贴图重复次数 = WORLD_SIZE / GROUND_TILE。 */
export const GROUND_REPEAT = WORLD_SIZE / GROUND_TILE;
/** 远景雾化近/远平面（随世界边长等比 ×2，与背景色一致自然消失）。 */
export const FOG_NEAR = WORLD_SIZE * 0.7;
export const FOG_FAR = WORLD_SIZE * 1.5;
/** 相机初始位置与 OrbitControls maxDistance（随世界边长等比缩放）。 */
export const CAMERA_START: [number, number, number] = [WORLD_SIZE * 0.35, WORLD_SIZE * 0.3, WORLD_SIZE * 0.35];
export const ORBIT_MAX_DISTANCE = WORLD_SIZE;

// ── v2.13 阶段 C 光照/天空常量 ────────────────────────────────
/** 主方向光位置（世界坐标）。 */
const SUN_POSITION: [number, number, number] = [20, 32, 16];
/** Sky 太阳方向：与主方向光同向归一化 ×100（[20,32,16] / |[20,32,16]| ≈ [0.516,0.826,0.413]）。 */
const SKY_SUN_POSITION: [number, number, number] = [51.6, 82.6, 41.3];
/** 方向光阴影相机半宽：覆盖全城（±WORLD_SIZE*0.6；默认 ±5 只能罩住原点一小块）。 */
const SHADOW_CAMERA_HALF = WORLD_SIZE * 0.6;
/** 雾色（与 Sky 地平线色接近，远景自然消隐）。 */
const FOG_COLOR = '#aeb8c6';

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

/**
 * 地面：80×80 plane + RepeatWrapping 沥青贴图（缺失 → 纯色 #141a24）。
 * 旧 gridHelper 已删除（消除黑线）。v2.12 阶段 2：40×40 → 80×80（面积 ×4）。
 */
function Ground() {
  // 14-3D渲染深化：共享贴图缓存（缺失 → 纯色 #141a24 降级链不变）
  const tex = useSharedTexture(streetTileUrl('asphalt_main'), {
    wrap: 'repeat',
    repeat: [GROUND_REPEAT, GROUND_REPEAT], // 80 / 8 = 10
  });

  return (
    <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0, 0]} receiveShadow>
      <planeGeometry args={[WORLD_SIZE, WORLD_SIZE]} />
      <meshStandardMaterial
        map={tex ?? undefined}
        color={tex ? '#ffffff' : '#141a24'}
        roughness={0.95}
        metalness={0.02}
      />
    </mesh>
  );
}

/**
 * 道路层：从每个非 finance 城区中心辐射到原点（金融 CBD）。
 * 主干道 vs 次干道按 from→to 距离判：len > 12 → main，否则 side。
 */
function RoadsLayer() {
  const roads = useMemo(() => {
    return WEALTH_DISTRICTS
      .filter((d) => d.id !== 'finance')
      .map((d) => {
        const c = districtCenter(d.id);
        const dx = 0 - c.x;
        const dz = 0 - c.z;
        const len = Math.sqrt(dx * dx + dz * dz);
        return {
          key: d.id,
          from: [c.x, c.z] as [number, number],
          to: [0, 0] as [number, number],
          len,
        };
      });
  }, []);

  return (
    <>
      {roads.map((r) => (
        <Road
          key={r.key}
          from={r.from}
          to={r.to}
          kind={r.len > 12 ? 'main' : 'side'}
        />
      ))}
    </>
  );
}

/**
 * 水系层（14-3D城市渲染深化 · 阶段 I + 15-3D城市全面真实感深化 · 阶段 P）：
 *   - 城市运河 + 物流港港池 + 两岸草皮收边（14 阶段 I）
 *   - 水面岸雾 WaterMist（15 阶段 P）：沿运河/港池两岸各加半透明雾 plane
 * 位置契约 01 文档 §3.1：运河 z=+17（滨河新区 riverside(14,10) 与教育/医疗城 z=22 之间），
 * 港池 (-30,-4) 物流港西侧。贴图缺失降级纯色水面（WaterPlane 内处理）。
 */
function WaterLayer() {
  const bankTex = useSharedTexture(streetTileUrl('sidewalk_side'), {
    wrap: 'repeat',
    repeat: [16, 1],
  });
  return (
    <>
      {/* 城市运河（64×3，横贯 x ∈ [-32, 32]） */}
      <WaterPlane x={0} z={17} w={64} d={3} />
      {/* 物流港港池（6×8） */}
      <WaterPlane x={-30} z={-4} w={6} d={8} />
      {/* 两岸草皮收边（窄条，色 #3f7a3a 与中央公园草地呼应） */}
      {[-1, 1].map((side) => (
        <mesh
          key={`bank-${side}`}
          rotation={[-Math.PI / 2, 0, 0]}
          position={[0, 0.024, 17 + side * 1.85]}
          receiveShadow
        >
          <planeGeometry args={[64, 0.7]} />
          <meshStandardMaterial
            map={bankTex ?? undefined}
            color={bankTex ? '#ffffff' : '#3f7a3a'}
            roughness={0.95}
          />
        </mesh>
      ))}
      {/* 阶段 P：运河两岸薄雾（覆盖水体外缘各 1.5 单位的过渡带） */}
      <WaterMist x={0} z={17 + 2.35} w={64} d={1.5} opacity={0.18} />
      <WaterMist x={0} z={17 - 2.35} w={64} d={1.5} opacity={0.18} />
      {/* 港池四周薄雾 */}
      <WaterMist x={-30 + 3.85} z={-4} w={1.5} d={8} opacity={0.15} />
      <WaterMist x={-30 - 3.85} z={-4} w={1.5} d={8} opacity={0.15} />
      <WaterMist x={-30} z={-4 + 4.85} w={6} d={1.5} opacity={0.15} />
      <WaterMist x={-30} z={-4 - 4.85} w={6} d={1.5} opacity={0.15} />
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
        dpr={[1, 1.75]}
        camera={{ position: CAMERA_START, fov: 45 }}
        gl={{ antialias: true, powerPreference: 'high-performance' }}
        onCreated={({ gl }) => {
          // v2.13 阶段 C：胶片色调映射（高光不过曝）+ 柔和阴影边缘
          gl.toneMapping = THREE.ACESFilmicToneMapping;
          gl.toneMappingExposure = 1.05;
          gl.shadowMap.type = THREE.PCFSoftShadowMap;
        }}
      >
        {/* v2.13 阶段 C：Sky 天空穹顶接管背景（删除纯色 <color>），太阳方向与主方向光一致 */}
        <Sky sunPosition={SKY_SUN_POSITION} turbidity={6} rayleigh={1.2} />
        {/* P1-A 新增：远景雾化（v2.12 随 80×80 地图等比 ×2；v2.13 雾色随天际线） */}
        <fog attach="fog" args={[FOG_COLOR, FOG_NEAR, FOG_FAR]} />
        <ambientLight intensity={0.45} />
        {/* P1-A 新增：天/地反弹 */}
        <hemisphereLight args={['#7a93b8', '#1a1f2a', 0.5]} />
        {/* v2.12 阶段 2：光位随世界边长等比 ×2（方向向量不变，阴影形态不变）；
            v2.13 阶段 C：2048 shadow map + bias/normalBias + 显式阴影相机覆盖全城 + 暖白日光 */}
        <directionalLight
          castShadow
          position={SUN_POSITION}
          color="#fff2e0"
          intensity={1.15}
          shadow-mapSize-width={2048}
          shadow-mapSize-height={2048}
          shadow-bias={-0.0002}
          shadow-normalBias={0.02}
          shadow-camera-left={-SHADOW_CAMERA_HALF}
          shadow-camera-right={SHADOW_CAMERA_HALF}
          shadow-camera-top={SHADOW_CAMERA_HALF}
          shadow-camera-bottom={-SHADOW_CAMERA_HALF}
          shadow-camera-far={WORLD_SIZE * 2}
        />
        <Ground />
        {/* 14-3D渲染深化：城市水系（运河 + 港池 + 岸草皮） */}
        <WaterLayer />
        {/* 15-3D渲染深化：氛围层（远景剪影 + 公园落叶 Sparkles） */}
        <AtmosphereLayer />
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
        {/* P1-A：单 plane → 分层 <Road /> 道路（含路灯阵列） */}
        <RoadsLayer />
        {/* P1-C：街道道具层（树 / 车辆 / 行人 / 标识 / 屋顶杂物）；
            v2.12 阶段 2：districts 由父层注入（props 化，适配 16 城区） */}
        <StreetPropsLayer districts={WEALTH_DISTRICTS} />
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
          enableDamping
          dampingFactor={0.08}
          maxPolarAngle={1.2}
          minDistance={8}
          maxDistance={ORBIT_MAX_DISTANCE}
          target={[0, 0, 0]}
        />
        <CameraReporter controlsRef={controlsRef} viewRef={viewRef} />
        <FocusController controlsRef={controlsRef} focusRef={focusRef} />
      </Canvas>
      {/* 小地图由 WealthGamePage 以绝对定位叠加（左上角） */}
    </div>
  );
}