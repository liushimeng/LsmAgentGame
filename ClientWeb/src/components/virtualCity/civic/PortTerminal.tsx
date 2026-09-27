/**
 * 物流港码头（18-AA · §5.2 PortTerminal）
 *   局部坐标：岸线沿 x，海侧朝 −z。岸吊 4 腿门架 + 横梁 + 悬臂 + 小车 + 吊具 + 钢缆；
 *   集装箱 3×3×2 共 12 只（4 色确定性）；系缆桩 4 只；码头面 `pbr/synth/concrete_n/r`。
 *   布点 (-30, -9.5)（02 §5.5 已预检通过）。
 *
 * 批次 28 二轮（DC 攻坚）：26 mesh → 4 mesh（几何全等合并，engine3d/geoMerge）：
 *   1) 码头面 mesh：concrete PBR 保持独立（receiveShadow 地面）。
 *   2) 岸吊钢结构 mesh（多 group）：4 腿+悬臂（浅钢 0.6/0.4）/ 横梁（深钢
 *      0.6/0.5）/ 吊具（红 0.7）三档材质分桶 —— metal_deck PBR 贴图逐档保留。
 *   3) 集装箱 mesh（顶点色）：12 只 4 色合并，颜色经顶点色逐件保留，
 *      metal_deck PBR + 统一 0.55/0.25（原值即统一）。
 *   4) 小型件 mesh（顶点色）：小车 + 钢缆 + 4 系缆桩合并。
 * 批次 28 二轮取舍：小型件粗糙度/金属度统一 0.55/0.5（原小车 0.4/0.7、
 *   钢缆 1.0/0、系缆桩 0.6/0.5 取中）；caster 裁剪——市政设施不投影
 *   （shadow pass 实测 1044 DC 超 500 阈值，只留楼体+树干）。
 */
