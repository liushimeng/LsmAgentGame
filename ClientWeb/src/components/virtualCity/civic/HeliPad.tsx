/**
 * 医疗直升机坪（18-AA · §5.2 HeliPad）。布点 (15.5, 22)。
 *
 * 批次 46「城市公用设施真实感」：接入 Blender GLB（`civic/heli_pad.glb`
 * ＋ `3d_script/build_heli_pad.py`）。
 *
 * ── 与另两件的差别：fallback **不需要重建** ───────────────────────────
 *   批次 18-AA 的程序化圆坪是 `CircleGeometry(u(14))` —— ⌀28 m 的 TLOF 尺度
 *   **本来就合规**（ICAO Annex 14 Vol.II：中型机位常用 ⌀15~30 m），
 *   且 GLB 同样是 ⌀28.2 m 的圆台 ⇒ 双路径包围盒天然一致（§27.3-7 满足）。
 *   缺的是台体厚度、FATO 圆环线宽、着陆区（TLOF）内嵌绿灯与助航边灯 ——
 *   这些在 GLB 侧补齐，fallback 侧保持现状（降级链只在 GLB 不可用时出现）。
 *
 * ── 标线标高（GLB 侧的三层叠压，fallback 用贴地 y 近似）─────────────────
 *   0.49 坪面 → 0.49 FATO 白色外盘(⌀27.4) → 0.50 内覆(⌀26.0) → 0.53 H / 着陆区灯。
 *   任何一层压错都会被上层整盘盖掉（批次 46 首版即踩过：H 标识被内覆盘吞没）。
 *
 * ── 降级链（§27.3-3）──────────────────────────────────────────────────
 *   `blenderModelsEnabled()` → `modelUrl` → GLB 载入 → 否则本文件的程序化几何。
 */
import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { CivicGlbPiece } from './CivicGlb';

const PAD_WHITE = '#d8d4ca';
const PAD_LINE = '#e8e8e8';
const LIGHT_RED = '#ff3b30';
const POLE_GREY = '#7a8290';
const CONE_ORANGE = '#ff8b1a';

/** 风向袋统一材质参数（原杆 metalness 0.5 / 锥 roughness 0.7 取中）。 */
const SOCK_ROUGH = 0.85;
const SOCK_METAL = 0.25;

/** 夜间自发光：着陆区内嵌绿灯 3.0 / 助航边灯 3.0。 */
const LIT = {
  HeliPad_TLOF_Green: 3.0,
  HeliPad_EdgeLight: 3.0,
};

/** 欧拉旋转 + 平移的部件矩阵（等价原 JSX 的 rotation + position 组合）。 */
function partMatrix(x: number, y: number, z: number, rx: number, ry: number, rz: number): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(rx, ry, rz)),
    new THREE.Vector3(1, 1, 1),
  );
}

export function HeliPad() {
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

  // 标线：FATO 圆环 + H 两竖一横（basic 半透明 + 顶点色）
  const markGeo = useMemo(() => {
    const parts: MergePart[] = [
      { geo: new THREE.RingGeometry(u(12.5), u(13), 48), matrix: partMatrix(0, 0.001, 0, -Math.PI / 2, 0, 0), color: PAD_LINE },
      { geo: new THREE.PlaneGeometry(u(0.6), u(7)), matrix: partMatrix(-u(3.5), 0.002, 0, -Math.PI / 2, 0, 0), color: PAD_WHITE },
      { geo: new THREE.PlaneGeometry(u(0.6), u(7)), matrix: partMatrix(u(3.5), 0.002, 0, -Math.PI / 2, 0, 0), color: PAD_WHITE },
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
    <CivicGlbPiece
      glbName="heli_pad"
      dimsKey="heliPad"
      infoId="civic.heli-pad"
      anchorY={1.4}
      position={[15.5, 0.05, 22]}
      litMaterials={LIT}
      fallback={
        <>
          <mesh geometry={padGeo} receiveShadow>
            <meshStandardMaterial color={PAD_WHITE} roughness={0.85} />
          </mesh>
          {/* 风向袋：圆柱杆 + 三角锥（caster 裁剪不投影） */}
          <mesh geometry={sockGeo}>
            <meshStandardMaterial vertexColors roughness={SOCK_ROUGH} metalness={SOCK_METAL} />
          </mesh>
          {/* 标线：FATO 圆环 + H（basic 半透明，DoubleSide） */}
          <mesh geometry={markGeo}>
            <meshBasicMaterial vertexColors side={THREE.DoubleSide} transparent opacity={0.9} />
          </mesh>
          {/* 4 角边灯（emissive 红） */}
          <mesh geometry={lightGeo}>
            <meshStandardMaterial color={LIGHT_RED} emissive={LIGHT_RED} emissiveIntensity={0.8} />
          </mesh>
        </>
      }
    />
  );
}
