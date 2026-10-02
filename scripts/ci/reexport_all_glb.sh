#!/usr/bin/env bash
# 批次 51 · 全城 GLB gamma 重导（sRGB → linear）
#
# 背景：glTF 2.0 规范要求 `pbrMetallicRoughness.baseColorFactor` 是**线性**色，
# 而 `__common__.make_material` 此前把 `CITY_PALETTE` 的 `#rrggbb`（设计师按屏幕
# 效果挑的 **sRGB** 值）除以 255 就直接塞进 Principled BSDF ⇒ **全城 72 个 GLB
# 整城提亮一档**（近黑的变压器壳渲成浅灰、沥青路面渲成水泥灰）。
# `__common__.srgb_to_linear` 修好之后，本脚本把全部 GLB 重导一遍。
#
# 用法：bash scripts/ci/reexport_all_glb.sh [输出目录前缀]
#   默认原地重导（ClientWeb/src/assets/models/**）。
#
# ⚠ 多输出脚本（sedan→sedan_silver、truck→truck_white）从**输出路径**推 sibling，
#   所以只需给「主」产物传路径。character 走 `--archetype` / `--variant` 两参。
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_ROOT="${1:-$ROOT/ClientWeb/src/assets/models}"
cd "$ROOT"

run() {  # run <script> <args...>
  local script="$1"; shift
  local t0=$SECONDS
  if timeout 900 blender --background --python "3d_script/${script}" -- "$@" >/tmp/reexport.log 2>&1; then
    local sz
    sz=$(stat -c%s "${!#}" 2>/dev/null || echo 0)
    printf '  ✅ %-34s %7d B  %3ds\n' "${!##$OUT_ROOT/}" "$sz" "$((SECONDS - t0))"
  else
    printf '  ❌ %-34s —— 见 /tmp/reexport.log\n' "${!#}"
    tail -4 /tmp/reexport.log | sed 's/^/       /'
    FAILED=$((FAILED + 1))
  fi
}

FAILED=0
echo "== civic =="
for n in city_hall comm_tower construction_site fire_station gas_station heli_pad \
         park_bench park_fitness park_lamp park_pavilion park_playground \
         police_station port_terminal rail_span rail_station sports_field \
         substation water_tower; do
  run "build_${n}.py" "$OUT_ROOT/civic/${n}.glb"
done

echo "== road =="
for n in bike_rack bridge_rail bus_stop canal_bank canal_reed mailbox \
         mast_arm_signal parking_meter pedestrian_signal street_sign \
         traffic_signal trash_can; do
  run "build_${n}.py" "$OUT_ROOT/road/${n}.glb"
done

echo "== nature =="
for n in cactus palm_tree pine_tree snow_mountain street_tree; do
  run "build_${n}.py" "$OUT_ROOT/nature/${n}.glb"
done
# ⚠ oak_tree（夏）必须走 build_oak_tree_season.py：build_oak_tree.py 是批次 19
#   的**横躺**遗留脚本，其文件头已标注废弃。详见该脚本的批次 51 说明。
for n in oak_tree oak_tree_spring oak_tree_autumn oak_tree_winter; do
  run "build_oak_tree_season.py" "$OUT_ROOT/nature/${n}.glb"
done

echo "== ocean =="
for n in cargo_ship lighthouse sailboat; do
  run "build_${n}.py" "$OUT_ROOT/ocean/${n}.glb"
done

echo "== vehicles（sedan / truck 一次产出两个涂装）=="
run "build_vehicle_sedan.py" "$OUT_ROOT/vehicles/sedan.glb"
run "build_vehicle_taxi.py"   "$OUT_ROOT/vehicles/taxi.glb"
run "build_vehicle_truck.py"  "$OUT_ROOT/vehicles/truck.glb"
run "build_vehicle_bus.py"    "$OUT_ROOT/vehicles/bus.glb"

echo "== characters =="
run "build_pedestrian.py" "$OUT_ROOT/characters/pedestrian_walk.glb"
for a in char_casual char_business char_student char_worker \
         char_parent char_service char_elder char_formal; do
  run "build_character.py" --archetype "$a" "$OUT_ROOT/characters/${a}.glb"
done
for g in f m u; do
  for age in elder middle senior young youth; do
    run "build_character.py" --variant "char_${g}_${age}" \
        "$OUT_ROOT/characters/char_${g}_${age}.glb"
  done
done

echo
if [ "$FAILED" -gt 0 ]; then
  echo "❌ ${FAILED} 个脚本失败"
  exit 1
fi
echo "✅ 全部 GLB 重导完成"
