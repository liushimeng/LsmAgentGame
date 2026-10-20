// Package vcplayer — tools_realworld.go: 批次65 City-Human 工具 realworld_info。
//
// 契约: lag_docs/虚拟城市/已实现/65-CityChrome实时数据通道与实时看板/
// 01-方案设计.md §3.7 — 沿批次35「单工具+op」范式(op=quotes|news|overview,
// category 可选),从 citychrome 全局 FeedStore 读紧凑 JSON,零额外 LLM 成本,
// 引擎语义零改动(只读真实世界快照,不产生任何游戏内动作)。
//
// import 方向:vcplayer → citychrome(citychrome 不依赖 vcplayer,无环)。
package vcplayer

import (
	"fmt"

	"LsmAgentGame/citychrome"
)

// realworldInfoText 生成 realworld_info 的紧凑 JSON 文本。
// 通道未启动(nil 单例)时返回明确错误文本(不 panic)。
func realworldInfoText(op, category string) (string, error) {
	svc := citychrome.Get()
	if svc == nil {
		return "", fmt.Errorf("真实世界数据通道未启动(city_chrome.enabled=false 或服务未起)")
	}
	return citychrome.FeedCompactJSON(svc.Feed(), op, category)
}
