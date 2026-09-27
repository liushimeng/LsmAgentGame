/**
 * 学校操场（18-AA · §5.2 SportsField）
 *   椭圆跑道（赭红 + 白分道线）+ 内场绿茵 + 足球门 ×2 + 看台 3 级台阶 + 篮球场 1 块。
 *   布点 (-16.5, 22)，占地 7×4.5 世界单位（02 §5.5 已预检）。
 *
 * 批次 28 二轮（DC 攻坚）：18 mesh → 4 mesh（几何全等合并，engine3d/geoMerge）：
 *   1) 内场草茵 mesh：foliage PBR 保持独立（贴图保留）。
 *   2) 看台 mesh：3 级台阶合并，concrete PBR 保留（材质原值统一，零取舍）。
 *   3) 纯色件 mesh（顶点色）：跑道 torus + 足球门 ×2（梁+双柱）+ 篮球场 plane +
 *      篮架（柱+板）共 9 件 —— 颜色经顶点色逐件保留。
 *   4) 分道白线 mesh：4 条 ring 切片合并（basic 半透明 + DoubleSide）。
 * 批次 28 二轮取舍：纯色件粗糙度统一 0.9（原 0.85–1.0 区间，金属度原全 0 不变）；
 *   caster 裁剪——操场设施不投影（shadow pass 实测 1044 DC 超 500 阈值，
 *   只留楼体+树干），receiveShadow 保留（地面/看台仍接收楼体投影）。
 */
import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { boxPart, mergeParts, type MergePart } from '@/engine3d';
import { useCivicPBR } from './CivicPBR';
import { u } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const TRACK_RED = '#a85040';
const LINE_WHITE = '#e8e8e8';
const FIELD_GREEN = '#5d9a4e';
const GOAL_WHITE = '#d8d4ca';
const STAND_GREY = '#a8a4a0';
const COURT_TAN = '#c4a945';

/** 纯色件统一材质参数（批次 28 二轮视觉取舍：原 roughness 0.85–1.0 取 0.9；金属度原全 0）。 */
const FLAT_ROUGH = 0.9;

/** 欧拉旋转 + 平移的部件矩阵（等价原 JSX 的 rotation + position 组合）。 */
function partMatrix(x: number, y: number, z: number, rx: number, ry: number, rz: number): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(rx, ry, rz)),
    new THREE.Vector3(1, 1, 1),
  );
}

export function SportsField() {
  const grass = useCivicPBR('foliage', [1.0, 1.0]);
  const concrete = useCivicPBR('concrete', [0.7, 0.7]);
  const g = grass.matProps;
  const c = concrete.matProps;
  // 椭圆跑道：torusGeometry（圆环），压扁成椭圆（rotation.x = π/2）
  // 半径 R = u(28) (28m)，管半径 r = u(4) (4m 跑道宽度) —— 实测标尺为「中小学操场」
  const info = useObjectInfoProps('civic.sports-field', { anchorY: 0.5 });

  // 1) 内场草茵
  const grassGeo = useMemo(() => {
    const geo = new THREE.CircleGeometry(u(24), 32);
    geo.applyMatrix4(partMatrix(0, 0.015, 0, -Math.PI / 2, 0, 0));
    return geo;
  }, []);
  useEffect(() => () => grassGeo.dispose(), [grassGeo]);

  // 2) 看台 3 级台阶：长边一侧（z=+30）
  const standGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (const i of [0, 1, 2]) {
      parts.push(boxPart(u(40), u(0.6), u(1.2), 0, 0.05 + i * u(0.6), u(33 + i * 0.6)));
    }
    return mergeParts(parts);
  }, []);
  useEffect(() => () => standGeo.dispose(), [standGeo]);

  // 3) 纯色件：跑道 + 足球门 ×2 + 篮球场 + 篮架（顶点色）
  const flatGeo = useMemo(() => {
    const parts: MergePart[] = [
      // 跑道椭圆环（torusGeometry 在水平面）
      { geo: new THREE.TorusGeometry(u(28), u(4), 8, 64), matrix: partMatrix(0, 0.012, 0, Math.PI / 2, 0, 0), color: TRACK_RED },
      // 篮球场：短边内侧（z=-30）
      { geo: new THREE.PlaneGeometry(u(15), u(7)), matrix: partMatrix(0, 0.014, -u(33), -Math.PI / 2, 0, 0), color: COURT_TAN },
      // 篮架：柱 + 板
      { geo: new THREE.CylinderGeometry(u(0.05), u(0.06), u(3), 6), x: -u(6.5), y: u(1.5), z: -u(33), color: GOAL_WHITE },
      { geo: new THREE.BoxGeometry(u(1.8), u(0.9), u(0.05)), x: -u(6.5), y: u(3), z: -u(33), color: GOAL_WHITE },
    ];
    // 足球门 ×2：长边中点（z=±28）—— 横梁 + 左右立柱
    for (const s of [-1, 1]) {
      const z = s * u(28);
      parts.push({ geo: new THREE.BoxGeometry(u(7.32), u(0.08), u(0.08)), x: 0, y: u(1.2), z, color: GOAL_WHITE });
      parts.push({ geo: new THREE.BoxGeometry(u(0.08), u(1.2), u(0.08)), x: -u(3.66), y: u(0.6), z, color: GOAL_WHITE });
      parts.push({ geo: new THREE.BoxGeometry(u(0.08), u(1.2), u(0.08)), x: u(3.66), y: u(0.6), z, color: GOAL_WHITE });
    }
    return mergeParts(parts);
  }, []);
  useEffect(() => () => flatGeo.dispose(), [flatGeo]);

  // 4) 跑道分道白线（4 条 ring 切片，basic 半透明 + DoubleSide）
  const laneGeo = useMemo(() => {
    const parts: MergePart[] = [u(27), u(27.5), u(28.5), u(29)].map((r) => ({
      geo: new THREE.RingGeometry(r - 0.05, r + 0.05, 64),
      matrix: partMatrix(0, 0.013, 0, -Math.PI / 2, 0, 0),
    }));
    return mergeParts(parts);
  }, []);
  useEffect(() => () => laneGeo.dispose(), [laneGeo]);

  return (
    <group {...info} position={[-16.5, 0, 22]}>
      {/* 内场草茵（foliage PBR） */}
      <mesh geometry={grassGeo} receiveShadow>
        <meshStandardMaterial
          color={FIELD_GREEN}
          {...g}
          roughness={g.roughnessMap ? undefined : 0.95}
        />
      </mesh>
      {/* 看台 3 级（concrete PBR；caster 裁剪不投影） */}
      <mesh geometry={standGeo} receiveShadow>
        <meshStandardMaterial color={STAND_GREY} {...c} roughness={c.roughnessMap ? undefined : 0.9} />
      </mesh>
      {/* 纯色件：跑道 + 球门 + 篮球场 + 篮架（caster 裁剪不投影） */}
      <mesh geometry={flatGeo} receiveShadow>
        <meshStandardMaterial vertexColors roughness={FLAT_ROUGH} />
      </mesh>
      {/* 分道白线 ×4 */}
      <mesh geometry={laneGeo}>
        <meshBasicMaterial color={LINE_WHITE} side={THREE.DoubleSide} transparent opacity={0.85} />
      </mesh>
    </group>
  );
}
