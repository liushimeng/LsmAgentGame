/**
 * ParkLamps — 中央公园园灯（批次 45 B3/C3：全园 12 盏，实例化 + 夜灯行为）。
 *
 * GB 51192-2016 §10.2 园内照明（功能性照明）；庭院灯厂家档 2.5~3.5 m 灯高 ⇒
 * 光池 ⌀5~8 m ⇒ 间距 10 m —— 四臂各 3 盏、两侧交错（parkLayout::PARK_LAMPS）。
 * 中央公园此前夜间全黑（路灯只照园外路缘），园灯是公园夜景骨架。
 *
 * 夜灯行为（三层，随 cityTimeStore.dayFactor01 渐变，同路灯范式）：
 *   ① 灯罩 emissive（GLB 材质名 `ParkLamp_Lantern_Mat`，按 url 共享 scene ⇒
 *      traverse 一次、全园同步；昼 0.15 → 夜 2.6）；
 *   ② 地面暖光斑（⌀6 m additive radial，昼 0 → 夜 0.55，drei Instances 1 DC）；
 *   ③ 兜底路径：程序化灯（12 盏杆+罩合并 mesh）+ 罩面 emissive 同步调制。
 *
 * 无光锥/辉光层 —— 庭院灯 3 m 光池小、密度低，两层已足（方案 45 §4 C4）。
 */
import { useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Instances, Instance } from '@react-three/drei';
import { blenderModelsEnabled, useSharedGLTF, cylPart, mergeParts } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { u, sizeTargetFor, DISTRICT_SURFACE_Y } from '../cityScale';
import { getDayNight } from '../cityTimeStore';
import { PARK_LAMPS } from './parkLayout';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';
import { GlbInstanced, type GlbInstanceTRS } from '../edge/glbInstanced';

/** 灯罩夜间 emissive 上限（昼 0.15 → 夜 2.6；路灯档 3.4 降一档——庭院灯更暗）。 */
const LANTERN_NIGHT = 2.6;
/** 光斑直径 6 m（灯高 3 m ⇒ 光池 ≈ 2 倍灯高）。 */
const POOL_SIZE = u(6);
const LANTERN_COLOR = '#ffd9a0';

/** 光斑 radial 贴图（CanvasTexture 模块级一次性；additive）。 */
function makePoolTexture(): THREE.CanvasTexture {
  const size = 64;
  const canvas = document.createElement('canvas');
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext('2d');
  if (ctx) {
    const g = ctx.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
    g.addColorStop(0, 'rgba(255,228,170,1)');
    g.addColorStop(0.45, 'rgba(255,210,140,0.35)');
    g.addColorStop(1, 'rgba(255,200,120,0)');
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, size, size);
  }
  const tex = new THREE.CanvasTexture(canvas);
  tex.colorSpace = THREE.SRGBColorSpace;
  return tex;
}

