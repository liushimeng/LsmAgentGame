/**
 * CanalBridge — 跨运河桥（批次 38 §4.4 重写：桥位/朝向/桥长改由**真实过河路段**驱动）。
 *
 * 根因（批次 38 §2 R7）：旧 `CanalBridgeSpots()` 无参、按**已删除的 spoke 径向**
 * 与 z=17 求交（`xAt = c.x*(CANAL_Z/c.z)`、`rotation = atan2(-c.x,-c.z)`），
 * 与现行路网完全脱钩 ——「路过的没桥，桥立着的没路」，且桥轴沿径向导致斜切水面。
 *
 * 新契约：
 *   - `canalBridgeSpots(net)` 接收真实 `RoadNetwork`，对**全部**路段与 z=CANAL_Z
 *     求交（南岸不再跳过），交点落在河道内即建桥；
 *   - **桥轴强制垂直河道**：运河东西走向（z=17 直线）⇒ rotation 恒为 0（桥长沿 +Z）；
 *   - 桥长按路段与河道法线（+Z）夹角派生：`max(3/cos(夹角) + 1.8, 4.2)`；
 *   - 桥宽取过河路段路幅（main 1.4 / side 0.9 / 一环 1.4）；
 *   - 距离 <2u 的过河点合并；一环路过河点（r=20 × z=17）一并出桥（§4.6）。
 *
 * 桥面 y_top 0.035 高于水面 0.028；过河路段本身不渲染路面（Road.tsx §4.6），
 * 视觉上「桥就是路的延续」。
 *
 * 批次 28 二轮合并语义保留：栏杆/端柱/桥墩/灯杆 1 mesh + 暖光灯球 1 mesh；
 * caster 裁剪（桥体不投影）。
 */

import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { streetTileUrl } from '@/assets/images/virtualCity';
import { modelUrl } from '@/assets/models';
import { VIRTUAL_CITY_DISTRICTS } from '@/types/virtualCity';
import {
  blenderModelsEnabled,
  useSharedGLTF,
  useSharedTexture,
  type GroupedMergePart,
  type MergePart,
  boxPart,
  cylPart,
  mergeGrouped,
  mergeParts,
} from '@/engine3d';
import { collectGlbPairs, GlbInstances, type GlbRenderPair } from './glbInstances';
import { useObjectInfoProps } from './objectInfo/useObjectInfoProps';
import {
  buildRoadNetwork,
  FIRST_RING_RADIUS,
  FIRST_RING_WIDTH,
  type RoadNetwork,
} from './roadNetwork';
import { ROAD_WIDTH_MAIN, ROAD_WIDTH_SIDE } from './cityScale';
import { CANAL_Z, CANAL_HALF_X, inWater } from './cityObstacles';

/** 桥面厚。顶面 = DECK_TOP_Y + DECK_H/2 ≈ 0.035：高于水面 0.028、仅高于路面一线。 */
const DECK_H = 0.03;
/** 桥面中心 y（顶面 0.035）。 */
const DECK_TOP_Y = 0.02;
/** 两岸引桥（桥面超出水缘的接路段），世界单位 0.9u = 9m/侧。 */
const APPROACH = 0.9;
/** 桥长下限（垂直跨最短：水宽 3 + 两侧引桥，按 §4.4 口径取 4.2）。 */
const MIN_LENGTH = 4.2;
/** 过河点合并距离（<2u 视为同一座桥）。 */
const MERGE_DIST = 2.0;
/**
 * `road/bridge_rail.glb` 平铺段长（世界单位 0.200 u = 2.0 m；art 契约 + verify_glb_aabb 实测）。
 * 接缝约定：段中心（x=0）是立柱，横杆跨满 2.0 m ⇒ 沿桥长端到端平铺即无缝
 * （相邻段横杆对头顶接；立柱在段中心，不在接缝处重合 z-fight）。
 */
