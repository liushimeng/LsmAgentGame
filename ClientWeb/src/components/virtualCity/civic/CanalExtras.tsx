/**
 * 运河生活（18-AA · §5.2 CanalExtras）
 *   2 艘小艇（船体 + 篷，慢速巡航）+ 系船柱 6 只 + 两岸护栏。
 *
 * 批次 38 R7：原写死的「避开跨河桥 x=±6.2」**作废** —— 桥位改由
 * `canalBridgeSpots()` 按真实过河路段生成（可能在 x=±18 等处），
 * 系船柱 / 护栏 / 游船起点统一按桥位表避让。
 *
 * 批次 28 二轮（DC 攻坚）：40 mesh → 4 mesh（几何全等合并，engine3d/geoMerge）：
 *   1/2) 小艇 ×2：每艘（船体 + 船头锥 + 船篷）3 件合 1 mesh（顶点色）——
 *      useFrame 漂移组保留独立 mesh，子装配整体随组移动。
 *   3) 系船柱 mesh：两岸 12 只圆柱合并。
 *   4) 护栏 mesh：两岸矮柱合并（材质原值统一，零取舍）。
 * 批次 28 二轮取舍：小艇件原粗糙度即统一 0.7（零取舍）；caster 裁剪——
 *   运河设施不投影（shadow pass 实测 1044 DC 超 500 阈值，只留楼体+树干）。
 */
