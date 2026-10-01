/**
 * Sign — 路名牌/标志杆（批次 42 街具真实感 · B1）：
 *
 * 双渲染路径（CLAUDE.md §27.3 硬约束 3/7，包围盒同取 cityScale.REAL_DIMS_M.streetSign
 * = 0.62 × 2.65 × 0.12 m，直立件）：
 *   - GLB 优先：`road/street_sign.glb` 双变体节点 `Sign_Traffic` / `Sign_Info`
 *     （仿 trash_can 双桶先例，两变体沿 X 排开）—— **不能用 `<Model>`**（会克隆整场景、
 *     两变体同屏），改走 useSharedGLTF + `getObjectByName(variant)` 按名取单节点
 *     （参考 RoadsideBins.tsx）后 clone + 内容盒归一化（防美术侧在 GLB 内平移变体）；
 *   - 程序化 fallback：锥度杆 + 2 抱箍 + 折边牌缘 + 法兰底座；牌面保留
 *     贴图 Billboard 分支（缺失走纯色板，traffic 蓝 / info 绿）。
 *
 * 布点（批次 42 C1）：固定在城区朝 finance 侧缘路口（与 mailbox 同侧错开 0.6 u），
 * rotation 使牌面（+Z）朝区外/朝路。
 */

import { memo, useMemo } from 'react';
import * as THREE from 'three';
import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/virtualCity';
import { modelUrl } from '@/assets/models';
import {
  blenderModelsEnabled,
  useSharedGLTF,
  useSharedTexture,
  type MergePart,
  boxPart,
  cylPart,
  mergeParts,
} from '@/engine3d';
import { u, worldDims, sizeTargetFor } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

type SignVariant = 'traffic' | 'info';

/** GLB 内双变体对象名（与 3d_script/build_street_sign.py join_objects 命名对齐）。 */
const VARIANT_NODE: Record<SignVariant, string> = {
  traffic: 'Sign_Traffic',
  info: 'Sign_Info',
};

/**
 * 尺寸/落地校验目标（dev 态）：**量测 Sign_Traffic 变体子树**（两变体沿 X 排开 ⇒
 * 整场景盒不是单牌尺寸；两变体同 0.62×2.65×0.12，量测其一即可，仿 trashCan 例）。
 */
const SIGN_SIZE_TARGET = sizeTargetFor('streetSign', {
  label: 'road/street_sign',
  measureNode: VARIANT_NODE.traffic,
});

// ── fallback 构图常量（米制经 u()；包络取 worldDims('streetSign') 唯一事实来源）──
const D = worldDims('streetSign'); // x 0.062 / y 0.265 / z 0.012（世界单位）
const PLATE_COLORS: Record<SignVariant, string> = {
  // 批次 42 §3.3：交通蓝底 / 信息绿（原红 #c83a3a 退出）
  traffic: '#1a4f9c',
  info: '#0d7a4a',
};
const POLE_COLOR = '#5b616e';
const FLANGE_COLOR = '#4a505c';

/** 牌面尺寸（真实 0.60 × 0.35 m；牌缘折边 18 mm 另计，z 厚 3 mm 铝板）。 */
const PLATE_W = u(0.6);
const PLATE_H = u(0.35);
const PLATE_Y = D.y - PLATE_H / 2 - u(0.02); // 牌顶留 20 mm ⇒ 顶缘 ≈ 表值 y

/**
 * fallback 杆件原语 → 顶点色合并 1 mesh（法兰/锥度杆/抱箍×2/折边牌缘框）。
 * 牌面（贴图/色板）独立，见组件 JSX。
 */
