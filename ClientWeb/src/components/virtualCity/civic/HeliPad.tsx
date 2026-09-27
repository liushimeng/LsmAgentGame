/**
 * 医疗直升机坪（18-AA · §5.2 HeliPad）
 *   圆坪（白色 H + 圆环标线 + 4 角边灯 + 风向袋）。布点 (15.5, 22)，直径 u(28)。
 *
 * 批次 28 二轮（DC 攻坚）：11 mesh → 4 mesh（几何全等合并，engine3d/geoMerge）：
 *   1) 圆坪 mesh：保持独立（receiveShadow 接地面 + 粗糙度 0.85 专属）。
 *   2) 风向袋 mesh（顶点色）：圆柱杆 + 三角锥合并 —— 颜色逐件保留。
 *   3) 标线 mesh（顶点色）：圆环 + H 两竖一横 4 件 plane 合并 —— 保留
 *      meshBasicMaterial 半透明（transparent 0.9 + DoubleSide）材质类。
 *   4) 边灯 mesh：4 颗 emissive 红球合并（材质逐字段保留）。
 * 批次 28 二轮取舍：杆/锥粗糙度统一 0.85（原 1.0 / 0.7）、金属度 0.25
 *   （原 0.5 / 0）；caster 裁剪——市政设施不投影（shadow pass 实测 1044 DC
 *   超 500 阈值，只留楼体+树干）。
 */
import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const PAD_WHITE = '#d8d4ca';
const PAD_LINE = '#e8e8e8';
const LIGHT_RED = '#ff3b30';
const POLE_GREY = '#7a8290';
const CONE_ORANGE = '#ff8b1a';

/** 风向袋统一材质参数（原杆 metalness 0.5 / 锥 roughness 0.7 取中）。 */
const SOCK_ROUGH = 0.85;
const SOCK_METAL = 0.25;

/** 欧拉旋转 + 平移的部件矩阵（等价原 JSX 的 rotation + position 组合）。 */
function partMatrix(x: number, y: number, z: number, rx: number, ry: number, rz: number): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(rx, ry, rz)),
    new THREE.Vector3(1, 1, 1),
  );
}

export function HeliPad() {
  const info = useObjectInfoProps('civic.heli-pad', { anchorY: 0.5 });

  // 圆坪（保持独立 mesh：地面 receiveShadow）
  const padGeo = useMemo(() => {
    const g = new THREE.CircleGeometry(u(14), 48);
    g.applyMatrix4(partMatrix(0, 0, 0, -Math.PI / 2, 0, 0));
    return g;
  }, []);
  useEffect(() => () => padGeo.dispose(), [padGeo]);

  // 风向袋：圆柱杆 + 三角锥（顶点色合并）
  const sockGeo = useMemo(
    () =>
      mergeParts([
        { geo: new THREE.CylinderGeometry(u(0.04), u(0.04), u(3), 6), x: -u(11.5), y: u(1.5), z: -u(11.5), color: POLE_GREY },
        { geo: new THREE.ConeGeometry(u(0.35), u(0.8), 4), x: -u(11.8), y: u(2.8), z: -u(11.5), color: CONE_ORANGE },
      ]),
    [],
  );
  useEffect(() => () => sockGeo.dispose(), [sockGeo]);

  // 标线：圆环 + H 两竖一横（4 件 plane，basic 半透明 + 顶点色）
  const markGeo = useMemo(() => {
    const parts: MergePart[] = [
      // 圆环标线（外圈）
      { geo: new THREE.RingGeometry(u(12.5), u(13), 48), matrix: partMatrix(0, 0.001, 0, -Math.PI / 2, 0, 0), color: PAD_LINE },
      // H 字符两竖
      { geo: new THREE.PlaneGeometry(u(0.6), u(7)), matrix: partMatrix(-u(3.5), 0.002, 0, -Math.PI / 2, 0, 0), color: PAD_WHITE },
      { geo: new THREE.PlaneGeometry(u(0.6), u(7)), matrix: partMatrix(u(3.5), 0.002, 0, -Math.PI / 2, 0, 0), color: PAD_WHITE },
      // H 字符一横
      { geo: new THREE.PlaneGeometry(u(7.5), u(0.6)), matrix: partMatrix(0, 0.002, 0, -Math.PI / 2, 0, 0), color: PAD_WHITE },
    ];
    return mergeParts(parts);
  }, []);
  useEffect(() => () => markGeo.dispose(), [markGeo]);

  // 4 角边灯（emissive 红球合并）
  const lightGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (const sx of [-1, 1]) {
      for (const sz of [-1, 1]) {
        parts.push({ geo: new THREE.SphereGeometry(u(0.25), 8, 8), x: sx * u(11), y: u(0.15), z: sz * u(11) });
      }
    }
    return mergeParts(parts);
  }, []);
  useEffect(() => () => lightGeo.dispose(), [lightGeo]);

  return (
    <group {...info} position={[15.5, 0.05, 22]}>
      {/* 圆坪 */}
      <mesh geometry={padGeo} receiveShadow>
        <meshStandardMaterial color={PAD_WHITE} roughness={0.85} />
      </mesh>
      {/* 风向袋：圆柱杆 + 三角锥（caster 裁剪不投影） */}
      <mesh geometry={sockGeo}>
        <meshStandardMaterial vertexColors roughness={SOCK_ROUGH} metalness={SOCK_METAL} />
      </mesh>
      {/* 标线：圆环 + H（basic 半透明，DoubleSide） */}
      <mesh geometry={markGeo}>
        <meshBasicMaterial vertexColors side={THREE.DoubleSide} transparent opacity={0.9} />
      </mesh>
      {/* 4 角边灯（emissive 红） */}
      <mesh geometry={lightGeo}>
        <meshStandardMaterial color={LIGHT_RED} emissive={LIGHT_RED} emissiveIntensity={0.8} />
      </mesh>
    </group>
  );
}