import React, { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { blenderModelsEnabled, cylPart, mergeParts, useSharedGLTF, type MergePart } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { u } from '../cityScale';
import { CANAL_Z, CANAL_HALF_X, CANAL_HALF_WIDTH, inWater, onRoadCorridor } from '../cityObstacles';
import { canalBridgeSpotsCached } from '../CanalBridge';
import { collectGlbPairs, GlbInstances } from '../glbInstances';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const HULL = '#a87055';
const CANOPY = '#c0392b';
const POST = '#3a3f4a';
const RAIL_GREY = '#7a8290';
const BANK_GREY = '#6d7684';
const REED_GREEN = '#5f8f4e';

/** 桥位横向避让半宽（桥宽半幅 + 0.6u 余量）。 */
const BRIDGE_KEEP_HALF = 1.6;
/**
 * `road/canal_bank.glb` 平铺段长（世界单位 0.800 u = 8.0 m；art 契约 +
 * verify_glb_aabb 实测：X 0.800 × Y 0.160 × Z 0.298，minY=0 贴地、节点 identity）。
 * 接缝约定：台阶居段中 ⇒ 每 8 m 一组亲水台阶，沿岸端到端平铺。
 */
const BANK_SEG = 0.8;
/**
 * 驳岸横河占深 0.298 u（2.98 m），水侧台阶挑出 0.228 u（2.28 m）进河面 ⇒
 * 段中心从水缘**向河心偏移** `0.228 − 0.298/2 = 0.079` u（0.79 m）。
 */
const BANK_INTO_WATER = 0.228;
const BANK_DEPTH = 0.298;
const BANK_Z_OFF = CANAL_HALF_WIDTH - (BANK_INTO_WATER - BANK_DEPTH / 2); // ≈1.421 u
/** 芦苇点缀间距（世界单位）。 */
const REED_SPACING = 5;
/** `road/canal_reed.glb` 尺寸（点缀式实例化；world ≈0.195×0.241×0.190 u）。 */
const REED_SCALE = 1;

export function CanalExtras() {
  // 批次 38 R7：按真实桥位表避让（替代写死 x=±6.2）
  const bridgeXs = useMemo(() => canalBridgeSpotsCached().map((s) => s.x), []);
  const nearBridge = (x: number) => bridgeXs.some((bx) => Math.abs(bx - x) < BRIDGE_KEEP_HALF);

  // 批次 38 §27.5：驳岸 / 芦苇 GLB 各一次加载 → 实例化平铺/点缀；缺失走程序化 fallback。
  const blenderOn = useMemo(() => blenderModelsEnabled(), []);
  const bankUrl = modelUrl('road', 'canal_bank');
  const reedUrl = modelUrl('road', 'canal_reed');
  const bankGltf = useSharedGLTF(bankUrl);
  const reedGltf = useSharedGLTF(reedUrl);
  const bankPairs = useMemo(
    () => (blenderOn ? collectGlbPairs(bankGltf?.scene ?? null) : []),
    [blenderOn, bankGltf],
  );
  const reedPairs = useMemo(
    () => (blenderOn ? collectGlbPairs(reedGltf?.scene ?? null) : []),
    [blenderOn, reedGltf],
  );

  const bollards = useMemo(() => {
    const out: Array<[number, number]> = [];
    for (let i = -3; i <= 3; i++) {
      if (i === 0) continue;
      const bx = i * 4.5;
      if (nearBridge(bx)) continue;
      out.push([bx, 0]);
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bridgeXs]);

  // 系船柱 6 只 ×2 岸（12 件合 1；沿 z=17±1.6 两岸）
  const bollardGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (const [bx] of bollards) {
      parts.push(cylPart(u(0.15), u(0.18), u(0.7), 8, bx, u(0.35), 17 + 1.6, POST));
      parts.push(cylPart(u(0.15), u(0.18), u(0.7), 8, bx, u(0.35), 17 - 1.6, POST));
    }
    return mergeParts(parts);
  }, [bollards]);
  useEffect(() => () => bollardGeo.dispose(), [bollardGeo]);

  // 两岸护栏矮柱（每岸 11 根，按桥位表避让）
  const railGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (const bx of [-12, -8, -4, 0, 4, 8, 12, 16, 20, 24, 28]) {
      if (nearBridge(bx)) continue;
      parts.push(cylPart(u(0.04), u(0.04), u(1.2), 6, bx, u(0.6), 17 + 2.0, RAIL_GREY));
      parts.push(cylPart(u(0.04), u(0.04), u(1.2), 6, bx, u(0.6), 17 - 2.0, RAIL_GREY));
    }
    return mergeParts(parts);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bridgeXs]);
  useEffect(() => () => railGeo.dispose(), [railGeo]);

  // 游船巡航起点：候选位中取离桥最远的两个（确定性，无随机）
  const boatSpots = useMemo(() => {
    const cands = [-9.5, -2, 16, 30, -22, 24, -30, 36];
    const scored = cands.map((x) => ({
      x,
      d: Math.min(...bridgeXs.map((bx) => Math.abs(bx - x)), 99),
    }));
    scored.sort((a, b) => b.d - a.d || a.x - b.x);
    return [scored[0].x, scored[1].x];
  }, [bridgeXs]);

  // 批次 38 R3 · §4.7(b)：驳岸 + 芦苇点位（岸线 z = CANAL_Z ± 挑水偏移；
  // 全部点位过 inWater/岸线判定 + 路廊净空 + 桥位避让，芦苇不长在路中间）
  const bankSpots = useMemo(() => {
    const out: Array<{ x: number; z: number; side: 1 | -1 }> = [];
    // 段长 0.8 u：段心从 −(HALF_X−seg/2) 起每 0.8 u 一段，端到端铺满两岸
    const n = Math.max(1, Math.round((CANAL_HALF_X * 2) / BANK_SEG));
    for (let i = 0; i < n; i++) {
      const x = -CANAL_HALF_X + (i + 0.5) * BANK_SEG;
      if (nearBridge(x)) continue;
      for (const side of [1, -1] as const) {
        out.push({ x, z: CANAL_Z + side * BANK_Z_OFF, side });
      }
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bridgeXs]);

  const reedSpots = useMemo(() => {
    const out: Array<{ x: number; z: number }> = [];
    const zOff = CANAL_HALF_WIDTH + 0.25;
    let i = 0;
    for (let x = -CANAL_HALF_X + 2; x <= CANAL_HALF_X - 2; x += REED_SPACING) {
      i++;
      if (i % 3 !== 0) continue; // 稀疏点缀（确定性，无随机）
      if (nearBridge(x)) continue;
      for (const side of [1, -1] as const) {
        const z = CANAL_Z + side * zOff;
        // 岸线判定：不落水域中央、不占路廊
        if (inWater(x, z, 0)) continue;
        if (onRoadCorridor(x, z, 0.2)) continue;
        out.push({ x, z });
      }
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bridgeXs]);

  // GLB 实例矩阵（驳岸：段长沿 +X 无需旋转；南岸绕 Y π 让台阶朝河心）。
  const bankMatrices = useMemo(() => {
    const out: THREE.Matrix4[] = [];
    for (const b of bankSpots) {
      out.push(
        new THREE.Matrix4().compose(
          new THREE.Vector3(b.x, 0, b.z),
          new THREE.Quaternion().setFromEuler(new THREE.Euler(0, b.side > 0 ? 0 : Math.PI, 0)),
          new THREE.Vector3(1, 1, 1),
        ),
      );
    }
    return out;
  }, [bankSpots]);
  const reedMatrices = useMemo(() => {
    const out: THREE.Matrix4[] = [];
    reedSpots.forEach((r, ri) => {
      out.push(
        new THREE.Matrix4().compose(
          new THREE.Vector3(r.x, 0, r.z),
          new THREE.Quaternion().setFromEuler(new THREE.Euler(0, (ri * 0.7) % (Math.PI * 2), 0)),
          new THREE.Vector3(REED_SCALE, REED_SCALE, REED_SCALE),
        ),
      );
    });
    return out;
  }, [reedSpots]);

  // 批次 28 B2：运河配套信息交互（系船柱/护栏/游船事件冒泡至根组）。
  const info = useObjectInfoProps('civic.canal-extras', { anchorY: 0.8 });

  return (
    <group {...info}>
      {/* 船 1：沿 +x 漂移折返 */}
      <Boat start={boatSpots[0]} range={boatSpots[0] > 0 ? -16 : 18} direction={boatSpots[0] > 0 ? -1 : 1} />
      {/* 船 2：沿 -x 漂移折返 */}
      <Boat start={boatSpots[1]} range={boatSpots[1] > 0 ? -16 : 18} direction={boatSpots[1] > 0 ? -1 : 1} />
      {/* 系船柱合 1（caster 裁剪不投影） */}
      <mesh geometry={bollardGeo}>
        <meshStandardMaterial vertexColors metalness={0.5} roughness={0.5} />
      </mesh>
      {/* 两岸护栏矮柱合 1（材质原值：metal 0.5 / 默认 roughness 1.0） */}
      <mesh geometry={railGeo}>
        <meshStandardMaterial vertexColors metalness={0.5} roughness={1.0} />
      </mesh>
      {/* 批次 38 §4.7(b)：驳岸挡墙 —— canal_bank.glb 8 m/段沿两岸平铺
          （实例化，1 draw call/对；GLB 缺失降级程序化条石）。 */}
      {bankPairs.length > 0 ? (
        <GlbInstances pairs={bankPairs} worldMatrices={bankMatrices} receiveShadow />
      ) : (
        bankSpots.map((b) => (
          <mesh key={`bank-fb-${b.side}-${b.x}`} position={[b.x, u(0.35), b.z]} receiveShadow>
            <boxGeometry args={[BANK_SEG, u(0.7), u(0.5)]} />
            <meshStandardMaterial color={BANK_GREY} roughness={0.9} />
          </mesh>
        ))
      )}
      {/* 批次 38 §4.7(b)：芦苇点缀 —— canal_reed.glb 点缀式实例化（无需平铺）。 */}
      {reedPairs.length > 0 ? (
        <GlbInstances pairs={reedPairs} worldMatrices={reedMatrices} />
      ) : (
        reedSpots.map((r, ri) => (
          <group key={`reed-fb-${ri}`} position={[r.x, 0, r.z]} rotation={[0, (ri * 0.7) % (Math.PI * 2), 0]}>
            {[0, 1, 2].map((k) => (
              <mesh key={k} position={[(k - 1) * u(0.25), u(0.45), 0]}>
                <coneGeometry args={[u(0.12), u(0.9), 5]} />
                <meshStandardMaterial color={REED_GREEN} roughness={0.85} />
              </mesh>
            ))}
          </group>
        ))
      )}
    </group>
  );
}

function Boat({ start, range, direction }: { start: number; range: number; direction: 1 | -1 }) {
  // 沿 x 在 [min(start, range), max(start, range)] 之间往复，1.5 单位/秒
  const ref = React.useRef<THREE.Group>(null);
  useFrame((_, delta) => {
    if (!ref.current) return;
    const lo = Math.min(start, range);
    const hi = Math.max(start, range);
    let x = ref.current.position.x + delta * 1.5 * direction;
    if (x > hi) {
      x = hi;
      ref.current.userData.dir = -direction;
    } else if (x < lo) {
      x = lo;
      ref.current.userData.dir = -direction;
    }
    ref.current.position.x = x;
    // 朝向根据方向
    ref.current.rotation.y = direction > 0 ? 0 : Math.PI;
  });
  // 船体 3 件合 1 mesh（船体 box + 船头锥绕 Z -π/8 + 船篷；粗糙度原统一 0.7）
  const hullGeo = useMemo(
    () =>
      mergeParts([
        // 船体：半柱壳（用 box 简化）
        { geo: new THREE.BoxGeometry(u(2.5), u(0.4), u(0.9)), x: 0, y: 0, z: 0, color: HULL },
        // 船头尖（绕 Z 倾斜 -π/8）
        { geo: new THREE.ConeGeometry(u(0.5), u(1.2), 3), rotZ: -Math.PI / 8, x: u(1.4), y: u(0.1), z: 0, color: HULL },
        // 船篷
        { geo: new THREE.BoxGeometry(u(1.4), u(0.5), u(0.85)), x: -u(0.3), y: u(0.7), z: 0, color: CANOPY },
      ]),
    [],
  );
  useEffect(() => () => hullGeo.dispose(), [hullGeo]);
  return (
    <group ref={ref} position={[start, u(0.15), 17]}>
      {/* 小艇（顶点色；caster 裁剪不投影；组随 useFrame 漂移） */}
      <mesh geometry={hullGeo}>
        <meshStandardMaterial vertexColors roughness={0.7} />
      </mesh>
    </group>
  );
}
