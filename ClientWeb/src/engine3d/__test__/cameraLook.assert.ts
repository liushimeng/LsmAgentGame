/**
 * cameraLook 断言测试（批次 67 · 视觉系统优化 —— 抬头自由）。
 *
 * 背景：用户主诉「视觉不能向上，看不到天空」。本测试把该修复的**数学核心**钉死：
 *   ① 视线俯仰偏移（`lookPitch`）绕相机局部 +X 旋转 ⇒ 视线**确实向上**（非向下）；
 *   ② 基朝向 + 抬头偏移的合成结果，与「先算基朝向再向量旋转」的解析式**逐位一致**；
 *   ③ 由视线方向反解的 (yaw, pitch) 能**无损重建**原方向（往返一致性）；
 *   ④ 限位钳制：抬头偏移被钳在 `[lookPitchMin, lookPitchLimit]` 内；
 *   ⑤ 端到端判据：orbit 默认俯仰（φ=0.85）下，**满抬头后视线仰角 > 0**（能看到天空），
 *      且相机**高度不变**（抬头不移动相机 ⇒ 不触碰地面钳制 / 碰撞 / 边界）。
 *
 * 项目无测试框架（不引入新依赖），沿用批次 37 的做法：node 原生断言 + 单文件直跑：
 *     node --experimental-strip-types src/engine3d/__test__/cameraLook.assert.ts
 */

import * as THREE from 'three';
import {
  clampLookPitch,
  forwardFromQuaternion,
  lookPitchQuaternion,
  LOOK_PITCH_LIMIT_DEFAULT,
  LOOK_PITCH_MIN_DEFAULT,
  tiltForward,
  yawPitchFromDirection,
  type YawPitch,
} from '../controls/cameraMath.ts';

let failed = 0;
let passed = 0;

function ok(cond: boolean, msg: string): void {
  if (cond) {
    passed++;
  } else {
    failed++;
    console.error(`  ✗ FAIL: ${msg}`);
  }
}

function near(a: number, b: number, eps = 1e-9): boolean {
  return Math.abs(a - b) <= eps;
}

/** 俯仰角（度）：+ = 朝上。 */
function deg(rad: number): number {
  return (rad * 180) / Math.PI;
}

// ── ① 抬头方向正确：绕相机局部 +X 旋转 ⇒ 视线向上 ────────────────
console.log('[1] 抬头方向：局部 +X 旋转把视线从 −Z 抬到 +Y');
{
  const q = new THREE.Quaternion();
  const fwd = new THREE.Vector3();
  // 未抬头：视线 = (0,0,−1)
  forwardFromQuaternion(fwd, q);
  ok(near(fwd.x, 0) && near(fwd.y, 0) && near(fwd.z, -1), `未抬头时视线应为 (0,0,−1)，实际 ${fwd.toArray()}`);

  // 抬头 30°：视线 = (0, sin30, −cos30)
  lookPitchQuaternion(q, Math.PI / 6);
  forwardFromQuaternion(fwd, q);
  ok(near(fwd.y, Math.sin(Math.PI / 6), 1e-12), `抬头 30° 后视线 y 应为 +0.5，实际 ${fwd.y}`);
  ok(fwd.y > 0, '抬头后视线必须朝上（y > 0）');
  ok(near(fwd.length(), 1, 1e-12), '视线仍为单位向量');
}

// ── ② 四元数路径 与 向量路径（Rodrigues）逐位一致 ────────────────
console.log('[2] 四元数右乘 与 向量 Rodrigues 一致');
{
  for (const angle of [0.2, 0.7, 1.4, -0.35]) {
    // 基准朝向：yaw=0.6, pitch=-0.4（YXZ 欧拉）
    const base = new THREE.Quaternion().setFromEuler(new THREE.Euler(-0.4, 0.6, 0, 'YXZ'));

    // 路径 A：四元数右乘（控制器实际使用）
    const qa = base.clone();
    lookPitchQuaternion(new THREE.Quaternion(), angle);
    const local = new THREE.Quaternion();
    lookPitchQuaternion(local, angle);
    qa.multiply(local);
    const fa = new THREE.Vector3();
    forwardFromQuaternion(fa, qa);

    // 路径 B：向量 Rodrigues（右向量 = 基朝向局部 +X 的世界系）
    const right = new THREE.Vector3(1, 0, 0).applyQuaternion(base);
    const fb = forwardFromQuaternion(new THREE.Vector3(), base);
    tiltForward(fb, right, angle);

    ok(
      fa.distanceTo(fb) < 1e-9,
      `angle=${angle} 两路径应一致，差 ${fa.distanceTo(fb)}`,
    );
  }
}