export function ParkLamps() {
  const info = useObjectInfoProps('park.lamp', { anchorY: 0.9 });
  const url = blenderModelsEnabled() ? modelUrl('civic', 'park_lamp') : '';
  const sizeTarget = useMemo(
    () => sizeTargetFor('parkLamp', { label: 'civic/park_lamp' }),
    [],
  );

  const instances = useMemo<GlbInstanceTRS[]>(
    () => PARK_LAMPS.map((s) => ({ position: [s.x, 0, s.z] })),
    [],
  );

  // ① GLB 灯罩材质（按名命中；共享 scene ⇒ 全园 12 盏一次生效）
  const { scene } = useSharedGLTF(url, sizeTarget);
  const lanternMatRef = useRef<THREE.MeshStandardMaterial | null>(null);
  useEffect(() => {
    lanternMatRef.current = null;
    if (!scene) return;
    scene.traverse((o) => {
      const m = (o as THREE.Mesh).material;
      if (!m) return;
      const mat = (Array.isArray(m) ? m[0] : m) as THREE.MeshStandardMaterial;
      if (mat?.name?.includes('ParkLamp_Lantern')) {
        mat.emissive ??= new THREE.Color(LANTERN_COLOR);
        lanternMatRef.current = mat;
      }
    });
  }, [scene]);

  // ③ 兜底灯罩材质（程序化路径；同色同调制）
  const fbLanternMat = useMemo(
    () =>
      new THREE.MeshStandardMaterial({
        name: 'ParkLamp_Lantern_Mat',
        color: '#f2e3c0',
        emissive: LANTERN_COLOR,
        emissiveIntensity: 0.15,
        roughness: 0.35,
      }),
    [],
  );
  useEffect(() => () => fbLanternMat.dispose(), [fbLanternMat]);

  // ③ 程序化兜底：12 盏（杆 + 灯罩）一次合并（罩独立材质 mesh）
  const fbPoleGeo = useMemo(
    () =>
      mergeParts(
        PARK_LAMPS.flatMap((s) => [
          cylPart(u(0.13), u(0.15), u(0.1), 8, s.x, u(0.05), s.z, '#4e545c'),
          cylPart(u(0.028), u(0.035), u(2.44), 8, s.x, u(1.4), s.z, '#4e545c'),
        ]),
      ),
    [],
  );
  useEffect(() => () => fbPoleGeo.dispose(), [fbPoleGeo]);

  // ② 地面光斑材质（additive；opacity 逐帧随昼夜）
  const poolMatRef = useRef<THREE.MeshBasicMaterial>(null);
  const poolTexture = useMemo(() => makePoolTexture(), []);
  useEffect(() => () => poolTexture.dispose(), [poolTexture]);

  useFrame((_state, delta) => {
    const day = getDayNight()?.dayFactor01 ?? 1;
    const k = Math.min(1, delta * 2);
    const night = 1 - day;
    const glbMat = lanternMatRef.current;
    if (glbMat) {
      const target = 0.15 + (LANTERN_NIGHT - 0.15) * night;
      glbMat.emissiveIntensity += (target - glbMat.emissiveIntensity) * k;
    }
    if (!glbMat) {
      const target = 0.15 + (LANTERN_NIGHT - 0.15) * night;
      fbLanternMat.emissiveIntensity += (target - fbLanternMat.emissiveIntensity) * k;
    }
    const p = poolMatRef.current;
    if (p) p.opacity += (Math.max(0, 1 - day * 2) * 0.55 - p.opacity) * k;
  });

  return (
    <group>
      <group {...info}>
        <GlbInstanced
          url={url}
          instances={instances}
          sizeTarget={sizeTarget}
          fallback={
            <>
              <mesh geometry={fbPoleGeo}>
                <meshStandardMaterial vertexColors roughness={0.5} metalness={0.6} />
              </mesh>
              {PARK_LAMPS.map((s, i) => (
                <mesh key={`lamp-fb-${i}`} position={[s.x, u(2.89), s.z]}>
                  <boxGeometry args={[u(0.24), u(0.3), u(0.24)]} />
                  <primitive object={fbLanternMat} attach="material" />
                </mesh>
              ))}
            </>
          }
        />
      </group>
      {/* ② 夜间地面暖光斑 ×12 → 1 draw call（纯视觉层不参与点选） */}
      <Instances
        key="park-lamp-pool"
        limit={PARK_LAMPS.length}
        range={PARK_LAMPS.length}
        raycast={() => null}
      >
        <planeGeometry args={[POOL_SIZE, POOL_SIZE]} />
        <meshBasicMaterial
          ref={poolMatRef}
          map={poolTexture}
          transparent
          opacity={0}
          depthWrite={false}
          blending={THREE.AdditiveBlending}
        />
        {PARK_LAMPS.map((s, i) => (
          <Instance
            key={`park-lamp-pool-${i}`}
            position={[s.x, DISTRICT_SURFACE_Y + 0.018, s.z]}
            rotation={[-Math.PI / 2, 0, 0]}
          />
        ))}
      </Instances>
    </group>
  );
}

export default ParkLamps;