const RAIL_SEG = 0.2;
/** 桥面顶 y（= DECK_TOP_Y + DECK_H/2）；GLB minY=0 贴桥面落位。 */
const DECK_TOP = DECK_TOP_Y + DECK_H / 2;
/** 程序化 fallback 纵梁中心 y（GLB 缺失时的原视觉）。 */
const FALLBACK_RAIL_Y = DECK_TOP + 0.09;

interface Props {
  /** 桥中心世界坐标。 */
  x: number;
  z: number;
  /** 桥轴朝向（弧度；批次 38 起恒为 0 —— 桥轴垂直东西向河道）。 */
  rotation: number;
  /** 桥面长（u；由过河路段与河道夹角派生）。 */
  length: number;
  /** 桥面宽（u；取过河路段路幅）。 */
  width?: number;
  /** 收尾衔接的路段 key（objectInfo 动态行）。 */
  roadKey?: string;
  /** GLB 栏杆不可用时画程序化纵梁（由 CanalBridges 按全局 GLB 状态统一下发）。 */
  fallbackRails?: boolean;
}

/** 单座桥。 */
export function CanalBridge({ x, z, rotation, length, width = ROAD_WIDTH_MAIN, roadKey, fallbackRails = true }: Props) {
  const asphalt = useSharedTexture(streetTileUrl('asphalt_main'), {
    wrap: 'repeat',
    repeat: [1, 2],
  });
  const info = useObjectInfoProps('road.bridge', {
    anchorY: 0.8,
    extra: roadKey ? [{ label: 'road', value: roadKey }] : undefined,
  });

  // ── 批次 28 二轮：静态件合并（几何逐件全等；灯组原嵌套 group 偏移烘进部件坐标）──
  // 批次 38：栏杆纵梁移出合并组 —— GLB 路径由 <BridgeRails> 全城实例化平铺
  // （2 m/段 × 桥长，两侧各一行），GLB 缺失时本组件回退程序化纵梁。
  const geos = useMemo(() => {
    const halfW = width / 2;
    const solids: MergePart[] = [];
    // 栏杆端柱 ×4
    for (const side of [-1, 1]) for (const end of [-1, 1]) {
      solids.push(boxPart(0.09, 0.22, 0.09, side * (halfW - 0.03), FALLBACK_RAIL_Y + 0.02, end * (length / 2 - 0.12), '#8a919c'));
    }
    // 桥墩 ×4（入水）
    for (const sx of [-1, 1]) for (const sz of [-1, 1]) {
      solids.push(boxPart(0.3, 0.5, 0.3, sx * (halfW * 0.6), -0.2, sz * (length / 2 - 0.35), '#6b7280'));
    }
    // 桥头灯立柱 ×2（暖光灯球另入 glow 组）
    for (const side of [-1, 1]) {
      solids.push(cylPart(0.025, 0.035, 0.56, 6, side * (halfW + 0.12), 0.28, -length / 2 + 0.15, '#4a5260'));
    }
    const glow: GroupedMergePart[] = [];
    for (const side of [-1, 1]) {
      glow.push({
        geo: new THREE.SphereGeometry(0.05, 8, 6),
        x: side * (halfW + 0.12), y: 0.58, z: -length / 2 + 0.15,
        mat: 0,
      });
    }
    return { solids: mergeParts(solids), glow: mergeGrouped(glow) };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [length, width]);
  useEffect(() => () => {
    geos.solids.dispose();
    geos.glow.dispose();
  }, [geos]);

  return (
    <group {...info} position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 桥面（贴图独立，接影） */}
      <mesh position={[0, DECK_TOP_Y, 0]} receiveShadow>
        <boxGeometry args={[width, DECK_H, length]} />
        <meshStandardMaterial
          map={asphalt ?? undefined}
          color={asphalt ? '#ffffff' : '#2a3240'}
          roughness={0.9}
          metalness={0.05}
        />
      </mesh>
      {/* 程序化纵梁 fallback（GLB 缺失/加载失败/关闭 Blender 时；与原视觉一致） */}
      {fallbackRails && (
        <>
          {[-1, 1].map((side) => (
            <mesh key={`rail-fb-${side}`} position={[side * (width / 2 - 0.03), FALLBACK_RAIL_Y, 0]}>
              <boxGeometry args={[0.06, 0.18, length]} />
              <meshStandardMaterial color="#9aa1ab" roughness={0.65} metalness={0.35} />
            </mesh>
          ))}
        </>
      )}
      {/* 端柱/桥墩/灯杆 —— 顶点色合并 1 mesh（批次 28 二轮） */}
      <mesh geometry={geos.solids}>
        <meshStandardMaterial vertexColors roughness={0.65} metalness={0.35} envMapIntensity={0.8} />
      </mesh>
      {/* 桥头灯暖光球 ×2 —— emissive 合并 1 mesh（材质逐字段与原一致） */}
      <mesh geometry={geos.glow}>
        <meshStandardMaterial color="#ffd9a0" emissive="#ffd9a0" emissiveIntensity={0.55} roughness={0.3} />
      </mesh>
    </group>
  );
}

