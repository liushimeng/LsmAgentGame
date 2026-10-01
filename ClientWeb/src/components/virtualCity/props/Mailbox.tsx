/**
 * Mailbox — 柱式邮筒（批次 42 街具真实感 · B1/D1）：
 *
 * 双渲染路径（CLAUDE.md §27.3 硬约束 3/7，包围盒同取 cityScale.REAL_DIMS_M.mailbox
 * = 0.60 × 1.40 × 0.60 m，直立件；投信口朝局部 +Z）：
 *   - GLB 优先：`road/mailbox.glb` 经 `<Model>`（engine3d，children-fallback 语义，
 *     GLB 成功时不渲染 children —— 零分支降级）；
 *   - 程序化 fallback：微锥筒身 + 弧顶出檐 + 投信口翻盖雨檐 + 铭牌 + 底座法兰
 *     + 取信门缝（**中国邮政绿 #0a6b45 系**，批次 42 起不再用红色）。
 *
 * 布点（批次 42 C1）：固定在城区朝 finance 侧缘（与 sign 同侧错开 0.6 u），
 * rotation 使投信口（+Z）朝区外/朝路。
 */

import { memo, useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { Model, type MergePart, boxPart, cylPart, mergeParts } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { u, worldDims, sizeTargetFor } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

/** 尺寸/落地校验目标（dev 态 glbSizeGuard；与 fallback 同行表值）。 */
const MAILBOX_SIZE_TARGET = sizeTargetFor('mailbox', { label: 'road/mailbox' });

// ── fallback 构图常量（米制经 u()；包络取 worldDims('mailbox') 唯一事实来源）──
const D = worldDims('mailbox'); // x 0.06 / y 0.14 / z 0.06（世界单位）
/** 中国邮政绿（批次 42：替换旧红 #c8453a；catalog 文案同步）。 */
const POST_GREEN = '#0a6b45';
const POST_GREEN_DARK = '#085538';
const SLOT_BLACK = '#1a1a1d';
const NAMEPLATE = '#c9a24a';

/**
 * fallback 原语 → 顶点色合并 1 mesh（法兰/筒身/弧顶/雨檐/铭牌/门缝）。
 * 包络：法兰 ⌀0.60 = 表值 x/z，总高 1.40 = 表值 y，minY=0 贴地。
 */
function mailboxParts(): MergePart[] {
  const parts: MergePart[] = [];
  const R_BOT = u(0.28);
  const R_TOP = u(0.26);
  const FLANGE_H = u(0.06);
  const CAP_Y = u(1.3); // 弧顶起始
  const BODY_H = CAP_Y - FLANGE_H;
  // 底座法兰（⌀ = 表值 x 0.60 m，4 膨胀螺栓用 4 短柱近似）
  parts.push(cylPart(D.x / 2, D.x / 2 + u(0.02), FLANGE_H, 16, 0, FLANGE_H / 2, 0, POST_GREEN_DARK));
  for (let i = 0; i < 4; i++) {
    const a = (i / 4) * Math.PI * 2 + Math.PI / 4;
    parts.push(cylPart(u(0.02), u(0.02), u(0.025), 6, Math.cos(a) * u(0.24), FLANGE_H + u(0.01), Math.sin(a) * u(0.24), SLOT_BLACK));
  }
  // 微锥筒身（下略粗）
  parts.push(cylPart(R_TOP, R_BOT, BODY_H, 18, 0, FLANGE_H + BODY_H / 2, 0, POST_GREEN));
  // 弧顶出檐（双段：檐圈 + 穹顶）
  parts.push(cylPart(u(0.27), R_TOP + u(0.015), u(0.05), 18, 0, CAP_Y + u(0.025), 0, POST_GREEN_DARK));
  {
    // 穹顶（球冠压扁；顶缘 ≈ 1.40 m）
    const geo = new THREE.SphereGeometry(u(0.27), 16, 8, 0, Math.PI * 2, 0, Math.PI / 2);
    parts.push({
      geo,
      matrix: new THREE.Matrix4().compose(
        new THREE.Vector3(0, CAP_Y + u(0.05), 0),
        new THREE.Quaternion(),
        new THREE.Vector3(1, u(0.32) / u(0.27), 1),
      ),
      color: POST_GREEN,
    });
  }
  // 投信口（宽 0.30 × 高 0.04，中心高 1.10 m）+ 翻盖雨檐（外挑下压；外缘贴表值 x/2）
  parts.push(boxPart(u(0.3), u(0.04), u(0.03), 0, u(1.1), R_TOP + u(0.012), SLOT_BLACK));
  parts.push(boxPart(u(0.34), u(0.018), u(0.04), 0, u(1.15), R_TOP + u(0.02), POST_GREEN_DARK));
  // 品牌铭牌（0.28 × 0.12，投信口下方微凸）
  parts.push(boxPart(u(0.28), u(0.12), u(0.018), 0, u(0.92), R_TOP + u(0.01), NAMEPLATE));
  // 取信门缝 + 把手（筒身下部）
  parts.push(boxPart(u(0.2), u(0.45), u(0.012), 0, u(0.52), R_TOP * 0.92, POST_GREEN_DARK));
  parts.push(boxPart(u(0.05), u(0.03), u(0.025), u(0.11), u(0.58), R_TOP * 0.92, SLOT_BLACK));
  return parts;
}

interface Props {
  x: number;
  z: number;
  rotation?: number;
}

// 批次 28 A1/A3：memo + 街具默认不投影（阴影 pass caster 裁剪）。
export const Mailbox = memo(function Mailbox({ x, z, rotation = 0 }: Props) {
  const info = useObjectInfoProps('prop.mailbox', { anchorY: 0.7 });
  const solidsGeo = useMemo(() => mergeParts(mailboxParts()), []);
  useEffect(() => () => solidsGeo.dispose(), [solidsGeo]);

  return (
    <group {...info} position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* GLB 优先（children = 程序化 fallback，加载中/失败/缺失三态自动降级） */}
      <Model url={modelUrl('road', 'mailbox')} sizeTarget={MAILBOX_SIZE_TARGET}>
        <mesh geometry={solidsGeo}>
          <meshStandardMaterial vertexColors roughness={0.55} metalness={0.25} />
        </mesh>
      </Model>
    </group>
  );
});

export default Mailbox;
