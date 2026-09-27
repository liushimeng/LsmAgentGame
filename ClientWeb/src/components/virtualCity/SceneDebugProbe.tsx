/**
 * SceneDebugProbe — 批次 28 二轮 · 场景归因工具（`?debug=1`）。
 *
 * ⚠️ 必须挂在 Canvas 内部（useThree 在 Canvas 外会崩）：
 *   <EngineCanvas>…<SceneDebugProbe />…</EngineCanvas>
 *
 * 挂载的全局（CDP / 控制台可直读）：
 *   - window.__cityScene     —— three Scene 引用
 *   - window.__cityRenderer  —— WebGLRenderer 引用
 *   - window.__cityBreakdown() —— 分桶统计，返回对象并 console.table：
 *       mesh / instancedMesh 实例数、triangles、材质组数（≈ 主 pass draw call）、
 *       caster（投影 mesh）数，按 userData.bucket（最近祖先）分桶：
 *       buildings / civic / landmarks / street-props / pedestrians / vehicles /
 *       roads / edge / water / other。
 *       另附 shadowMap.enabled=false 对照渲染的 calls 差值（阴影 pass DC 归因）。
 *
 * 实现约束：零常驻开销（非 debug 查询串直接 return null，不订阅不遍历）；
 * breakdown 手动渲染两帧测量（shadowMap.needsUpdate 强制一次 shadow pass），
 * 结束后还原 renderer 状态，不影响 r3f 主渲染循环。
 */

import { useEffect } from 'react';
import { useThree } from '@react-three/fiber';
import * as THREE from 'three';

/** 分桶统计行。 */
interface BucketRow {
  bucket: string;
  meshes: number;
  /** instancedMesh 实例数（mesh 计 1）。 */
  instances: number;
  triangles: number;
  /** 材质组数合计（单材质 mesh 记 1）≈ 主 pass draw call 下界。 */
  groups: number;
  casters: number;
}

interface BreakdownResult {
  total: BucketRow;
  buckets: BucketRow[];
  /** 手动渲染实测：shadowMap.enabled=false / true 的 renderer.info.render.calls。 */
  render: {
    callsWithoutShadow: number;
    callsWithShadow: number;
    shadowPassCalls: number;
    triangles: number;
  };
  /** scene.children 顶层对象数（协调开销参考）。 */
  topLevelObjects: number;
}

function emptyRow(bucket: string): BucketRow {
  return { bucket, meshes: 0, instances: 0, triangles: 0, groups: 0, casters: 0 };
}

/** 最近祖先的 userData.bucket（无标签 → 'other'）。 */
function bucketOf(obj: THREE.Object3D): string {
  let o: THREE.Object3D | null = obj;
  while (o) {
    const b = (o.userData as { bucket?: unknown }).bucket;
    if (typeof b === 'string') return b;
    o = o.parent;
  }
  return 'other';
}

export function SceneDebugProbe() {
  const scene = useThree((s) => s.scene);
  const gl = useThree((s) => s.gl);
  const camera = useThree((s) => s.camera);

  const enabled =
    typeof window !== 'undefined' && window.location.search.includes('debug=1');

  useEffect(() => {
    if (!enabled) return;
    const w = window as unknown as Record<string, unknown>;
    w.__cityScene = scene;
    w.__cityRenderer = gl;

    const breakdown = (): BreakdownResult => {
      const rows = new Map<string, BucketRow>();
      const rowOf = (b: string): BucketRow => {
        let r = rows.get(b);
        if (!r) {
          r = emptyRow(b);
          rows.set(b, r);
        }
        return r;
      };
      const total = emptyRow('TOTAL');
      scene.traverse((o) => {
        const mesh = o as THREE.Mesh;
        if (!(mesh as unknown as { isMesh?: boolean }).isMesh) return;
        const b = bucketOf(mesh);
        const row = rowOf(b);
        const geo = mesh.geometry as THREE.BufferGeometry | undefined;
        const triCount = geo?.index
          ? geo.index.count / 3
          : (geo?.getAttribute('position')?.count ?? 0) / 3;
        const inst = mesh as unknown as THREE.InstancedMesh;
        const instCount =
          (inst as unknown as { isInstancedMesh?: boolean }).isInstancedMesh
            ? inst.count
            : 0;
        const groupCount = Array.isArray(mesh.material)
          ? Math.max(mesh.geometry.groups.length, 1)
          : 1;
        for (const r of [row, total]) {
          r.meshes += 1;
          r.instances += instCount;
          r.triangles += triCount * (instCount > 0 ? instCount : 1);
          r.groups += groupCount;
          if (mesh.castShadow) r.casters += 1;
        }
      });
      const buckets = [...rows.values()].sort((a, b) => b.meshes - a.meshes);

      // 阴影 pass DC 归因。⚠️ three r169 WebGLRenderer.render() 在 shadowMap.render()
      // **之后**才 info.reset() —— renderer.info.render.calls 从不含 shadow pass
      // （批次 28 一轮「采样无双峰」即此测量伪影，节流本身工作正常）。
      // 此处 autoReset=false + 手动 reset，先关 shadowMap 渲染一帧、再强制
      // needsUpdate=true 渲染一帧，差值即 shadow pass DC。
      const sm = gl.shadowMap;
      const info = gl.info;
      const prevAuto = sm.autoUpdate;
      const prevEnabled = sm.enabled;
      const prevNeeds = sm.needsUpdate;
      const prevInfoAutoReset = info.autoReset;
      let callsWithoutShadow = -1;
      let callsWithShadow = -1;
      let tris = -1;
      try {
        info.autoReset = false;
        sm.enabled = false;
        sm.autoUpdate = false;
        sm.needsUpdate = false;
        info.reset();
        gl.render(scene, camera);
        callsWithoutShadow = info.render.calls;
        sm.enabled = true;
        sm.needsUpdate = true;
        info.reset();
        gl.render(scene, camera);
        callsWithShadow = info.render.calls;
        tris = info.render.triangles;
      } finally {
        info.autoReset = prevInfoAutoReset;
        sm.enabled = prevEnabled;
        sm.autoUpdate = prevAuto;
        sm.needsUpdate = prevNeeds;
        info.reset();
      }

      const result: BreakdownResult = {
        total,
        buckets,
        render: {
          callsWithoutShadow,
          callsWithShadow,
          shadowPassCalls: callsWithShadow - callsWithoutShadow,
          triangles: tris,
        },
        topLevelObjects: scene.children.length,
      };
      // eslint-disable-next-line no-console
      console.table(buckets);
      // eslint-disable-next-line no-console
      console.log('[cityBreakdown]', result);
      return result;
    };

    w.__cityBreakdown = breakdown;
    // eslint-disable-next-line no-console
    console.log('[engine3d] city scene probe mounted: __cityScene / __cityRenderer / __cityBreakdown()');
    return () => {
      delete w.__cityScene;
      delete w.__cityRenderer;
      delete w.__cityBreakdown;
    };
  }, [enabled, scene, gl, camera]);

  if (!enabled) return null;
  return null;
}