/**
 * 全城桥栏杆平铺（批次 38 §4.7(b)）：`road/bridge_rail.glb` 每段 2.0 m，
 * 沿桥长 `round(L/0.2)` 段、两侧各一行；**全城合一批 InstancedMesh**
 * （1 draw call/geometry-material 对，不逐段 mount React）。
 * GLB 缺失时不渲染本层（由各桥的 fallbackRails 程序化纵梁承接）。
 */
function BridgeRails({ spots, pairs }: { spots: BridgeSpot[]; pairs: GlbRenderPair[] }) {
  const matrices = useMemo(() => {
    const out: THREE.Matrix4[] = [];
    const q = new THREE.Quaternion();
    // GLB 段长沿 local +X；桥长沿 world +Z ⇒ 绕 Y 转 π/2 对齐
    q.setFromEuler(new THREE.Euler(0, Math.PI / 2, 0));
    for (const s of spots) {
      const n = Math.max(1, Math.round(s.length / RAIL_SEG));
      for (const side of [-1, 1] as const) {
        for (let i = 0; i < n; i++) {
          // 段中心 (i+0.5)*seg − L/2：端到端平铺覆盖 [−L/2, +L/2]（art 接缝约定）
          const localZ = (i + 0.5) * RAIL_SEG - s.length / 2;
          out.push(
            new THREE.Matrix4().compose(
              new THREE.Vector3(
                s.x + side * (s.width / 2 - 0.03),
                DECK_TOP,
                s.z + localZ,
              ),
              q,
              new THREE.Vector3(1, 1, 1),
            ),
          );
        }
      }
    }
    return out;
  }, [spots]);
  return <GlbInstances pairs={pairs} worldMatrices={matrices} receiveShadow />;
}

export interface BridgeSpot {
  key: string;
  x: number;
  z: number;
  /** 桥轴朝向：批次 38 起恒为 0（垂直东西向河道）。 */
  rotation: number;
  /** 桥面长（u）。 */
  length: number;
  /** 桥面宽（u）。 */
  width: number;
  /** 收尾衔接的路段 key。 */
  roadKey: string;
}

/**
 * 通式求桥位（批次 38 §4.4）：对**全部**路段（含南岸）与运河 z=CANAL_Z 求交，
 * 交点落在河道内即建桥；一环路（r=20）与 z=17 的两个交点一并出桥。
 * 桥轴强制垂直河道（rotation 恒 0），桥长按路段与河道法线夹角派生。
 *
 * **禁止**再用径向口径 `atan2(-c.x,-c.z)` / `xAt = c.x*(CANAL_Z/c.z)`（§5.3-4）。
 */
