//go:build balance_gate

// Package virtual_city — mc_gate_detail_on.go: 事件明细落盘开关(gate 构建)。
// balance_gate 构建下 gate 运行落盘 events.jsonl 明细(方案 §5)。
package virtual_city

// mcGateDetail 事件明细落盘开关:true = gate 运行写 events.jsonl 明细。
const mcGateDetail = true