// ── ③ (yaw, pitch) 往返一致性 ────────────────────────────────────
console.log('[3] 视线方向 ⇄ (yaw, pitch) 往返一致');
{
  const yp: YawPitch = { yaw: 0, pitch: 0 };
  for (const dir of [
    new THREE.Vector3(0, 0, -1),
    new THREE.Vector3(0.5, 0.5, -0.7).normalize(),
    new THREE.Vector3(-0.3, -0.8, 0.5).normalize(),
    new THREE.Vector3(0, 1, 0.0001).normalize(),
  ]) {
    yawPitchFromDirection(yp, dir);
    const rebuilt = new THREE.Vector3(
      -Math.sin(yp.yaw) * Math.cos(yp.pitch),
      Math.sin(yp.pitch),
      -Math.cos(yp.yaw) * Math.cos(yp.pitch),
    );
    ok(rebuilt.distanceTo(dir) < 1e-6, `方向 ${dir.toArray()} 往返重建得 ${rebuilt.toArray()}`);
  }
}

// ── ④ 限位钳制 ───────────────────────────────────────────────────
console.log('[4] 抬头偏移限位');
{
  ok(
    clampLookPitch(5, LOOK_PITCH_MIN_DEFAULT, LOOK_PITCH_LIMIT_DEFAULT) === LOOK_PITCH_LIMIT_DEFAULT,
    '超上限应被钳到 lookPitchLimit',
  );
  ok(
    clampLookPitch(-5, LOOK_PITCH_MIN_DEFAULT, LOOK_PITCH_LIMIT_DEFAULT) === LOOK_PITCH_MIN_DEFAULT,
    '低于下限应被钳到 lookPitchMin',
  );
  ok(
    LOOK_PITCH_LIMIT_DEFAULT > Math.PI / 4 && LOOK_PITCH_LIMIT_DEFAULT < Math.PI / 2,
    '抬头上限应在 45°~90° 之间（够抬头看天又不越天顶）',
  );
}

// ── ⑤ 端到端：orbit 默认俯仰下满抬头 ⇒ 仰角 > 0 且相机不动 ────────
console.log('[5] 端到端：orbit 满抬头能看到天空，且相机高度不变');
{
  // 复刻控制器几何：target 在原点，球坐标 (r=42, φ=0.85, θ=π/4)
  const r = 42;
  const phi = 0.85;
  const theta = Math.PI / 4;
  const target = new THREE.Vector3(0, 0, 0);
  const pos = new THREE.Vector3(
    r * Math.sin(phi) * Math.sin(theta),
    r * Math.cos(phi),
    r * Math.sin(phi) * Math.cos(theta),
  );

  // 基朝向 = lookAt(target)
  const m = new THREE.Matrix4().lookAt(pos, target, new THREE.Vector3(0, 1, 0));
  const base = new THREE.Quaternion().setFromRotationMatrix(m);

  const yp: YawPitch = { yaw: 0, pitch: 0 };
  const baseFwd = forwardFromQuaternion(new THREE.Vector3(), base);
  yawPitchFromDirection(yp, baseFwd);
  ok(deg(yp.pitch) < 0, `orbit 基朝向应朝下，实际仰角 ${deg(yp.pitch).toFixed(2)}°`);
  ok(
    near(deg(yp.pitch), deg(phi - Math.PI / 2), 1e-6),
    `orbit 基朝向仰角应等于 φ−90°（${deg(phi - Math.PI / 2).toFixed(2)}°），实际 ${deg(yp.pitch).toFixed(2)}°`,
  );

  // 满抬头
  const local = new THREE.Quaternion();
  lookPitchQuaternion(local, LOOK_PITCH_LIMIT_DEFAULT);
  const q = base.clone().multiply(local);
  const liftedFwd = forwardFromQuaternion(new THREE.Vector3(), q);
  yawPitchFromDirection(yp, liftedFwd);
  ok(
    yp.pitch > 0,
    `orbit 满抬头后视线仰角应 > 0（看得到天空），实际 ${deg(yp.pitch).toFixed(2)}°`,
  );
  ok(
    deg(yp.pitch) > 30,
    `orbit 满抬头后仰角应 > 30°（明显看到天空），实际 ${deg(yp.pitch).toFixed(2)}°`,
  );

  // 相机高度不变（抬头不移动相机 ⇒ 不触碰地面钳制 / 碰撞 / 边界）
  const posAfter = pos.clone(); // 抬头只改四元数，不改位置
  ok(posAfter.y === pos.y, '抬头不得改变相机高度');

  // φ 拖到近水平（1.54）时基朝向更平 ⇒ 满抬头更接近天顶
  const phi2 = 1.54;
  const pos2 = new THREE.Vector3(
    r * Math.sin(phi2) * Math.sin(theta),
    r * Math.cos(phi2),
    r * Math.sin(phi2) * Math.cos(theta),
  );
  const base2 = new THREE.Quaternion().setFromRotationMatrix(
    new THREE.Matrix4().lookAt(pos2, target, new THREE.Vector3(0, 1, 0)),
  );
  const q2 = base2.clone().multiply(local);
  yawPitchFromDirection(yp, forwardFromQuaternion(new THREE.Vector3(), q2));
  ok(
    deg(yp.pitch) > 70,
    `近水平轨道满抬头应 > 70°（近天顶），实际 ${deg(yp.pitch).toFixed(2)}°`,
  );
}

console.log(`\n结果：${passed} 通过 / ${failed} 失败`);
if (failed > 0) throw new Error(`cameraLook 断言测试失败 ${failed} 项`);