export function canalBridgeSpots(net: RoadNetwork): BridgeSpot[] {
  const spots: BridgeSpot[] = [];
  const zLine = CANAL_Z;

  const push = (
    x: number,
    dirX: number,
    dirZ: number,
    width: number,
    roadKey: string,
    key: string,
  ) => {
    if (Math.abs(x) > CANAL_HALF_X) return;
    if (!inWater(x, zLine, 0)) return; // 只在河道内建桥
    // 路段方向与河道法线（+Z）的夹角：cos = |dirZ| / |dir|（方向归一后）
    const lenDir = Math.hypot(dirX, dirZ) || 1;
    const cosAngle = Math.min(1, Math.abs(dirZ) / lenDir);
    // 夹角越大（路段越斜）桥越长：水宽 3u / cos + 两岸引桥 2×0.9u，下限 4.2u
    const length = Math.max(3 / Math.max(cosAngle, 1e-3) + 2 * APPROACH, MIN_LENGTH);
    spots.push({
      key,
      x,
      z: zLine,
      rotation: 0, // 垂直河道：桥轴沿 +Z（§4.4 硬约束 1）
      length,
      width,
      roadKey,
    });
  };

  // ① 真实过河路段（connector / arterial / edgeLink 全部；南岸不再跳过）
  for (const s of net.segments) {
    const [ax, az] = s.from;
    const [bx, bz] = s.to;
    const dz = bz - az;
    if (Math.abs(dz) < 1e-9) continue; // 平行河道，无交点
    const t = (zLine - az) / dz;
    if (t <= 0.001 || t >= 0.999) continue; // 端点恰在河上不算独立过河点
    const x = ax + (bx - ax) * t;
    const width = s.kind === 'main' ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE;
    push(x, bx - ax, dz, width, s.key, `bridge-${s.key}`);
  }

  // ② 一环路过河点（r=20 圆 × z=17 ⇒ x=±√(r²−17²)；§4.6「该两段由桥承载」）
  const disc = FIRST_RING_RADIUS * FIRST_RING_RADIUS - zLine * zLine;
  if (disc > 0) {
    const xr = Math.sqrt(disc);
    for (const sx of [-1, 1]) {
      const x = sx * xr;
      // 交点处切向（圆上逆时针）：(-z, x)/r —— 路段方向即切向
      push(x, -zLine, x, FIRST_RING_WIDTH, 'first-ring', `bridge-first-ring-${sx > 0 ? 'e' : 'w'}`);
    }
  }

  // 距离 <2u 的过河点合并（保留先到者；表序确定性）
  const out: BridgeSpot[] = [];
  for (const s of spots) {
    if (out.some((o) => Math.hypot(o.x - s.x, o.z - s.z) < MERGE_DIST)) continue;
    out.push(s);
  }
  return out;
}

/**
 * 惰性缓存的默认桥位表（CanalExtras 避让复用；与 CanalBridges(带 net) 同输入同输出）。
 * 未接线 net 时自建同规约路廊 —— 不允许「空表 = 不避让」的静默降级。
 */
let cachedSpots: BridgeSpot[] | null = null;
export function canalBridgeSpotsCached(net?: RoadNetwork): BridgeSpot[] {
  if (!cachedSpots) {
    cachedSpots = canalBridgeSpots(net ?? buildRoadNetwork(VIRTUAL_CITY_DISTRICTS, 0));
  }
  return cachedSpots;
}

/** 全部运河桥（VirtualCityCityMap 单挂载点；net 由父层注入，缺省自建）。 */
export function CanalBridges({ net }: { net?: RoadNetwork }) {
  const spots = useMemo(() => canalBridgeSpotsCached(net), [net]);
  // 批次 38：桥栏杆 GLB 全城一次加载 → 平铺实例化；缺失走各桥程序化 fallback。
  const url = modelUrl('road', 'bridge_rail');
  const { scene } = useSharedGLTF(url);
  const blenderOn = useMemo(() => blenderModelsEnabled(), []);
  const railPairs = useMemo(
    () => (blenderOn ? collectGlbPairs(scene ?? null) : []),
    [blenderOn, scene],
  );
  const railsReady = railPairs.length > 0;
  return (
    <>
      {spots.map((s) => (
        <CanalBridge
          key={s.key}
          x={s.x}
          z={s.z}
          rotation={s.rotation}
          length={s.length}
          width={s.width}
          roadKey={s.roadKey}
          fallbackRails={!railsReady}
        />
      ))}
      {railsReady && <BridgeRails spots={spots} pairs={railPairs} />}
    </>
  );
}
