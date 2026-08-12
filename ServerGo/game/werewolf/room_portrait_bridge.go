// Package werewolf — room_portrait_bridge.go: §20260810-10 U2 模型自画像装配桥接。
//
// 职责:把 SelfPortraitData(werewolf 包镜像)→ wwplayer.SelfPortraitStats
// → wwplayer.BuildSelfPortraitText 文案,供 StartAgentsLocked 赋值给
// Agent.SelfPortraitText。独立文件便于单测 + 避免 room_agent.go 继续膨胀。
package werewolf

import (
	"LsmWebGame/agent/wwplayer"
)

// buildSelfPortraitTextFor 生成指定 modelKey 的自画像注入文本。
// data == nil(DB 无该模型记录)→ 通用自画像基线(wwplayer.BuildSelfPortraitText
// 内部处理 !SampleOK 分支)。
func buildSelfPortraitTextFor(modelKey string, data *SelfPortraitData) string {
	if data == nil {
		return wwplayer.BuildSelfPortraitText(nil)
	}
	return wwplayer.BuildSelfPortraitText(&wwplayer.SelfPortraitStats{
		Games:         data.Games,
		WinRate:       data.WinRate,
		WolfGames:     data.WolfGames,
		WolfWinRate:   data.WolfWinRate,
		GoodGames:     data.GoodGames,
		GoodWinRate:   data.GoodWinRate,
		AvgWinRateAll: data.AvgWinRateAll,
		SampleOK:      data.SampleOK,
	})
}
