//go:build !balance_gate

// Package virtual_city — mc_gate_detail_off.go: 事件明细落盘开关(默认构建)。
// 默认 CI 不落盘 events.jsonl(零写盘开销,T10 时延无增长;方案 §5)。
package virtual_city

// mcGateDetail 事件明细落盘开关:false = 只写 RunRecord.event_counts 聚合。
const mcGateDetail = false