import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { boxPart, cylPart, mergeGrouped, mergeParts, type GroupedMergePart, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { hashStr, mulberry32 } from './rand';
import { useCivicPBR } from './CivicPBR';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const CONCRETE_COLOR = '#6b7280';
const STEEL_COLOR = '#8a8d96';
const STEEL_DARK = '#5a6270';
const CABLE_DARK = '#3a3f4a';
const SPREADER_RED = '#c0392b';
const CONTAINER_COLORS = ['#c0392b', '#2471a3', '#e67e22', '#27ae60'];

/** 小型件统一材质参数（批次 28 二轮视觉取舍：原 0.4–1.0 / 0–0.7 取中）。 */
const SMALL_ROUGH = 0.55;
const SMALL_METAL = 0.5;

export function PortTerminal() {
  const concrete = useCivicPBR('concrete', [0.8, 0.8]);
  const metal = useCivicPBR('metal_deck', [0.9, 0.9]);

  // 集装箱确定性 4 色轮转
  const containers = useMemo(() => {
    const rnd = mulberry32(hashStr('port:containers'));
    const out: Array<{ x: number; z: number; layer: 0 | 1; color: string }> = [];
    for (let row = 0; row < 2; row++) {
      for (let cx = 0; cx < 3; cx++) {
        for (let cz = 0; cz < 2; cz++) {
          out.push({
            x: -u(11) + cx * u(6),
            z: -u(2.5) + cz * u(2.5),
            layer: row as 0 | 1,
            color: CONTAINER_COLORS[Math.floor(rnd() * CONTAINER_COLORS.length)],
          });
        }
      }
    }
    return out;
  }, []);

  const concreteProps = concrete.matProps;
  const metalProps = metal.matProps;

  // 1) 码头面：80m × 25m，`u(80) × u(25)`（契约 02 §5.2）
  const deckGeo = useMemo(() => {
    const g = new THREE.PlaneGeometry(u(80), u(25));
    g.applyMatrix4(
      new THREE.Matrix4().compose(
        new THREE.Vector3(0, 0.03, 0),
        new THREE.Quaternion().setFromEuler(new THREE.Euler(-Math.PI / 2, 0, 0)),
        new THREE.Vector3(1, 1, 1),
      ),
    );
    return g;
  }, []);
  useEffect(() => () => deckGeo.dispose(), [deckGeo]);

  // 2) 岸吊钢结构（多 group 分桶：0 浅钢腿+悬臂 / 1 深钢横梁 / 2 红吊具）
  const frameGeo = useMemo(() => {
    const parts: GroupedMergePart[] = [];
    // 腿：4 根 box 0.5×22m×0.5m @ x=±9m, z=±4m，y=11m（顶高 u(22)）
    for (const sx of [-1, 1]) {
      for (const sz of [-1, 1]) {
        parts.push({ geo: new THREE.BoxGeometry(u(0.5), u(22), u(0.5)), x: sx * u(9), y: u(11), z: sz * u(4), mat: 0 });
      }
    }
    // 海侧悬臂：伸向 +z（局部坐标，岸线朝 -z）
    parts.push({ geo: new THREE.BoxGeometry(u(10), u(0.8), u(0.8)), x: 0, y: u(22.5), z: u(5.5), mat: 0 });
    // 横梁：u(22) 跨度 × u(1.2) 高 × u(1.0) 深，y=22.5
    parts.push({ geo: new THREE.BoxGeometry(u(22), u(1.2), u(1.0)), x: 0, y: u(22.5), z: 0, mat: 1 });
    // 吊具（从小车下垂，钢缆并入小型件 mesh）
    parts.push({ geo: new THREE.BoxGeometry(u(2.4), u(0.35), u(1.6)), x: u(3), y: u(13.5), z: u(5.5), mat: 2 });
    return mergeGrouped(parts);
  }, []);
  useEffect(() => () => frameGeo.dispose(), [frameGeo]);

  // 3) 集装箱堆：3×3 平铺 + 2 层（仅在岸侧 +z 端；尺寸 6×2.6×2.4）—— 顶点色
  const boxGeo = useMemo(
    () =>
      mergeParts(
        containers.map((c) =>
          boxPart(u(6), u(2.6), u(2.4), c.x, 0.05 + (c.layer ? u(2.8) : u(1.4)), c.z, c.color),
        ),
      ),
    [containers],
  );
  useEffect(() => () => boxGeo.dispose(), [boxGeo]);

  // 4) 小型件：小车 + 钢缆 + 系缆桩 4 只（顶点色）
  const smallGeo = useMemo(() => {
    const parts: MergePart[] = [
      // 小车：在悬臂上滑行（u(1.6) × u(1.0) × u(1.4)）
      boxPart(u(1.6), u(1.0), u(1.4), u(3), u(22.5), u(5.5), STEEL_DARK),
      // 钢缆：从小车下垂
      cylPart(0.006, 0.006, u(8), 6, u(3), u(17.5), u(5.5), CABLE_DARK),
    ];
    // 系缆桩 4 只：沿岸每 u(15) 一只
    for (const cx of [-1.5, -0.5, 0.5, 1.5]) {
      parts.push(cylPart(u(0.15), u(0.15), u(0.5), 8, cx * u(15), u(0.25), -u(11.5), STEEL_DARK));
    }
    return mergeParts(parts);
  }, []);
  useEffect(() => () => smallGeo.dispose(), [smallGeo]);

  // 钢结构材质表（3 档：贴图与粗糙度逐档保留原值）
  const frameMaterials = useMemo(
    () => [
      // 0：4 腿 + 悬臂（STEEL_COLOR，metal_deck，0.6/0.4）
      new THREE.MeshStandardMaterial({ color: STEEL_COLOR, ...metalProps, metalness: 0.6, roughness: 0.4 }),
      // 1：横梁（STEEL_DARK，metal_deck，0.6/0.5）
      new THREE.MeshStandardMaterial({ color: STEEL_DARK, ...metalProps, metalness: 0.6, roughness: 0.5 }),
      // 2：吊具（#c0392b，metal_deck，metalness 默认 0）
      new THREE.MeshStandardMaterial({ color: SPREADER_RED, ...metalProps, roughness: 0.7 }),
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [metalProps],
  );
  useEffect(() => () => frameMaterials.forEach((m) => m.dispose()), [frameMaterials]);

  // 批次 28 B2：港口码头信息交互。
  const info = useObjectInfoProps('civic.port-terminal', { anchorY: 3 });

  return (
    <group {...info} position={[-30, 0, -9.5]} rotation={[0, Math.PI, 0]}>
      {/* 码头面 */}
      <mesh geometry={deckGeo} receiveShadow>
        <meshStandardMaterial
          color={CONCRETE_COLOR}
          {...concreteProps}
          roughness={concreteProps.roughnessMap ? undefined : 0.85}
          metalness={0.05}
        />
      </mesh>
      {/* 岸吊钢结构（3 材质组分桶，DC=3；caster 裁剪不投影） */}
      <mesh geometry={frameGeo} material={frameMaterials} />
      {/* 集装箱堆（顶点色 + metal_deck PBR） */}
      <mesh geometry={boxGeo}>
        <meshStandardMaterial vertexColors {...metalProps} roughness={0.55} metalness={0.25} />
      </mesh>
      {/* 小车 + 钢缆 + 系缆桩（顶点色） */}
      <mesh geometry={smallGeo}>
        <meshStandardMaterial vertexColors roughness={SMALL_ROUGH} metalness={SMALL_METAL} />
      </mesh>
    </group>
  );
}
