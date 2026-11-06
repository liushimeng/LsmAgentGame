// Package virtual_city — errcodes_batch86.go: 批次86 新增错误码的**收口**
// (2026-11-06 §可靠性与可观测性)。
//
// 两条错误码已登记进全局 errcode 表(ServerGo/errcode/errcode.go):
//
//	ErrVirtualCitySnapshotNotFound   = 35066 // 快照不存在 / 已损坏 / 无可关联快照
//	ErrVirtualCityEventTraceNotFound = 35067 // 事件生命周期留痕不存在
//
// 本文件只保留两个 helper:在全局码之上拼接具体 id,保持调用方签名不变。
// 文案口径与 errcode.DefaultMessages 一致(空 id 时逐字相等)。
package virtual_city

import "LsmAgentGame/errcode"

// errSnapshotNotFound 构造 35066(空 id 用全局默认文案,带 id 时附具体快照)。
func errSnapshotNotFound(snapshotID string) *errcode.Error {
	if snapshotID == "" {
		return errcode.Code(errcode.ErrVirtualCitySnapshotNotFound)
	}
	return errcode.CodeMsg(errcode.ErrVirtualCitySnapshotNotFound,
		"快照不存在或已损坏:"+snapshotID)
}

// errEventTraceNotFound 构造 35067(空 id 用全局默认文案,带 id 时附具体 trace)。
func errEventTraceNotFound(traceID string) *errcode.Error {
	if traceID == "" {
		return errcode.Code(errcode.ErrVirtualCityEventTraceNotFound)
	}
	return errcode.CodeMsg(errcode.ErrVirtualCityEventTraceNotFound,
		"事件生命周期留痕不存在:"+traceID)
}
