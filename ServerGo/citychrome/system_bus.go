// Package citychrome — system_bus.go: City-Chrome 的 citybus 薄委托入口(批次81)。
//
// LsmAgentGame-City-System 经 citybus 调本 Agent 时,ws/main 装配层调用
// SystemBusPayload(ctx, category):category 为空 = 全部类目(取轮转下一类目)。
//
// 依赖纪律:本包**不反向依赖** ws / main / citysystem / game/virtual_city。
// 触发逻辑由 main.go 装配层经 BusTrigger 注入(批次81 方案 §3.1「注册点」
// 表:City-Chrome 类级 handler = 触发一次采集 mission)。
package citychrome

import (
	"context"
	"fmt"
	"strings"
)

// BusTrigger 由 main.go 装配层注入(真正触发一次 mission 的逻辑,通常绑
// Service.SystemBusTrigger)。BusTrigger == nil 时 SystemBusPayload 返回
// 可读说明而非 error,避免 System Agent 侧误判为故障。
//
// 装配约定:须在服务开始接收总线消息前设置(启动期一次性写入,运行期只读)。
var BusTrigger func(ctx context.Context, category string) (string, error)

// SystemBusPayload 是 LsmAgentGame-City-System 经 citybus 调用本 Agent 的
// 薄委托入口(批次81)。category 为空 = 全部类目。
//
// 行为:触发一次采集(若服务启用且有调度能力),返回人读摘要;服务离线/未启用
// 时返回可读说明而不是 error(避免 System Agent 侧误判为故障)。
func SystemBusPayload(ctx context.Context, category string) (string, error) {
	if BusTrigger == nil {
		return "采集服务未接入总线（BusTrigger 未注入）", nil
	}
	return BusTrigger(ctx, category)
}

// SystemBusTrigger 是 Service 侧的总线触发实现:复用既有 Refresh 入队一次
// mission,把结果格式成人读摘要(批次81)。
//
// 设计取舍:所有**可预期**的运营态(未启用 / 类目非法 / 限流 / 入队失败)
// 都返回可读文本 + nil error —— 它们是状态而非故障,System Agent 应如实
// 转述给人类,而不是报「工具调用失败」。仅当未来出现真正意外的内部错误时
// 才返回 error。
func (s *Service) SystemBusTrigger(ctx context.Context, category string) (string, error) {
	if s == nil {
		return "采集服务未启动，无法触发采集。", nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return fmt.Sprintf("采集触发已取消：%v", err), nil
		}
	}
	if !s.cfg.Enabled {
		return "采集数据通道未启用（city_chrome.enabled=false），无法触发采集。", nil
	}
	category = strings.TrimSpace(category)
	if category != "" && !IsMissionCategory(category) {
		return fmt.Sprintf("未知采集类目 %q（可选：a_share/hk/us/metal/futures/crypto/macro/news；留空=自动选择）。", category), nil
	}
	accepted, queued, e := s.Refresh(category)
	if e != nil {
		return fmt.Sprintf("未能触发%s采集：%s", busCategoryLabel(category, queued), e.Error()), nil
	}
	if !accepted {
		return fmt.Sprintf("未能触发%s采集（服务繁忙，请稍后再试）。", busCategoryLabel(category, queued)), nil
	}
	return fmt.Sprintf("已触发%s采集任务：采集 Agent 将异步访问真实站点，结果产出后可在实时看板查看。",
		busCategoryLabel(category, queued)), nil
}

// busCategoryLabel 返回类目的展示文案(空 = 自动选择)。
func busCategoryLabel(requested, queued string) string {
	if queued != "" {
		return "「" + queued + "」"
	}
	if requested != "" {
		return "「" + requested + "」"
	}
	return "（自动选择类目）"
}
