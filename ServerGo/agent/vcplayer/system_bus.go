// Package vcplayer — system_bus.go: 批次81 City-Human 总线接入(薄委托)。
//
// 契约: lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/
// CityHuman-Agent设计.md §9;01-方案设计.md §3.1(Agent 清单)。
//
// 职责:LsmAgentGame-City-System 经 citybus 调用本 Agent 时,返回本房全体
// 居民的**只读**摘要人读文本。本文件只做「取快照 + 格式化」——不感知引擎、
// 不感知 LLM、不产生任何游戏内动作。
//
// 依赖纪律(批次81):
//   - **禁止 import game/virtual_city** —— 该包已 import vcplayer
//     (agent_runner.go / agent_sense.go 等),反向依赖会成环;因此居民摘要类型
//     ResidentBrief 定义在本包(中性结构),由装配层(main.go)把 vcMgr 的房间
//     快照适配成 []ResidentBrief。
//   - 本包**不 import citybus** —— 总线注册在 main.go 装配层,本包只提供被
//     注册的函数,保持 Agent 包对总线零感知。
package vcplayer

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ResidentBrief 是 City-System 经总线看到的居民摘要(中性结构,避免
// vcplayer → game/virtual_city 的反向依赖)。
//
// 字段全部来自房间快照,只读;Seat 为**引擎座位下标(0 起)**,与
// vctypes.PeerBrief.Seat / game.state 同源。人读文本按前端惯例显示
// 「#Seat+1」(见 formatResidentBriefs)。
type ResidentBrief struct {
	Seat     int
	Nickname string
	ModelKey string
	Alive    bool
	Cash     int64
	District string
}

// RoomResidentsSource 由 main.go 装配层注入(从 vcMgr 取房间快照)。
// 实现方须保证:只读、不阻塞(纯内存快照)、房间不存在/未开局时返回 error。
type RoomResidentsSource interface {
	SystemBusResidents(roomID string) ([]ResidentBrief, error)
}

// ctxAwareResidentsSource 是**可选**的 context 感知变体:注入的 source 若同时
// 实现本接口,SystemBusResidents 会把 ctx 透传下去(让快照读取支持取消/超时);
// 未实现则退化为普通调用 + 调用前 ctx.Err() 检查。
type ctxAwareResidentsSource interface {
	SystemBusResidentsContext(ctx context.Context, roomID string) ([]ResidentBrief, error)
}

// CityRoomSource 是全局注入点(装配层在 main.go 设置;见设计文档 §9)。
// nil(未注入)时 SystemBusResidents 返回可读说明而**非** error —— 总线调用方
// (System Agent)据此在对话里如实说明「居民摘要不可用」,不误导用户。
var CityRoomSource RoomResidentsSource

// cityRoomSourceUnavailableMsg 是未注入时的可读说明(不返回 error)。
const cityRoomSourceUnavailableMsg = "居民摘要不可用:CityRoomSource 未注入(装配层尚未完成 vcplayer 总线接线)"

// SystemBusResidents 是 LsmAgentGame-City-System 经 citybus 调用本 Agent 的
// 薄委托入口(批次81)。返回本房居民摘要的人读文本。
//
// roomID 来自 citybus.Message.RoomID(类级注册时由 main.go 的 handler 透传)。
// ctx 用于取消/超时:先检查 ctx.Err();若 source 实现 ctxAwareResidentsSource
// 则透传 ctx,否则按普通调用处理(source 为纯内存快照,无阻塞 I/O)。
func SystemBusResidents(ctx context.Context, roomID string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if CityRoomSource == nil {
		return cityRoomSourceUnavailableMsg, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var (
		briefs []ResidentBrief
		err    error
	)
	if cs, ok := CityRoomSource.(ctxAwareResidentsSource); ok {
		briefs, err = cs.SystemBusResidentsContext(ctx, roomID)
	} else {
		briefs, err = CityRoomSource.SystemBusResidents(roomID)
	}
	if err != nil {
		return "", err
	}
	return formatResidentBriefs(roomID, briefs), nil
}

// formatResidentBriefs 把居民摘要渲染成人读文本(System Agent 直接回灌模型)。
// 按座位升序输出,与 source 顺序无关;座位显示沿用前端惯例 #Seat+1。
func formatResidentBriefs(roomID string, briefs []ResidentBrief) string {
	if len(briefs) == 0 {
		return fmt.Sprintf("房间 %s 暂无居民。", roomID)
	}
	sorted := make([]ResidentBrief, len(briefs))
	copy(sorted, briefs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Seat < sorted[j].Seat })

	alive := 0
	for _, b := range sorted {
		if b.Alive {
			alive++
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "房间 %s 居民 %d 人(存活 %d):", roomID, len(sorted), alive)
	for _, b := range sorted {
		fmt.Fprintf(&sb, "\n#%d %s(ModelKey=%s, 区=%s, 现金=%d",
			b.Seat+1, b.Nickname, b.ModelKey, b.District, b.Cash)
		if !b.Alive {
			sb.WriteString(", 已死亡")
		}
		sb.WriteString(")")
	}
	return sb.String()
}