function signPostParts(): MergePart[] {
  const parts: MergePart[] = [];
  // 法兰底座（0.20×0.20×0.10 m + 4 螺栓）
  parts.push(cylPart(u(0.1), u(0.115), u(0.1), 12, 0, u(0.05), 0, FLANGE_COLOR));
  for (let i = 0; i < 4; i++) {
    const a = (i / 4) * Math.PI * 2 + Math.PI / 4;
    parts.push(cylPart(u(0.012), u(0.012), u(0.03), 6, Math.cos(a) * u(0.075), u(0.105), Math.sin(a) * u(0.075), POLE_COLOR));
  }
  // 锥度杆（⌀60→76 mm，杆顶埋入牌背）
  const poleH = PLATE_Y;
  parts.push(cylPart(u(0.03), u(0.038), poleH, 10, 0, u(0.1) + poleH / 2, 0, POLE_COLOR));
  // 抱箍 ×2（⌀80 mm 环，牌背固定；薄环用短圆柱近似）
  for (const cy of [PLATE_Y - u(0.1), PLATE_Y - u(0.22)]) {
    parts.push(cylPart(u(0.042), u(0.042), u(0.03), 10, 0, cy, -D.z * 0.35, FLANGE_COLOR));
  }
  // 折边牌缘框（圆角 R10 的折边感：四边薄条外挑 18 mm）
  const edge = u(0.018);
  const zEdge = -D.z * 0.1;
  parts.push(boxPart(PLATE_W + edge * 2, edge, u(0.028), 0, PLATE_Y + PLATE_H / 2 + edge / 2, zEdge, POLE_COLOR));
  parts.push(boxPart(PLATE_W + edge * 2, edge, u(0.028), 0, PLATE_Y - PLATE_H / 2 - edge / 2, zEdge, POLE_COLOR));
  for (const sx of [-1, 1]) {
    parts.push(boxPart(edge, PLATE_H + edge * 2, u(0.028), sx * (PLATE_W / 2 + edge / 2), PLATE_Y, zEdge, POLE_COLOR));
  }
  return parts;
}

interface Props {
  x: number;
  z: number;
  rotation?: number;
  variant?: SignVariant;
}

// 批次 28 A1：memo —— props 稳定引用（原语 / 常量）。
export const Sign = memo(function Sign({ x, z, rotation = 0, variant = 'traffic' }: Props) {
  // 14-3D渲染深化：共享贴图缓存（fallback 次级降级）
  const tex = useSharedTexture(propUrl('sign', variant));
  const info = useObjectInfoProps('prop.sign', {
    anchorY: 2.4,
    extra: [{ label: 'variant', value: variant }],
  });

  const url = blenderModelsEnabled() ? modelUrl('road', 'street_sign') : '';
  const { scene } = useSharedGLTF(url, SIGN_SIZE_TARGET);
  // 按名取变体节点 + clone；内容盒归一化（x/z 取中心、y 取 −min，抗 GLB 内变体平移）
  const glbNode = useMemo(() => {
    if (!scene) return null;
    const src = scene.getObjectByName(VARIANT_NODE[variant]) ?? null;
    if (!src) return null;
    const node = src.clone(true);
    const box = new THREE.Box3().setFromObject(node);
    const c = box.getCenter(new THREE.Vector3());
    node.position.x -= c.x;
    node.position.z -= c.z;
    node.position.y -= box.min.y;
    return node;
  }, [scene, variant]);

  const postGeo = useMemo(() => mergeParts(signPostParts()), []);

  return (
    <group {...info} position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {glbNode ? (
        <primitive object={glbNode} />
      ) : (
        <>
          {/* 杆件合并（法兰/锥度杆/抱箍/折边牌缘） */}
          <mesh geometry={postGeo}>
            <meshStandardMaterial vertexColors roughness={0.55} metalness={0.5} />
          </mesh>
          {/* 牌面：贴图 Billboard（次级降级）/ 纯色板 */}
          {tex ? (
            <Billboard position={[0, PLATE_Y, 0]}>
              <mesh>
                <planeGeometry args={[PLATE_W, PLATE_H]} />
                <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
              </mesh>
            </Billboard>
          ) : (
            <mesh position={[0, PLATE_Y, 0]}>
              <boxGeometry args={[PLATE_W, PLATE_H, u(0.012)]} />
              <meshStandardMaterial color={PLATE_COLORS[variant]} roughness={0.35} metalness={0.15} />
            </mesh>
          )}
        </>
      )}
    </group>
  );
});
